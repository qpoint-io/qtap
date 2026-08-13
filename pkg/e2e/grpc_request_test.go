package e2e

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"go.uber.org/zap"
)

func TestGRPCRequestRunTerminatesPartialCreation(t *testing.T) {
	fake := &grpcRequestContainer{terminated: make(chan struct{})}
	original := genericContainer
	genericContainer = func(context.Context, testcontainers.GenericContainerRequest) (testcontainers.Container, error) {
		return fake, errors.New("partial create failed")
	}
	t.Cleanup(func() { genericContainer = original })

	container := (&GRPCRequest{ImageURL: "test-image"}).Run(t.Context(), zap.NewNop())
	require.NotNil(t, container)

	result, err := container.WaitForExit(t.Context())
	require.NoError(t, err)
	require.Equal(t, -1, result.ExitCode)
	require.ErrorContains(t, result.Error, "partial create failed")
	select {
	case <-fake.terminated:
	case <-time.After(time.Second):
		t.Fatal("partially created container was not terminated")
	}
	select {
	case <-container.resultCh:
		t.Fatal("published more than one result")
	default:
	}
}

func TestGRPCRequestRunPublishesReadinessTimeout(t *testing.T) {
	fake := &grpcRequestContainer{terminated: make(chan struct{})}
	original := genericContainer
	genericContainer = func(context.Context, testcontainers.GenericContainerRequest) (testcontainers.Container, error) {
		return fake, nil
	}
	t.Cleanup(func() { genericContainer = original })

	container := (&GRPCRequest{
		ImageURL:         "test-image",
		ReadinessFile:    "/tmp/ready",
		ReadinessTimeout: 20 * time.Millisecond,
	}).Run(t.Context(), zap.NewNop())
	waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	result, err := container.WaitForExit(waitCtx)
	require.NoError(t, err)
	require.Equal(t, -1, result.ExitCode)
	require.ErrorContains(t, result.Error, "readiness handshake")
	select {
	case pid := <-container.processPID:
		t.Fatalf("published unexpected PID %d", pid)
	default:
	}
	select {
	case <-container.resultCh:
		t.Fatal("published more than one result")
	default:
	}
}

type grpcRequestContainer struct {
	testcontainers.Container
	terminated chan struct{}
}

func (c *grpcRequestContainer) Start(context.Context) error { return nil }

func (c *grpcRequestContainer) State(context.Context) (*dockercontainer.State, error) {
	return &dockercontainer.State{Running: false, ExitCode: 0}, nil
}

func (c *grpcRequestContainer) Terminate(context.Context, ...testcontainers.TerminateOption) error {
	close(c.terminated)
	return nil
}

func (c *grpcRequestContainer) CopyFileFromContainer(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not ready")
}
