package http1

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"github.com/rs/xid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

var tracer = telemetry.Tracer()

type Session struct {
	// context
	ctx context.Context

	// session id
	id string

	// logger
	logger *zap.Logger

	// domain
	domain string

	// socket connection
	conn *connection.Connection

	// parser
	parser *Parser

	// plugin
	pluginManager *plugins.Manager
	pluginConn    *plugins.Connection

	reqBytes atomic.Int64
	resBytes atomic.Int64

	// total bytes written
	closed atomic.Bool
}

func NewSession(ctx context.Context, logger *zap.Logger, domain string, conn *connection.Connection, pluginManager *plugins.Manager) *Session {
	ctx, span := tracer.Start(ctx, "session")
	span.SetAttributes(attribute.String("session.type", "http1"))

	if logger == nil {
		logger = zap.NewNop()
	}

	id := xid.New().String()
	span.SetAttributes(attribute.String("session.id", id))

	// create a plugin connection
	var pluginConn *plugins.Connection
	if pluginManager != nil {
		var err error
		pluginConn, err = pluginManager.NewConnection(ctx, plugins.ConnectionType_HTTP, conn, id)
		if err != nil {
			logger.Error("creating plugin connection", zap.Error(err))
		}
	}

	s := &Session{
		ctx:           ctx,
		id:            id,
		logger:        logger.With(zap.String("session_id", id)),
		domain:        domain,
		conn:          conn,
		pluginManager: pluginManager,
		pluginConn:    pluginConn,
		reqBytes:      atomic.Int64{},
		resBytes:      atomic.Int64{},
	}

	s.parser = NewParser(ctx, s)

	return s
}

func (s *Session) Write(phase Phase, data []byte) error {
	if err := s.parser.ProcessEvent(phase, data); err != nil {
		return err
	}

	switch phase {
	case PhaseRequest:
		s.reqBytes.Add(int64(len(data)))
	case PhaseResponse:
		s.resBytes.Add(int64(len(data)))
	}

	return nil
}

// OnRequest handles HTTP request callbacks
func (s *Session) OnRequest(req *Request, noBody bool) {
	span := trace.SpanFromContext(s.ctx)
	span.AddEvent("http.request", trace.WithAttributes(
		attribute.String("request.method", req.Method),
		attribute.String("request.url", req.URL.String()),
		attribute.String("request.proto", req.Proto),
		attribute.String("request.host", req.Host)),
	)

	if req.Host != "" && !strings.EqualFold(req.Host, s.domain) {
		s.conn.SetDomain(req.Host)
	}

	if s.pluginConn != nil {
		// TODO(Jon): migrate to use our request object
		if req.URL.Scheme == "" {
			if s.conn.IsTLS {
				req.URL.Scheme = "https"
			} else {
				req.URL.Scheme = "http"
			}
		}
		r := &http.Request{
			Method:     req.Method,
			RequestURI: req.RequestURI,
			Proto:      req.Proto,
			Host:       req.Host,
			URL:        req.URL,
			Header:     req.Headers,
		}

		s.logger.Debug("http request", zap.Any("request", req))

		// set the request
		s.pluginConn.SetRequest(r)

		// call the request headers callback
		if err := s.pluginConn.OnHttpRequestHeaders(noBody); err != nil {
			s.logger.Error("plugin request headers", zap.Error(err))
		}
	}
}

// OnRequestBody handles HTTP request body chunks
func (s *Session) OnRequestBody(chunk []byte, isComplete bool) {
	span := trace.SpanFromContext(s.ctx)
	span.AddEvent("http.request.body", trace.WithAttributes(
		attribute.Int("request.body.size", len(chunk)),
		attribute.Bool("request.body.complete", isComplete),
	))

	s.logger.Debug("http request body", zap.Int("length", len(chunk)), zap.Bool("done", isComplete))

	if s.pluginConn != nil {
		// call the request body callback
		if err := s.pluginConn.OnHttpRequestBody(chunk, isComplete); err != nil {
			s.logger.Error("plugin request body", zap.Error(err))
		}
	}
}

// OnInterimResponse handles HTTP interim response callbacks (1xx responses)
func (s *Session) OnInterimResponse(resp *Response) {
	span := trace.SpanFromContext(s.ctx)
	span.AddEvent("http.interim.response", trace.WithAttributes(
		attribute.Int("response.status_code", resp.StatusCode),
		attribute.String("response.status", resp.Status),
		attribute.String("response.version", resp.Proto),
	))

	s.logger.Debug("http interim response",
		zap.Int("status_code", resp.StatusCode),
		zap.String("status", resp.Status),
		zap.String("version", resp.Proto))
}

// OnResponse handles HTTP response callbacks
func (s *Session) OnResponse(resp *Response, noBody bool) {
	span := trace.SpanFromContext(s.ctx)
	span.AddEvent("http.response", trace.WithAttributes(
		attribute.Int("response.status_code", resp.StatusCode),
		attribute.String("response.status", resp.Status),
		attribute.String("response.version", resp.Proto),
	))

	s.logger.Debug("http response", zap.Int("status_code", resp.StatusCode), zap.String("status", resp.Status), zap.String("version", resp.Proto))

	if s.pluginConn != nil {
		// TODO(Jon): migrate to use our response object
		r := &http.Response{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Proto:      resp.Proto,
			Header:     resp.Headers,
		}

		// set the response
		s.pluginConn.SetResponse(r)

		// call the response headers callback
		if err := s.pluginConn.OnHttpResponseHeaders(noBody); err != nil {
			s.logger.Error("plugin response headers", zap.Error(err))
		}
	}
}

// OnResponseBody handles HTTP response body chunks
func (s *Session) OnResponseBody(chunk []byte, isComplete bool) {
	span := trace.SpanFromContext(s.ctx)
	span.AddEvent("http.response.body", trace.WithAttributes(
		attribute.Int("response.body.size", len(chunk)),
		attribute.Bool("response.body.complete", isComplete),
	))

	s.logger.Debug("http response body", zap.Int("length", len(chunk)), zap.Bool("done", isComplete))

	// call the request is finished callback
	if s.pluginConn != nil {
		// call the response body callback
		if err := s.pluginConn.OnHttpResponseBody(chunk, isComplete); err != nil {
			s.logger.Error("plugin response body", zap.Error(err))
		}
	}
}

// OnError handles HTTP parsing error callbacks
func (s *Session) OnError(err error) {
	span := trace.SpanFromContext(s.ctx)
	span.AddEvent("http.error", trace.WithAttributes(
		attribute.String("error", err.Error()),
	))

	s.logger.Error("http error", zap.Error(err))
}

// OnDone handles transaction completion
func (s *Session) OnDone() {
	span := trace.SpanFromContext(s.ctx)
	span.AddEvent("http.done")

	if err := s.parser.WaitForCompletion(); err != nil && !errors.Is(err, context.Canceled) {
		// We ignore context canceled errors because if the Parser.Close() is called,
		// it will cancel the context as part of the cleanup process. This is expected.
		s.logger.Error("waiting for parser to complete", zap.Error(err))
	}

	if err := s.Close(); err != nil {
		s.logger.Error("closing session", zap.Error(err))
	}
}

func (s *Session) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	span := trace.SpanFromContext(s.ctx)
	defer span.End()
	span.AddEvent("http.close", trace.WithAttributes(
		attribute.Int64("session.rd_bytes", s.resBytes.Load()),
		attribute.Int64("session.wr_bytes", s.reqBytes.Load()),
	))

	// Order is important here. When closing the parser, it will
	// give let any queued events continue to be processed.
	if err := s.parser.Close(); err != nil {
		s.logger.Error("closing parser", zap.Error(err))
		return err
	}

	if s.pluginConn != nil {
		// Save metadata
		s.pluginConn.Meta().SetReadBytes(s.resBytes.Load())
		s.pluginConn.Meta().SetWriteBytes(s.reqBytes.Load())

		s.pluginConn.Teardown()
	}

	return nil
}

func (s *Session) Closed() bool {
	return s.closed.Load()
}
