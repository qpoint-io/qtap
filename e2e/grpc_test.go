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

func TestGRPC(t *testing.T) {
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
		// closes, so poll until it appears.
		var grpcReq *eventstore.Request
		require.Eventually(t, func() bool {
			for _, r := range e2ectx.EventStore.GetByCtxID(ctxID).Requests {
				if r.URLPath == "/grpc.health.v1.Health/Check" && r.Status == http.StatusOK {
					grpcReq = r
					return true
				}
			}
			return false
		}, 10*time.Second, 100*time.Millisecond, "no gRPC Health/Check request with status 200")
		assert.Equal(t, "/grpc.health.v1.Health/Check", grpcReq.URLPath)
		assert.Equal(t, http.StatusOK, grpcReq.Status)
	})
}
