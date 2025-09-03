package http1

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSession_HandleInterimResponse(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	// Create session
	session := NewSession(ctx, logger, "example.com", &connection.Connection{}, nil)
	defer session.Close()

	// Handle 100 Continue response
	continueResp := &http.Response{
		StatusCode: http.StatusContinue,
		Status:     "100 Continue",
		Header:     http.Header{},
	}

	err := session.HandleInterimResponse(continueResp)
	require.NoError(t, err)

	// Verify interim response was recorded
	require.Len(t, session.responseChain.InterimResponses, 1)
	require.Equal(t, 100, session.responseChain.InterimResponses[0].StatusCode)
}

// TestSession_SimpleGETRequest tests a basic GET request with no body
func TestSession_SimpleGETRequest(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: false,
	}

	session := NewSession(ctx, logger, "example.com", conn, nil)
	defer session.Close()

	// Create a simple GET request
	req := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Path: "/api/test"},
		Header: http.Header{
			"Host":       []string{"example.com"},
			"User-Agent": []string{"test-client/1.0"},
		},
	}

	// Process the request
	session.CreateRequest(req, true) // no body

	// Verify request state
	require.Equal(t, SessionStateResponseHeaders, session.State)
	require.Equal(t, "http", session.req.URL.Scheme)
	require.Equal(t, session.id, session.req.Header.Get("qpoint-request-id"))

	// Create a 200 OK response
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
	}

	session.CreateResponse(resp, true) // no body

	// Verify response state
	require.Equal(t, SessionStateDone, session.State)
	require.Equal(t, http.StatusOK, session.res.StatusCode)
}

// TestSession_POSTWithBody tests a POST request with body data
func TestSession_POSTWithBody(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: true,
	}

	session := NewSession(ctx, logger, "api.example.com", conn, nil)
	defer session.Close()

	// Create POST request
	req := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/submit"},
		Header: http.Header{
			"Content-Type":   []string{"application/json"},
			"Content-Length": []string{"25"},
		},
	}

	session.CreateRequest(req, false) // has body

	// Verify initial state
	require.Equal(t, SessionStateRequestBody, session.State)
	require.Equal(t, "https", session.req.URL.Scheme)

	// Send request body
	bodyData := `{"message": "hello"}`
	session.WriteRequestBody([]byte(bodyData), true)

	// Verify state after body
	require.Equal(t, SessionStateResponseHeaders, session.State)

	// Create response
	resp := &http.Response{
		StatusCode: http.StatusCreated,
		Status:     "201 Created",
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
	}

	session.CreateResponse(resp, false) // has body

	// Verify response state
	require.Equal(t, SessionStateResponseBody, session.State)

	// Send response body
	session.WriteResponseBody([]byte(`{"id": 123}`), true)

	// Verify final state
	require.Equal(t, SessionStateDone, session.State)
}

// TestSession_ExpectContinue_Success tests successful Expect: 100-Continue flow
func TestSession_ExpectContinue_Success(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: true,
	}

	session := NewSession(ctx, logger, "upload.example.com", conn, nil)
	defer session.Close()

	// Create PUT request with Expect: 100-Continue
	req := &http.Request{
		Method: http.MethodPut,
		URL:    &url.URL{Path: "/upload/file.txt"},
		Header: http.Header{
			"Content-Length": []string{"1024"},
			"Content-Type":   []string{"text/plain"},
			"Expect":         []string{"100-continue"},
		},
	}

	session.CreateRequest(req, false)

	// Verify expect continue was detected
	require.True(t, session.expectContinue)
	require.Equal(t, SessionStateAwaitingInterim, session.State)

	// Handle 100 Continue response
	continueResp := &http.Response{
		StatusCode: http.StatusContinue,
		Status:     "100 Continue",
		Header:     http.Header{},
	}

	err := session.HandleInterimResponse(continueResp)
	require.NoError(t, err)

	// Verify state transition to request body
	require.Equal(t, SessionStateRequestBody, session.State)

	// Verify interim response was recorded
	require.Len(t, session.responseChain.InterimResponses, 1)
	require.Equal(t, 100, session.responseChain.InterimResponses[0].StatusCode)

	// Send request body after 100 Continue
	session.WriteRequestBody([]byte("file content here"), true)

	// Verify state after body
	require.Equal(t, SessionStateResponseHeaders, session.State)

	// Send final response
	finalResp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
	}

	session.CreateResponse(finalResp, true)

	// Verify final state
	require.Equal(t, SessionStateDone, session.State)
	require.Equal(t, finalResp, session.responseChain.FinalResponse)
}

// TestSession_ExpectContinue_Rejection tests server rejecting Expect: 100-Continue
func TestSession_ExpectContinue_Rejection(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: false,
	}

	session := NewSession(ctx, logger, "api.example.com", conn, nil)
	defer session.Close()

	// Create request with Expect: 100-Continue
	req := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/api/data"},
		Header: http.Header{
			"Content-Length": []string{"500"},
			"Expect":         []string{"100-continue"},
		},
	}

	session.CreateRequest(req, false)

	// Verify expect continue state
	require.True(t, session.expectContinue)
	require.Equal(t, SessionStateAwaitingInterim, session.State)

	// Server responds with 417 Expectation Failed instead of 100 Continue
	rejectionResp := &http.Response{
		StatusCode: http.StatusExpectationFailed,
		Status:     "417 Expectation Failed",
		Header: http.Header{
			"Content-Type": []string{"text/plain"},
		},
	}

	session.CreateResponse(rejectionResp, false)

	// Should transition directly to response body (skipping interim)
	require.Equal(t, SessionStateResponseBody, session.State)
	require.Equal(t, rejectionResp, session.res)

	// Send error response body
	session.WriteResponseBody([]byte("Request entity too large"), true)

	// Verify final state
	require.Equal(t, SessionStateDone, session.State)
}

// TestSession_MultipleInterimResponses tests handling multiple 1xx responses
func TestSession_MultipleInterimResponses(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: false,
	}

	session := NewSession(ctx, logger, "processing.example.com", conn, nil)
	defer session.Close()

	// Create request
	req := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/long-process"},
		Header: http.Header{
			"Content-Length": []string{"100"},
		},
	}

	session.CreateRequest(req, false)
	session.WriteRequestBody([]byte("process this data"), true)

	// Send 102 Processing
	processingResp := &http.Response{
		StatusCode: http.StatusProcessing,
		Status:     "102 Processing",
		Header:     http.Header{},
	}

	err := session.HandleInterimResponse(processingResp)
	require.NoError(t, err)

	// Send 103 Early Hints
	hintsResp := &http.Response{
		StatusCode: http.StatusEarlyHints,
		Status:     "103 Early Hints",
		Header: http.Header{
			"Link": []string{"</style.css>; rel=preload; as=style"},
		},
	}

	err = session.HandleInterimResponse(hintsResp)
	require.NoError(t, err)

	// Verify both interim responses recorded
	require.Len(t, session.responseChain.InterimResponses, 2)
	require.Equal(t, 102, session.responseChain.InterimResponses[0].StatusCode)
	require.Equal(t, 103, session.responseChain.InterimResponses[1].StatusCode)
	require.Equal(t, "</style.css>; rel=preload; as=style", session.responseChain.InterimResponses[1].Header.Get("Link"))

	// Send final response
	finalResp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
	}

	session.CreateResponse(finalResp, true)

	require.Equal(t, SessionStateDone, session.State)
	require.Equal(t, finalResp, session.responseChain.FinalResponse)
}

// TestSession_WebSocketUpgrade tests 101 Switching Protocols for WebSocket
func TestSession_WebSocketUpgrade(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: true,
	}

	session := NewSession(ctx, logger, "ws.example.com", conn, nil)
	defer session.Close()

	// Create WebSocket upgrade request
	req := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Path: "/ws"},
		Header: http.Header{
			"Connection":             []string{"Upgrade"},
			"Upgrade":                []string{"websocket"},
			"Sec-Websocket-Key":      []string{"dGhlIHNhbXBsZSBub25jZQ=="},
			"Sec-Websocket-Version":  []string{"13"},
			"Sec-Websocket-Protocol": []string{"chat"},
		},
	}

	session.CreateRequest(req, true)

	// Send 101 Switching Protocols response
	upgradeResp := &http.Response{
		StatusCode: http.StatusSwitchingProtocols,
		Status:     "101 Switching Protocols",
		Header: http.Header{
			"Connection":             []string{"Upgrade"},
			"Upgrade":                []string{"websocket"},
			"Sec-Websocket-Accept":   []string{"s3pPLMBiTxaQ9kYGzzhZRbK+xOo="},
			"Sec-Websocket-Protocol": []string{"chat"},
		},
	}

	session.CreateResponse(upgradeResp, true)

	// Verify protocol upgrade
	require.Equal(t, SessionStateProtocolSwitch, session.State)
	require.True(t, session.protocolUpgraded)
	require.True(t, session.IsProtocolUpgraded())

	// Verify upgrade info
	upgradeInfo := session.GetUpgradeInfo()
	require.NotNil(t, upgradeInfo)
	require.Equal(t, "websocket", upgradeInfo.Protocol)
	// Use direct access since the header was stored with this specific key
	acceptHeaders := upgradeInfo.Headers["Sec-Websocket-Accept"]
	require.Len(t, acceptHeaders, 1)
	require.Equal(t, "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=", acceptHeaders[0])

	// Verify response chain
	require.Len(t, session.responseChain.InterimResponses, 1)
	require.Equal(t, upgradeResp, session.responseChain.FinalResponse)
}

// TestSession_HTTP2Upgrade tests 101 Switching Protocols for HTTP/2
func TestSession_HTTP2Upgrade(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: false,
	}

	session := NewSession(ctx, logger, "h2.example.com", conn, nil)
	defer session.Close()

	// Create HTTP/2 upgrade request
	req := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Path: "/"},
		Header: http.Header{
			"Connection":     []string{"Upgrade, HTTP2-Settings"},
			"Upgrade":        []string{"h2c"},
			"HTTP2-Settings": []string{"AAMAAABkAARAAAAAAAIAAAAA"},
		},
	}

	session.CreateRequest(req, true)

	// Send 101 Switching Protocols response
	upgradeResp := &http.Response{
		StatusCode: http.StatusSwitchingProtocols,
		Status:     "101 Switching Protocols",
		Header: http.Header{
			"Connection": []string{"Upgrade"},
			"Upgrade":    []string{"h2c"},
		},
	}

	session.CreateResponse(upgradeResp, true)

	// Verify protocol upgrade
	require.Equal(t, SessionStateProtocolSwitch, session.State)
	require.True(t, session.IsProtocolUpgraded())

	// Verify upgrade info
	upgradeInfo := session.GetUpgradeInfo()
	require.NotNil(t, upgradeInfo)
	require.Equal(t, "h2c", upgradeInfo.Protocol)
}

// TestSession_PrematureClose tests connection closed before completion
func TestSession_PrematureClose(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: false,
	}

	session := NewSession(ctx, logger, "example.com", conn, nil)

	// Create request
	req := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/api/data"},
		Header: http.Header{
			"Content-Length": []string{"100"},
		},
	}

	session.CreateRequest(req, false)

	// Verify initial state
	require.Equal(t, SessionStateRequestBody, session.State)

	// Close session before completing request/response
	session.Close()

	// Verify session marked as closed
	require.True(t, session.IsClosed())

	// Verify response was created as 499 Canceled
	require.NotNil(t, session.res)
	require.Equal(t, 499, session.res.StatusCode)
	require.Equal(t, "Canceled", session.res.Status)
}

// TestSession_499ClientCanceled tests client disconnection scenario
func TestSession_499ClientCanceled(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: false,
	}

	session := NewSession(ctx, logger, "example.com", conn, nil)
	defer session.Close()

	// Create request but don't provide response (simulating client disconnect)
	req := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Path: "/slow-endpoint"},
		Header: http.Header{},
	}

	session.CreateRequest(req, true)

	// Simulate nil response (connection closed)
	session.CreateResponse(nil, false)

	// Verify 499 response was created
	require.NotNil(t, session.res)
	require.Equal(t, 499, session.res.StatusCode)
	require.Equal(t, "Canceled", session.res.Status)
}

// TestSession_HEADRequest tests HEAD request (no response body expected)
func TestSession_HEADRequest(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: false,
	}

	session := NewSession(ctx, logger, "example.com", conn, nil)
	defer session.Close()

	// Create HEAD request
	req := &http.Request{
		Method: http.MethodHead,
		URL:    &url.URL{Path: "/resource"},
		Header: http.Header{
			"Host": []string{"example.com"},
		},
	}

	session.CreateRequest(req, true)

	// Create response with headers but no body
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header: http.Header{
			"Content-Type":   []string{"text/html"},
			"Content-Length": []string{"1234"}, // HEAD responses can include Content-Length
		},
	}

	session.CreateResponse(resp, true) // no body for HEAD

	// Verify response state goes directly to done
	require.Equal(t, SessionStateDone, session.State)
	require.Equal(t, "1234", session.res.Header.Get("Content-Length"))
}

// TestSession_StateTransitions tests all valid state transitions
func TestSession_StateTransitions(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: false,
	}

	session := NewSession(ctx, logger, "example.com", conn, nil)
	defer session.Close()

	// Initial state
	require.Equal(t, SessionStateRequestHeaders, session.State)

	// Create request with body
	req := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/test"},
		Header: http.Header{
			"Content-Length": []string{"10"},
		},
	}

	session.CreateRequest(req, false)

	// Should transition to request body
	require.Equal(t, SessionStateRequestBody, session.State)

	// Send request body
	session.WriteRequestBody([]byte("test data"), true)

	// Should transition to response headers
	require.Equal(t, SessionStateResponseHeaders, session.State)

	// Create response with body
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header: http.Header{
			"Content-Type": []string{"text/plain"},
		},
	}

	session.CreateResponse(resp, false)

	// Should transition to response body
	require.Equal(t, SessionStateResponseBody, session.State)

	// Send response body
	session.WriteResponseBody([]byte("response"), true)

	// Should transition to done
	require.Equal(t, SessionStateDone, session.State)
}

// TestSession_RequestBodyWhileAwaitingInterim tests protocol violation handling
func TestSession_RequestBodyWhileAwaitingInterim(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: false,
	}

	session := NewSession(ctx, logger, "example.com", conn, nil)
	defer session.Close()

	// Create request with Expect: 100-Continue
	req := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/upload"},
		Header: http.Header{
			"Content-Length": []string{"100"},
			"Expect":         []string{"100-continue"},
		},
	}

	session.CreateRequest(req, false)

	// Verify awaiting interim state
	require.Equal(t, SessionStateAwaitingInterim, session.State)

	// Try to send body before receiving 100 Continue (protocol violation)
	session.WriteRequestBody([]byte("premature body data"), false)

	// Should still be in awaiting interim state (data ignored)
	require.Equal(t, SessionStateAwaitingInterim, session.State)

	// Now send 100 Continue
	continueResp := &http.Response{
		StatusCode: http.StatusContinue,
		Status:     "100 Continue",
		Header:     http.Header{},
	}

	err := session.HandleInterimResponse(continueResp)
	require.NoError(t, err)

	// Now should accept body
	require.Equal(t, SessionStateRequestBody, session.State)

	session.WriteRequestBody([]byte("proper body data"), true)
	require.Equal(t, SessionStateResponseHeaders, session.State)
}

// TestSession_HostHeaderLogic tests host header logic without triggering SetDomain
func TestSession_HostHeaderLogic(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: false,
	}

	session := NewSession(ctx, logger, "original.com", conn, nil)
	defer session.Close()

	// Create request with same host as domain (won't trigger SetDomain)
	req := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Path: "/"},
		Host:   "original.com", // Same as session domain
		Header: http.Header{},
	}

	session.CreateRequest(req, true)

	// Verify host header was preserved
	require.Equal(t, "original.com", req.Host)
	require.Equal(t, "http", req.URL.Scheme)
}

// TestSession_GetMethods tests various getter methods
func TestSession_GetMethods(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: false,
	}

	session := NewSession(ctx, logger, "example.com", conn, nil)
	defer session.Close()

	// Test initial state
	require.False(t, session.IsClosed())
	require.False(t, session.IsParsingResponse())
	require.False(t, session.IsProtocolUpgraded())
	require.Nil(t, session.GetUpgradeInfo())
	require.Empty(t, session.GetInterimResponses())

	// Add some interim responses
	interim1 := &InterimResponse{
		StatusCode: 100,
		Status:     "100 Continue",
		Header:     http.Header{},
		Timestamp:  time.Now(),
	}
	interim2 := &InterimResponse{
		StatusCode: 102,
		Status:     "102 Processing",
		Header:     http.Header{},
		Timestamp:  time.Now(),
	}

	session.responseChain.InterimResponses = append(session.responseChain.InterimResponses, interim1, interim2)

	// Test getter methods
	interims := session.GetInterimResponses()
	require.Len(t, interims, 2)
	require.Equal(t, 100, interims[0].StatusCode)
	require.Equal(t, 102, interims[1].StatusCode)

	// Test StateString method
	require.Equal(t, "request_headers", session.StateString())

	session.State = SessionStateRequestBody
	require.Equal(t, "request_body", session.StateString())

	session.State = SessionStateAwaitingInterim
	require.Equal(t, "awaiting_interim", session.StateString())

	session.State = SessionStateInterimResponse
	require.Equal(t, "interim_response", session.StateString())

	session.State = SessionStateResponseHeaders
	require.Equal(t, "response_headers", session.StateString())

	session.State = SessionStateResponseBody
	require.Equal(t, "response_body", session.StateString())

	session.State = SessionStateProtocolSwitch
	require.Equal(t, "protocol_switch", session.StateString())

	session.State = SessionStateDone
	require.Equal(t, "done", session.StateString())

	// Test unknown state
	session.State = SessionState(999)
	require.Equal(t, "unknown", session.StateString())
}

// TestSession_PluginManagerIntegration tests that plugin manager is properly called
// Note: This test verifies the session works with nil plugin manager (common case)
func TestSession_PluginManagerIntegration(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	conn := &connection.Connection{
		IsTLS: false,
	}

	// Test with nil plugin manager (should work fine)
	session := NewSession(ctx, logger, "example.com", conn, nil)
	defer session.Close()

	// Create request
	req := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/api/test"},
		Header: http.Header{
			"Content-Type":   []string{"application/json"},
			"Content-Length": []string{"13"},
		},
	}

	session.CreateRequest(req, false)

	// Verify no plugin connection was created
	require.Nil(t, session.pluginConn)

	// Send request body
	bodyData := `{"test":true}`
	session.WriteRequestBody([]byte(bodyData), true)

	// Create response
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
	}

	session.CreateResponse(resp, false)

	// Send response body
	respBody := `{"success":true}`
	session.WriteResponseBody([]byte(respBody), true)

	// Verify session completed successfully
	require.Equal(t, SessionStateDone, session.State)
	require.Equal(t, resp, session.res)
}
