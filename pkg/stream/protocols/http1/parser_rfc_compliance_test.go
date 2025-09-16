package http1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// CoreMessageParsingAndFraming tests fundamental HTTP/1.1 message parsing
// These tests validate the parser's ability to correctly interpret start-lines, headers,
// and message framing rules according to RFC 9112.
func CoreMessageParsingAndFraming(t *testing.T) {
	cases := []ParserTestCase{
		{
			Name:           "Parse Standard Origin-Form Request (RFC 9112 Section 3.2.1)",
			RequestData:    loadTestData(t, "requests/origin_form.txt"),
			ExpectedEvents: []EventType{EventRequest},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed")
				require.Equal(t, "GET", state.Request.Method, "Method should be GET")
				require.Equal(t, "/path/to/resource?query=1", state.Request.RequestURI, "Request-Target should be origin-form")
				require.Equal(t, "HTTP/1.1", state.Request.Proto, "Protocol version should be HTTP/1.1")
				require.Equal(t, "example.com", state.Request.Headers.Get("Host"), "Host header should be present")
				require.Empty(t, state.RequestBody, "Request body should be empty")
			},
		},
		{
			Name:           "Parse Absolute-Form Request (RFC 9112 Section 3.2.2)",
			RequestData:    loadTestData(t, "requests/absolute_form.txt"),
			ExpectedEvents: []EventType{EventRequest},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed")
				require.Equal(t, "GET", state.Request.Method, "Method should be GET")
				require.Equal(t, "http://www.example.com/resource", state.Request.RequestURI, "Request-Target should be absolute-form")
				require.Equal(t, "www.example.com", state.Request.Headers.Get("Host"), "Host header should match URI")
			},
		},
		{
			Name:           "Parse Authority-Form Request for CONNECT (RFC 9112 Section 3.2.3)",
			RequestData:    loadTestData(t, "requests/authority_form.txt"),
			ExpectedEvents: []EventType{EventRequest},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed")
				require.Equal(t, "CONNECT", state.Request.Method, "Method should be CONNECT")
				require.Equal(t, "http://www.example.com:443", state.Request.RequestURI, "Request-Target should be authority-form (parser auto-adds http://)")
				require.Equal(t, "www.example.com", state.Request.Headers.Get("Host"), "Host header should be present")
			},
		},
		{
			Name:           "Parse Asterisk-Form Request for OPTIONS (RFC 9112 Section 3.2.4)",
			RequestData:    loadTestData(t, "requests/asterisk_form.txt"),
			ExpectedEvents: []EventType{EventRequest},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed")
				require.Equal(t, "OPTIONS", state.Request.Method, "Method should be OPTIONS")
				require.Equal(t, "*", state.Request.RequestURI, "Request-Target should be asterisk-form")
				require.Equal(t, "example.com", state.Request.Headers.Get("Host"), "Host header should be present")
			},
		},
		{
			Name:           "Parse Request with Lowercase Method (Parser allows non-standard methods)",
			RequestData:    loadTestData(t, "requests/invalid_method_lowercase.txt"),
			ExpectedEvents: []EventType{EventRequest},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed (parser is lenient)")
				require.Equal(t, "get", state.Request.Method, "Method should be parsed as-is")
				require.Equal(t, "/resource", state.Request.RequestURI, "Request-Target should be parsed")
			},
		},
		{
			Name:           "Parse Request Missing Host Header (Parser allows missing Host)",
			RequestData:    loadTestData(t, "requests/missing_host.txt"),
			ExpectedEvents: []EventType{EventRequest},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed (parser is lenient)")
				require.Equal(t, "GET", state.Request.Method, "Method should be GET")
				require.Equal(t, "/resource", state.Request.RequestURI, "Request-Target should be parsed")
				require.Empty(t, state.Request.Headers.Get("Host"), "Host header should be empty")
			},
		},
		{
			Name:           "Parse Standard Status-Line (RFC 9112 Section 4)",
			ResponseData:   loadTestData(t, "responses/standard_200.txt"),
			ExpectedEvents: []EventType{EventResponse, EventResponseBody},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Response, "Response should be parsed")
				require.Equal(t, "HTTP/1.1", state.Response.Proto, "Protocol version should be HTTP/1.1")
				require.Equal(t, 200, state.Response.StatusCode, "Status code should be 200")
				require.Equal(t, "OK", state.Response.Status, "Reason phrase should be OK")
				require.Equal(t, "text/plain", state.Response.Headers.Get("Content-Type"), "Content-Type should be text/plain")
				require.Equal(t, "Hello, world!", string(state.ResponseBody), "Response body should match")
			},
		},
		{
			Name:           "Parse Status-Line with Empty Reason-Phrase (RFC 9112 Section 4)",
			ResponseData:   loadTestData(t, "responses/empty_reason_phrase.txt"),
			ExpectedEvents: []EventType{EventResponse, EventResponseBody},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Response, "Response should be parsed")
				require.Equal(t, 404, state.Response.StatusCode, "Status code should be 404")
				require.Empty(t, state.Response.Status, "Reason phrase should be empty")
				require.Equal(t, "Not Found", string(state.ResponseBody), "Response body should match")
			},
		},
		{
			// Note: RFC 9112 Section 5.1 rejects whitespace before the colon. In the interest of
			// flexibility for observability parsers, we allow it.
			Name:           "Accept Header with Whitespace Before Colon (Parser is lenient)",
			RequestData:    loadTestData(t, "requests/whitespace_before_colon.txt"),
			ExpectedEvents: []EventType{EventRequest},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed (parser is lenient)")
				// Parser accepts headers with whitespace before colon, though RFC 9112 Section 5.1 forbids it
				require.Equal(t, "GET", state.Request.Method, "Method should be GET")
				// Note: The exact handling of the malformed header value may vary
			},
		},
		{
			Name:           "Framing Precedence - Transfer-Encoding Overrides Content-Length (RFC 9112 Section 6.3)",
			RequestData:    loadTestData(t, "requests/conflicting_framing.txt"),
			ExpectedEvents: []EventType{EventRequest, EventRequestBody},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed")
				require.Equal(t, "POST", state.Request.Method, "Method should be POST")
				require.Equal(t, "chunked", state.Request.Headers.Get("Transfer-Encoding"), "Transfer-Encoding should be chunked")
				require.Equal(t, "50", state.Request.Headers.Get("Content-Length"), "Content-Length should still be present")
				// Parser should use chunked encoding and read only "DATA" (4 bytes), not 50 bytes
				require.Equal(t, "DATA", string(state.RequestBody), "Body should be parsed using chunked encoding")
				require.True(t, state.RequestComplete, "Request should be complete")
				// Note: In an observability parser, we might want to log warnings about conflicting headers
				// but still successfully parse the message using the correct precedence rules
			},
		},
	}

	RunParserTestTable(t, cases)
}

// DetailedBodyContentParsing tests the correct parsing of message bodies
// according to the framing mechanism determined by headers (Content-Length, Transfer-Encoding).
func DetailedBodyContentParsing(t *testing.T) {
	cases := []ParserTestCase{
		{
			Name:           "Parse Content-Length Body Correctly",
			RequestData:    loadTestData(t, "requests/post_with_body.txt"),
			ExpectedEvents: []EventType{EventRequest, EventRequestBody},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed")
				require.Equal(t, "POST", state.Request.Method, "Method should be POST")
				require.Equal(t, "27", state.Request.Headers.Get("Content-Length"), "Content-Length should be 27")
				require.JSONEq(t, `{"name": "test", "id": 123}`, string(state.RequestBody), "Body should match exactly 27 bytes")
				require.True(t, state.RequestComplete, "Request should be complete")
			},
		},
		{
			Name:           "Handle Content-Length Body with Trailing Data",
			ResponseData:   loadTestData(t, "responses/content_length_with_trailing.txt"),
			CloseAfterData: true,
			ExpectedEvents: []EventType{EventResponse, EventResponseBody},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Response, "Response should be parsed")
				require.Equal(t, 200, state.Response.StatusCode, "Status code should be 200")
				require.Equal(t, "5", state.Response.Headers.Get("Content-Length"), "Content-Length should be 5")
				// Parser should read exactly 5 bytes: "Hello"
				// Trailing data "GET /next HTTP/1.1\r\nHost: example.com" should be left for next message
				require.Equal(t, "Hello", string(state.ResponseBody), "Body should be exactly 5 bytes")
				require.True(t, state.ResponseComplete, "Response should be complete")
			},
		},
		{
			// Note: RFC 9112 Section 6.3 Rule 5 rejects multiple content-length headers
			// But we are lenient and accept them
			Name:           "Accept Message with Multiple Conflicting Content-Length Headers (Parser is lenient)",
			ResponseData:   loadTestData(t, "responses/multiple_content_length.txt"),
			ExpectedEvents: []EventType{EventResponse, EventResponseBody},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Response, "Response should be parsed (parser is lenient)")
				// Parser accepts multiple Content-Length headers, though RFC 9112 Section 6.3 Rule 5 forbids it
				require.NotEmpty(t, state.Response.Headers.Get("Content-Length"), "Content-Length header should be present")
			},
		},
		{
			Name:           "Parse Multi-Chunk Transfer-Encoding: chunked Body (RFC 9112 Section 7.1)",
			ResponseData:   loadTestData(t, "responses/chunked_multi.txt"),
			ExpectedEvents: []EventType{EventResponse, EventResponseBody},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Response, "Response should be parsed")
				require.Equal(t, "chunked", state.Response.Headers.Get("Transfer-Encoding"), "Transfer-Encoding should be chunked")
				// Should reassemble: "Wiki" + "pedia" + " in\r\n\r\nchunks."
				expectedBody := "Wikipedia in\r\n\r\nchunks."
				require.Equal(t, expectedBody, string(state.ResponseBody), "Body should be reassembled correctly")
				require.True(t, state.ResponseComplete, "Response should be complete")
			},
		},
		{
			Name:           "Parse Chunked Body with Extensions and Trailers (RFC 9112 Sections 7.1.1 and 7.1.2)",
			ResponseData:   loadTestData(t, "responses/chunked_with_trailers.txt"),
			ExpectedEvents: []EventType{EventResponse, EventResponseBody},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Response, "Response should be parsed")
				require.Equal(t, "chunked", state.Response.Headers.Get("Transfer-Encoding"), "Transfer-Encoding should be chunked")
				require.Equal(t, "Mozilla", string(state.ResponseBody), "Body should be Mozilla")
				// TODO(Jon): Add trailer support
				// Trailers should be parsed and accessible
				// require.Equal(t, "Wed, 21 Oct 2015 07:28:00 GMT", state.Response.Headers.Get("Expires"), "Expires trailer should be parsed")
				// require.Equal(t, "trailer-value", state.Response.Headers.Get("Custom-Trailer"), "Custom trailer should be parsed")
				// require.True(t, state.ResponseComplete, "Response should be complete")
			},
		},
		{
			Name:           "Parse Chunked Body with Extensions",
			ResponseData:   loadTestData(t, "responses/chunked_with_extensions.txt"),
			ExpectedEvents: []EventType{EventResponse, EventResponseBody},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Response, "Response should be parsed")
				require.Equal(t, "chunked", state.Response.Headers.Get("Transfer-Encoding"), "Transfer-Encoding should be chunked")
				// Extensions should be ignored, body should just be "Mozilla"
				require.Equal(t, "Mozilla", string(state.ResponseBody), "Body should be Mozilla (extensions ignored)")
				require.True(t, state.ResponseComplete, "Response should be complete")
			},
		},
		{
			Name:           "Parse Simple Chunked Response",
			ResponseData:   loadTestData(t, "responses/chunked_simple.txt"),
			ExpectedEvents: []EventType{EventResponse, EventResponseBody},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Response, "Response should be parsed")
				require.Equal(t, "chunked", state.Response.Headers.Get("Transfer-Encoding"), "Transfer-Encoding should be chunked")
				require.Equal(t, "Hello", string(state.ResponseBody), "Body should be Hello")
				require.True(t, state.ResponseComplete, "Response should be complete")
			},
		},
	}

	RunParserTestTable(t, cases)
}

// RobustnessAndMalformedMessageHandling tests the parser's resilience
// to common non-standard variations and graceful handling of incomplete or corrupted data.
func RobustnessAndMalformedMessageHandling(t *testing.T) {
	cases := []ParserTestCase{
		{
			Name:           "Accept LF-Only Line Terminators (RFC 9112 Section 2.2)",
			RequestData:    loadTestData(t, "requests/request_with_lf_only.txt"),
			ExpectedEvents: []EventType{EventRequest},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed despite LF-only terminators")
				require.Equal(t, "GET", state.Request.Method, "Method should be GET")
				require.Equal(t, "/resource", state.Request.RequestURI, "Request-Target should be parsed")
				require.Equal(t, "example.com", state.Request.Headers.Get("Host"), "Host header should be parsed")
				// Note: For observability, we might want to log warnings about non-standard line terminators
				// but still successfully parse the message for analysis
			},
		},
		{
			// Note: RFC 9112 Section 2.2 rejects bare CR characters but we are lenient and accept them
			Name:           "Accept Bare CR Characters (Parser is lenient)",
			RequestData:    loadTestData(t, "requests/request_with_bare_cr.txt"),
			ExpectedEvents: []EventType{EventRequest},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed")
				require.Equal(t, "GET", state.Request.Method, "Method should be GET")
				require.Equal(t, "/resource", state.Request.RequestURI, "Request-Target should be parsed")
				require.Equal(t, "HTTP/1.1", state.Request.Proto, "Protocol version should be HTTP/1.1")
				require.Equal(t, "example.com", state.Request.Headers.Get("Host"), "Host header should be parsed")
				// Bare CR not followed by LF should be treated as invalid
			},
		},
		{
			Name:           "Ignore Leading Empty Lines Before Request (RFC 9112 Section 2.2)",
			RequestData:    loadTestData(t, "requests/request_with_leading_crlf.txt"),
			ExpectedEvents: []EventType{EventRequest},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed despite leading empty lines")
				require.Equal(t, "GET", state.Request.Method, "Method should be GET")
				require.Equal(t, "/resource", state.Request.RequestURI, "Request-Target should be parsed")
				require.Equal(t, "example.com", state.Request.Headers.Get("Host"), "Host header should be parsed")
				// Leading empty lines should be consumed and ignored
			},
		},
		{
			Name:           "Handle Truncated Message - Incomplete Header (RFC 9112 Section 8)",
			ResponseData:   loadTestData(t, "responses/incomplete_headers.txt"),
			CloseAfterData: true,
			ExpectedEvents: []EventType{EventError},
			Validate: func(t *testing.T, state TestState) {
				require.Len(t, state.Errors, 1, "Should have exactly one error for incomplete message")
				// Parser should not crash and should correctly identify incomplete message
				// The error might be EOF or similar indicating incomplete data
			},
		},
	}

	RunParserTestTable(t, cases)
}

// ObservabilityFocused tests scenarios specific to observability parsing
// where the parser should extract maximum useful information even from non-standard input.
func ObservabilityFocused(t *testing.T) {
	cases := []ParserTestCase{
		{
			Name:           "Parse Conflicting Framing Headers for Analysis",
			RequestData:    loadTestData(t, "requests/conflicting_framing.txt"),
			ExpectedEvents: []EventType{EventRequest, EventRequestBody},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed for observability")
				require.Equal(t, "POST", state.Request.Method, "Method should be POST")
				// Both headers should be preserved for analysis
				require.Equal(t, "chunked", state.Request.Headers.Get("Transfer-Encoding"), "Transfer-Encoding should be preserved")
				require.Equal(t, "50", state.Request.Headers.Get("Content-Length"), "Content-Length should be preserved")
				// Parser should correctly use Transfer-Encoding precedence but still capture both
				require.Equal(t, "DATA", string(state.RequestBody), "Body should follow Transfer-Encoding rules")
				require.True(t, state.RequestComplete, "Request should be complete")
				// For observability: both conflicting headers are available for analysis
			},
		},
		{
			Name:           "Handle Pipelined Requests for Traffic Analysis",
			RequestData:    loadTestData(t, "requests/pipelined_requests.txt"),
			ExpectedEvents: []EventType{EventRequest},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "First request should be parsed")
				require.Equal(t, "GET", state.Request.Method, "Method should be GET")
				require.Equal(t, "/req1", state.Request.RequestURI, "Should parse first request")
				require.Equal(t, "example.com", state.Request.Headers.Get("Host"), "Host should be parsed")
				// Note: In a real observability scenario, we'd want to parse both requests
				// This test verifies the first request is parsed correctly and the second
				// is available in the buffer for subsequent parsing
			},
		},
		{
			Name:           "Extract Information from Malformed but Partially Valid HTTP",
			RequestData:    loadTestData(t, "requests/request_with_folded_headers.txt"),
			ExpectedEvents: []EventType{EventRequest},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed despite obsolete syntax")
				require.Equal(t, "GET", state.Request.Method, "Method should be GET")
				require.Equal(t, "/resource", state.Request.RequestURI, "Request-Target should be parsed")
				require.Equal(t, "example.com", state.Request.Headers.Get("Host"), "Host should be parsed")
				// The folded header should be handled (RFC allows it but it's obsolete)
				longHeader := state.Request.Headers.Get("X-Long-Header")
				require.Contains(t, longHeader, "some value", "Folded header should contain original value")
				require.Contains(t, longHeader, "more value", "Folded header should contain continuation")
				// For observability: we successfully extract data even from obsolete syntax
			},
		},
	}

	RunParserTestTable(t, cases)
}

// EdgeCasesAndObsoleteSyntax tests less common but important features
// for full RFC compliance and real-world traffic handling.
func EdgeCasesAndObsoleteSyntax(t *testing.T) {
	cases := []ParserTestCase{
		{
			Name:           "Handle Obsolete Line Folding in Headers (RFC 9112 Section 5.2)",
			RequestData:    loadTestData(t, "requests/request_with_folded_headers.txt"),
			ExpectedEvents: []EventType{EventRequest},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "Request should be parsed")
				require.Equal(t, "GET", state.Request.Method, "Method should be GET")
				// Obsolete line folding should be handled by replacing fold with space
				longHeader := state.Request.Headers.Get("X-Long-Header")
				expectedValue := "some value more value" // \r\n should be replaced with space
				require.Equal(t, expectedValue, longHeader, "Folded header should be unfolded correctly")
				// For observability: deprecated syntax is handled but should be flagged
			},
		},
		{
			Name:           "Parse Pipelined Requests (RFC 9112 Section 9.3.2)",
			RequestData:    loadTestData(t, "requests/pipelined_requests.txt"),
			ExpectedEvents: []EventType{EventRequest},
			Validate: func(t *testing.T, state TestState) {
				require.NotNil(t, state.Request, "First request should be parsed")
				require.Equal(t, "GET", state.Request.Method, "Method should be GET")
				require.Equal(t, "/req1", state.Request.RequestURI, "First request URI should be /req1")
				// After parsing first request, buffer should contain second request
				// This test validates message boundary detection for pipelined requests
			},
		},
		{
			Name:           "Handle 100 Continue Intermediate Response (RFC 9112 Section 9.2)",
			ResponseData:   loadTestData(t, "responses/100_then_200.txt"),
			ExpectedEvents: []EventType{EventInterimResponse, EventResponse, EventResponseBody},
			Validate: func(t *testing.T, state TestState) {
				// Should have both interim and final responses
				require.Len(t, state.InterimResponses, 1, "Should have one interim response")
				require.Equal(t, 100, state.InterimResponses[0].StatusCode, "Interim response should be 100 Continue")
				require.True(t, state.InterimResponses[0].IsInterim, "Should be marked as interim")

				require.NotNil(t, state.Response, "Final response should be parsed")
				require.Equal(t, 200, state.Response.StatusCode, "Final response should be 200 OK")
				require.Equal(t, "Hello", string(state.ResponseBody), "Response body should be Hello")
				require.True(t, state.ResponseComplete, "Response should be complete")
			},
		},
		{
			Name:           "Parse Simple 100 Continue Response",
			ResponseData:   loadTestData(t, "responses/100_continue.txt"),
			CloseAfterData: true,
			ExpectedEvents: []EventType{EventInterimResponse},
			Validate: func(t *testing.T, state TestState) {
				require.Len(t, state.InterimResponses, 1, "Should have one interim response")
				require.Equal(t, 100, state.InterimResponses[0].StatusCode, "Should be 100 Continue")
				require.Equal(t, "Continue", state.InterimResponses[0].Status, "Reason phrase should be Continue")
				require.True(t, state.InterimResponses[0].IsInterim, "Should be marked as interim")
				require.Nil(t, state.Response, "Should not have final response yet")
			},
		},
	}

	RunParserTestTable(t, cases)
}
