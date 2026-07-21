package status_test

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/status"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestLifecycleEndpointsFollowReadinessTransitions(t *testing.T) {
	addr := availableAddress(t)
	core, logs := observer.New(zap.InfoLevel)
	var ready atomic.Bool
	server := status.NewBaseStatusServer(addr, zap.New(core), ready.Load)
	require.NoError(t, server.Start())
	t.Cleanup(func() { require.NoError(t, server.Stop()) })
	require.Equal(t, 1, logs.FilterMessage("status server listening").Len())

	client := &http.Client{Timeout: time.Second}
	assertEndpoints := func(t *testing.T, statusCode int, healthBody, readinessBody string) {
		t.Helper()

		// Phase 5 intentionally gives healthz the same lifecycle semantics as readyz;
		// no separate liveness endpoint is exposed.
		for endpoint, expectedBody := range map[string]string{"/healthz": healthBody, "/readyz": readinessBody} {
			response, err := client.Get("http://" + addr + endpoint)
			require.NoError(t, err)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, statusCode, response.StatusCode, endpoint)
			require.Equal(t, expectedBody, string(body), endpoint)
		}
	}

	t.Run("startup is unavailable", func(t *testing.T) {
		assertEndpoints(t, http.StatusServiceUnavailable, "not healthy", "not ready")
	})

	ready.Store(true)
	t.Run("ready is available", func(t *testing.T) {
		assertEndpoints(t, http.StatusOK, "healthy", "ready")
	})

	ready.Store(false)
	t.Run("shutdown transition is unavailable", func(t *testing.T) {
		assertEndpoints(t, http.StatusServiceUnavailable, "not healthy", "not ready")
	})
}

func TestServersUsePrivateMuxes(t *testing.T) {
	first := status.NewBaseStatusServer("127.0.0.1:0", zap.NewNop(), func() bool { return true })
	second := status.NewBaseStatusServer("127.0.0.1:0", zap.NewNop(), func() bool { return true })

	require.NotSame(t, http.DefaultServeMux, first.Mux())
	require.NotSame(t, first.Mux(), second.Mux())

	request := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	response := httptest.NewRecorder()
	first.Mux().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
}

func TestStopIsSafeAndIdempotent(t *testing.T) {
	t.Run("before start", func(t *testing.T) {
		server := status.NewBaseStatusServer("127.0.0.1:0", zap.NewNop(), func() bool { return true })

		require.NoError(t, server.Stop())
		require.NoError(t, server.Stop())
		require.NoError(t, server.Start())
		require.NoError(t, server.Stop())
	})

	t.Run("after start", func(t *testing.T) {
		server := status.NewBaseStatusServer(availableAddress(t), zap.NewNop(), func() bool { return true })
		require.NoError(t, server.Start())

		require.NoError(t, server.Stop())
		require.NoError(t, server.Stop())
	})
}

func TestStartReturnsListenErrorsWithoutLoggingSuccess(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, occupied.Close()) })

	tests := []struct {
		name string
		addr string
	}{
		{name: "occupied address", addr: occupied.Addr().String()},
		{name: "invalid address", addr: "127.0.0.1:not-a-port"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zap.InfoLevel)
			server := status.NewBaseStatusServer(tt.addr, zap.New(core), func() bool { return true })

			require.Error(t, server.Start())
			require.Equal(t, 0, logs.FilterMessage("status server listening").Len())
		})
	}
}

func availableAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())
	return addr
}
