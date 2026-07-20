package ca_test

import (
	"os"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/ca"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestContainerScanReleasesLockWhenInitFails(t *testing.T) {
	container := ca.NewContainer(1, -1, nil, ca.InjectStrategyEbpf, zap.NewNop(), nil, nil, nil, nil)
	p := &process.Process{Pid: -1, Env: map[string]string{}}

	_, err := container.Scan(p)
	require.ErrorContains(t, err, "initializing container")

	done := make(chan struct{})
	var retryErr error
	go func() {
		defer close(done)
		_, retryErr = container.Scan(p)
	}()

	select {
	case <-done:
		require.ErrorContains(t, retryErr, "initializing container", "failed initialization must be retried")
	case <-time.After(time.Second):
		t.Fatal("second Scan blocked because the container mutex remained locked")
	}
}

func TestContainerScanReturnsInjectionFailure(t *testing.T) {
	container := ca.NewContainer(1, os.Getpid(), nil, ca.InjectStrategyManual, zap.NewNop(), nil, nil, nil, nil)
	p := &process.Process{
		Pid:    os.Getpid(),
		RootID: 1,
		Env:    map[string]string{"SSL_CERT_FILE": "/missing-ca.pem"},
	}

	found, err := container.Scan(p)

	require.False(t, found)
	require.ErrorContains(t, err, "failed to inject cert")
}
