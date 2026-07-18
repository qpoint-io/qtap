//go:build linux

package egress

import (
	"context"
	"net"
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
			}
			if tt.listen6 != "" {
				require.NotNil(t, f.listener6)
				addr := f.listener6.Addr().(*net.TCPAddr)
				require.Positive(t, addr.Port)
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
