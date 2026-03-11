package report

import (
	"testing"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/stretchr/testify/assert"
)

// --- mock types implementing plugins.Headers and plugins.HeaderValue ---

type mockHeaderValue string

func (v mockHeaderValue) String() string      { return string(v) }
func (v mockHeaderValue) Bytes() []byte       { return []byte(v) }
func (v mockHeaderValue) Equal(s string) bool { return string(v) == s }

type mockHeaders struct {
	data map[string]string
}

func newMockHeaders(m map[string]string) *mockHeaders {
	return &mockHeaders{data: m}
}

func (h *mockHeaders) Get(key string) (plugins.HeaderValue, bool) {
	v, ok := h.data[key]
	return mockHeaderValue(v), ok
}

func (h *mockHeaders) Values(key string, iter func(value plugins.HeaderValue)) {
	if v, ok := h.data[key]; ok {
		iter(mockHeaderValue(v))
	}
}

func (h *mockHeaders) Set(key, value string)  { h.data[key] = value }
func (h *mockHeaders) Remove(key string)      { delete(h.data, key) }
func (h *mockHeaders) All() map[string]string { return h.data }

// --- tests ---

func TestPromoteHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		keys       []string
		reqHeaders map[string]string // nil map means nil Headers
		resHeaders map[string]string
		expected   map[string]string
	}{
		{
			name:       "no promote headers configured",
			keys:       nil,
			reqHeaders: map[string]string{"X-Req": "val"},
			resHeaders: map[string]string{"X-Res": "val"},
			expected:   nil,
		},
		{
			name:       "empty promote headers slice",
			keys:       []string{},
			reqHeaders: map[string]string{"X-Req": "val"},
			resHeaders: map[string]string{"X-Res": "val"},
			expected:   nil,
		},
		{
			name:       "matching request header only",
			keys:       []string{"X-Trace-Id"},
			reqHeaders: map[string]string{"X-Trace-Id": "abc123"},
			resHeaders: map[string]string{},
			expected:   map[string]string{"req.X-Trace-Id": "abc123"},
		},
		{
			name:       "matching response header only",
			keys:       []string{"X-Trace-Id"},
			reqHeaders: map[string]string{},
			resHeaders: map[string]string{"X-Trace-Id": "res456"},
			expected:   map[string]string{"res.X-Trace-Id": "res456"},
		},
		{
			name:       "both request and response headers match",
			keys:       []string{"X-Trace-Id"},
			reqHeaders: map[string]string{"X-Trace-Id": "req-val"},
			resHeaders: map[string]string{"X-Trace-Id": "res-val"},
			expected: map[string]string{
				"req.X-Trace-Id": "req-val",
				"res.X-Trace-Id": "res-val",
			},
		},
		{
			name:       "header key not found in either",
			keys:       []string{"X-Missing"},
			reqHeaders: map[string]string{"X-Other": "a"},
			resHeaders: map[string]string{"X-Other": "b"},
			expected:   nil,
		},
		{
			name:       "multiple headers some present some missing",
			keys:       []string{"X-A", "X-B", "X-C"},
			reqHeaders: map[string]string{"X-A": "a1"},
			resHeaders: map[string]string{"X-B": "b2"},
			expected: map[string]string{
				"req.X-A": "a1",
				"res.X-B": "b2",
			},
		},
		{
			name:       "nil reqheaders response only",
			keys:       []string{"X-Id"},
			reqHeaders: nil,
			resHeaders: map[string]string{"X-Id": "resp"},
			expected:   map[string]string{"res.X-Id": "resp"},
		},
		{
			name:       "nil resheaders request only",
			keys:       []string{"X-Id"},
			reqHeaders: map[string]string{"X-Id": "requ"},
			resHeaders: nil,
			expected:   map[string]string{"req.X-Id": "requ"},
		},
		{
			name:       "both reqheaders and resheaders nil",
			keys:       []string{"X-Id"},
			reqHeaders: nil,
			resHeaders: nil,
			expected:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var req, res plugins.Headers
			if tt.reqHeaders != nil {
				req = newMockHeaders(tt.reqHeaders)
			}
			if tt.resHeaders != nil {
				res = newMockHeaders(tt.resHeaders)
			}

			got := promoteHeaders(tt.keys, req, res)
			assert.Equal(t, tt.expected, got)
		})
	}
}
