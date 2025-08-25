package http1

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"time"

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
	ctx, span := tracer.Start(ctx, "http1.NewStreamParser")
	span.SetAttributes(attribute.String("type", fmt.Sprintf("%T", *(new(T)))))

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
	var span trace.Span
	sp.ctx, span = tracer.Start(sp.ctx, "http1.parse")
	span.SetAttributes(attribute.String("type", fmt.Sprintf("%T", *(new(T)))))
	defer span.End()

	reader := bufio.NewReader(sp.reader)

	var (
		msg           any
		err           error
		contentLength int64
	)

	sp.logger.Info("🐲 parser started")
	parseStart := time.Now()
	defer func() {
		sp.logger.Info("⏱️ parser completed", zap.String("duration", time.Since(parseStart).String()))
	}()

	switch any(*(new(T))).(type) {
	case *http.Request:
		var req *http.Request
		req, err = http.ReadRequest(reader)
		if err == nil {
			defer req.Body.Close() // Close immediately after successful read
		}
		if req != nil {
			contentLength = req.ContentLength
			dmp, err := httputil.DumpRequest(req, true)
			if err == nil {
				sp.logger.Info("💩 request", zap.String("dmp", string(dmp)), zap.Error(err))
			}
			span.SetAttributes(attribute.String("dmp", string(dmp)))
		}

		msg = req
	case *http.Response:
		var resp *http.Response
		resp, err = http.ReadResponse(reader, nil)
		if err == nil {
			defer resp.Body.Close() // Close immediately after successful read
		}
		if resp != nil {
			contentLength = resp.ContentLength
			dmp, err := httputil.DumpResponse(resp, true)
			if err == nil {
				sp.logger.Info("💩 response", zap.String("dmp", string(dmp)), zap.Error(err))
			}
			span.SetAttributes(attribute.String("dmp", string(dmp)))
		}

		msg = resp
	default:
		err = fmt.Errorf("unsupported type: %T", *(new(T)))
	}
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			sp.logger.Warn("🍓🍓🍓 connection closed before complete payload transfer or stream blocked due to unread data", zap.Error(err), zap.String("type", fmt.Sprintf("%T", *(new(T)))), zap.Stack("stack"), zap.Int64("content_length", contentLength))
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
					sp.logger.Warn("🍓🍓🍓🍓🍓🍓  connection closed before complete payload transfer or stream blocked due to unread data", zap.Error(err), zap.Stack("stack"))
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

func (sp *StreamParser[T]) Close(closer string) error {
	span := trace.SpanFromContext(sp.ctx)
	defer span.End()

	sp.logger = sp.logger.With(zap.String("closer", closer))
	sp.logger.Debug("closing stream parser")

	span.SetAttributes(attribute.String("closer", closer))

	return sp.reader.Close()
}

func chunked(te []string) bool { return len(te) > 0 && te[0] == "chunked" }
