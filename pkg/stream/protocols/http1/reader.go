package http1

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"github.com/qpoint-io/qtap/pkg/telemetry"
)

// BufferedReader provides blocking reads over a bytes buffer with context cancellation support.
type BufferedReader struct {
	ctx    context.Context
	buf    *bytes.Buffer
	mu     sync.RWMutex
	notify chan struct{}
}

// NewBufferedReader creates a new BufferedReader instance.
func NewBufferedReader(ctx context.Context) *BufferedReader {
	return &BufferedReader{
		ctx:    ctx,
		buf:    bytes.NewBuffer(nil),
		notify: make(chan struct{}, 1),
	}
}

// Read implements io.Reader. It blocks until data is available or the context is cancelled.
func (r *BufferedReader) Read(p []byte) (n int, err error) {
	ctx, span := telemetry.Tracer().Start(r.ctx, "BufferedReader.Read")
	defer span.End()

	span.SetAttributes(
		attribute.Int("buffer_size", len(p)),
		attribute.String("operation", "read"),
	)

	r.mu.RLock()
	if r.buf == nil {
		r.mu.RUnlock()
		span.SetAttributes(attribute.String("error", "buffer is nil"))
		return 0, fmt.Errorf("buffer is nil on read: %w", io.ErrUnexpectedEOF)
	}

	for r.buf.Len() == 0 {
		// we need to reset the buffer here because our readWaiter prevents
		// the bytes.Buffer from hitting a zero length read until the stream
		// is closed. For long running chunked streams, this will cause the
		// buffer to constantly grow, while all the previous data has already
		// been read.
		r.buf.Reset()
		r.mu.RUnlock()

		span.AddEvent("waiting_for_data")
		// wait for the buffer to be written to
		select {
		case <-r.notify:
		case <-ctx.Done():
			span.SetAttributes(attribute.String("error", ctx.Err().Error()))
			return 0, ctx.Err()
		}

		r.mu.RLock()
		if r.buf == nil {
			r.mu.RUnlock()
			span.SetAttributes(attribute.String("error", "buffer is nil after notify"))
			return 0, fmt.Errorf("notify: buffer is nil: %w", io.ErrUnexpectedEOF)
		}
	}

	n, err = r.buf.Read(p)
	span.SetAttributes(
		attribute.Int("bytes_read", n),
		attribute.Int("buffer_remaining", r.buf.Len()),
		attribute.String("read_contents", string(p)),
	)
	if err != nil {
		span.SetAttributes(attribute.String("read_error", err.Error()))
	}

	r.mu.RUnlock()
	return n, err
}

// Write adds data to the internal buffer.
func (r *BufferedReader) Write(p []byte) (n int, err error) {
	_, span := telemetry.Tracer().Start(r.ctx, "BufferedReader.Write")
	defer span.End()

	span.SetAttributes(
		attribute.Int("data_size", len(p)),
		attribute.String("operation", "write"),
	)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.buf == nil {
		span.SetAttributes(attribute.String("error", "buffer is nil"))
		return 0, io.ErrClosedPipe
	}

	n, err = r.buf.Write(p)
	span.SetAttributes(
		attribute.Int("bytes_written", n),
		attribute.Int("buffer_size_after_write", r.buf.Len()),
		attribute.String("write_contents", string(p)),
	)

	if err != nil {
		span.SetAttributes(attribute.String("write_error", err.Error()))
	}

	select {
	case r.notify <- struct{}{}:
		span.AddEvent("notified_readers")
	default:
		span.AddEvent("notification_skipped")
	}
	return n, err
}

// Close implements io.Closer.
func (r *BufferedReader) Close() error {
	_, span := telemetry.Tracer().Start(r.ctx, "BufferedReader.Close")
	defer span.End()

	r.mu.Lock()
	defer r.mu.Unlock()

	zap.L().Info("💜 closing buffered reader", zap.Int("bytes_left", r.buf.Len()))

	span.SetAttributes(attribute.Int("bytes_remaining", r.buf.Len()))

	r.buf = nil
	select {
	case r.notify <- struct{}{}:
		span.AddEvent("notified_readers_on_close")
	default:
		span.AddEvent("notification_skipped_on_close")
	}

	return nil
}
