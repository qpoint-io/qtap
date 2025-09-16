package http1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Callbacks interface for parsed HTTP messages
type Callbacks interface {
	OnRequest(*Request, bool)   // request, noBody
	OnRequestBody([]byte, bool) // chunk data, isComplete
	OnInterimResponse(*Response)
	OnResponse(*Response, bool)  // response, noBody
	OnResponseBody([]byte, bool) // chunk data, isComplete
	OnError(error)
	OnDone() // called when transaction is complete
}

// ErrorCallback is a function type for reporting errors
type ErrorCallback func(error)

// BodyCallback is a function type for handling body data
type BodyCallback func([]byte, bool)

// Phase indicates whether data is from request or response
type Phase int

const (
	PhaseRequest Phase = iota
	PhaseResponse
)

// TransactionState coordinates between request and response processors
type TransactionState struct {
	mu sync.Mutex

	closed atomic.Bool

	// Current request/response for this transaction
	request  *Request
	response *Response

	// Coordination signals
	expectingContinue bool
	continueReceived  chan struct{}
	requestComplete   chan struct{}
	transactionDone   chan struct{}

	// Tracking state
	requestBodyExpected  bool
	responseBodyExpected bool
}

// Parser handles a single HTTP/1.1 transaction
type Parser struct {
	ctx       context.Context
	cancel    context.CancelFunc
	callbacks Callbacks

	// The pipes that bridge event world to io.Reader world
	requestReader  *io.PipeReader
	requestWriter  *io.PipeWriter
	responseReader *io.PipeReader
	responseWriter *io.PipeWriter

	// Shared state between processors
	state *TransactionState

	// Wait group for processor goroutines
	wg sync.WaitGroup
}

// NewParser creates a new HTTP/1.1 parser for a single transaction
func NewParser(ctx context.Context, callbacks Callbacks) *Parser {
	ctx, span := tracer.Start(ctx, "parser")
	span.SetAttributes(attribute.String("parser.type", "http1"))

	ctx, cancel := context.WithCancel(ctx)

	// Create the pipes that will ferry data between events and processors
	reqReader, reqWriter := io.Pipe()
	respReader, respWriter := io.Pipe()

	p := &Parser{
		ctx:            ctx,
		cancel:         cancel,
		callbacks:      callbacks,
		requestReader:  reqReader,
		requestWriter:  reqWriter,
		responseReader: respReader,
		responseWriter: respWriter,
		state: &TransactionState{
			continueReceived: make(chan struct{}),
			requestComplete:  make(chan struct{}),
			transactionDone:  make(chan struct{}),
		},
	}

	// Start the processor goroutines
	// These will block on reads until data arrives through the pipes
	p.wg.Add(2)
	go p.processRequestStream()
	go p.processResponseStream()

	return p
}

// ProcessEvent handles incoming data events by writing them to the appropriate pipe
func (p *Parser) ProcessEvent(phase Phase, data []byte) error {
	span := trace.SpanFromContext(p.ctx)
	span.AddEvent("http.parser.process_event", trace.WithAttributes(
		attribute.Int("phase", int(phase)),
		attribute.Int("data_size", len(data)),
	))

	// The beauty of this: we just write to the pipe and the processors
	// handle everything. The write might block if the processor isn't ready,
	// which gives us natural backpressure.

	if phase == PhaseRequest {
		_, err := p.requestWriter.Write(data)
		return err
	} else {
		_, err := p.responseWriter.Write(data)
		return err
	}
}

// processRequestStream runs in its own goroutine, reading from the request pipe
func (p *Parser) processRequestStream() {
	defer p.wg.Done()

	request, err := ReadRequest(p.requestReader)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			p.callbacks.OnError(fmt.Errorf("parsing request headers: %w", err))
		}
		p.cancel()
		return
	}

	// Store in shared state
	p.state.mu.Lock()
	p.state.request = request
	p.state.requestBodyExpected = expectsRequestBody(request)
	p.state.expectingContinue = request.Headers.Get("Expect") == "100-continue"
	p.state.mu.Unlock()

	// Notify callback
	p.callbacks.OnRequest(request, !p.state.requestBodyExpected)

	// Now handle the body if expected
	if p.state.requestBodyExpected {
		// If we're expecting 100-continue, wait for it
		if p.state.expectingContinue {
			select {
			case <-p.state.continueReceived:
				// Server sent 100 Continue, proceed with body
			case <-p.ctx.Done():
				return
			}
		}

		// Read the body using the appropriate reader
		bodyReader := p.setupBodyReader(request.Headers, p.requestReader)
		streamBody(bodyReader, p.callbacks.OnRequestBody, p.callbacks.OnError)
	}

	// Flush any extra data that may have been written to the request writer
	// This could be something like a body was longer then the Content-Length header.
	// TODO(Jon): We may want to note this in our callback for user investgiation.
	_, _ = io.Copy(io.Discard, p.requestReader)

	// Signal that request is complete
	close(p.state.requestComplete)
}

// processResponseStream runs in its own goroutine, reading from the response pipe
func (p *Parser) processResponseStream() {
	defer p.wg.Done()

	for {
		// Read and parse headers
		response, err := ReadResponse(p.responseReader)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				p.callbacks.OnError(fmt.Errorf("parsing response headers: %w", err))
			}
			p.cancel()
			return
		}

		// Handle interim responses (1xx)
		if response.StatusCode >= 100 && response.StatusCode < 200 {
			response.IsInterim = true

			p.callbacks.OnInterimResponse(response)

			// If this is 100 Continue, signal the request processor
			if response.StatusCode == http.StatusContinue {
				p.state.mu.Lock()
				if p.state.expectingContinue {
					close(p.state.continueReceived)
					p.state.expectingContinue = false
				}
				p.state.mu.Unlock()
			}

			// Special case: 101 Switching Protocols ends the HTTP transaction
			if response.StatusCode == http.StatusSwitchingProtocols {
				p.callbacks.OnResponse(response, true) // 101 responses have no body
				p.callbacks.OnDone()
				close(p.state.transactionDone)
				return
			}

			// Continue reading for the final response
			continue
		}

		// This is the final response
		p.state.mu.Lock()
		p.state.response = response
		var requestMethod string
		if p.state.request != nil {
			requestMethod = p.state.request.Method
		}

		p.state.responseBodyExpected = expectsResponseBody(requestMethod, response)
		p.state.mu.Unlock()

		// Notify callback
		p.callbacks.OnResponse(response, !p.state.responseBodyExpected)

		// Read the body if expected
		if p.state.responseBodyExpected {
			bodyReader := p.setupBodyReader(response.Headers, p.responseReader)
			streamBody(bodyReader, p.callbacks.OnResponseBody, p.callbacks.OnError)
		}

		// Flush any extra data that may have been written to the response writer
		// This could be something like a body was longer then the Content-Length header.
		// TODO(Jon): We may want to note this in our callback for user investgiation.
		_, _ = io.Copy(io.Discard, p.responseReader)

		// TOOD(Jon): Not a fan of this, but it's common for consumers to try and
		// clean up the parser on OnDone, but if they call Close(), it will end
		// in a deadlock.
		go p.callbacks.OnDone()

		// Transaction complete
		close(p.state.transactionDone)
		return
	}
}

// setupBodyReader creates the appropriate reader for the request body
func (p *Parser) setupBodyReader(headers http.Header, baseReader io.Reader) io.Reader {
	// First layer: Handle Transfer-Encoding (transmission layer)
	bodyReader := setupTransferEncodingReader(headers, baseReader)

	// Second layer: Handle Content-Encoding (content layer)
	bodyReader = setupContentEncodingReader(headers, bodyReader, p.callbacks.OnError)

	return bodyReader
}

// Close cleans up resources and stops processor goroutines
func (p *Parser) Close() error {
	// If we've already closed, don't do anything
	if !p.state.closed.CompareAndSwap(false, true) {
		return nil
	}

	span := trace.SpanFromContext(p.ctx)
	defer span.End()
	span.AddEvent("http.parser.close")

	// Close the pipes - this will cause the processors to exit with EOF
	p.requestWriter.CloseWithError(nil)
	p.responseWriter.CloseWithError(nil)

	// Cancel the context to signal goroutines to stop
	p.cancel()

	// Wait for goroutines to finish
	p.wg.Wait()

	return nil
}

// IsComplete returns true if the transaction is complete
func (p *Parser) IsComplete() bool {
	select {
	case <-p.state.transactionDone:
		return true
	default:
		return false
	}
}

// WaitForCompletion blocks until the transaction is complete or context is cancelled
func (p *Parser) WaitForCompletion() error {
	select {
	case <-p.state.transactionDone:
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}
