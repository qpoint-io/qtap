//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	e2epkg "github.com/qpoint-io/qtap/pkg/e2e"
	"github.com/qpoint-io/qtap/pkg/plugins/httpcapture"
	"github.com/qpoint-io/qtap/pkg/plugins/report"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
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

func TestGRPC_Capture(t *testing.T) {
	ctx := e2ectx.TestCtx(t)

	// Start in-process gRPC server with the standard health check service
	lis, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	port := lis.Addr().(*net.TCPAddr).Port

	grpcServer := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, health.NewServer())
	reflection.Register(grpcServer)
	go grpcServer.Serve(lis) //nolint:errcheck
	defer grpcServer.Stop()

	ctx.WithConfig(t, func(c *config.Config) {
		c.Tap.IgnoreLoopback = false
		c.Tap.Direction = config.TrafficDirection_EGRESS

		var pluginConfYaml yaml.Node
		err := pluginConfYaml.Encode(&httpcapture.HttpCaptureConfig{
			Level:  httpcapture.CaptureLevelFull,
			Format: httpcapture.OutputFormatJSON,
		})
		require.NoError(t, err)

		c.Stacks[c.Tap.Http.Stack] = config.Stack{
			Plugins: []config.Plugin{
				{
					Type:   string(httpcapture.PluginTypeHttpCapture),
					Config: pluginConfYaml,
				},
				{
					Type: string(report.PluginTypeReport),
				},
			},
		}
	}, func(t *testing.T) {
		target := fmt.Sprintf("%s:%d", ctx.MachineIP().String(), port)
		ctxID := e2epkg.NewID()

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

		// Wait for gRPC transaction artifact
		var artifact *eventstore.Artifact
		require.Eventually(t, func() bool {
			for _, a := range e2ectx.EventStore.GetByCtxID(ctxID).Artifacts {
				if a.Type == eventstore.ArtifactType_GRPCTransaction {
					artifact = a
					return true
				}
			}
			return false
		}, 10*time.Second, 100*time.Millisecond, "no gRPC transaction artifact found")

		assert.Equal(t, eventstore.ArtifactType_GRPCTransaction, artifact.Type)

		var transaction httpcapture.GrpcTransaction
		err = json.Unmarshal(artifact.Data, &transaction)
		require.NoError(t, err)

		assert.Equal(t, "grpc.health.v1.Health", transaction.Service)
		assert.Equal(t, "Check", transaction.Method)
		assert.Equal(t, "0", transaction.GrpcStatus)
		assert.Equal(t, "OK", transaction.GrpcStatusName)

		// Validate that the payload was captured (plaintext gRPC should have body data)
		require.NotEmpty(t, transaction.Request.Body, "request body should be captured for plaintext gRPC at full capture level")
		require.NotEmpty(t, transaction.Response.Body, "response body should be captured for plaintext gRPC at full capture level")

		// Validate request/response headers were captured
		assert.NotEmpty(t, transaction.Request.Headers, "request headers should be captured at full capture level")
		assert.NotEmpty(t, transaction.Response.Headers, "response headers should be captured at full capture level")
		assert.Equal(t, "application/grpc", transaction.Request.ContentType)

		// Validate request body is a valid gRPC-framed HealthCheckRequest.
		// gRPC frames: 1 byte compressed flag + 4 bytes message length + protobuf payload.
		reqBody := transaction.Request.Body
		require.GreaterOrEqual(t, len(reqBody), 5, "request body too short for gRPC frame header")
		assert.Equal(t, byte(0), reqBody[0], "request compressed flag should be 0 (uncompressed)")
		var healthReq grpc_health_v1.HealthCheckRequest
		reqPayload := reqBody[5:] // skip 5-byte gRPC frame header
		err = proto.Unmarshal(reqPayload, &healthReq)
		require.NoError(t, err, "request body should unmarshal as HealthCheckRequest")
		// HealthCheckRequest with no service field means "overall health"
		assert.Empty(t, healthReq.Service, "health check request service should be empty (overall health)")

		// Validate response body is a valid gRPC-framed HealthCheckResponse with SERVING status.
		resBody := transaction.Response.Body
		require.GreaterOrEqual(t, len(resBody), 5, "response body too short for gRPC frame header")
		assert.Equal(t, byte(0), resBody[0], "response compressed flag should be 0 (uncompressed)")
		var healthResp grpc_health_v1.HealthCheckResponse
		resPayload := resBody[5:] // skip 5-byte gRPC frame header
		err = proto.Unmarshal(resPayload, &healthResp)
		require.NoError(t, err, "response body should unmarshal as HealthCheckResponse")
		assert.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, healthResp.Status, "health check response should be SERVING")
	})
}
