package e2e

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

// TestSuiteRunner runs a test suite
type TestSuiteRunner struct {
	Context *Context
	Suite   *TestSuite
	Logger  *zap.Logger
}

// Run executes all tests in the suite
func (r *TestSuiteRunner) Run(t *testing.T, ctx *Context) {
	t.Run(r.Suite.name, func(t *testing.T) {
		r.run(t, ctx)
	})
}

// run executes all tests in the suite
func (r *TestSuiteRunner) run(t *testing.T, ctx *Context) {
	t.Logf("Running test suite: %s", r.Suite.name)
	t.Logf("Total test cases: %d", len(r.Suite.testCases))

	if len(r.Suite.skipped) > 0 {
		for _, skip := range r.Suite.skipped {
			t.Run(skip.name, func(t *testing.T) {
				t.Skip(skip.reason)
			})
		}
	}

	r.Logger = ctx.L

	// Group tests by HTTP protocol to reuse servers
	testsByHTTPProto := r.groupTestsByHTTPProto()

	for httpProto, tests := range testsByHTTPProto {
		// Create appropriate server for this HTTP protocol
		server := r.createTestServer(t, httpProto, ctx.MachineIP().String(), tests[0])
		defer server.Close()

		// Run all tests for this HTTP protocol
		for _, tc := range tests {
			t.Run(tc.Name, func(t *testing.T) {
				// This sets up the test context for Qtap, eg. specific configs
				tctx := ctx.TestCtx(t)
				if tc.Request != nil {
					tc.Request.WithExtraEnvVar("QPOINT_TAGS", "ctxid:"+tctx.ID)
				}
				tctx.WithConfig(t, tc.ConfigMutator, func(t *testing.T) {
					r.runSingleTest(t, tctx, tc, server)
				})
			})
		}
	}
}

// groupTestsByHTTPProto groups tests by HTTP protocol to use the same http server
// for all the tests that require it. This is a bit unecessary and we could spin all
// of the http servers up at once and leave them up.
//
// TODO(Jon): Spin up all of of the require servers at once and ignore this. Then we
// can add the ability to adding a sql-like GroupBy() function onto the suite so that
// we can indicate how we want tests grouped to highlight the functionality that is
// being tested.
func (r *TestSuiteRunner) groupTestsByHTTPProto() map[HTTPProtocol][]TestCase {
	groups := make(map[HTTPProtocol][]TestCase)
	for _, tc := range r.Suite.testCases {
		groups[tc.Request.Proto] = append(groups[tc.Request.Proto], tc)
	}
	return groups
}

func (r *TestSuiteRunner) createTestServer(t *testing.T, httpProto HTTPProtocol, machineIP string,
	sampleTest TestCase) *httptest.Server {
	// Create request expectations - strict validation
	expectations := RequestExpectations{
		Method:  sampleTest.Request.Method,
		Proto:   httpProto,
		Headers: sampleTest.Request.Headers,
		Body:    sampleTest.Request.Body,
	}

	// TODO(Jon): Implement a request expectation mutator check here
	// so that if we know a specific language/client/etc is going to change
	// we can ensure that the expected request is updated accordingly.

	validator := NewRequestValidator(t, expectations)

	// Base handler
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success"}`))
	})

	// Wrap with validation
	handler := validator.Middleware(baseHandler)

	// Create server based on HTTP protocol and TLS requirement
	useTLS := sampleTest.Request.TLS
	server, err := NewHTTPServer(machineIP, handler, httpProto, useTLS)

	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	return server
}

func (r *TestSuiteRunner) runSingleTest(t *testing.T, tctx *TestContext, tc TestCase, server *httptest.Server) {
	ctx := t.Context()

	// Update request URL to point to test server
	tc.Request.URL = server.URL + tc.Request.URL

	// Run the container
	container := tc.Request.Run(ctx, r.Logger)

	// Wait for any async operations
	// time.Sleep(100 * time.Millisecond)

	var containerResult ContainerResult = <-container.resultCh

	// Create validation context
	validationCtx := ValidationContext{
		TestContext: tctx,
		TestCase:    &tc,
		Container:   &containerResult,
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
