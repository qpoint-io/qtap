package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	r.Logger = ctx.L
	if r.Logger == nil {
		r.Logger = zap.NewNop()
	}
	r.Logger.Info("Running test suite", zap.String("suite", r.Suite.name))
	r.Logger.Debug("Total test cases", zap.Int("count", len(r.Suite.testCases)), zap.Int("skipped", len(r.Suite.skipped)))

	// Group tests by HTTP protocol to reuse servers
	testsByHTTPProto := r.groupTestsByHTTPProto()

	for serverConfig, tests := range testsByHTTPProto {
		// Create appropriate server for this HTTP protocol
		server := r.createTestServer(t, serverConfig.protocol, ctx.MachineIP().String(), r.Suite.handler, tests[0])
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

// groupTestsByHTTPProto groups tests by HTTP protocol and TLS mode to use the same
// HTTP server for all the tests that require it. We could instead spin all
// of the http servers up at once and leave them up.
//
// TODO(Jon): Spin up all of of the require servers at once and ignore this. Then we
// can add the ability to adding a sql-like GroupBy() function onto the suite so that
// we can indicate how we want tests grouped to highlight the functionality that is
// being tested.
type httpServerConfig struct {
	protocol HTTPProtocol
	tls      bool
}

func (r *TestSuiteRunner) groupTestsByHTTPProto() map[httpServerConfig][]TestCase {
	groups := make(map[httpServerConfig][]TestCase)
	for _, tc := range r.Suite.testCases {
		if tc.Request == nil {
			continue
		}
		config := httpServerConfig{protocol: tc.Request.Proto, tls: tc.Request.TLS}
		groups[config] = append(groups[config], tc)
	}
	return groups
}

func (r *TestSuiteRunner) createTestServer(t *testing.T, httpProto HTTPProtocol, machineIP string,
	suiteHandler http.Handler, sampleTest TestCase) *httptest.Server {
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

	// Use custom handler if provided, otherwise use default
	var baseHandler http.Handler
	if suiteHandler != nil {
		baseHandler = suiteHandler
	} else {
		// Default handler
		baseHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		})
	}

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
	if r.Logger == nil {
		r.Logger = zap.NewNop()
	}
	if tc.Request == nil {
		t.Error("HTTP test has no request")
		return
	}
	if server == nil {
		t.Error("HTTP test has no server")
		return
	}

	// Update request URL to point to test server
	tc.Request.URL = server.URL + tc.Request.URL

	// Run the container
	container := tc.Request.Run(ctx, r.Logger)
	if container == nil {
		t.Error("HTTP request returned no container")
		return
	}
	if container.Container != nil {
		defer func() {
			terminateCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			if err := container.Terminate(terminateCtx); err != nil {
				r.Logger.Warn("failed to terminate container", zap.Error(err))
			}
		}()
	}

	// Wait for any async operations
	// time.Sleep(100 * time.Millisecond)

	if tc.Request.ReadinessFile != "" && container.Container != nil {
		containerID := container.GetContainerID()
		if len(containerID) > 12 {
			containerID = containerID[:12]
		}

		r.Logger.Debug("waiting for process information", zap.String("container_id", containerID))
		readinessCtx, cancel := context.WithTimeout(ctx, tc.Request.readinessTimeout())
		defer cancel()
		var pid int
		select {
		case pid = <-container.processPID:
		case result := <-container.resultCh:
			t.Errorf("container exited before publishing process information: exit code %d: %v", result.ExitCode, result.Error)
			return
		case <-readinessCtx.Done():
			t.Errorf("waiting for process information: %v", readinessCtx.Err())
			return
		}

		r.Logger.Debug("waiting for TLS attachment on process and container", zap.Int("pid", pid), zap.String("container_id", containerID))
		if tctx == nil {
			t.Error("HTTP readiness requires a test context")
			return
		}
		attachResult := make(chan error, 1)
		go func() {
			attachResult <- tctx.WaitForProcess(NewProcessKey(containerID, pid), tc.Request.readinessTimeout())
		}()
		select {
		case err := <-attachResult:
			if err != nil {
				t.Errorf("waiting for TLS attachment: %v", err)
				return
			}
		case result := <-container.resultCh:
			t.Errorf("container exited before TLS attachment: exit code %d: %v", result.ExitCode, result.Error)
			return
		case <-readinessCtx.Done():
			t.Errorf("waiting for TLS attachment: %v", readinessCtx.Err())
			return
		}

		r.Logger.Debug("🆕 creating readiness signal in container")
		if err := container.Container.CopyToContainer(readinessCtx, []byte("Q"), tc.Request.ReadinessFile+".ready", 0644); err != nil {
			t.Errorf("creating readiness signal in container: %v", err)
			return
		}
	}

	resultCtx, cancel := context.WithTimeout(ctx, tc.Request.lifecycleTimeout())
	defer cancel()
	containerResult, err := container.WaitForExit(resultCtx)
	if err != nil {
		t.Errorf("waiting for container result: %v", err)
		return
	}

	r.Logger.Debug("⭕ Container result", zap.String("container_logs", containerResult.Combined()))

	// Check container exit code
	if containerResult.ExitCode != 0 {
		t.Errorf("Container exited with code %d: %v",
			containerResult.ExitCode, containerResult.Error)
	}

	// Create validation context
	validationCtx := ValidationContext{
		TestContext: tctx,
		TestCase:    &tc,
		Container:   containerResult,
	}

	// Run all validations
	for _, validation := range tc.Validations {
		if err := validation(t, validationCtx); err != nil {
			t.Errorf("Validation failed: %v", err)
		}
	}
}
