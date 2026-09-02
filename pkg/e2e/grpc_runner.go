package e2e

import (
	"net"
	"testing"

	"go.uber.org/zap"
)

// GRPCTestSuiteRunner runs a gRPC test suite
type GRPCTestSuiteRunner struct {
	Suite  *GRPCTestSuite
	Logger *zap.Logger
}

// Run executes all tests in the gRPC suite
func (r *GRPCTestSuiteRunner) Run(t *testing.T, ctx *Context) {
	t.Run(r.Suite.name, func(t *testing.T) {
		r.run(t, ctx)
	})
}

func (r *GRPCTestSuiteRunner) run(t *testing.T, ctx *Context) {
	r.Logger.Info("Running gRPC test suite", zap.String("suite", r.Suite.name))
	r.Logger.Debug("Total test cases", zap.Int("count", len(r.Suite.testCases)), zap.Int("skipped", len(r.Suite.skipped)))

	r.Logger = ctx.L

	// Group tests by TLS to reuse gRPC servers
	testsByTLS := r.groupTestsByTLS()

	for useTLS, tests := range testsByTLS {
		machineIP := ctx.MachineIP().String()
		var serverAddr string

		if useTLS {
			server, lis, err := NewGRPCTLSServer(machineIP)
			if err != nil {
				t.Fatalf("failed to create gRPC TLS server: %v", err)
			}
			go server.Serve(lis) //nolint:errcheck
			defer server.Stop()
			serverAddr = lis.Addr().String()
		} else {
			server, lis, err := NewGRPCPlainServer(machineIP)
			if err != nil {
				t.Fatalf("failed to create gRPC plain server: %v", err)
			}
			go server.Serve(lis) //nolint:errcheck
			defer server.Stop()
			serverAddr = lis.Addr().String()
		}

		for _, tc := range tests {
			t.Run(tc.Name, func(t *testing.T) {
				tctx := ctx.TestCtx(t)
				if tc.Request != nil {
					tc.Request.Server = serverAddr
					tc.Request.WithExtraEnvVar("QPOINT_TAGS", "ctxid:"+tctx.ID)
				}
				tctx.WithConfig(t, tc.ConfigMutator, func(t *testing.T) {
					r.runSingleTest(t, tctx, tc)
				})
			})
		}
	}
}

func (r *GRPCTestSuiteRunner) groupTestsByTLS() map[bool][]GRPCTestCase {
	groups := make(map[bool][]GRPCTestCase)
	for _, tc := range r.Suite.testCases {
		groups[tc.Request.TLS] = append(groups[tc.Request.TLS], tc)
	}
	return groups
}

func (r *GRPCTestSuiteRunner) runSingleTest(t *testing.T, tctx *TestContext, tc GRPCTestCase) {
	ctx := t.Context()

	// Verify server address is set
	host, port, err := net.SplitHostPort(tc.Request.Server)
	if err != nil {
		t.Fatalf("invalid server address %q: %v", tc.Request.Server, err)
	}
	r.Logger.Debug("gRPC test target", zap.String("host", host), zap.String("port", port))

	container := tc.Request.Run(ctx, r.Logger)
	if container == nil {
		t.Fatal("failed to start gRPC container")
		return
	}

	// Readiness handshake: wait for process PID, attach probes, then signal ready
	if tc.Request.ReadinessFile != "" {
		containerID := container.GetContainerID()
		if len(containerID) > 12 {
			containerID = containerID[:12]
		}

		r.Logger.Debug("waiting for process information", zap.String("container_id", containerID))
		var pid int
		select {
		case pid = <-container.processPID:
		case <-ctx.Done():
			t.Errorf("%v", ctx.Err())
			return
		}

		r.Logger.Debug("waiting for TLS attachment on process and container", zap.Int("pid", pid), zap.String("container_id", containerID))
		if err := tctx.WaitForProcess(NewProcessKey(containerID, pid), tc.Request.ReadinessTimeout); err != nil {
			r.Logger.Warn("failed to wait for process", zap.Error(err))
		} else {
			r.Logger.Debug("creating readiness signal in container")
			if err := container.CopyToContainer(ctx, []byte("Q"), tc.Request.ReadinessFile+".ready", 0644); err != nil {
				r.Logger.Warn("failed to create file in container", zap.Error(err))
			}
		}
	}

	containerResult := <-container.resultCh

	r.Logger.Debug("gRPC container result",
		zap.Int("exit_code", containerResult.ExitCode),
		zap.String("logs", containerResult.Combined()))

	if containerResult.ExitCode != 0 {
		t.Errorf("Container exited with code %d: %v\nOutput: %s",
			containerResult.ExitCode, containerResult.Error, containerResult.Combined())
	}

	validationCtx := ValidationContext{
		TestContext:  tctx,
		GRPCTestCase: &tc,
		Container:    &containerResult,
	}

	for _, validation := range tc.Validations {
		if err := validation(t, validationCtx); err != nil {
			t.Errorf("Validation failed: %v", err)
		}
	}
}
