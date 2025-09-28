package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

// TestSuiteRunner runs a test suite
type TestSuiteRunner struct {
	Suite     *TestSuite
	MachineIP string
	Logger    *zap.Logger
	// Add your agent capture here
	// Agent *AgentCapture
	// TODO(Jon): this is where we need to add the Context to create the test context which
	// starts the qtap service and mocks the event store. The AgentCapture above is just a
	// placeholder for the event store mock.
}

// Run executes all tests in the suite
func (r *TestSuiteRunner) Run(t *testing.T) {
	t.Logf("Running test suite: %s", r.Suite.name)
	t.Logf("Total test cases: %d", len(r.Suite.testCases))

	if len(r.Suite.skipped) > 0 {
		t.Logf("Skipped combinations:")
		for _, skip := range r.Suite.skipped {
			t.Logf("  - %s", skip)
		}
	}

	// Group tests by HTTP version to reuse servers
	testsByHTTPVersion := r.groupTestsByHTTPVersion()

	for httpVersion, tests := range testsByHTTPVersion {
		t.Run("HTTP/"+httpVersion, func(t *testing.T) {
			// Create appropriate server for this HTTP version
			server, validator := r.createTestServer(t, httpVersion, tests[0])
			defer server.Close()

			// Run all tests for this HTTP version
			for _, tc := range tests {
				t.Run(tc.Name, func(t *testing.T) {
					r.runSingleTest(t, tc, server, validator)
				})
			}
		})
	}
}

func (r *TestSuiteRunner) groupTestsByHTTPVersion() map[string][]TestCase {
	groups := make(map[string][]TestCase)
	for _, tc := range r.Suite.testCases {
		groups[tc.Request.HTTPVersion] = append(groups[tc.Request.HTTPVersion], tc)
	}
	return groups
}

func (r *TestSuiteRunner) createTestServer(t *testing.T, httpVersion string,
	sampleTest TestCase) (*httptest.Server, *RequestValidator) {

	// Create request expectations - strict validation
	expectations := RequestExpectations{
		Method:      sampleTest.Request.Method,
		HTTPVersion: httpVersion,
		Headers:     sampleTest.Request.Headers,
		Body:        sampleTest.Request.Body,
	}

	validator := NewRequestValidator(t, expectations)

	// Base handler
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	})

	// Wrap with validation
	handler := validator.Middleware(baseHandler)

	// Create server based on HTTP version
	var server *httptest.Server
	var err error

	switch httpVersion {
	case "2":
		server, err = NewHTTP2OnlyTestServer(r.MachineIP, handler)
	case "1.1":
		server, err = NewHTTP11OnlyTestServer(r.MachineIP, handler)
	case "1.0":
		server, err = NewPlainHTTP11TestServer(r.MachineIP, handler)
	default:
		t.Fatalf("unsupported HTTP version: %s", httpVersion)
	}

	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	return server, validator
}

func (r *TestSuiteRunner) runSingleTest(t *testing.T, tc TestCase,
	server *httptest.Server, validator *RequestValidator) {

	ctx := context.Background()

	// Update request URL to point to test server
	tc.Request.URL = server.URL + tc.Request.URL

	// Start agent capture if available
	// r.Agent.StartCapture(tc.Name)

	// Run the container
	container := tc.Request.Run(ctx, r.Logger)

	// Wait for any async operations
	// time.Sleep(100 * time.Millisecond)

	var containerResult ContainerResult
	// TODO(Jon): wrap this in a timer
	containerResult = <-container.resultCh

	// Get captured data
	capturedRequests := validator.GetCaptured()

	// Get agent events if available
	// var agentEvents []AgentEvent
	// agentEvents = r.Agent.GetEvents(tc.Name)

	// Create validation context
	validationCtx := ValidationContext{
		// TestCase:    &tc,
		Container:  &containerResult,
		ServerReqs: capturedRequests,
		// AgentEvents: agentEvents,
	}

	// Run all validations
	for _, validation := range tc.Validations {
		if err := validation(t, validationCtx); err != nil {
			t.Errorf("Validation failed: %v", err)
		}
	}

	// Check container exit code
	if containerResult.ExitCode != 0 {
		t.Errorf("Container exited with code %d: %v",
			containerResult.ExitCode, containerResult.Error)
	}
}
