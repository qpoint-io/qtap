package http1

import (
	"context"
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type Phase int

const (
	PhaseRequest Phase = iota
	PhaseResponse
)

// HTTPStream manages the read/write & open/close events
// for an http req/res connection stream based on socket events.
type HTTPStream struct {
	// context
	ctx context.Context

	// logging
	logger *zap.Logger

	// connection domain
	domain string

	// plugin manager
	pluginManager *plugins.Manager

	// the current req/res session
	session *Session

	// socket connection
	conn *connection.Connection

	// closed
	closed bool

	// mutex
	mu sync.Mutex
}

type HTTPStreamOpt func(*HTTPStream)

func SetPluginManager(manager *plugins.Manager) HTTPStreamOpt {
	return func(s *HTTPStream) {
		s.pluginManager = manager
	}
}

func NewHTTPStream(ctx context.Context, domain string, logger *zap.Logger, conn *connection.Connection, opts ...HTTPStreamOpt) *HTTPStream {
	ctx, span := tracer.Start(ctx, "http1.Stream")
	span.SetAttributes(attribute.String("stream.type", "http1"))

	// init a stream
	s := &HTTPStream{
		ctx:    ctx,
		domain: domain,
		logger: logger,
		conn:   conn,
	}

	// set options
	for _, opt := range opts {
		opt(s)
	}

	// return the stream
	return s
}

func (t *HTTPStream) Process(event *connection.DataEvent) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// if the stream is closed or in an unrecoverable error state, do nothing
	if t.closed || (t.session != nil && t.session.State == SessionStateError) {
		return nil
	}

	// determine the phase
	var phase Phase

	// process request/response
	switch {
	case t.conn.OpenEvent.Source == connection.Server && event.Direction == connection.Ingress:
		phase = PhaseRequest
	case t.conn.OpenEvent.Source == connection.Client && event.Direction == connection.Egress:
		phase = PhaseRequest
	case t.conn.OpenEvent.Source == connection.Client && event.Direction == connection.Ingress:
		phase = PhaseResponse
	case t.conn.OpenEvent.Source == connection.Server && event.Direction == connection.Egress:
		phase = PhaseResponse
	}

	// if we're processing a response and we get a request, we need to close
	if phase == PhaseRequest && t.session != nil && t.session.IsParsingResponse() {
		t.logger.Debug("closing session due to new request during response",
			zap.String("session_id", t.session.id),
			zap.String("session_state", t.session.StateString()))
		t.session.Close()
	}

	// if we don't have a session and we get a response, we need to ignore
	if phase == PhaseResponse && t.session == nil {
		t.logger.Debug("ignoring response data - no active session")
		return nil
	}

	// create a session if we don't have one
	if t.session == nil || t.session.IsClosed() || t.session.State == SessionStateDone {
		t.logger.Debug("creating new session",
			zap.Bool("no_session", t.session == nil),
			zap.Bool("is_closed", t.session != nil && t.session.IsClosed()),
			zap.Bool("is_done", t.session != nil && t.session.State == SessionStateDone))
		t.session = NewSession(t.ctx, t.logger, t.domain, t.conn, t.pluginManager)
	}

	// process the data
	switch phase {
	case PhaseRequest:
		t.writeRequest(event.Data)
	case PhaseResponse:
		t.writeResponse(event.Data)
	}

	return nil
}

func (t *HTTPStream) writeRequest(data []byte) {
	if t.session == nil {
		t.logger.Debug("http/1 invalid session state (request body)", zap.String("state", "nil"))
		return
	}

	// update the bytes
	t.session.wrBytes += int64(len(data))

	_, err := t.session.requestParser.Write(data)
	if err != nil {
		t.logger.Error("error processing request bytes", zap.Error(err))
	}
}

func (t *HTTPStream) writeResponse(data []byte) {
	if t.session == nil {
		t.logger.Debug("http/1 invalid session state (response body)", zap.String("state", "nil"))
		return
	}

	// update the bytes
	t.session.rdBytes += int64(len(data))

	_, err := t.session.responseParser.Write(data)
	if err != nil {
		t.logger.Error("error processing response bytes", zap.Error(err))
	}
}

func (t *HTTPStream) Close() {
	span := trace.SpanFromContext(t.ctx)
	defer span.End()

	t.logger.Debug("closing http/1 stream")

	ctx, cancel := context.WithTimeout(t.ctx, 5*time.Second)
	defer cancel()

	for {
		t.mu.Lock()
		if t.session == nil || t.session.State == SessionStateDone || t.session.State == SessionStateError {
			t.mu.Unlock()
			goto close
		}
		t.mu.Unlock()

		select {
		case <-ctx.Done():
			t.logger.Debug("closing session; context done", zap.Error(ctx.Err()))
			goto close
		default:
			time.Sleep(10 * time.Millisecond) // Small delay to avoid busy-waiting
		}
	}

close:
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.session != nil {
		t.logger.Debug("closing session")
		t.session.Close()
	}

	t.closed = true
}

func (t *HTTPStream) Closed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.closed
}
