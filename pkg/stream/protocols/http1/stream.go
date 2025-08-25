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

func (p Phase) String() string {
	switch p {
	case PhaseRequest:
		return "request"
	case PhaseResponse:
		return "response"
	}
	return "unknown"
}

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

	// req/res session
	session     *Session
	lastSession *Session

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
		t.logger.Info("❌ relieving session; request received after response", zap.Stack("stack"), zap.String("session_id", t.session.id))

		// if we already have a lastSession, force close it since it should be done
		if t.lastSession != nil {
			t.logger.Warn("forcing close of previous lastSession", zap.String("session_id", t.lastSession.id))
			t.lastSession.Close("stream/Process/force")
		}

		// move current session to lastSession and start graceful completion
		t.lastSession = t.session
		t.session = nil

		// start goroutine to wait for lastSession completion
		go func(session *Session) {
			if err := session.WaitForCompletion(5 * time.Second); err != nil {
				t.logger.Warn("last session didn't complete gracefully",
					zap.String("session_id", session.id),
					zap.Error(err))
				session.Close("stream/Process/timeout")
			}
		}(t.lastSession)
	}

	// if we don't have a session and we get a response, we need to ignore
	if phase == PhaseResponse && t.session == nil {
		return nil
	}

	// create a session if we don't have one
	if t.session == nil || t.session.IsClosed() || t.session.State == SessionStateDone {
		t.session = NewSession(t.ctx, t.logger, t.domain, t.conn, t.pluginManager)

	}

	t.logger.Info("⭕ http1 stream event", zap.String("phase", phase.String()), zap.String("direction", event.Direction.String()), zap.String("source", t.conn.OpenEvent.Source.String()), zap.String("session_id", t.session.id))

	// process the data
	switch phase {
	case PhaseRequest:
		span := trace.SpanFromContext(t.ctx)
		span.SetAttributes(
			attribute.String("http.request.data", string(event.Data)),
			attribute.Int("http.request.length", len(event.Data)),
		)
		t.writeRequest(event.Data)
	case PhaseResponse:
		span := trace.SpanFromContext(t.ctx)
		span.SetAttributes(
			attribute.String("http.response.data", string(event.Data)),
			attribute.Int("http.response.length", len(event.Data)),
		)
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

	t.mu.Lock()
	defer t.mu.Unlock()

	// wait for current session to complete gracefully
	if t.session != nil {
		if err := t.session.WaitForCompletion(10 * time.Second); err != nil {
			t.logger.Warn("current session didn't complete gracefully",
				zap.String("session_id", t.session.id),
				zap.Error(err))
		}
		t.session.Close("stream/Close")
	}

	// wait for last session to complete (should already be mostly done)
	if t.lastSession != nil {
		if err := t.lastSession.WaitForCompletion(2 * time.Second); err != nil {
			t.logger.Warn("last session didn't complete gracefully",
				zap.String("session_id", t.lastSession.id),
				zap.Error(err))
		}
		t.lastSession.Close("stream/Close")
	}

	t.closed = true
}

func (t *HTTPStream) Closed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.closed
}
