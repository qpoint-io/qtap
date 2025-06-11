package connection

import (
	"context"
	"net"
	"sort"
	"strings"
	"testing"

	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/qnet"
	"github.com/qpoint-io/qtap/pkg/resolvable"
	"github.com/qpoint-io/qtap/pkg/tags"
	"github.com/qpoint-io/qtap/pkg/tlsutils"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestParseHostString(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantHost     string
		wantPort     string
		wantIsIPAddr bool
	}{
		{
			name:         "domain without port",
			input:        "example.com",
			wantHost:     "example.com",
			wantPort:     "",
			wantIsIPAddr: false,
		},
		{
			name:         "domain with port",
			input:        "example.com:8080",
			wantHost:     "example.com",
			wantPort:     "8080",
			wantIsIPAddr: false,
		},
		{
			name:         "IPv4 without port",
			input:        "192.168.1.1",
			wantHost:     "192.168.1.1",
			wantPort:     "",
			wantIsIPAddr: true,
		},
		{
			name:         "IPv4 with port",
			input:        "192.168.1.1:443",
			wantHost:     "192.168.1.1",
			wantPort:     "443",
			wantIsIPAddr: true,
		},
		{
			name:         "IPv6 without port",
			input:        "2001:db8::1",
			wantHost:     "2001:db8::1",
			wantPort:     "",
			wantIsIPAddr: true,
		},
		{
			name:         "IPv6 with port",
			input:        "[2001:db8::1]:8080",
			wantHost:     "2001:db8::1",
			wantPort:     "8080",
			wantIsIPAddr: true,
		},
		{
			name:         "with whitespace",
			input:        "  example.com:8080  ",
			wantHost:     "example.com",
			wantPort:     "8080",
			wantIsIPAddr: false,
		},
		// Invalid cases
		{
			name:         "malformed string with special chars",
			input:        ";-j|}j|}j|}j|",
			wantHost:     "",
			wantPort:     "",
			wantIsIPAddr: false,
		},
		{
			name:         "binary garbage data",
			input:        "d}P#}P#}P#}}zm} P#}P#}P#}}um",
			wantHost:     "",
			wantPort:     "",
			wantIsIPAddr: false,
		},
		{
			name:         "empty string",
			input:        "",
			wantHost:     "",
			wantPort:     "",
			wantIsIPAddr: false,
		},
		{
			name:         "only special characters",
			input:        "!@#$%^&*()",
			wantHost:     "",
			wantPort:     "",
			wantIsIPAddr: false,
		},
		{
			name:         "invalid port number",
			input:        "example.com:99999",
			wantHost:     "example.com",
			wantPort:     "",
			wantIsIPAddr: false,
		},
		{
			name:         "port with letters",
			input:        "example.com:abc",
			wantHost:     "example.com",
			wantPort:     "",
			wantIsIPAddr: false,
		},
		{
			name:         "domain starting with hyphen",
			input:        "-example.com",
			wantHost:     "",
			wantPort:     "",
			wantIsIPAddr: false,
		},
		{
			name:         "domain ending with hyphen",
			input:        "example-.com",
			wantHost:     "",
			wantPort:     "",
			wantIsIPAddr: false,
		},
		{
			name:         "domain with valid subdomain",
			input:        "sub.example.com",
			wantHost:     "sub.example.com",
			wantPort:     "",
			wantIsIPAddr: false,
		},
		{
			name:         "domain with invalid characters",
			input:        "example_.com",
			wantHost:     "",
			wantPort:     "",
			wantIsIPAddr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHost, gotPort, gotIsIPAddr := parseHostString(tt.input)

			if gotHost != tt.wantHost {
				t.Errorf("parseHostString() gotHost = %v, want %v", gotHost, tt.wantHost)
			}
			if gotPort != tt.wantPort {
				t.Errorf("parseHostString() gotPort = %v, want %v", gotPort, tt.wantPort)
			}
			if gotIsIPAddr != tt.wantIsIPAddr {
				t.Errorf("parseHostString() gotIsIPAddr = %v, want %v", gotIsIPAddr, tt.wantIsIPAddr)
			}
		})
	}
}

func TestIsValidDomain(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		expected bool
	}{
		// Valid domains
		{"simple domain", "example.com", true},
		{"subdomain", "sub.example.com", true},
		{"single label", "localhost", true},
		{"domain with numbers", "example123.com", true},
		{"domain with hyphens", "my-domain-name.com", true},
		{"max length label", strings.Repeat("a", 63) + ".com", true},

		// Invalid domains
		{"empty string", "", false},
		{"space in domain", "invalid domain.com", false},
		{"domain too long", strings.Repeat("a", 254), false},
		{"label too long", strings.Repeat("a", 64) + ".com", false},
		{"invalid characters", "example!.com", false},
		{"starts with hyphen", "-example.com", false},
		{"ends with hyphen", "example-.com", false},
		{"double dots", "example..com", false},
		{"starts with dot", ".example.com", false},
		{"ends with dot", "example.com.", false},
		{"special characters", "exam@ple.com", false},
		{"underscore", "exam_ple.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidDomain(tt.domain)
			if result != tt.expected {
				t.Errorf("isValidDomain(%q) = %v, want %v", tt.domain, result, tt.expected)
			}
		})
	}
}

func TestValidatePort(t *testing.T) {
	tests := []struct {
		name    string
		port    string
		want    string
		wantErr bool
	}{
		{
			name:    "valid port",
			port:    "8080",
			want:    "8080",
			wantErr: false,
		},
		{
			name:    "minimum valid port",
			port:    "1",
			want:    "1",
			wantErr: false,
		},
		{
			name:    "maximum valid port",
			port:    "65535",
			want:    "65535",
			wantErr: false,
		},
		{
			name:    "port zero",
			port:    "0",
			want:    "",
			wantErr: true,
		},
		{
			name:    "negative port",
			port:    "-1",
			want:    "",
			wantErr: true,
		},
		{
			name:    "port too large",
			port:    "65536",
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-numeric port",
			port:    "abc",
			want:    "",
			wantErr: true,
		},
		{
			name:    "empty port",
			port:    "",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validatePort(tt.port)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePort() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("validatePort() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConnection_ControlValues(t *testing.T) {
	proc := process.NewProcess(0, "")

	proc.SetUser(1000, "testuser")
	proc.SetHostname("testhost")
	proc.Binary = "curl"
	proc.Exe = "/usr/bin/curl"
	proc.Env = map[string]string{
		"TEST_ENV": "testvalue",
	}
	container := &process.Container{
		ID:    "dba11ada3983ee0d6dda08b584f940b6cc4941bdfc3b953a76313b6915da60ff",
		Name:  "testcontainer",
		Image: "testimage",
		Labels: map[string]string{
			"is-container": "i guess",
		},
	}
	pod := &process.Pod{
		Name:      "testpod",
		Namespace: "testnamespace",
		Labels: map[string]string{
			"pod-version": "v1",
		},
	}
	proc.Container = resolvable.Static(container).WithBgContext()
	proc.Pod = resolvable.Static(pod).WithBgContext()

	conn := NewConnection(
		context.Background(),
		zaptest.NewLogger(t),
		&OpenEvent{
			Source:     Client,
			SocketType: SocketType_TCP,
			Local: qnet.NetAddrFromTCPAddr(&net.TCPAddr{
				IP:   net.ParseIP("192.168.1.1"),
				Port: 34893,
			}),
			Remote: qnet.NetAddrFromTCPAddr(&net.TCPAddr{
				IP:   net.ParseIP("1.2.3.4"),
				Port: 443,
			}),
		},
		WithProcess(proc),
		WithTags(tags.FromValues(map[string]string{
			"test": "test",
			"tag2": "ok",
		})),
	)
	conn.Protocol = Protocol_HTTP2
	conn.TLSClientHello = &tlsutils.ClientHello{
		SNI:     "example.com",
		Version: tlsutils.VersionTLS12,
		ALPNs:   []string{"test-alpn"},
	}

	cv := conn.ControlValues()
	// sort tags to stabilize test results
	sort.Strings(cv["tags"].([]string))

	require.Equal(t, map[string]any{
		"protocol":  "http2",
		"type":      "tcp",
		"direction": "egress-external",
		"src": map[string]any{
			"process": map[string]any{
				"hostname": "testhost",
				"user": map[string]any{
					"id":   uint(1000),
					"name": "testuser",
				},
				"binary": "curl",
				"path":   "/usr/bin/curl",
				"env": map[string]any{
					"TEST_ENV": "testvalue",
				},
			},
			"ip":   net.ParseIP("192.168.1.1"),
			"port": 34893,
			"container": map[string]any{
				"id":    "dba11ada3983",
				"name":  "testcontainer",
				"image": "testimage",
				"labels": map[string]any{
					"is-container": "i guess",
				},
			},
			"pod": map[string]any{
				"name":      "testpod",
				"namespace": "testnamespace",
				"labels": map[string]any{
					"pod-version": "v1",
				},
			},
		},
		"dst": map[string]any{
			"ip":   net.ParseIP("1.2.3.4"),
			"port": 443,
		},
		"tls": map[string]any{
			"enabled": true,
			"sni":     "example.com",
			"version": 1.2,
			"alpn":    []string{"test-alpn"},
		},
		"tags": []string{
			"bin:curl",
			"host:testhost",
			"ip:192.168.1.1",
			"strategy:observe",
			"tag2:ok",
			"test:test",
		},
	}, cv)
}
