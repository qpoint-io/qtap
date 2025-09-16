package http1_old

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/andybalholm/brotli"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

var (
	ErrMalformedRequest = errors.New("malformed HTTP request")
)

var tracer = telemetry.Tracer()

// HeaderHandler is a callback function type for handling parsed HTTP messages
type HeaderHandler[T any] func(msg T, noBody bool)

// BodyHandler is a callback function type for handling raw data chunks
type BodyHandler func(chunk []byte, done bool)

// bodyEncodingHandler is a callback function type for handling raw data chunks
type bodyEncodingHandler func(io.Reader) (io.ReadCloser, error)

// gzipBodyEncodingHandler is a callback function type for handling raw data chunks
func gzipBodyEncodingHandler(body io.Reader) (io.ReadCloser, error) {
	return gzip.NewReader(body)
}

// closer wraps a brotli.Reader to implement io.ReadCloser
type closer struct {
	io.Reader
}

func (b closer) Close() error {
	return nil
}

// brotliBodyEncodingHandler is a callback function type for handling raw data chunks
func brotliBodyEncodingHandler(body io.Reader) (io.ReadCloser, error) {
	return closer{brotli.NewReader(body)}, nil
}

// StreamParser is a generic type that can parse either requests or responses
type StreamParser[T any] struct {
	ctx           context.Context
	logger        *zap.Logger
	reader        *BufferedReader
	headerHandler HeaderHandler[T]
	bodyHandler   BodyHandler
}

// NewStreamParser creates a new StreamParser with the specified handlers
func NewStreamParser[T any](ctx context.Context, logger *zap.Logger, messageHandler HeaderHandler[T], chunkHandler BodyHandler) *StreamParser[T] {
	sp := &StreamParser[T]{
		ctx:           ctx,
		logger:        logger.With(zap.String("type", fmt.Sprintf("%T", *(new(T))))),
		reader:        NewBufferedReader(ctx),
		headerHandler: messageHandler,
		bodyHandler:   chunkHandler,
	}

	return sp
}

func (sp *StreamParser[T]) parse() error {
	reader := bufio.NewReader(sp.reader)

	var (
		msg           any
		err           error
		contentLength int64
	)

	switch any(*(new(T))).(type) {
	case *http.Request:
		var req *http.Request
		req, err = http.ReadRequest(reader)
		if err == nil {
			defer req.Body.Close() // Close immediately after successful read
		}
		if req != nil {
			contentLength = req.ContentLength
		}
		msg = req
	case *http.Response:
		// For responses, we need to handle multiple responses (interim + final)
		msg, contentLength, err = sp.parseResponseChain(reader)
	default:
		err = fmt.Errorf("unsupported type: %T", *(new(T)))
	}
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			sp.logger.Warn("connection closed before complete payload transfer or stream blocked due to unread data", zap.Error(err))
		} else {
			sp.logger.Debug("malformed HTTP message", zap.Error(err))
			return ErrMalformedRequest
		}

		return err
	}

	var (
		body             io.ReadCloser
		transferEncoding []string
		contentEncoding  string
	)
	switch v := msg.(type) {
	case *http.Request:
		if v != nil {
			body = v.Body
			transferEncoding = v.TransferEncoding
			contentEncoding = v.Header.Get("Content-Encoding")
		}
	case *http.Response:
		if v != nil {
			body = v.Body
			transferEncoding = v.TransferEncoding
			contentEncoding = v.Header.Get("Content-Encoding")
		}
	}

	eventAttrs := []attribute.KeyValue{
		attribute.Int64("http.content_length", contentLength),
		attribute.Bool("http.chunked", chunked(transferEncoding)),
	}
	if contentEncoding != "" {
		eventAttrs = append(eventAttrs, attribute.String("http.content_encoding", contentEncoding))
	}
	span := trace.SpanFromContext(sp.ctx)
	span.AddEvent("http1.message", trace.WithAttributes(eventAttrs...))

	if sp.headerHandler != nil {
		if msg != nil {
			sp.headerHandler(any(msg).(T), body == nil || body == http.NoBody)
		} else {
			var zeroValue T
			sp.headerHandler(zeroValue, false)
		}
	}

	if body != nil && body != http.NoBody {
		var h []bodyEncodingHandler
		switch contentEncoding {
		case "gzip":
			h = append(h, gzipBodyEncodingHandler)
		case "br":
			h = append(h, brotliBodyEncodingHandler)
		}

		err = sp.handleBody(body, transferEncoding, h...)
		if err != nil {
			return fmt.Errorf("handling body: %w", err)
		}
	}

	return nil
}

func (sp *StreamParser[T]) handleBody(body io.Reader, encoding []string, handlers ...bodyEncodingHandler) error {
	for _, handler := range handlers {
		var err error
		body, err = handler(body)
		if err != nil {
			return fmt.Errorf("handling body encoding: %w", err)
		}
	}

	var retErr error
	for {
		buf := make([]byte, 1024)
		n, err := body.Read(buf)
		if n > 0 && sp.bodyHandler != nil {
			sp.bodyHandler(buf[:n], false)
		}
		if n == 0 || err == io.EOF {
			break
		} else if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				if chunked(encoding) {
					// This event happens when the client or server abandons a connection without properly cleaning it up
					// or terminating the connection prematurely. For example, reading the header of a http/1.1 request
					// that contains a chunked transfer-encoding and closing the connection before reading the body.
					// This is a normal (albeit unexpected) event and we can warn on it and continue.
					sp.logger.Warn("connection closed before complete payload transfer or stream blocked due to unread data", zap.Error(err))
				}
				break
			}

			retErr = fmt.Errorf("reading body: %w", err)
			break
		}
	}

	if sp.bodyHandler != nil {
		sp.bodyHandler(nil, true)
	}

	return retErr
}

func (sp *StreamParser[T]) Write(data []byte) (int, error) {
	return sp.reader.Write(data)
}

// parseResponseChain handles parsing multiple responses (interim + final)
func (sp *StreamParser[T]) parseResponseChain(reader *bufio.Reader) (any, int64, error) {
	var finalResponse *http.Response
	var contentLength int64

	// Keep reading responses until we get a non-1xx response
	for {
		resp, err := http.ReadResponse(reader, nil)
		if err != nil {
			return nil, 0, err
		}

		sp.logger.Debug("parsed response", zap.Int("status", resp.StatusCode))

		// If this is an interim response (1xx), handle it and continue
		if resp.StatusCode >= 100 && resp.StatusCode < 200 {
			sp.logger.Debug("detected interim response", zap.Int("status", resp.StatusCode))

			// Close the response body for interim responses (they shouldn't have bodies)
			if resp.Body != nil {
				resp.Body.Close()
			}

			// Call the header handler for interim response
			if sp.headerHandler != nil {
				sp.headerHandler(any(resp).(T), true) // interim responses have no body
			}

			// For protocol switching (101), this is the final response
			if resp.StatusCode == http.StatusSwitchingProtocols {
				finalResponse = resp
				contentLength = resp.ContentLength
				break
			}

			// For other interim responses (like 100 Continue), continue parsing for final response
			sp.logger.Debug("continuing to parse for final response after interim", zap.Int("interim_status", resp.StatusCode))
			continue
		}

		// This is a final response (non-1xx)
		// Don't close the body here - let the main parse() method handle it
		finalResponse = resp
		if finalResponse != nil {
			contentLength = finalResponse.ContentLength
		}
		break
	}

	return finalResponse, contentLength, nil
}

func (sp *StreamParser[T]) Close() error {
	sp.logger.Debug("closing stream parser")
	return sp.reader.Close()
}

func chunked(te []string) bool { return len(te) > 0 && te[0] == "chunked" }
