package tlsutils

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTLSVersionString(t *testing.T) {
	tests := []struct {
		name     string
		version  TLSVersion
		expected string
	}{
		{"TLS 1.0", VersionTLS10, "TLS 1.0"},
		{"TLS 1.1", VersionTLS11, "TLS 1.1"},
		{"TLS 1.2", VersionTLS12, "TLS 1.2"},
		{"TLS 1.3", VersionTLS13, "TLS 1.3"},
		{"Unknown", TLSVersion(0x0305), "0x0305"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.version.String(); got != tt.expected {
				t.Errorf("TLSVersion.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTLSVersionFloat(t *testing.T) {
	tests := []struct {
		name     string
		version  TLSVersion
		expected float64
	}{
		{"TLS 1.0", VersionTLS10, 1.0},
		{"TLS 1.1", VersionTLS11, 1.1},
		{"TLS 1.2", VersionTLS12, 1.2},
		{"TLS 1.3", VersionTLS13, 1.3},
		{"Unknown", TLSVersion(0x0305), 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.version.Float(); got != tt.expected {
				t.Errorf("TLSVersion.Float() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestClientHelloControlValues(t *testing.T) {
	tests := []struct {
		name     string
		hello    *ClientHello
		expected map[string]any
	}{
		{
			"Complete ClientHello",
			&ClientHello{
				SNI:     "example.com",
				Version: VersionTLS13,
				ALPNs:   []string{"h2", "http/1.1"},
			},
			map[string]any{
				"enabled": true,
				"version": 1.3,
				"sni":     "example.com",
				"alpn":    []string{"h2", "http/1.1"},
			},
		},
		{
			"Empty ClientHello",
			&ClientHello{},
			map[string]any{
				"enabled": true,
				"version": 0.0,
				"sni":     "",
				"alpn":    []string(nil),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.hello.ControlValues(); !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("ClientHello.ControlValues() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseALPNFromExtensions(t *testing.T) {
	tests := []struct {
		name       string
		extensions []byte
		expected   []string
	}{
		{
			name:       "empty extensions",
			extensions: []byte{},
			expected:   nil,
		},
		{
			name:       "too short extensions",
			extensions: []byte{0x00, 0x01},
			expected:   nil,
		},
		{
			name: "no ALPN extension",
			extensions: []byte{
				0x00, 0x17, // Extension type 23 (not ALPN)
				0x00, 0x04, // Extension length 4
				0x01, 0x02, 0x03, 0x04, // Data
			},
			expected: nil,
		},
		{
			name: "single ALPN protocol",
			extensions: []byte{
				0x00, 0x10, // Extension type 16 (ALPN)
				0x00, 0x05, // Extension length 5
				0x00, 0x03, // Protocol list length 3
				0x02,       // Protocol length 2
				0x68, 0x32, // "h2"
			},
			expected: []string{"h2"},
		},
		{
			name: "multiple ALPN protocols",
			extensions: []byte{
				0x00, 0x10, // Extension type 16 (ALPN)
				0x00, 0x0E, // Extension length 14
				0x00, 0x0C, // Protocol list length 12
				0x02,       // Protocol length 2
				0x68, 0x32, // "h2"
				0x08,                                           // Protocol length 8
				0x68, 0x74, 0x74, 0x70, 0x2F, 0x31, 0x2E, 0x31, // "http/1.1"
			},
			expected: []string{"h2", "http/1.1"},
		},
		{
			name: "ALPN with other extensions",
			extensions: []byte{
				0x00, 0x17, // Extension type 23 (not ALPN)
				0x00, 0x04, // Extension length 4
				0x01, 0x02, 0x03, 0x04, // Data
				0x00, 0x10, // Extension type 16 (ALPN)
				0x00, 0x07, // Extension length 7
				0x00, 0x05, // Protocol list length 5
				0x02,       // Protocol length 2
				0x68, 0x32, // "h2"
				0x01, // Protocol length 1
				0x78, // "x"
			},
			expected: []string{"h2", "x"},
		},
		{
			name: "empty ALPN protocol list",
			extensions: []byte{
				0x00, 0x10, // Extension type 16 (ALPN)
				0x00, 0x02, // Extension length 2
				0x00, 0x00, // Protocol list length 0
			},
			expected: nil,
		},
		{
			name: "malformed ALPN - invalid protocol list length",
			extensions: []byte{
				0x00, 0x10, // Extension type 16 (ALPN)
				0x00, 0x04, // Extension length 4
				0xFF, 0xFF, // Invalid protocol list length (too large)
				0x00, 0x00,
			},
			expected: nil,
		},
		{
			name: "malformed ALPN - invalid protocol length",
			extensions: []byte{
				0x00, 0x10, // Extension type 16 (ALPN)
				0x00, 0x05, // Extension length 5
				0x00, 0x03, // Protocol list length 3
				0xFF,       // Invalid protocol length (too large)
				0x68, 0x32, // Data
			},
			expected: nil,
		},
		{
			name: "malformed extension - invalid extension length",
			extensions: []byte{
				0x00, 0x10, // Extension type 16 (ALPN)
				0xFF, 0xFF, // Invalid extension length (too large)
			},
			expected: nil,
		},
		{
			name: "zero-length protocol in ALPN",
			extensions: []byte{
				0x00, 0x10, // Extension type 16 (ALPN)
				0x00, 0x06, // Extension length 6
				0x00, 0x04, // Protocol list length 4
				0x00,       // Protocol length 0 (should be skipped)
				0x02,       // Protocol length 2
				0x68, 0x32, // "h2"
			},
			expected: []string{"h2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseALPNFromExtensions(tt.extensions)
			require.Equal(t, tt.expected, result, "ALPN protocols should match expected")
		})
	}
}

func TestParseSupportedVersionsFromExtensions(t *testing.T) {
	tests := []struct {
		name       string
		extensions []byte
		expected   uint16
	}{
		{
			name:       "empty extensions",
			extensions: []byte{},
			expected:   0,
		},
		{
			name:       "too short extensions",
			extensions: []byte{0x00, 0x01},
			expected:   0,
		},
		{
			name: "no supported versions extension",
			extensions: []byte{
				0x00, 0x10, // Extension type 16 (not supported versions)
				0x00, 0x04, // Extension length 4
				0x01, 0x02, 0x03, 0x04, // Data
			},
			expected: 0,
		},
		{
			name: "single supported version TLS 1.3",
			extensions: []byte{
				0x00, 0x2B, // Extension type 43 (supported versions)
				0x00, 0x03, // Extension length 3
				0x02,       // Versions list length 2
				0x03, 0x04, // TLS 1.3 (0x0304)
			},
			expected: 0x0304,
		},
		{
			name: "multiple supported versions",
			extensions: []byte{
				0x00, 0x2B, // Extension type 43 (supported versions)
				0x00, 0x07, // Extension length 7
				0x06,       // Versions list length 6
				0x03, 0x01, // TLS 1.0 (0x0301)
				0x03, 0x03, // TLS 1.2 (0x0303)
				0x03, 0x04, // TLS 1.3 (0x0304)
			},
			expected: 0x0304, // Should return highest version
		},
		{
			name: "supported versions with other extensions",
			extensions: []byte{
				0x00, 0x10, // Extension type 16 (not supported versions)
				0x00, 0x02, // Extension length 2
				0x01, 0x02, // Data
				0x00, 0x2B, // Extension type 43 (supported versions)
				0x00, 0x03, // Extension length 3
				0x02,       // Versions list length 2
				0x03, 0x03, // TLS 1.2 (0x0303)
			},
			expected: 0x0303,
		},
		{
			name: "invalid versions list length (odd number)",
			extensions: []byte{
				0x00, 0x2B, // Extension type 43 (supported versions)
				0x00, 0x04, // Extension length 4
				0x03,             // Versions list length 3 (odd, invalid)
				0x03, 0x03, 0x04, // Invalid data
			},
			expected: 0,
		},
		{
			name: "versions list length too small",
			extensions: []byte{
				0x00, 0x2B, // Extension type 43 (supported versions)
				0x00, 0x02, // Extension length 2
				0x01, // Versions list length 1 (too small)
				0x03, // Invalid data
			},
			expected: 0,
		},
		{
			name: "versions outside valid TLS range",
			extensions: []byte{
				0x00, 0x2B, // Extension type 43 (supported versions)
				0x00, 0x07, // Extension length 7
				0x06,       // Versions list length 6
				0x03, 0x00, // Version 0x0300 (below TLS 1.0)
				0x03, 0x05, // Version 0x0305 (above TLS 1.3)
				0x03, 0x02, // TLS 1.1 (0x0302) - valid
			},
			expected: 0x0302, // Only valid version returned
		},
		{
			name: "malformed extension - invalid extension length",
			extensions: []byte{
				0x00, 0x2B, // Extension type 43 (supported versions)
				0xFF, 0xFF, // Invalid extension length (too large)
			},
			expected: 0,
		},
		{
			name: "empty supported versions list",
			extensions: []byte{
				0x00, 0x2B, // Extension type 43 (supported versions)
				0x00, 0x01, // Extension length 1
				0x00, // Versions list length 0
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSupportedVersionsFromExtensions(tt.extensions)
			require.Equal(t, tt.expected, result, "Supported version should match expected")
		})
	}
}

func TestParseClientHello(t *testing.T) {
	tests := []struct {
		name    string
		record  []byte
		want    *ClientHello
		wantErr string
	}{
		{
			name:    "empty record",
			record:  []byte{},
			want:    nil,
			wantErr: "empty record",
		},
		{
			name: "valid TLS 1.2 ClientHello with SNI",
			record: []byte{
				// TLS Record Layer
				0x16,       // Content Type: Handshake
				0x03, 0x01, // TLS Version 1.0 (compatibility)
				0x00, 0x3F, // Length: 63 bytes
				// Handshake Protocol
				0x01,             // Handshake Type: Client Hello
				0x00, 0x00, 0x3B, // Length: 59 bytes
				0x03, 0x03, // Client Version: TLS 1.2
				// Random (32 bytes)
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
				0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
				0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F,
				0x00,       // Session ID Length
				0x00, 0x02, // Cipher Suites Length
				0xC0, 0x2F, // Cipher Suite
				0x01,       // Compression Methods Length
				0x00,       // Compression Method: null
				0x00, 0x10, // Extensions Length: 16 bytes
				// SNI Extension
				0x00, 0x00, // Extension Type: Server Name
				0x00, 0x0C, // Extension Length: 12 bytes
				0x00, 0x0A, // Server Name List Length: 10 bytes
				0x00,       // Server Name Type: host_name
				0x00, 0x07, // Server Name Length: 7 bytes
				0x65, 0x78, 0x61, 0x6D, 0x70, 0x6C, 0x65, // "example"
			},
			want: &ClientHello{
				SNI:     "example",
				Version: VersionTLS12,
				ALPNs:   nil,
			},
			wantErr: "",
		},
		{
			name: "valid TLS 1.3 ClientHello with SNI and ALPN",
			record: []byte{
				// TLS Record Layer
				0x16,       // Content Type: Handshake
				0x03, 0x01, // TLS Version 1.0 (compatibility)
				0x00, 0x74, // Length: 116 bytes (5 header + 111 handshake)
				// Handshake Protocol
				0x01,             // Handshake Type: Client Hello
				0x00, 0x00, 0x70, // Length: 112 bytes
				0x03, 0x03, // Client Version: TLS 1.2 (compatibility)
				// Random (32 bytes)
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
				0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
				0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F,
				0x00,       // Session ID Length
				0x00, 0x02, // Cipher Suites Length
				0x13, 0x01, // Cipher Suite: TLS_AES_128_GCM_SHA256
				0x01,       // Compression Methods Length
				0x00,       // Compression Method: null
				0x00, 0x45, // Extensions Length: 69 bytes
				// SNI Extension (20 bytes total)
				0x00, 0x00, // Extension Type: Server Name
				0x00, 0x10, // Extension Length: 16 bytes
				0x00, 0x0E, // Server Name List Length: 14 bytes
				0x00,       // Server Name Type: host_name
				0x00, 0x0B, // Server Name Length: 11 bytes
				0x65, 0x78, 0x61, 0x6D, 0x70, 0x6C, 0x65, 0x2E, 0x63, 0x6F, 0x6D, // "example.com"
				// ALPN Extension (18 bytes total)
				0x00, 0x10, // Extension Type: ALPN
				0x00, 0x0E, // Extension Length: 14 bytes
				0x00, 0x0C, // Protocol List Length: 12 bytes
				0x02,       // Protocol Length: 2
				0x68, 0x32, // "h2"
				0x08,                                           // Protocol Length: 8
				0x68, 0x74, 0x74, 0x70, 0x2F, 0x31, 0x2E, 0x31, // "http/1.1"
				// Supported Versions Extension (9 bytes total)
				0x00, 0x2B, // Extension Type: Supported Versions
				0x00, 0x05, // Extension Length: 5 bytes
				0x04,       // Supported Versions Length: 4 bytes
				0x03, 0x04, // TLS 1.3
				0x03, 0x03, // TLS 1.2
				// Session Ticket Extension (4 bytes total)
				0x00, 0x23, // Extension Type: Session Ticket
				0x00, 0x00, // Extension Length: 0 bytes
				// EC Point Formats Extension (6 bytes total)
				0x00, 0x0B, // Extension Type: EC Point Formats
				0x00, 0x02, // Extension Length: 2 bytes
				0x01, // EC Point Formats Length: 1
				0x00, // Uncompressed
				// Supported Groups Extension (12 bytes total)
				0x00, 0x0A, // Extension Type: Supported Groups
				0x00, 0x08, // Extension Length: 8 bytes
				0x00, 0x06, // Supported Groups List Length: 6 bytes
				0x00, 0x17, // secp256r1
				0x00, 0x18, // secp384r1
				0x00, 0x19, // secp521r1
			},
			want: &ClientHello{
				SNI:     "example.com",
				Version: VersionTLS13,
				ALPNs:   []string{"h2", "http/1.1"},
			},
			wantErr: "",
		},
		{
			name: "invalid TLS packet - not a handshake",
			record: []byte{
				0x17,       // Content Type: Application Data (not Handshake)
				0x03, 0x03, // TLS Version
				0x00, 0x05, // Length
				0x01, 0x02, 0x03, 0x04, 0x05, // Data
			},
			want:    nil,
			wantErr: "no ClientHello found in TLS handshake",
		},
		{
			name: "malformed TLS record - too short",
			record: []byte{
				0x16,       // Content Type: Handshake
				0x03, 0x03, // TLS Version
				// Missing length and data
			},
			want:    nil,
			wantErr: "packet decoding error",
		},
		{
			name: "handshake without ClientHello",
			record: []byte{
				// TLS Record Layer
				0x16,       // Content Type: Handshake
				0x03, 0x03, // TLS Version
				0x00, 0x0A, // Length: 10 bytes
				// Handshake Protocol
				0x02,             // Handshake Type: Server Hello (not Client Hello)
				0x00, 0x00, 0x06, // Length: 6 bytes
				0x03, 0x03, // Server Version
				0x00, 0x00, 0x00, 0x00, // Random (truncated)
			},
			want:    nil,
			wantErr: "Unknown TLS handshake type", // gopacket error message
		},
		{
			name: "ClientHello with empty SNI",
			record: []byte{
				// TLS Record Layer
				0x16,       // Content Type: Handshake
				0x03, 0x01, // TLS Version 1.0 (compatibility)
				0x00, 0x38, // Length: 56 bytes
				// Handshake Protocol
				0x01,             // Handshake Type: Client Hello
				0x00, 0x00, 0x34, // Length: 52 bytes
				0x03, 0x03, // Client Version: TLS 1.2
				// Random (32 bytes)
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
				0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
				0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F,
				0x00,       // Session ID Length
				0x00, 0x02, // Cipher Suites Length
				0xC0, 0x2F, // Cipher Suite
				0x01,       // Compression Methods Length
				0x00,       // Compression Method: null
				0x00, 0x09, // Extensions Length: 9 bytes
				// SNI Extension with empty name
				0x00, 0x00, // Extension Type: Server Name
				0x00, 0x05, // Extension Length: 5 bytes
				0x00, 0x03, // Server Name List Length: 3 bytes
				0x00,       // Server Name Type: host_name
				0x00, 0x00, // Server Name Length: 0 bytes (empty)
			},
			want: &ClientHello{
				SNI:     "",
				Version: VersionTLS12,
				ALPNs:   nil,
			},
			wantErr: "",
		},
		{
			name: "ClientHello without extensions",
			record: []byte{
				// TLS Record Layer
				0x16,       // Content Type: Handshake
				0x03, 0x01, // TLS Version 1.0 (compatibility)
				0x00, 0x2F, // Length: 47 bytes (adjusted)
				// Handshake Protocol
				0x01,             // Handshake Type: Client Hello
				0x00, 0x00, 0x2B, // Length: 43 bytes (adjusted)
				0x03, 0x02, // Client Version: TLS 1.1
				// Random (32 bytes)
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
				0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
				0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F,
				0x00,       // Session ID Length
				0x00, 0x02, // Cipher Suites Length
				0xC0, 0x2F, // Cipher Suite
				0x01,       // Compression Methods Length
				0x00,       // Compression Method: null
				0x00, 0x00, // Extensions Length: 0 (empty extensions)
			},
			want: &ClientHello{
				SNI:     "",
				Version: VersionTLS11,
				ALPNs:   nil,
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseClientHello(tt.record)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.NotNil(t, got)
				require.Equal(t, tt.want.SNI, got.SNI, "SNI should match")
				require.Equal(t, tt.want.Version, got.Version, "Version should match")
				require.Equal(t, tt.want.ALPNs, got.ALPNs, "ALPNs should match")
			}
		})
	}
}
