package e2e

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"go.uber.org/zap"
)

func TestHTTPRequestRunPublishesCreationFailure(t *testing.T) {
	original := genericContainer
	genericContainer = func(context.Context, testcontainers.GenericContainerRequest) (testcontainers.Container, error) {
		return nil, errors.New("create failed")
	}
	t.Cleanup(func() { genericContainer = original })

	container := (&HTTPRequest{ImageURL: "test-image"}).Run(t.Context(), zap.NewNop())
	require.NotNil(t, container)

	waitCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result, err := container.WaitForExit(waitCtx)
	require.NoError(t, err)
	require.Equal(t, -1, result.ExitCode)
	require.ErrorContains(t, result.Error, "create failed")

	select {
	case <-container.resultCh:
		t.Fatal("published more than one result")
	default:
	}
}

func TestHTTPRequestWithoutReadinessHasLifecycleTimeout(t *testing.T) {
	request := HTTPRequest{
		Timeout:              2 * time.Second,
		Requests:             5,
		ConcurrentRequests:   2,
		DelayBetweenRequests: time.Second,
	}

	require.GreaterOrEqual(t, request.lifecycleTimeout(), 40*time.Second)
}

func TestHTTPRequestRunTerminatesPartialCreation(t *testing.T) {
	fake := &completedContainer{terminated: make(chan struct{})}
	original := genericContainer
	genericContainer = func(context.Context, testcontainers.GenericContainerRequest) (testcontainers.Container, error) {
		return fake, errors.New("partial create failed")
	}
	t.Cleanup(func() { genericContainer = original })

	container := (&HTTPRequest{ImageURL: "test-image"}).Run(t.Context(), zap.NewNop())
	result, err := container.WaitForExit(t.Context())
	require.NoError(t, err)
	require.ErrorContains(t, result.Error, "partial create failed")
	select {
	case <-fake.terminated:
	case <-time.After(time.Second):
		t.Fatal("partially created container was not terminated")
	}
}

type completedContainer struct {
	testcontainers.Container
	terminated chan struct{}
}

func (c *completedContainer) Start(context.Context) error { return nil }

func (c *completedContainer) State(context.Context) (*dockercontainer.State, error) {
	return &dockercontainer.State{Running: false, ExitCode: 0}, nil
}

func (c *completedContainer) Terminate(context.Context, ...testcontainers.TerminateOption) error {
	close(c.terminated)
	return nil
}

func (c *completedContainer) CopyFileFromContainer(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not ready")
}

func TestRunSingleHTTPTestTerminatesCompletedContainer(t *testing.T) {
	fake := &completedContainer{terminated: make(chan struct{})}
	original := genericContainer
	genericContainer = func(context.Context, testcontainers.GenericContainerRequest) (testcontainers.Container, error) {
		return fake, nil
	}
	t.Cleanup(func() { genericContainer = original })

	server := httptest.NewServer(nil)
	defer server.Close()
	runner := TestSuiteRunner{Logger: zap.NewNop()}
	runner.runSingleTest(t, nil, TestCase{Request: &HTTPRequest{URL: "/", ImageURL: "test-image"}}, server)

	select {
	case <-fake.terminated:
	case <-time.After(time.Second):
		t.Fatal("container was not terminated")
	}
}

func TestHTTPRequestRunPublishesReadinessTimeout(t *testing.T) {
	fake := &completedContainer{terminated: make(chan struct{})}
	original := genericContainer
	genericContainer = func(context.Context, testcontainers.GenericContainerRequest) (testcontainers.Container, error) {
		return fake, nil
	}
	t.Cleanup(func() { genericContainer = original })

	container := (&HTTPRequest{
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
	case <-container.resultCh:
		t.Fatal("published more than one result")
	default:
	}
}
