# HTTP/1.1 Parser Testing Guide

## Overview

This guide documents the testing framework for the HTTP/1.1 parser in the http1v2 package. The parser processes HTTP wire protocol data through a `ProcessEvent(phase, chunk)` interface and emits events via callbacks. Our testing framework provides reliable, channel-based synchronization to validate parser behavior against the HTTP/1.1 RFC.

## Core Components

### 1. TestCallbackRecorder

A channel-based callback recorder that captures all parser events for verification.

**Usage:**
```go
recorder := NewTestCallbackRecorder(
    WithTimeout(2 * time.Second),    // Set event wait timeout
    WithBufferSize(100),              // Set event channel buffer size
)
```

**Key Methods:**
- `WaitForEvent(eventType)` - Wait for a specific event type
- `WaitForEvents(types...)` - Wait for events in order
- `WaitForEventsFlexible(timeout, types...)` - Wait for events in any order
- `CollectEvents(stopOnDone)` - Collect all events until timeout
- `GetState()` - Get accumulated parser state

### 2. Event Types

The parser emits these event types through callbacks:

```go
const (
    EventRequest         // OnRequest callback fired
    EventRequestBody     // OnRequestBody callback fired
    EventInterimResponse // OnInterimResponse callback fired (e.g., 100 Continue)
    EventResponse        // OnResponse callback fired
    EventResponseBody    // OnResponseBody callback fired
    EventError          // OnError callback fired
    EventDone           // OnDone callback fired (transaction complete)
)
```

### 3. Test Patterns

#### Simple Singular Test

For testing specific scenarios with fine-grained control:

```go
func TestSpecificScenario(t *testing.T) {
    // Load test data
    requestData := loadTestData(t, "requests/example.txt")
    responseData := loadTestData(t, "responses/example.txt")
    
    // Create recorder
    recorder := NewTestCallbackRecorder(WithTimeout(1 * time.Second))
    
    // Create parser
    parser := NewParser(t.Context(), recorder)
    defer parser.Close()
    
    // Send request
    err := parser.ProcessEvent(PhaseRequest, requestData)
    require.NoError(t, err)
    
    // Wait for and verify request event
    reqEvent, err := recorder.WaitForEvent(EventRequest)
    require.NoError(t, err)
    require.Equal(t, "GET", reqEvent.Request.Method)
    
    // Send response
    err = parser.ProcessEvent(PhaseResponse, responseData)
    require.NoError(t, err)
    
    // Wait for response events
    events, err := recorder.WaitForEvents(EventResponse, EventResponseBody)
    require.NoError(t, err)
    
    // Verify final state
    state := recorder.GetState()
    require.Equal(t, 200, state.Response.StatusCode)
    require.Contains(t, string(state.ResponseBody), "expected content")
}
```

#### Table-Driven Tests

For testing multiple scenarios efficiently:

```go
func TestHTTPScenarios(t *testing.T) {
    cases := []ParserTestCase{
        {
            Name:           "GET request with JSON response",
            RequestData:    loadTestData(t, "requests/get.txt"),
            ResponseData:   loadTestData(t, "responses/json.txt"),
            ExpectedEvents: []EventType{EventRequest, EventResponse, EventResponseBody},
            Validate: func(t *testing.T, state TestState) {
                require.Equal(t, "GET", state.Request.Method)
                require.Equal(t, 200, state.Response.StatusCode)
                require.Equal(t, "application/json", state.Response.Headers.Get("Content-Type"))
            },
        },
        {
            Name:           "POST with chunked response",
            RequestData:    loadTestData(t, "requests/post.txt"),
            ResponseData:   loadTestData(t, "responses/chunked.txt"),
            CloseAfterData: true, // Close connection after sending data
            ExpectedEvents: []EventType{EventRequest, EventRequestBody, EventResponse, EventResponseBody},
            Validate: func(t *testing.T, state TestState) {
                require.Equal(t, "POST", state.Request.Method)
                require.NotEmpty(t, state.RequestBody)
                require.Equal(t, "chunked", state.Response.Headers.Get("Transfer-Encoding"))
            },
        },
    }
    
    RunParserTestTable(t, cases)
}
```

#### Testing Partial Data Transmission

For testing network conditions and streaming behavior:

```go
func TestPartialDataTransmission(t *testing.T) {
    recorder := NewTestCallbackRecorder()
    parser := NewParser(t.Context(), recorder)
    defer parser.Close()
    
    // Use ChunkedDataSender for controlled transmission
    sender := NewChunkedDataSender(t, parser)
    
    // Send data in small chunks with delays (simulates slow network)
    requestData := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
    err := sender.SendInChunks(PhaseRequest, requestData, 10, 5*time.Millisecond)
    require.NoError(t, err)
    
    // Or send line by line
    responseData := []byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nHello")
    err = sender.SendByLines(PhaseResponse, responseData, 2*time.Millisecond)
    require.NoError(t, err)
    
    // Verify events arrived correctly
    state := recorder.GetState()
    require.Equal(t, "GET", state.Request.Method)
    require.Equal(t, []byte("Hello"), state.ResponseBody)
}
```

#### Event Collection and Analysis

For complex scenarios requiring event sequence verification:

```go
func TestEventSequencing(t *testing.T) {
    recorder := NewTestCallbackRecorder()
    parser := NewParser(t.Context(), recorder)
    defer parser.Close()
    
    // Process data
    parser.ProcessEvent(PhaseRequest, requestData)
    parser.ProcessEvent(PhaseResponse, responseData)
    
    // Create event collector
    collector := NewEventCollector(t, recorder)
    
    // Wait for specific event and collect all events until then
    events, found := collector.WaitForEventType(EventResponseBody, 1*time.Second)
    require.True(t, found)
    
    // Verify event sequence
    collector.AssertEventSequence([]EventType{
        EventRequest,
        EventResponse,
        EventResponseBody,
    })
    
    // Analyze specific event types
    bodyEvents := collector.GetEventsByType(EventResponseBody)
    require.Len(t, bodyEvents, 1)
}
```

## Test Data Organization

Place test data files in `testdata/` subdirectories:

```
http1v2/
├── testdata/
│   ├── requests/
│   │   ├── get_simple.txt
│   │   ├── post_with_body.txt
│   │   └── api_get_with_auth.txt
│   └── responses/
│       ├── get_200_with_body.txt
│       ├── 401_with_connection_close.txt
│       └── chunked_response.txt
```

Load test data using the helper function:
```go
requestData := loadTestData(t, "requests/get_simple.txt")
```

## Best Practices

### 1. Use Appropriate Wait Methods

- **`WaitForEvent()`** - When you expect a specific event next
- **`WaitForEvents()`** - When events must occur in a specific order
- **`WaitForEventsFlexible()`** - When events can arrive in any order
- **`CollectEvents()`** - When you need to analyze all events

### 2. Handle Connection Close Properly

For testing Connection: close behavior:
```go
{
    Name:           "Connection close test",
    RequestData:    requestData,
    ResponseData:   responseData,
    CloseAfterData: true,  // Ensures parser.Close() is called
    ExpectedEvents: []EventType{EventRequest, EventResponse, EventResponseBody},
    Validate: func(t *testing.T, state TestState) {
        // Connection close allows body without Content-Length
        require.NotEmpty(t, state.ResponseBody)
    },
}
```

### 3. Test Edge Cases

Essential edge cases to test:

1. **Chunked Transfer Encoding**
   - Multiple chunks
   - Zero-size chunks
   - Chunk extensions
   - Trailers

2. **Content-Length Scenarios**
   - Content-Length: 0
   - Missing Content-Length with Connection: close
   - Mismatched Content-Length

3. **Interim Responses**
   - 100 Continue
   - 102 Processing
   - Multiple interim responses

4. **Partial Data**
   - Headers split across chunks
   - Body split across chunks
   - Incomplete transactions

### 4. Validate Both Events and State

Always verify both individual events and final accumulated state:

```go
// Verify specific event
event, err := recorder.WaitForEvent(EventResponse)
require.NoError(t, err)
require.Equal(t, 200, event.Response.StatusCode)

// Also verify accumulated state
state := recorder.GetState()
require.True(t, state.ResponseComplete)
require.Equal(t, expectedBody, string(state.ResponseBody))
```

### 5. Set Reasonable Timeouts

- Use short timeouts (1-2 seconds) for expected events
- Use longer timeouts only when testing slow/chunked transmission
- Configure timeout per test based on complexity:

```go
recorder := NewTestCallbackRecorder(
    WithTimeout(500 * time.Millisecond), // Fast tests
)
```

## Common Test Patterns

### Testing Request Without Body

```go
{
    Name:         "GET request no body",
    RequestData:  []byte("GET /path HTTP/1.1\r\nHost: example.com\r\n\r\n"),
    ExpectedEvents: []EventType{EventRequest},
    Validate: func(t *testing.T, state TestState) {
        require.True(t, state.Request.NoBody)
        require.Empty(t, state.RequestBody)
    },
}
```

### Testing Request With Body

```go
{
    Name:         "POST with JSON body",
    RequestData:  []byte("POST /api HTTP/1.1\r\nHost: example.com\r\nContent-Length: 13\r\n\r\n{\"key\":\"val\"}"),
    ExpectedEvents: []EventType{EventRequest, EventRequestBody},
    Validate: func(t *testing.T, state TestState) {
        require.False(t, state.Request.NoBody)
        require.Equal(t, `{"key":"val"}`, string(state.RequestBody))
        require.True(t, state.RequestComplete)
    },
}
```

### Testing Streaming Response

```go
{
    Name:         "Streaming response",
    ResponseData: []byte("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nHello\r\n0\r\n\r\n"),
    ExpectedEvents: []EventType{EventResponse, EventResponseBody},
    Validate: func(t *testing.T, state TestState) {
        require.Equal(t, "chunked", state.Response.Headers.Get("Transfer-Encoding"))
        require.Equal(t, "Hello", string(state.ResponseBody))
        require.True(t, state.ResponseComplete)
    },
}
```

### Testing Error Conditions

```go
func TestMalformedRequest(t *testing.T) {
    recorder := NewTestCallbackRecorder()
    parser := NewParser(t.Context(), recorder)
    defer parser.Close()
    
    // Send malformed data
    err := parser.ProcessEvent(PhaseRequest, []byte("INVALID REQUEST\r\n\r\n"))
    
    // Wait for error event
    event, err := recorder.WaitForEvent(EventError)
    require.NoError(t, err)
    require.NotNil(t, event.Error)
    require.Contains(t, event.Error.Error(), "malformed")
}
```

## Debugging Tips

### 1. Inspect All Events

When a test fails, collect all events to understand what happened:

```go
// In failing test, temporarily add:
events := recorder.CollectEvents(false)
for i, e := range events {
    t.Logf("Event %d: Type=%v", i, e.Type)
    if e.Request != nil {
        t.Logf("  Request: %s %s", e.Request.Method, e.Request.RequestURI)
    }
    if e.Response != nil {
        t.Logf("  Response: %d %s", e.Response.StatusCode, e.Response.Status)
    }
    if e.Error != nil {
        t.Logf("  Error: %v", e.Error)
    }
}
```

### 2. Check Event Timing

Use the EventCollector to understand event timing:

```go
collector := NewEventCollector(t, recorder)
collector.CollectFor(500 * time.Millisecond)
t.Logf("Received %d events in 500ms", len(collector.events))
```

### 3. Verify Test Data

Always verify test data files are properly formatted:

```go
// Add debug output
t.Logf("Request data (%d bytes): %q", len(requestData), requestData)
```

## Performance Considerations

1. **Use buffered channels**: Default buffer size is 100 events
2. **Minimize wait times**: Use appropriate timeouts for each test
3. **Run tests in parallel** when possible:
   ```go
   t.Run(tc.Name, func(t *testing.T) {
       t.Parallel() // If tests are independent
       RunParserTest(t, tc)
   })
   ```

## Adding New Tests

When adding tests for new HTTP/1.1 features:

1. Create test data files in `testdata/`
2. Define test cases using `ParserTestCase` struct
3. Focus on RFC compliance edge cases
4. Test both successful and error scenarios
5. Verify both events and final state
6. Document any RFC sections being tested

Example for testing new feature:

```go
// TestRFC7230Section3_2 tests message parsing requirements from RFC 7230 Section 3.2
func TestRFC7230Section3_2(t *testing.T) {
    cases := []ParserTestCase{
        {
            Name: "RFC 7230 3.2.3 - Whitespace in header field values",
            RequestData: []byte("GET / HTTP/1.1\r\nHost:  example.com  \r\n\r\n"),
            ExpectedEvents: []EventType{EventRequest},
            Validate: func(t *testing.T, state TestState) {
                // Verify whitespace is trimmed per RFC
                require.Equal(t, "example.com", state.Request.Headers.Get("Host"))
            },
        },
        // Add more RFC compliance tests...
    }
    
    RunParserTestTable(t, cases)
}
```

This framework enables comprehensive, reliable testing of HTTP/1.1 parser implementations with minimal boilerplate and maximum clarity.