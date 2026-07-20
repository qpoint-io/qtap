//go:build linux

package egress

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestForwarder_StartStop(t *testing.T) {
	logger := zap.NewNop()
	ctx := t.Context()

	tests := []struct {
		name    string
		listen4 string
		listen6 string
	}{
		{
			name:    "valid ports",
			listen4: "127.0.0.1:0", // Port 0 lets the OS choose a free port
			listen6: "[::1]:0",
		},
		{
			name:    "ipv4 only",
			listen4: "127.0.0.1:0",
			listen6: "",
		},
		{
			name:    "ipv6 only",
			listen4: "",
			listen6: "[::1]:0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := NewForwarder(ctx, logger, tt.listen4, tt.listen6, nil, nil)
			require.NoError(t, err)

			err = f.Start()
			require.NoError(t, err)

			// Verify listeners are active
			if tt.listen4 != "" {
				require.NotNil(t, f.listener4)
				addr := f.listener4.Addr().(*net.TCPAddr)
				require.Positive(t, addr.Port)
			} else {
				require.Nil(t, f.listener4)
			}
			if tt.listen6 != "" {
				require.NotNil(t, f.listener6)
				addr := f.listener6.Addr().(*net.TCPAddr)
				require.Positive(t, addr.Port)
			} else {
				require.Nil(t, f.listener6)
			}

			// Check error from Stop
			require.NoError(t, f.Stop())

			// Verify listeners are closed
			if tt.listen4 != "" {
				_, err = net.Dial("tcp4", f.listener4.Addr().String())
				require.Error(t, err)
			}
			if tt.listen6 != "" {
				_, err = net.Dial("tcp6", f.listener6.Addr().String())
				require.Error(t, err)
			}
		})
	}
}

func TestUpstreamServerNameFallsBackToDestination(t *testing.T) {
	require.Equal(t, "service.example", upstreamServerName("service.example", "192.0.2.1"))
	require.Equal(t, "192.0.2.1", upstreamServerName("", "192.0.2.1"))
}

func TestForwarder_StartRollsBackIPv4ListenerWhenIPv6BindFails(t *testing.T) {
	available4, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	listen4 := available4.Addr().String()
	require.NoError(t, available4.Close())

	occupied6, err := net.Listen("tcp6", "[::1]:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, occupied6.Close()) })

	f, err := NewForwarder(t.Context(), zap.NewNop(), listen4, occupied6.Addr().String(), nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Stop() })

	require.Error(t, f.Start())
	require.Nil(t, f.listener4)

	rebound4, err := net.Listen("tcp4", listen4)
	require.NoError(t, err, "the IPv4 listener must be released after the IPv6 bind fails")
	require.NoError(t, rebound4.Close())
}

func TestForwarder_StartRetainsFailedRollbackForStopRetry(t *testing.T) {
	closeErr := errors.New("close IPv4")
	bindErr := errors.New("bind IPv6")
	listener := newBlockingCloseListener(closeErr, nil)

	f, err := NewForwarder(t.Context(), zap.NewNop(), "127.0.0.1:1", "[::1]:1", nil, nil)
	require.NoError(t, err)
	f.listen = func(network, _ string) (net.Listener, error) {
		if network == "tcp4" {
			return listener, nil
		}
		return nil, bindErr
	}

	err = f.Start()
	require.ErrorIs(t, err, bindErr)
	require.ErrorIs(t, err, closeErr)
	require.Same(t, listener, f.listener4)

	require.NoError(t, f.Stop())
	require.Equal(t, 2, listener.CloseCalls())
}

func TestForwarder_StartAndStopAreIdempotent(t *testing.T) {
	f, err := NewForwarder(t.Context(), zap.NewNop(), "127.0.0.1:0", "", nil, nil)
	require.NoError(t, err)

	require.NoError(t, f.Start())
	addr := f.listener4.Addr().String()
	require.NoError(t, f.Start())
	require.Equal(t, addr, f.listener4.Addr().String())

	require.NoError(t, f.Stop())
	require.NoError(t, f.Stop())
	require.Error(t, f.Start())
}

func TestForwarder_StopClosesBothListenersAndJoinsErrors(t *testing.T) {
	err4 := errors.New("close IPv4")
	err6 := errors.New("close IPv6")
	listener4 := &closeErrorListener{err: err4}
	listener6 := &closeErrorListener{err: err6}
	f, err := NewForwarder(t.Context(), zap.NewNop(), "", "", nil, nil)
	require.NoError(t, err)
	f.listener4 = listener4
	f.listener6 = listener6
	f.started = true

	err = f.Stop()
	require.ErrorIs(t, err, err4)
	require.ErrorIs(t, err, err6)
	require.Equal(t, 1, listener4.closeCalls)
	require.Equal(t, 1, listener6.closeCalls)
}

func TestForwarder_StopRetriesFailedListenerCleanup(t *testing.T) {
	closeErr := errors.New("close IPv4")
	listener := &closeErrorListener{closeErrors: []error{closeErr, nil}}
	f, err := NewForwarder(t.Context(), zap.NewNop(), "", "", nil, nil)
	require.NoError(t, err)
	f.listener4 = listener
	f.started = true

	require.ErrorIs(t, f.Stop(), closeErr)
	require.NoError(t, f.Stop())
	require.NoError(t, f.Stop())
	require.Equal(t, 2, listener.closeCalls)
}

func TestForwarder_StopDoesNotWaitAfterListenerCloseFailure(t *testing.T) {
	closeErr := errors.New("close IPv4")
	listener := newBlockingCloseListener(closeErr, nil)
	f, err := NewForwarder(t.Context(), zap.NewNop(), "", "", nil, nil)
	require.NoError(t, err)
	f.listener4 = listener
	f.started = true
	f.wg.Go(func() { f.acceptConnections(listener) })
	require.Eventually(t, listener.AcceptStarted, time.Second, time.Millisecond)

	stopResult := make(chan error, 1)
	go func() { stopResult <- f.Stop() }()
	select {
	case err := <-stopResult:
		require.ErrorIs(t, err, closeErr)
	case <-time.After(time.Second):
		require.Fail(t, "Stop waited for an accept loop that the failed close did not unblock")
	}

	require.NoError(t, f.Stop())
	require.Equal(t, 2, listener.CloseCalls())
}

func TestForwarder_StopWaitsForActiveConnectionHandlers(t *testing.T) {
	f, err := NewForwarder(t.Context(), zap.NewNop(), "127.0.0.1:0", "", nil, nil)
	require.NoError(t, err)

	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	f.handleConn = func(conn net.Conn) {
		close(handlerStarted)
		<-releaseHandler
		_ = conn.Close()
	}
	t.Cleanup(func() {
		select {
		case <-releaseHandler:
		default:
			close(releaseHandler)
		}
		_ = f.Stop()
	})

	require.NoError(t, f.Start())
	client, err := net.Dial("tcp4", f.listener4.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	require.Eventually(t, func() bool {
		select {
		case <-handlerStarted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	stopResult := make(chan error, 1)
	go func() { stopResult <- f.Stop() }()
	select {
	case err := <-stopResult:
		require.Failf(t, "Stop returned early", "Stop returned %v while a connection handler was active", err)
	case <-time.After(50 * time.Millisecond):
	}

	startResult := make(chan error, 1)
	go func() { startResult <- f.Start() }()
	select {
	case err := <-startResult:
		require.Error(t, err)
	case <-time.After(100 * time.Millisecond):
		close(releaseHandler)
		<-stopResult
		<-startResult
		require.Fail(t, "Start blocked on the lifecycle mutex while Stop waited for a handler")
	}

	close(releaseHandler)
	select {
	case err := <-stopResult:
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.Fail(t, "Stop did not return after the connection handler exited")
	}
}

func TestForwarder_StopClosesActiveDownstreamConnections(t *testing.T) {
	f, err := NewForwarder(t.Context(), zap.NewNop(), "127.0.0.1:0", "", nil, nil)
	require.NoError(t, err)

	handlerStarted := make(chan struct{})
	f.handleConn = func(conn net.Conn) {
		close(handlerStarted)
		var buf [1]byte
		_, _ = conn.Read(buf[:])
	}
	require.NoError(t, f.Start())

	client, err := net.Dial("tcp4", f.listener4.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	require.Eventually(t, func() bool {
		select {
		case <-handlerStarted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	stopResult := make(chan error, 1)
	go func() { stopResult <- f.Stop() }()
	select {
	case err := <-stopResult:
		require.NoError(t, err)
	case <-time.After(100 * time.Millisecond):
		_ = client.Close()
		<-stopResult
		require.Fail(t, "Stop did not close the active downstream connection")
	}
}

func TestForwarder_IsDestinationSelf(t *testing.T) {
	logger := zap.NewNop()
	ctx := t.Context()

	tests := []struct {
		name     string
		listen4  string
		listen6  string
		testAddr *net.TCPAddr
		want     bool
	}{
		{
			name:    "match ipv4 listener",
			listen4: "127.0.0.1:8080",
			testAddr: &net.TCPAddr{
				IP:   net.ParseIP("127.0.0.1"),
				Port: 8080,
			},
			want: true,
		},
		{
			name:    "different ipv4 port",
			listen4: "127.0.0.1:8080",
			testAddr: &net.TCPAddr{
				IP:   net.ParseIP("127.0.0.1"),
				Port: 8081,
			},
			want: false,
		},
		{
			name:    "match ipv6 listener",
			listen6: "[::1]:8080",
			testAddr: &net.TCPAddr{
				IP:   net.ParseIP("::1"),
				Port: 8080,
			},
			want: true,
		},
		{
			name:    "different ipv6 address",
			listen6: "[::1]:8080",
			testAddr: &net.TCPAddr{
				IP:   net.ParseIP("::2"),
				Port: 8080,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := NewForwarder(ctx, logger, tt.listen4, tt.listen6, nil, nil)
			require.NoError(t, err)

			err = f.Start()
			require.NoError(t, err)

			// Check error from Stop in defer
			defer func() {
				require.NoError(t, f.Stop())
			}()

			got := f.isDestinationSelf(tt.testAddr)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestForwarder_AcceptConnections(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	f, err := NewForwarder(ctx, logger, "127.0.0.1:0", "", nil, nil)
	require.NoError(t, err)

	err = f.Start()
	require.NoError(t, err)

	// Check error from Stop in defer
	defer func() {
		require.NoError(t, f.Stop())
	}()

	// Get the actual port that was assigned
	require.NotNil(t, f.listener4)
	addr := f.listener4.Addr().String()

	// Test that we can connect
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// Write some data
	testData := []byte("test data")
	_, err = conn.Write(testData)
	require.NoError(t, err)

	// Give the forwarder a moment to process
	time.Sleep(100 * time.Millisecond)
}

func TestForwarder_NilChecks(t *testing.T) {
	var f *Forwarder

	// Test Start with nil forwarder
	err := f.Start()
	require.Error(t, err)
	assert.Equal(t, "forwarder is nil", err.Error())

	// Test Stop with nil forwarder
	err = f.Stop()
	require.Error(t, err)
	assert.Equal(t, "forwarder is nil", err.Error())
}

type closeErrorListener struct {
	err         error
	closeErrors []error
	closeCalls  int
}

func (l *closeErrorListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }

func (l *closeErrorListener) Close() error {
	l.closeCalls++
	if l.closeCalls <= len(l.closeErrors) {
		return l.closeErrors[l.closeCalls-1]
	}
	return l.err
}

func (l *closeErrorListener) Addr() net.Addr { return &net.TCPAddr{} }

type blockingCloseListener struct {
	mu            sync.Mutex
	closeErrors   []error
	closeCalls    int
	acceptStarted chan struct{}
	closed        chan struct{}
	acceptOnce    sync.Once
	closeOnce     sync.Once
}

func newBlockingCloseListener(closeErrors ...error) *blockingCloseListener {
	return &blockingCloseListener{
		closeErrors:   closeErrors,
		acceptStarted: make(chan struct{}),
		closed:        make(chan struct{}),
	}
}

func (l *blockingCloseListener) Accept() (net.Conn, error) {
	l.acceptOnce.Do(func() { close(l.acceptStarted) })
	<-l.closed
	return nil, net.ErrClosed
}

func (l *blockingCloseListener) Close() error {
	l.mu.Lock()
	l.closeCalls++
	call := l.closeCalls
	var err error
	if call <= len(l.closeErrors) {
		err = l.closeErrors[call-1]
	}
	l.mu.Unlock()
	if err == nil {
		l.closeOnce.Do(func() { close(l.closed) })
	}
	return err
}

func (l *blockingCloseListener) Addr() net.Addr { return &net.TCPAddr{} }

func (l *blockingCloseListener) AcceptStarted() bool {
	select {
	case <-l.acceptStarted:
		return true
	default:
		return false
	}
}

func (l *blockingCloseListener) CloseCalls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closeCalls
}
