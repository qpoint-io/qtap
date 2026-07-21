package remote

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/httpclient"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRemoteProviderReloadWaitsForCallback(t *testing.T) {
	handlerErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("config: {}\n"))
		handlerErr <- err
	}))
	defer server.Close()

	provider := NewRemoteConfigProvider(zap.NewNop(), server.URL, httpclient.New(""))
	entered := make(chan struct{})
	release := make(chan struct{})
	provider.OnConfigChange(func(*config.Config) (func(), error) {
		return func() {
			close(entered)
			<-release
		}, nil
	})

	done := make(chan struct{})
	reloadErr := make(chan error, 1)
	go func() {
		reloadErr <- provider.Reload()
		close(done)
	}()
	<-entered
	select {
	case <-done:
		t.Fatal("reload returned before configuration application completed")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	<-done
	require.NoError(t, <-reloadErr)
	require.NoError(t, <-handlerErr)
}

func TestRemoteProviderConcurrentReloadsAreSerialized(t *testing.T) {
	const reloads = 5

	type request struct {
		sequence int
		release  chan struct{}
	}

	requests := make(chan request, reloads)
	handlerErr := make(chan error, reloads)
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	var requestSequence atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			maximum := maxInFlight.Load()
			if current <= maximum || maxInFlight.CompareAndSwap(maximum, current) {
				break
			}
		}

		sequence := int(requestSequence.Add(1))
		release := make(chan struct{})
		requests <- request{sequence: sequence, release: release}
		<-release

		w.WriteHeader(http.StatusOK)
		_, err := fmt.Fprintf(w, "config:\n  tags:\n    - key: request\n      source: env\n      location: %q\n", strconv.Itoa(sequence))
		handlerErr <- err
	}))
	defer server.Close()

	provider := NewRemoteConfigProvider(zap.NewNop(), server.URL, httpclient.New(""))
	var callbackMu sync.Mutex
	callbackOrder := make([]string, 0, reloads)
	provider.OnConfigChange(func(cfg *config.Config) (func(), error) {
		callbackMu.Lock()
		callbackOrder = append(callbackOrder, cfg.Tags[0].Location)
		callbackMu.Unlock()
		return nil, nil
	})

	start := make(chan struct{})
	reloadErr := make(chan error, reloads)
	for range reloads {
		go func() {
			<-start
			reloadErr <- provider.Reload()
		}()
	}
	close(start)

	acquisitionOrder := make([]string, 0, reloads)
	for range reloads {
		request := <-requests
		acquisitionOrder = append(acquisitionOrder, strconv.Itoa(request.sequence))
		close(request.release)
	}

	for range reloads {
		require.NoError(t, <-reloadErr)
		require.NoError(t, <-handlerErr)
	}
	require.EqualValues(t, 1, maxInFlight.Load())
	require.Equal(t, acquisitionOrder, callbackOrder)
}
