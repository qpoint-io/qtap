//go:build linux

package l7detect

import (
	"io"
	"testing"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
)

// mockPeeker implements the Peeker interface for testing
type mockPeeker struct {
	data []byte
	err  error
}

func (m *mockPeeker) Peek(n int) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	if n > len(m.data) {
		return m.data, nil
	}
	return m.data[:n], nil
}

func TestDetectTLS(t *testing.T) {
	tests := []struct {
		name     string
		peeker   Peeker
		expected bool
	}{
		{"TLS Handshake", &mockPeeker{data: []byte{0x16, 0x03, 0x01}}, true},
		{"Not TLS Handshake", &mockPeeker{data: []byte{0x15, 0x03, 0x01}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := DetectTLS(tt.peeker)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectProtocol(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name     string
		peeker   Peeker
		expected connection.Protocol
	}{
		{"HTTP/2", &mockPeeker{data: []byte(http2.ClientPreface)}, connection.Protocol_HTTP2},
		{"HTTP/1.x", &mockPeeker{data: []byte("GET / HTTP/1.1")}, connection.Protocol_HTTP1},
		{"Redis", &mockPeeker{data: []byte("*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n")}, connection.Protocol_REDIS},
		{"Unknown", &mockPeeker{data: []byte("UNKNOWN")}, connection.Protocol_UNKNOWN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := DetectProtocol(logger, tt.peeker)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectClientHello(t *testing.T) {
	// Create a more realistic TLS ClientHello message
	clientHelloBytes := []byte{
		0x16,       // Content Type: Handshake
		0x03, 0x01, // Version: TLS 1.0
		0x00, 0x31, // Length (adjust based on actual content)
		0x01,             // Handshake Type: ClientHello
		0x00, 0x00, 0x2d, // Length
		0x03, 0x03, // Client Version: TLS 1.2
		// Random (32 bytes)
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		0x00,       // Session ID Length
		0x00, 0x04, // Cipher Suites Length
		0x00, 0x01, 0x00, 0x02, // Cipher Suites
		0x01,       // Compression Methods Length
		0x00,       // Compression Method
		0x00, 0x00, // Extensions Length
	}

	tests := []struct {
		name           string
		peeker         Peeker
		expectedSNI    string
		expectedProtos []string
		expectError    bool
	}{
		{
			name:           "Basic ClientHello",
			peeker:         &mockPeeker{data: clientHelloBytes},
			expectedSNI:    "",
			expectedProtos: nil,
			expectError:    false,
		},
		{
			name:           "Error case - too short",
			peeker:         &mockPeeker{data: []byte{0x16, 0x03, 0x01}, err: io.EOF},
			expectedSNI:    "",
			expectedProtos: nil,
			expectError:    true,
		},
		{
			name:           "Not a TLS handshake",
			peeker:         &mockPeeker{data: []byte{0x15, 0x03, 0x01, 0x00, 0x02}},
			expectedSNI:    "",
			expectedProtos: nil,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := DetectClientHello(tt.peeker)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedSNI, ch.SNI)
			assert.Equal(t, tt.expectedProtos, ch.ALPNs)
		})
	}
}

func TestIsHTTP1(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected bool
	}{
		{
			name:     "Valid GET request",
			input:    []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"),
			expected: true,
		},
		{
			name:     "Valid POST request",
			input:    []byte("POST /api HTTP/1.1\r\nHost: example.com\r\n\r\n"),
			expected: true,
		},
		{
			name:     "Valid HTTP response",
			input:    []byte("HTTP/1.1 200 OK\r\nContent-Type: text/html\r\n\r\n"),
			expected: true,
		},
		{
			name:     "Invalid HTTP2 request",
			input:    []byte(http2.ClientPreface),
			expected: false,
		},
		{
			name:     "Invalid non-HTTP data",
			input:    []byte("This is not an HTTP request"),
			expected: false,
		},
		{
			name:     "Empty input",
			input:    []byte{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peeker := &mockPeeker{data: tt.input}

			result, err := isHTTP1(peeker)
			if err != nil && tt.expected {
				t.Errorf("isHTTP1() error = %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("isHTTP1() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsHTTP2(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected bool
	}{
		{
			name:     "Valid HTTP2 client preface",
			input:    []byte(http2.ClientPreface),
			expected: true,
		},
		{
			name:     "Partial HTTP2 client preface",
			input:    []byte(http2.ClientPreface[:12]),
			expected: false,
		},
		{
			name:     "Invalid HTTP1 GET request",
			input:    []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"),
			expected: false,
		},
		{
			name:     "Invalid HTTP1 POST request",
			input:    []byte("POST /api HTTP/1.1\r\nHost: example.com\r\n\r\n"),
			expected: false,
		},
		{
			name:     "Invalid non-HTTP data",
			input:    []byte("This is not an HTTP request"),
			expected: false,
		},
		{
			name:     "Empty input",
			input:    []byte{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peeker := &mockPeeker{data: tt.input}

			result, err := isHTTP2(peeker)
			if err != nil && tt.expected {
				t.Errorf("isHTTP2() error = %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("isHTTP2() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsRedis(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected bool
	}{
		{
			name:     "Valid Redis SET command",
			input:    []byte("*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n"),
			expected: true,
		},
		{
			name:     "Valid Redis GET command",
			input:    []byte("*2\r\n$3\r\nGET\r\n$3\r\nkey\r\n"),
			expected: true,
		},
		{
			name:     "Valid Redis PING command",
			input:    []byte("*1\r\n$4\r\nPING\r\n"),
			expected: true,
		},
		{
			name:     "Invalid - not RESP array",
			input:    []byte("+OK\r\n"),
			expected: false,
		},
		{
			name:     "Invalid - zero count",
			input:    []byte("*0\r\n"),
			expected: false,
		},
		{
			name:     "Invalid - HTTP request",
			input:    []byte("GET / HTTP/1.1\r\n"),
			expected: false,
		},
		{
			name:     "Empty input",
			input:    []byte{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peeker := &mockPeeker{data: tt.input}

			result, err := isRedis(peeker)
			if err != nil && tt.expected {
				t.Errorf("isRedis() error = %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("isRedis() = %v, want %v", result, tt.expected)
			}
		})
	}
}
