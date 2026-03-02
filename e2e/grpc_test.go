//go:build e2e

package e2e

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	e2epkg "github.com/qpoint-io/qtap/pkg/e2e"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

func TestGRPCLanguages(t *testing.T) {
	configMut := func(c *config.Config) {
		c.Tap.IgnoreLoopback = false
		c.Tap.Direction = config.TrafficDirection_EGRESS
	}

	suite, err := e2epkg.NewGRPCTestSuite("gRPC Echo").
		WithConfig(configMut).
		WithOS("alpine").
		WithLanguage(e2epkg.Go, "1.25.1").
		// WithLanguage(e2epkg.Java, "21").
		WithLanguage(e2epkg.Python, "3.12.0").
		WithLanguage(e2epkg.NodeJS, "22.16.0").
		WithLanguage(e2epkg.Ruby, "3.4.5").
		// WithLanguage(e2epkg.PHP, "8.3").
		WithMessage(`{"message":"hello"}`).
		WithPlaintextOnly().
		WithValidation(func(t *testing.T, ctx e2epkg.ValidationContext) error {
			tc := ctx.GRPCTestCase

			// Wait for gRPC request to appear in the event store.
			// The babel images use the proto service babel.v1.EchoService
			// with method UnaryEcho on the wire.
			var grpcReq *eventstore.GrpcRequest
			require.Eventually(t, func() bool {
				events := e2ectx.EventStore.GetByCtxID(ctx.TestContext.ID)
				for _, r := range events.GrpcRequests {
					if r.GrpcService == "babel.v1.EchoService" && r.GrpcMethod == "UnaryEcho" {
						grpcReq = r
						return true
					}
				}
				return false
			}, 10*time.Second, 100*time.Millisecond,
				"no gRPC babel.v1.EchoService/UnaryEcho request for %s:%s", tc.Language, tc.Version)

			// Verify gRPC metadata
			assert.Equal(t, "babel.v1.EchoService", grpcReq.GrpcService)
			assert.Equal(t, "UnaryEcho", grpcReq.GrpcMethod)
			assert.Equal(t, "0", grpcReq.GrpcStatus)
			assert.Equal(t, "OK", grpcReq.GrpcStatusName)
			assert.Equal(t, http.StatusOK, grpcReq.Status)

			// Verify qtap observed content (bytes flowing through the wire)
			assert.Equal(t, "/babel.v1.EchoService/UnaryEcho", grpcReq.URLPath)
			assert.Equal(t, "application/grpc", grpcReq.ContentType)
			assert.Greater(t, grpcReq.WrBytes, int64(0), "expected client to send request bytes")
			assert.Greater(t, grpcReq.RdBytes, int64(0), "expected client to receive response bytes")
			assert.Greater(t, grpcReq.Duration, int64(0), "expected positive request duration")

			return nil
		}).
		Build()

	require.NoError(t, err)
	require.NotNil(t, suite)

	runner := &e2epkg.GRPCTestSuiteRunner{
		Suite:  suite,
		Logger: e2ectx.L,
	}
	runner.Run(t, e2ectx)
}

func TestGRPCLocal(t *testing.T) {
	ctx := e2ectx.TestCtx(t)

	// Start in-process gRPC server with the standard health check service
	lis, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	port := lis.Addr().(*net.TCPAddr).Port

	grpcServer := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, health.NewServer())
	reflection.Register(grpcServer) // needed so grpcurl can resolve the service
	go grpcServer.Serve(lis)        //nolint:errcheck
	defer grpcServer.Stop()

	ctx.WithConfig(t, func(c *config.Config) {
		c.Tap.IgnoreLoopback = false
		c.Tap.Direction = config.TrafficDirection_EGRESS
	}, func(t *testing.T) {
		target := fmt.Sprintf("%s:%d", ctx.MachineIP().String(), port)
		ctxID := e2epkg.NewID()

		// Run grpcurl container as gRPC client
		container, err := testcontainers.Run(
			ctx,
			"fullstorydev/grpcurl:latest",
			testcontainers.WithCmd("-plaintext", target, "grpc.health.v1.Health/Check"),
			testcontainers.WithEnv(map[string]string{
				"QPOINT_TAGS": "ctxid:" + ctxID,
			}),
			testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(10*time.Second)),
		)
		t.Cleanup(func() { testcontainers.TerminateContainer(container) })
		require.NoError(t, err)

		// grpcurl probes gRPC reflection before the real call, so multiple
		// connections to port will appear. Wait for one with L7Protocol_GRPC.
		var grpcConn *eventstore.Connection
		require.Eventually(t, func() bool {
			for _, c := range e2ectx.EventStore.GetByCtxID(ctxID).Connections {
				if c.L7Protocol != eventstore.L7Protocol_GRPC {
					continue
				}
				if dst, ok := c.Destination.(*eventstore.ConnectionEndpointRemote); ok {
					if int(dst.Address.Port) == port {
						grpcConn = c
						return true
					}
				}
			}
			return false
		}, 10*time.Second, 100*time.Millisecond, "no gRPC connection to server port %d", port)
		assert.Equal(t, eventstore.L7Protocol_GRPC, grpcConn.L7Protocol)

		// The Health/Check request may arrive after the reflection connection
		// closes, so poll until it appears in GrpcRequests.
		var grpcReq *eventstore.GrpcRequest
		require.Eventually(t, func() bool {
			for _, r := range e2ectx.EventStore.GetByCtxID(ctxID).GrpcRequests {
				if r.URLPath == "/grpc.health.v1.Health/Check" {
					grpcReq = r
					return true
				}
			}
			return false
		}, 10*time.Second, 100*time.Millisecond, "no gRPC Health/Check request in GrpcRequests")

		assert.Equal(t, "/grpc.health.v1.Health/Check", grpcReq.URLPath)
		assert.Equal(t, http.StatusOK, grpcReq.Status)
		assert.Equal(t, "grpc.health.v1.Health", grpcReq.GrpcService)
		assert.Equal(t, "Check", grpcReq.GrpcMethod)
		assert.Equal(t, "0", grpcReq.GrpcStatus)
		assert.Equal(t, "OK", grpcReq.GrpcStatusName)
	})
}
