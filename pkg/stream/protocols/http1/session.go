package http1

import (
	"context"
	"net/http"
	"net/http/httputil"
	"sync"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/rs/xid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type SessionState int

const (
	SessionStateRequestHeaders SessionState = iota
	SessionStateRequestBody
	SessionStateResponseHeaders
	SessionStateResponseBody
	SessionStateDone
)

type Session struct {
	mu sync.RWMutex

	// context
	ctx context.Context

	// session id
	id string
	// the current state of this http session
	State SessionState

	// domain
	domain string

	// total bytes written
	wrBytes int64
	// total bytes read
	rdBytes int64

	// request/response
	req *http.Request
	res *http.Response

	// parsers
	requestParser  *StreamParser[*http.Request]
	responseParser *StreamParser[*http.Response]

	// socket connection
	conn *connection.Connection

	// plugin connection
	pluginConn *plugins.Connection

	// plugin manager
	pluginManager *plugins.Manager

	// logger
	logger *zap.Logger

	// have we already closed the session
	closed bool
}

func NewSession(ctx context.Context, logger *zap.Logger, domain string, conn *connection.Connection, pluginManager *plugins.Manager) *Session {
	ctx, span := tracer.Start(ctx, "Session")
	span.SetAttributes(attribute.String("session.type", "http1"))

	if logger == nil {
		logger = zap.NewNop()
	}

	id := xid.New().String()

	s := &Session{
		State:         SessionStateRequestHeaders,
		ctx:           ctx,
		id:            id,
		logger:        logger.With(zap.String("qpoint_request_id", id)),
		domain:        domain,
		conn:          conn,
		pluginManager: pluginManager,
	}

	// create the request parser
	// nolint:bodyclose // Request body is closed in Session.Close()
	s.requestParser = NewStreamParser(s.ctx, s.logger, s.CreateRequest, s.WriteRequestBody)

	// create the response parser
	// nolint:bodyclose // Response body is closed in Session.Close()
	s.responseParser = NewStreamParser(s.ctx, s.logger, s.CreateResponse, s.WriteResponseBody)

	go s.Run()

	return s
}

func (s *Session) Run() {
	err := s.requestParser.parse()
	if err != nil {
		s.logger.Error("error parsing request", zap.Error(err))
	}
	err = s.responseParser.parse()
	if err != nil {
		s.logger.Error("error parsing response", zap.Error(err))
		if s.req != nil {
			dump, err := httputil.DumpRequest(s.req, false)
			if err != nil {
				s.logger.Error("error dumping request", zap.Error(err))
			}

			s.logger.Warn("dumped request", zap.String("request", string(dump)))
		}
	}
	s.Close()
}

func (s *Session) CreateRequest(req *http.Request, noBody bool) {
	span := trace.SpanFromContext(s.ctx)
	span.SetAttributes(attribute.String("request.id", s.id))

	s.mu.Lock()
	defer s.mu.Unlock()

	s.req = req

	if s.req == nil {
		// this should never happen
		s.logger.Error("invalid request; empty request")
		return
	}

	// set the Qpoint request ID
	s.req.Header.Set("qpoint-request-id", s.id)

	// Determine the scheme based on the connection type (you may need to adjust this)
	scheme := "http"
	if s.conn.IsTLS {
		scheme = "https"
	}
	s.req.URL.Scheme = scheme

	// if host is set and not the same as the domain, update the connection domain
	if s.req.Host != "" && s.req.Host != s.domain {
		s.conn.SetDomain(s.req.Host)
	}

	// set the state
	if noBody {
		s.State = SessionStateResponseHeaders
	} else {
		s.State = SessionStateRequestBody
	}

	// create a plugin connection
	if s.pluginManager != nil {
		var err error
		s.pluginConn, err = s.pluginManager.NewConnection(s.ctx, plugins.ConnectionType_HTTP, s.conn, s.id)
		if err != nil {
			s.logger.Error("creating plugin connection", zap.Error(err))
		}
	}

	if s.pluginConn != nil {
		// set the request
		s.pluginConn.SetRequest(s.req)

		// call the request headers callback
		if err := s.pluginConn.OnHttpRequestHeaders(true); err != nil {
			s.logger.Error("plugin request headers", zap.Error(err))
		}
	}
}

func (s *Session) CreateResponse(res *http.Response, noBody bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logger.Debug("creating response", zap.Int("status", res.StatusCode), zap.String("status", res.Status))

	s.res = res

	// if we don't have a response, create a 499 canceled response
	// this typically happens when the connection is closed before the response is fully received
	if s.res == nil {
		s.res = &http.Response{
			StatusCode: 499,
			Status:     "Canceled",
		}
	}

	// set the state
	if noBody {
		s.State = SessionStateDone
	} else {
		s.State = SessionStateResponseBody
	}

	if s.pluginConn != nil {
		// set the response
		s.pluginConn.SetResponse(s.res)

		// call the response headers callback
		if err := s.pluginConn.OnHttpResponseHeaders(true); err != nil {
			s.logger.Error("plugin response headers", zap.Error(err))
		}
	}
}

func (s *Session) WriteRequestBody(data []byte, done bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logger.Debug("writing request body", zap.Int("length", len(data)))

	if s.pluginConn != nil {
		// call the request body callback
		if err := s.pluginConn.OnHttpRequestBody(data, done); err != nil {
			s.logger.Error("plugin request body", zap.Error(err))
		}
	}

	// have we reached the end of the stream?
	if !done {
		return
	}

	// set the state
	s.State = SessionStateResponseHeaders
}

func (s *Session) WriteResponseBody(data []byte, done bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logger.Debug("writing response body", zap.Int("length", len(data)))

	// call the request is finished callback
	if s.pluginConn != nil {
		// call the response body callback
		if err := s.pluginConn.OnHttpResponseBody(data, done); err != nil {
			s.logger.Error("plugin response body", zap.Error(err))
		}
	}

	// have we reached the end of the stream?
	if !done {
		return
	}

	// set the state
	s.logger.Debug("setting state to done")
	s.State = SessionStateDone
}

func (s *Session) Close() {
	span := trace.SpanFromContext(s.ctx)
	defer span.End()

	s.mu.Lock()
	defer s.mu.Unlock()

	// if we've already closed the session, don't do anything
	if s.closed {
		return
	}

	s.logger.Debug("closing session", zap.String("state", s.StateString()))

	// close the parsers
	err := s.requestParser.Close()
	if err != nil {
		s.logger.Error("closing request parser", zap.Error(err))
	}
	err = s.responseParser.Close()
	if err != nil {
		s.logger.Error("closing response parser", zap.Error(err))
	}

	// if we're not done, we've ended prematurely
	if s.State != SessionStateDone {
		span.SetStatus(codes.Error, "http/1 session ended prematurely")
		span.SetAttributes(attribute.String("session.state", s.StateString()))
		s.logger.Debug("http/1 session ended prematurely", zap.String("state", s.StateString()))

		// if we have a response, set the status code to 499
		if s.res != nil {
			s.res.StatusCode = 499
			s.res.Status = "Canceled"
		}
	}

	// if we don't have a response, create a 499 canceled response
	if s.res == nil {
		s.res = &http.Response{
			StatusCode: 499,
			Status:     "Canceled",
		}
	}

	if s.pluginConn != nil {
		// update the bandwidth metadata
		s.pluginConn.Meta().SetReadBytes(s.rdBytes)
		s.pluginConn.Meta().SetWriteBytes(s.wrBytes)
		span.SetAttributes(
			attribute.Int64("wr_bytes", s.wrBytes),
			attribute.Int64("rd_bytes", s.rdBytes),
		)

		// teardown the plugin connection
		s.pluginConn.Teardown()
	}

	// close the session
	s.closed = true
}

func (s *Session) IsClosed() bool {
	return s.closed
}

func (s *Session) IsParsingResponse() bool {
	return s.State == SessionStateResponseHeaders || s.State == SessionStateResponseBody
}

func (s *Session) StateString() string {
	switch s.State {
	case SessionStateRequestHeaders:
		return "request_headers"
	case SessionStateRequestBody:
		return "request_body"
	case SessionStateResponseHeaders:
		return "response_headers"
	case SessionStateResponseBody:
		return "response_body"
	case SessionStateDone:
		return "done"
	default:
		return "unknown"
	}
}
