package http1_old

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
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
	r.mu.RLock()
	if r.buf == nil {
		r.mu.RUnlock()
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

		// wait for the buffer to be written to
		select {
		case <-r.notify:
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		}

		r.mu.RLock()
		if r.buf == nil {
			r.mu.RUnlock()
			return 0, fmt.Errorf("notify: buffer is nil: %w", io.ErrUnexpectedEOF)
		}
	}

	n, err = r.buf.Read(p)
	r.mu.RUnlock()
	return n, err
}

// Write adds data to the internal buffer.
func (r *BufferedReader) Write(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.buf == nil {
		return 0, io.ErrClosedPipe
	}

	n, err = r.buf.Write(p)
	select {
	case r.notify <- struct{}{}:
	default:
	}
	return n, err
}

// Close implements io.Closer.
func (r *BufferedReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.buf = nil
	select {
	case r.notify <- struct{}{}:
	default:
	}

	return nil
}
