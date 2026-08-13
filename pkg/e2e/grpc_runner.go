package e2e

import (
	"context"
	"net"
	"testing"
	"time"

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
	r.Logger = ctx.L
	if r.Logger == nil {
		r.Logger = zap.NewNop()
	}
	r.Logger.Info("Running gRPC test suite", zap.String("suite", r.Suite.name))
	r.Logger.Debug("Total test cases", zap.Int("count", len(r.Suite.testCases)), zap.Int("skipped", len(r.Suite.skipped)))

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
	if r.Logger == nil {
		r.Logger = zap.NewNop()
	}
	if tc.Request == nil {
		t.Fatal("gRPC test has no request")
	}

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
	if container.Container != nil {
		defer func() {
			terminateCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			if err := container.Terminate(terminateCtx); err != nil {
				r.Logger.Warn("failed to terminate gRPC container", zap.Error(err))
			}
		}()
	}

	// Readiness handshake: wait for process PID, attach probes, then signal ready
	if tc.Request.ReadinessFile != "" {
		if container.Container == nil {
			result, err := container.WaitForExit(ctx)
			if err != nil {
				t.Fatalf("waiting for gRPC container startup result: %v", err)
			}
			t.Fatalf("gRPC container startup failed: exit code %d: %v", result.ExitCode, result.Error)
		}
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
			t.Errorf("gRPC container exited before publishing process information: exit code %d: %v", result.ExitCode, result.Error)
			return
		case <-readinessCtx.Done():
			t.Errorf("waiting for gRPC process information: %v", readinessCtx.Err())
			return
		}

		r.Logger.Debug("waiting for TLS attachment on process and container", zap.Int("pid", pid), zap.String("container_id", containerID))
		attachResult := make(chan error, 1)
		go func() {
			attachResult <- tctx.WaitForProcess(NewProcessKey(containerID, pid), tc.Request.readinessTimeout())
		}()
		select {
		case err := <-attachResult:
			if err != nil {
				t.Errorf("waiting for gRPC TLS attachment: %v", err)
				return
			}
		case result := <-container.resultCh:
			t.Errorf("gRPC container exited before TLS attachment: exit code %d: %v", result.ExitCode, result.Error)
			return
		case <-readinessCtx.Done():
			t.Errorf("waiting for gRPC TLS attachment: %v", readinessCtx.Err())
			return
		}

		r.Logger.Debug("creating readiness signal in container")
		if err := container.Container.CopyToContainer(readinessCtx, []byte("Q"), tc.Request.ReadinessFile+".ready", 0644); err != nil {
			t.Errorf("creating gRPC readiness signal in container: %v", err)
			return
		}
	}

	resultCtx, cancel := context.WithTimeout(ctx, tc.Request.lifecycleTimeout())
	defer cancel()
	containerResult, err := container.WaitForExit(resultCtx)
	if err != nil {
		t.Errorf("waiting for gRPC container result: %v", err)
		return
	}

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
		Container:    containerResult,
	}

	for _, validation := range tc.Validations {
		if err := validation(t, validationCtx); err != nil {
			t.Errorf("Validation failed: %v", err)
		}
	}
}
