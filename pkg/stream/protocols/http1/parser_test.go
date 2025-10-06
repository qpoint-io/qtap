package http1

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// loadTestData loads test data from the testdata directory
func loadTestData(t *testing.T, filename string) []byte {
	t.Helper()
	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to load test data: %s", filename)
	return data
}

// Example of using the new test harness for a simple singular test
func TestSimpleHTTPTransactionWithChannels(t *testing.T) {
	// Load test data
	requestData := loadTestData(t, "requests/get_simple.txt")
	responseData := loadTestData(t, "responses/get_200_with_body.txt")

	// Create test recorder
	recorder := NewTestCallbackRecorder(
		WithTimeout(2 * time.Second), // Shorter timeout for faster tests
	)

	// Create parser with test context
	ctx := t.Context()
	if deadline, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	parser := NewParser(ctx, recorder)
	defer parser.Close()

	// Send request data
	err := parser.ProcessEvent(PhaseRequest, requestData)
	require.NoError(t, err)

	// Wait for request event
	event, err := recorder.WaitForEvent(EventRequest)
	require.NoError(t, err)
	require.NotNil(t, event.Request)
	require.Equal(t, "GET", event.Request.Method)
	require.Equal(t, "/api/users", event.Request.RequestURI)

	// Send response data
	err = parser.ProcessEvent(PhaseResponse, responseData)
	require.NoError(t, err)

	// Wait for response and body events
	events, err := recorder.WaitForEvents(EventResponse, EventResponseBody)
	require.NoError(t, err)
	require.Len(t, events, 2)

	// Verify response
	respEvent := events[0]
	require.Equal(t, 200, respEvent.Response.StatusCode)
	require.Equal(t, "application/json", respEvent.Response.Headers.Get("Content-Type"))

	// Get final state to verify accumulated data
	state := recorder.GetState()
	expectedBody := `[{"id": 1, "name": "John Doe", "email": "john@example.com"}, {"id": 2, "name": "Jane Doe"}]`
	require.Equal(t, expectedBody, string(state.ResponseBody))
}

// Example of table-driven tests using the new harness
func TestHTTPTransactionTableDriven(t *testing.T) {
	cases := []ParserTestCase{
		{
			Name:           "Simple GET with JSON response",
			RequestData:    loadTestData(t, "requests/get_simple.txt"),
			ResponseData:   loadTestData(t, "responses/get_200_with_body.txt"),
			ExpectedEvents: []EventType{EventRequest, EventResponse, EventResponseBody},
			Validate: func(t *testing.T, state TestState) {
				// Validate request
				require.NotNil(t, state.Request)
				require.Equal(t, "GET", state.Request.Method)
				require.Equal(t, "/api/users", state.Request.RequestURI)
				require.Equal(t, "example.com", state.Request.Headers.Get("Host"))

				// Validate response
				require.NotNil(t, state.Response)
				require.Equal(t, 200, state.Response.StatusCode)
				require.Equal(t, "application/json", state.Response.Headers.Get("Content-Type"))

				// Validate body
				expectedBody := `[{"id": 1, "name": "John Doe", "email": "john@example.com"}, {"id": 2, "name": "Jane Doe"}]`
				require.Equal(t, expectedBody, string(state.ResponseBody))
			},
		},
		{
			Name:           "GET with auth and 401 response",
			RequestData:    loadTestData(t, "requests/api_get_with_auth.txt"),
			ResponseData:   loadTestData(t, "responses/401_with_connection_close.txt"),
			CloseAfterData: true, // Close to simulate connection close
			ExpectedEvents: []EventType{EventRequest, EventResponse, EventResponseBody},
			Validate: func(t *testing.T, state TestState) {
				// Validate request with auth header
				require.NotNil(t, state.Request)
				require.Equal(t, "GET", state.Request.Method)
				require.Equal(t, "/repos/company/hello-world", state.Request.RequestURI)
				require.Equal(t, "Bearer token123", state.Request.Headers.Get("Authorization"))

				// Validate 401 response
				require.NotNil(t, state.Response)
				require.Equal(t, 401, state.Response.StatusCode)
				require.Equal(t, "Unauthorized", state.Response.Status)
				require.Equal(t, "close", state.Response.Headers.Get("Connection"))

				// Validate error body
				require.Contains(t, string(state.ResponseBody), "Bad credentials")

				// Note: DoneCalled may or may not be true depending on timing
				// The connection:close handling is asynchronous
			},
		},
	}

	RunParserTestTable(t, cases)
}

// Example of testing chunked data processing
func TestChunkedDataProcessing(t *testing.T) {
	requestData := loadTestData(t, "requests/get_simple.txt")
	responseData := loadTestData(t, "responses/get_200_with_body.txt")

	// Create recorder with small timeout for fast tests
	recorder := NewTestCallbackRecorder(WithTimeout(1 * time.Second))

	ctx := t.Context()
	parser := NewParser(ctx, recorder)
	defer parser.Close()

	// Send request in chunks to test partial processing
	mid := len(requestData) / 2
	err := parser.ProcessEvent(PhaseRequest, requestData[:mid])
	require.NoError(t, err)
	err = parser.ProcessEvent(PhaseRequest, requestData[mid:])
	require.NoError(t, err)

	// Wait for request to be parsed
	reqEvent, err := recorder.WaitForEvent(EventRequest)
	require.NoError(t, err)
	require.Equal(t, "GET", reqEvent.Request.Method)

	// Send response data
	err = parser.ProcessEvent(PhaseResponse, responseData)
	require.NoError(t, err)

	// Collect all events for analysis
	events := recorder.CollectEvents(false)

	// Count event types
	var responseCount, bodyCount int
	for _, e := range events {
		switch e.Type {
		case EventResponse:
			responseCount++
		case EventResponseBody:
			bodyCount++
		}
	}

	require.Equal(t, 1, responseCount, "Should have exactly one response event")
	require.GreaterOrEqual(t, bodyCount, 1, "Should have at least one body event")

	// Verify final state
	state := recorder.GetState()
	require.NotNil(t, state.Response)
	require.Equal(t, 200, state.Response.StatusCode)
}

// Example using the advanced test helpers
func TestAdvancedChunkedTransmission(t *testing.T) {
	requestData := loadTestData(t, "requests/get_simple.txt")
	responseData := loadTestData(t, "responses/get_200_example_com.txt")

	// Create recorder and parser
	recorder := NewTestCallbackRecorder(WithTimeout(2 * time.Second))
	ctx := t.Context()
	parser := NewParser(ctx, recorder)
	defer parser.Close()

	// Use ChunkedDataSender to simulate slow network
	sender := NewChunkedDataSender(t, parser)

	// Send request data in 10-byte chunks with 1ms delay
	err := sender.SendInChunks(PhaseRequest, requestData, 10, 1*time.Millisecond)
	require.NoError(t, err)

	// Create event collector
	collector := NewEventCollector(t, recorder)

	// Wait for request event and collect any other events
	events, found := collector.WaitForEventType(EventRequest, 500*time.Millisecond)
	require.True(t, found, "Should receive request event")
	require.GreaterOrEqual(t, len(events), 1, "Should have at least one event")

	// Send response line by line to simulate very slow connection
	err = sender.SendByLines(PhaseResponse, responseData, 500*time.Microsecond)
	require.NoError(t, err)

	// Collect events for a bit
	collector.CollectFor(100 * time.Millisecond)

	// Verify we got response events
	responseEvents := collector.GetEventsByType(EventResponse)
	require.Len(t, responseEvents, 1, "Should have exactly one response event")

	bodyEvents := collector.GetEventsByType(EventResponseBody)
	require.GreaterOrEqual(t, len(bodyEvents), 44, "Should have at least one body event")

	// Get final state
	state := recorder.GetState()
	require.NotNil(t, state.Request)
	require.Equal(t, "GET", state.Request.Method)
	require.NotNil(t, state.Response)
	require.Equal(t, 200, state.Response.StatusCode)
}
