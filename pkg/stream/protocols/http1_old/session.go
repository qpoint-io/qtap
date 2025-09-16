package http1_old

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

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
	SessionStateAwaitingInterim // Waiting for 100-Continue or other interim response
	SessionStateInterimResponse // Processing 1xx response
	SessionStateResponseHeaders
	SessionStateResponseBody
	SessionStateProtocolSwitch // Handling 101 Switching Protocols
	SessionStateDone
)

// InterimResponse represents a 1xx informational response
type InterimResponse struct {
	StatusCode int
	Status     string
	Header     http.Header
	Timestamp  time.Time
}

// UpgradeInfo contains information about protocol upgrades
type UpgradeInfo struct {
	Protocol string // "websocket", "h2c", etc.
	Headers  http.Header
}

// ResponseChain tracks all responses for a single request
type ResponseChain struct {
	InterimResponses []*InterimResponse // All 1xx responses
	FinalResponse    *http.Response     // The final non-1xx response
	ProtocolUpgrade  *UpgradeInfo       // Protocol switch info (if any)
}

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

	// response chain for handling interim responses
	responseChain *ResponseChain

	// flags for special handling
	expectContinue   bool // true if request has Expect: 100-Continue
	protocolUpgraded bool // true if protocol was upgraded (101 response)

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
		logger:        logger.With(zap.String("request_id", id)),
		domain:        domain,
		conn:          conn,
		pluginManager: pluginManager,
		responseChain: &ResponseChain{
			InterimResponses: make([]*InterimResponse, 0),
		},
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
	// Run request and response parsers concurrently
	// This allows for bidirectional communication like expect-continue
	var wg sync.WaitGroup
	wg.Add(2)

	// Parse request
	go func() {
		defer wg.Done()
		err := s.requestParser.parse()
		if err != nil {
			s.logger.Error("error parsing request", zap.Error(err))
		}
	}()

	// Parse response
	go func() {
		defer wg.Done()
		err := s.responseParser.parse()
		if err != nil {
			s.logger.Error("error parsing response", zap.Error(err))
		}
	}()

	// Wait for both parsers to complete
	wg.Wait()
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

	s.logger.Debug("creating request", zap.String("method", s.req.Method), zap.String("url", s.req.URL.String()))

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

	// check for Expect: 100-Continue header
	if s.req.Header.Get("Expect") == "100-continue" {
		s.expectContinue = true
		s.logger.Debug("detected Expect: 100-Continue header")
	}

	// set the state based on body and expect-continue
	switch {
	case noBody:
		s.State = SessionStateResponseHeaders
	case s.expectContinue:
		s.State = SessionStateAwaitingInterim
	default:
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

	// if we don't have a response, create a 499 canceled response
	// this typically happens when the connection is closed before the response is fully received
	if res == nil {
		res = &http.Response{
			StatusCode: 499,
			Status:     "Canceled",
		}
	}

	s.logger.Debug("creating response", zap.Int("status", res.StatusCode), zap.String("status", res.Status))

	// Handle interim responses (1xx) differently
	if res.StatusCode >= 100 && res.StatusCode < 200 {
		s.logger.Debug("detected interim response in CreateResponse", zap.Int("status", res.StatusCode))
		// This is an interim response, handle it separately
		if err := s.HandleInterimResponse(res); err != nil {
			s.logger.Error("error handling interim response", zap.Error(err))
		} else {
			s.logger.Debug("successfully handled interim response", zap.Int("status", res.StatusCode))
		}

		// For protocol switches (101), this is the final response
		if res.StatusCode == http.StatusSwitchingProtocols {
			s.responseChain.FinalResponse = res
			s.res = res
			s.State = SessionStateProtocolSwitch
		}

		// For other interim responses, don't set as final response yet
		s.logger.Debug("returning early after interim response", zap.Int("status", res.StatusCode))
		return
	}

	// This is a final response (non-1xx)
	s.res = res
	s.responseChain.FinalResponse = res

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

// HandleInterimResponse processes 1xx informational responses
func (s *Session) HandleInterimResponse(res *http.Response) error {
	s.logger.Debug("HandleInterimResponse called", zap.Int("status", res.StatusCode))

	// Note: Do not lock here as this is called from CreateResponse which already holds the lock

	if res.StatusCode < 100 || res.StatusCode >= 200 {
		s.logger.Error("invalid interim response status code", zap.Int("status", res.StatusCode))
		return fmt.Errorf("not an interim response: %d", res.StatusCode)
	}

	s.logger.Debug("handling interim response", zap.Int("status", res.StatusCode), zap.String("status_text", res.Status), zap.String("current_state", s.StateString()))

	// Create interim response record
	interim := &InterimResponse{
		StatusCode: res.StatusCode,
		Status:     res.Status,
		Header:     res.Header.Clone(),
		Timestamp:  time.Now(),
	}

	// Add to response chain
	s.responseChain.InterimResponses = append(s.responseChain.InterimResponses, interim)

	// Handle specific interim response types
	switch res.StatusCode {
	case http.StatusContinue: // Continue
		if s.expectContinue {
			s.logger.Debug("received 100 Continue, ready for request body")
			s.State = SessionStateRequestBody
		} else {
			s.logger.Warn("received unexpected 100 Continue response")
		}

	case http.StatusSwitchingProtocols: // Switching Protocols
		s.logger.Debug("received 101 Switching Protocols")
		s.protocolUpgraded = true
		s.State = SessionStateProtocolSwitch

		// Extract upgrade information
		if upgrade := res.Header.Get("Upgrade"); upgrade != "" {
			s.responseChain.ProtocolUpgrade = &UpgradeInfo{
				Protocol: upgrade,
				Headers:  res.Header.Clone(),
			}
		}

	case http.StatusProcessing: // Processing (WebDAV)
		s.logger.Debug("received 102 Processing")
		// Stay in current state, continue waiting

	case http.StatusEarlyHints: // Early Hints
		s.logger.Debug("received 103 Early Hints")
		// Stay in current state, continue waiting

	default:
		s.logger.Debug("received unknown 1xx response", zap.Int("status", res.StatusCode))
	}

	// Call plugin for interim response
	if s.pluginConn != nil {
		if err := s.pluginConn.OnHttpResponseHeaders(false); err != nil {
			s.logger.Error("plugin interim response headers", zap.Error(err))
		}
	}

	return nil
}

func (s *Session) WriteRequestBody(data []byte, done bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logger.Debug("writing request body", zap.Int("length", len(data)), zap.Bool("done", done), zap.String("state", s.StateString()))

	// If we're awaiting an interim response and haven't received 100 Continue yet,
	// this shouldn't normally happen since clients wait for 100-Continue before sending body
	if s.State == SessionStateAwaitingInterim && s.expectContinue {
		s.logger.Warn("received request body while still awaiting 100-Continue - protocol violation or timing issue",
			zap.Int("length", len(data)), zap.Bool("done", done))
		// Just ignore the data - the client should resend after 100-Continue
		return
	}

	// Process the body data normally
	s.processRequestBody(data, done)
}

// processRequestBody handles the actual request body processing and plugin callbacks
func (s *Session) processRequestBody(data []byte, done bool) {
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

	// set the state based on current state - always transition to expecting response
	s.logger.Debug("request body complete, transitioning to response headers state")
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
	return s.State == SessionStateResponseHeaders || s.State == SessionStateResponseBody ||
		s.State == SessionStateAwaitingInterim || s.State == SessionStateInterimResponse
}

// CanAcceptRequestBody returns true if the session can accept request body data
func (s *Session) CanAcceptRequestBody() bool {
	return s.State == SessionStateRequestBody ||
		(s.State == SessionStateAwaitingInterim && s.expectContinue)
}

// IsProtocolUpgraded returns true if the connection has switched protocols
func (s *Session) IsProtocolUpgraded() bool {
	return s.protocolUpgraded
}

// GetUpgradeInfo returns information about the protocol upgrade, if any
func (s *Session) GetUpgradeInfo() *UpgradeInfo {
	if s.responseChain != nil {
		return s.responseChain.ProtocolUpgrade
	}
	return nil
}

// GetInterimResponses returns all interim responses received
func (s *Session) GetInterimResponses() []*InterimResponse {
	if s.responseChain != nil {
		return s.responseChain.InterimResponses
	}
	return nil
}

func (s *Session) StateString() string {
	switch s.State {
	case SessionStateRequestHeaders:
		return "request_headers"
	case SessionStateRequestBody:
		return "request_body"
	case SessionStateAwaitingInterim:
		return "awaiting_interim"
	case SessionStateInterimResponse:
		return "interim_response"
	case SessionStateResponseHeaders:
		return "response_headers"
	case SessionStateResponseBody:
		return "response_body"
	case SessionStateProtocolSwitch:
		return "protocol_switch"
	case SessionStateDone:
		return "done"
	default:
		return "unknown"
	}
}
