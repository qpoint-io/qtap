package e2e

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewHTTP1Server(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	server, err := NewHTTP1TLSServer("127.0.0.1", handler)
	require.NoError(t, err)
	require.NotNil(t, server)
	defer server.Close()

	resp, err := server.Client().Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "OK", string(body))
}

func TestNewHTTP1PlainServer(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	server, err := NewHTTP1PlainServer("127.0.0.1", handler)
	require.NoError(t, err)
	require.NotNil(t, server)
	defer server.Close()

	resp, err := server.Client().Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "OK", string(body))
}

func TestNewHTTP2Server(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	server, err := NewHTTP2TLSServer("127.0.0.1", handler)
	require.NoError(t, err)
	require.NotNil(t, server)
	defer server.Close()

	resp, err := server.Client().Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "OK", string(body))
}
