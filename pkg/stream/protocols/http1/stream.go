package http1

import (
	"context"
	"sync"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"go.uber.org/zap"
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

	// session
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

	if t.session == nil || t.session.Closed() {
		t.session = NewSession(t.ctx, t.logger, t.domain, t.conn, t.pluginManager)
	}

	if err := t.session.Write(phase, event.Data); err != nil {
		t.logger.Error("error processing event", zap.Error(err))
		return err
	}

	return nil
}

func (t *HTTPStream) Close() {
	t.logger.Debug("closing http/1 stream")
	if err := t.session.Close(); err != nil {
		t.logger.Error("closing session", zap.Error(err))
	}
	t.closed = true
}

func (t *HTTPStream) Closed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.closed
}
