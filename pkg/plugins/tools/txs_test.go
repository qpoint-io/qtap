package tools

import (
	"testing"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/stretchr/testify/require"
)

// mockHeaderValue implements the plugins.HeaderValue interface for testing
type mockHeaderValue struct {
	value string
}

func (m mockHeaderValue) String() string {
	return m.value
}

func (m mockHeaderValue) Bytes() []byte {
	return []byte(m.value)
}

func (m mockHeaderValue) Equal(str string) bool {
	return m.value == str
}

// mockHeaders is a mock implementation of the Headers interface for testing
type mockHeaders struct {
	headers map[string]string
}

func (m *mockHeaders) Get(key string) (plugins.HeaderValue, bool) {
	v, ok := m.headers[key]
	if !ok {
		return nil, false
	}
	return mockHeaderValue{value: v}, ok
}

func (m *mockHeaders) Values(key string, iter func(value plugins.HeaderValue)) {
	if v, ok := m.headers[key]; ok {
		iter(mockHeaderValue{value: v})
	}
}

func (m *mockHeaders) Set(key, value string) {
	m.headers[key] = value
}

func (m *mockHeaders) Remove(key string) {
	delete(m.headers, key)
}

func (m *mockHeaders) All() map[string]string {
	return m.headers
}

func TestHeaderMap_RulePairs(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		expected map[string]any
	}{
		{
			name:     "empty headers",
			headers:  map[string]string{},
			expected: map[string]any{},
		},
		{
			name: "headers with colon prefix",
			headers: map[string]string{
				":method":    "GET",
				":path":      "/api/v1/users",
				":scheme":    "https",
				":authority": "example.com",
			},
			expected: map[string]any{
				"method": "GET",
				"path":   "/api/v1/users",
				"scheme": "https",
				"url":    "https://example.com/api/v1/users",
				"host":   "example.com",
			},
		},
		{
			name: "regular headers",
			headers: map[string]string{
				"content-type": "application/json",
				"User-Agent":   "test-client",
				"x-request-id": "123456",
			},
			expected: map[string]any{
				"headers": map[string]any{
					"content-type": "application/json",
					"user-agent":   "test-client",
					"x-request-id": "123456",
				},
			},
		},
		{
			name: "mixed headers",
			headers: map[string]string{
				":method":      "POST",
				":path":        "/api/v1/data",
				":scheme":      "https",
				":authority":   "api.example.com",
				"content-type": "application/json",
				"user-agent":   "test-client",
			},
			expected: map[string]any{
				"method": "POST",
				"path":   "/api/v1/data",
				"scheme": "https",
				"headers": map[string]any{
					"content-type": "application/json",
					"user-agent":   "test-client",
				},
				"url":  "https://api.example.com/api/v1/data",
				"host": "api.example.com",
			},
		},
		{
			name: "missing scheme",
			headers: map[string]string{
				":path":      "/api/v1/items",
				":authority": "example.org",
			},
			expected: map[string]any{
				"path": "/api/v1/items",
				"host": "example.org",
			},
		},
		{
			name: "missing authority",
			headers: map[string]string{
				":scheme": "http",
				":path":   "/api/v1/products",
			},
			expected: map[string]any{
				"scheme": "http",
				"path":   "/api/v1/products",
			},
		},
		{
			name: "custom prefix",
			headers: map[string]string{
				":method":    "GET",
				"user-agent": "mozilla",
			},
			expected: map[string]any{
				"method": "GET",
				"headers": map[string]any{
					"user-agent": "mozilla",
				},
			},
		},
		{
			name: "response prefix with status",
			headers: map[string]string{
				":status":      "200",
				":scheme":      "https",
				":authority":   "example.com",
				":path":        "/api/v1/success",
				"content-type": "application/json",
			},
			expected: map[string]any{
				// When prefix is "response", the ":status" header should be
				// converted from a string to an integer after initial addition
				"status": 200, // Integer, not string
				"scheme": "https",
				"path":   "/api/v1/success",
				"headers": map[string]any{
					"content-type": "application/json",
				},
				"url":  "https://example.com/api/v1/success",
				"host": "example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockH := &mockHeaders{headers: tt.headers}
			headerMap := NewHeaderMap(mockH)

			result := headerMap.RulePairs()

			require.Equal(t, tt.expected, result, "RulePairs should return expected map")
		})
	}
}

func TestBuildAuthorityURL(t *testing.T) {
	tests := []struct {
		name           string
		scheme         string
		authority      string
		path           string
		expectedURL    string
		expectedResult bool
	}{
		{"Simple domain", "http", "example.com", "/api", "http://example.com/api", true},
		{"Domain with subdomain", "http", "sub.example.com", "/path", "http://sub.example.com/path", true},
		{"Domain with port", "http", "example.com:8080", "/test", "http://example.com:8080/test", true},
		{"HTTPS domain", "https", "secure.com", "/secure", "https://secure.com/secure", true},
		{"IPv4 address", "http", "192.168.1.1", "/ipv4", "http://192.168.1.1/ipv4", true},
		{"IPv4 with port", "http", "192.168.1.1:8080", "/port", "http://192.168.1.1:8080/port", true},
		{"IPv6 address", "http", "[2001:db8::1]", "/ipv6", "http://[2001:db8::1]/ipv6", true},
		{"IPv6 with port", "http", "[2001:db8::1]:8080", "/ipv6port", "http://[2001:db8::1]:8080/ipv6port", true},
		{"Localhost", "http", "localhost", "/local", "http://localhost/local", true},
		{"Localhost with port", "http", "localhost:3000", "/localport", "http://localhost:3000/localport", true},
		{"Empty path", "http", "example.com", "", "http://example.com", true},
		{"Path with query", "http", "example.com", "/search?q=test", "http://example.com/search%3Fq=test", true},
		{"Path with fragment", "http", "example.com", "/page#section", "http://example.com/page%23section", true},
		{"Invalid authority", "http", "http://[invalid", "/test", "", false},
		{"Authority with username", "http", "user@example.com", "/auth", "http://user@example.com/auth", true},
		{"Authority with username and password", "http", "user:pass@example.com", "/auth", "http://user:pass@example.com/auth", true},
		{"Punycode domain", "http", "xn--80akhbyknj4f.xn--p1ai", "/punycode", "http://xn--80akhbyknj4f.xn--p1ai/punycode", true},
		{"Double slash in path", "http", "example.com", "//double/slash", "http://example.com/double/slash", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotResult := buildAuthorityURL(tt.scheme, tt.authority, tt.path)
			if gotURL != tt.expectedURL || gotResult != tt.expectedResult {
				t.Errorf("buildAuthorityURL(%q, %q, %q) = (%q, %v), want (%q, %v)",
					tt.scheme, tt.authority, tt.path, gotURL, gotResult, tt.expectedURL, tt.expectedResult)
			}
		})
	}
}
