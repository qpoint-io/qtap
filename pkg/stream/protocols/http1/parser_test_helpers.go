package http1

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Event types for testing
type EventType int

const (
	EventRequest EventType = iota
	EventRequestBody
	EventInterimResponse
	EventResponse
	EventResponseBody
	EventError
	EventDone
)

func (e EventType) String() string {
	return []string{
		"EventRequest",
		"EventRequestBody",
		"EventInterimResponse",
		"EventResponse",
		"EventResponseBody",
		"EventError",
		"EventDone",
	}[e]
}

// Event represents a callback event for testing
type Event struct {
	Type     EventType
	Request  *Request
	Response *Response
	Body     []byte
	Complete bool
	NoBody   bool
	Error    error
}

// TestCallbackRecorder captures parser events with channel-based synchronization
type TestCallbackRecorder struct {
	mu sync.Mutex

	// Event channel for observers
	events chan Event

	// Accumulated state
	request          *Request
	requestBody      []byte
	requestComplete  bool
	interimResponses []*Response
	response         *Response
	responseBody     []byte
	responseComplete bool
	errors           []error
	doneCalled       bool

	// Options
	bufferSize int
	timeout    time.Duration
}

// TestCallbackOption configures the TestCallbackRecorder
type TestCallbackOption func(*TestCallbackRecorder)

// WithBufferSize sets the event channel buffer size
func WithBufferSize(size int) TestCallbackOption {
	return func(r *TestCallbackRecorder) {
		r.bufferSize = size
	}
}

// WithTimeout sets the default timeout for waiting operations
func WithTimeout(d time.Duration) TestCallbackOption {
	return func(r *TestCallbackRecorder) {
		r.timeout = d
	}
}

// NewTestCallbackRecorder creates a new test callback recorder
func NewTestCallbackRecorder(opts ...TestCallbackOption) *TestCallbackRecorder {
	r := &TestCallbackRecorder{
		bufferSize: 100,             // Default buffer size
		timeout:    5 * time.Second, // Default timeout
	}

	for _, opt := range opts {
		opt(r)
	}

	r.events = make(chan Event, r.bufferSize)
	return r
}

// OnRequest implements Callbacks
func (r *TestCallbackRecorder) OnRequest(req *Request, noBody bool) {
	r.mu.Lock()
	r.request = req
	r.mu.Unlock()

	r.events <- Event{
		Type:    EventRequest,
		Request: req,
		NoBody:  noBody,
	}
}

// OnRequestBody implements Callbacks
func (r *TestCallbackRecorder) OnRequestBody(data []byte, complete bool) {
	r.mu.Lock()
	if data != nil {
		r.requestBody = append(r.requestBody, data...)
	}
	if complete {
		r.requestComplete = true
	}
	r.mu.Unlock()

	// Make a copy of data for the event
	var bodyCopy []byte
	if data != nil {
		bodyCopy = make([]byte, len(data))
		copy(bodyCopy, data)
	}

	r.events <- Event{
		Type:     EventRequestBody,
		Body:     bodyCopy,
		Complete: complete,
	}
}

// OnInterimResponse implements Callbacks
func (r *TestCallbackRecorder) OnInterimResponse(resp *Response) {
	r.mu.Lock()
	r.interimResponses = append(r.interimResponses, resp)
	r.mu.Unlock()

	r.events <- Event{
		Type:     EventInterimResponse,
		Response: resp,
	}
}

// OnResponse implements Callbacks
func (r *TestCallbackRecorder) OnResponse(resp *Response, noBody bool) {
	r.mu.Lock()
	r.response = resp
	r.mu.Unlock()

	r.events <- Event{
		Type:     EventResponse,
		Response: resp,
		NoBody:   noBody,
	}
}

// OnResponseBody implements Callbacks
func (r *TestCallbackRecorder) OnResponseBody(data []byte, complete bool) {
	r.mu.Lock()
	if data != nil {
		r.responseBody = append(r.responseBody, data...)
	}
	if complete {
		r.responseComplete = true
	}
	r.mu.Unlock()

	// Make a copy of data for the event
	var bodyCopy []byte
	if data != nil {
		bodyCopy = make([]byte, len(data))
		copy(bodyCopy, data)
	}

	r.events <- Event{
		Type:     EventResponseBody,
		Body:     bodyCopy,
		Complete: complete,
	}
}

// OnError implements Callbacks
func (r *TestCallbackRecorder) OnError(err error) {
	r.mu.Lock()
	r.errors = append(r.errors, err)
	r.mu.Unlock()

	r.events <- Event{
		Type:  EventError,
		Error: err,
	}
}

// OnDone implements Callbacks
func (r *TestCallbackRecorder) OnDone() {
	r.mu.Lock()
	r.doneCalled = true
	r.mu.Unlock()

	r.events <- Event{
		Type: EventDone,
	}
}

// WaitForEvent waits for a specific event type with timeout
func (r *TestCallbackRecorder) WaitForEvent(eventType EventType) (*Event, error) {
	timer := time.NewTimer(r.timeout)
	defer timer.Stop()

	for {
		select {
		case event := <-r.events:
			if event.Type == eventType {
				return &event, nil
			}
			// Skip unexpected events and keep waiting
			// This allows for flexibility in event ordering
		case <-timer.C:
			return nil, fmt.Errorf("timeout waiting for event %v", eventType)
		}
	}
}

// WaitForEvents waits for multiple events in order
func (r *TestCallbackRecorder) WaitForEvents(eventTypes ...EventType) ([]*Event, error) {
	events := make([]*Event, 0, len(eventTypes))
	for _, eventType := range eventTypes {
		event, err := r.WaitForEvent(eventType)
		if err != nil {
			return events, err
		}
		events = append(events, event)
	}
	return events, nil
}

// WaitForEventsFlexible waits for multiple events but allows them to arrive in any order
func (r *TestCallbackRecorder) WaitForEventsFlexible(timeout time.Duration, eventTypes ...EventType) ([]*Event, error) {
	required := make(map[EventType]bool)
	for _, et := range eventTypes {
		required[et] = true
	}

	events := make([]*Event, 0)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for len(required) > 0 {
		select {
		case event := <-r.events:
			events = append(events, &event)
			if required[event.Type] {
				delete(required, event.Type)
			}
		case <-timer.C:
			var missing []EventType
			for et := range required {
				missing = append(missing, et)
			}
			return events, fmt.Errorf("timeout waiting for events %v", missing)
		}
	}
	return events, nil
}

// WaitForCompletion waits for the OnDone callback or timeout
func (r *TestCallbackRecorder) WaitForCompletion() error {
	_, err := r.WaitForEvent(EventDone)
	return err
}

// CollectEvents collects all events until timeout or done
func (r *TestCallbackRecorder) CollectEvents(stopOnDone bool) []Event {
	var events []Event
	timer := time.NewTimer(100 * time.Millisecond) // Short timeout for collecting
	defer timer.Stop()

	for {
		select {
		case event := <-r.events:
			events = append(events, event)
			if stopOnDone && event.Type == EventDone {
				return events
			}
			timer.Reset(100 * time.Millisecond)
		case <-timer.C:
			return events
		}
	}
}

// GetState returns the current accumulated state (thread-safe)
func (r *TestCallbackRecorder) GetState() TestState {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Make copies of slices
	errorsCopy := make([]error, len(r.errors))
	copy(errorsCopy, r.errors)

	interimCopy := make([]*Response, len(r.interimResponses))
	copy(interimCopy, r.interimResponses)

	requestBodyCopy := make([]byte, len(r.requestBody))
	copy(requestBodyCopy, r.requestBody)

	responseBodyCopy := make([]byte, len(r.responseBody))
	copy(responseBodyCopy, r.responseBody)

	return TestState{
		Request:          r.request,
		RequestBody:      requestBodyCopy,
		RequestComplete:  r.requestComplete,
		InterimResponses: interimCopy,
		Response:         r.response,
		ResponseBody:     responseBodyCopy,
		ResponseComplete: r.responseComplete,
		Errors:           errorsCopy,
		DoneCalled:       r.doneCalled,
	}
}

// TestState represents the accumulated parser state
type TestState struct {
	Request          *Request
	RequestBody      []byte
	RequestComplete  bool
	InterimResponses []*Response
	Response         *Response
	ResponseBody     []byte
	ResponseComplete bool
	Errors           []error
	DoneCalled       bool
}

// ParserTestCase defines a test case for parser testing
type ParserTestCase struct {
	Name           string
	RequestData    []byte
	ResponseData   []byte
	ExpectedEvents []EventType
	Validate       func(t *testing.T, state TestState)
	CloseAfterData bool // Whether to close parser after sending data
}

// RunParserTest runs a single parser test case
func RunParserTest(t *testing.T, tc ParserTestCase) {
	t.Helper()

	// Create recorder
	recorder := NewTestCallbackRecorder()

	// Create parser
	ctx := t.Context()
	parser := NewParser(ctx, recorder)

	// Send request data if provided
	if tc.RequestData != nil {
		err := parser.ProcessEvent(PhaseRequest, tc.RequestData)
		require.NoError(t, err, "failed to process request data")
	}

	// Send response data if provided
	if tc.ResponseData != nil {
		err := parser.ProcessEvent(PhaseResponse, tc.ResponseData)
		require.NoError(t, err, "failed to process response data")
	}

	// Close if requested (do this in a deferred function to ensure cleanup)
	if tc.CloseAfterData {
		defer parser.Close()
		// Give some time for data to be processed before closing
		time.Sleep(10 * time.Millisecond)
		parser.Close()
	} else {
		defer parser.Close()
	}

	// Wait for expected events if specified
	if len(tc.ExpectedEvents) > 0 {
		events, err := recorder.WaitForEventsFlexible(2*time.Second, tc.ExpectedEvents...)
		eventTypesReceived := make([]string, len(events))
		for i, event := range events {
			eventTypesReceived[i] = event.Type.String()
		}
		require.NoErrorf(t, err, "failed waiting for expected events, events received: %s", strings.Join(eventTypesReceived, ", "))
		require.GreaterOrEqual(t, len(events), len(tc.ExpectedEvents), "unexpected number of events")
	}

	// Get final state and validate
	state := recorder.GetState()
	if tc.Validate != nil {
		tc.Validate(t, state)
	}
}

// RunParserTestTable runs a table of parser test cases
func RunParserTestTable(t *testing.T, cases []ParserTestCase) {
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			RunParserTest(t, tc)
		})
	}
}

// ChunkedDataSender helps test partial data transmission scenarios
type ChunkedDataSender struct {
	parser *Parser
	t      *testing.T
}

// NewChunkedDataSender creates a helper for sending data in chunks
func NewChunkedDataSender(t *testing.T, parser *Parser) *ChunkedDataSender {
	return &ChunkedDataSender{
		parser: parser,
		t:      t,
	}
}

// SendInChunks sends data in specified chunk sizes with optional delay
func (c *ChunkedDataSender) SendInChunks(phase Phase, data []byte, chunkSize int, delay time.Duration) error {
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}

		chunk := data[i:end]
		if err := c.parser.ProcessEvent(phase, chunk); err != nil {
			return err
		}

		if delay > 0 && end < len(data) {
			time.Sleep(delay)
		}
	}
	return nil
}

// SendByLines sends data line by line with optional delay
func (c *ChunkedDataSender) SendByLines(phase Phase, data []byte, delay time.Duration) error {
	lines := bytes.Split(data, []byte("\r\n"))
	for i, line := range lines {
		// Add back the CRLF except for the last empty line
		if i < len(lines)-1 {
			line = append(line, []byte("\r\n")...)
		}

		if len(line) > 0 {
			if err := c.parser.ProcessEvent(phase, line); err != nil {
				return err
			}

			if delay > 0 && i < len(lines)-1 {
				time.Sleep(delay)
			}
		}
	}
	return nil
}

// EventCollector helps collect and analyze events
type EventCollector struct {
	recorder *TestCallbackRecorder
	events   []Event
	t        *testing.T
}

// NewEventCollector creates a new event collector
func NewEventCollector(t *testing.T, recorder *TestCallbackRecorder) *EventCollector {
	return &EventCollector{
		recorder: recorder,
		events:   make([]Event, 0),
		t:        t,
	}
}

// CollectFor collects events for a specified duration
func (c *EventCollector) CollectFor(duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	for {
		select {
		case event := <-c.recorder.events:
			c.events = append(c.events, event)
		case <-timer.C:
			return
		}
	}
}

// WaitForEventType waits for a specific event type and returns all collected events
func (c *EventCollector) WaitForEventType(eventType EventType, timeout time.Duration) ([]Event, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case event := <-c.recorder.events:
			c.events = append(c.events, event)
			if event.Type == eventType {
				return c.events, true
			}
		case <-timer.C:
			return c.events, false
		}
	}
}

// GetEventsByType returns all events of a specific type
func (c *EventCollector) GetEventsByType(eventType EventType) []Event {
	var filtered []Event
	for _, e := range c.events {
		if e.Type == eventType {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// AssertEventSequence verifies events occurred in expected order
func (c *EventCollector) AssertEventSequence(expected []EventType) {
	c.t.Helper()

	if len(c.events) < len(expected) {
		c.t.Errorf("Expected %d events, got %d", len(expected), len(c.events))
		return
	}

	for i, expectedType := range expected {
		if i >= len(c.events) {
			c.t.Errorf("Missing event at position %d: expected %v", i, expectedType)
			continue
		}
		if c.events[i].Type != expectedType {
			c.t.Errorf("Event mismatch at position %d: expected %v, got %v",
				i, expectedType, c.events[i].Type)
		}
	}
}
