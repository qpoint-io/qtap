package devtools

import (
	"testing"

	"github.com/qpoint-io/rulekit"
)

func TestParseFilter(t *testing.T) {
	tests := []struct {
		name      string
		expr      string
		wantErr   bool
		wantEmpty bool
	}{
		{
			name:      "empty expression",
			expr:      "",
			wantEmpty: true,
		},
		{
			name: "simple equality",
			expr: "process.pid == 1234",
		},
		{
			name: "string equality",
			expr: "http.method == 'GET'",
		},
		{
			name: "numeric comparison",
			expr: "http.status >= 400",
		},
		{
			name: "compound AND",
			expr: "process.pid == 1234 AND http.status >= 400",
		},
		{
			name: "compound OR",
			expr: "http.method == 'GET' OR http.method == 'POST'",
		},
		{
			name: "negation",
			expr: "NOT http.status == 404",
		},
		{
			name: "contains",
			expr: "http.path contains '/api/'",
		},
		{
			name: "regex match",
			expr: "http.path matches /api\\/v[0-9]+/",
		},
		{
			name: "function call",
			expr: "in_zone(http.domain, 'example.com')",
		},
		{
			name:    "invalid syntax - incomplete",
			expr:    "process.pid ==",
			wantErr: true,
		},
		{
			name:    "invalid syntax - bad operator",
			expr:    "process.pid === 1234",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseFilter(tt.expr)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.wantEmpty {
				if !filter.IsEmpty() {
					t.Error("expected empty filter")
				}
				return
			}

			if filter.IsEmpty() {
				t.Error("expected non-empty filter")
			}
			if filter.Expression() != tt.expr {
				t.Errorf("Expression() = %q, want %q", filter.Expression(), tt.expr)
			}
		})
	}
}

func TestEventFilter_Matches(t *testing.T) {
	tests := []struct {
		name   string
		expr   string
		event  *Event
		want   bool
	}{
		{
			name:  "nil filter matches everything",
			expr:  "",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "GET"}}),
			want:  true,
		},
		{
			name:  "system events always pass",
			expr:  "http.method == 'POST'",
			event: NewEvent("system.connected", map[string]any{"topics": []string{}}),
			want:  true,
		},
		{
			name:  "process pid equals - match",
			expr:  "process.pid == 1234",
			event: NewEvent("process.started", map[string]any{"pid": 1234}),
			want:  true,
		},
		{
			name:  "process pid equals - no match",
			expr:  "process.pid == 1234",
			event: NewEvent("process.started", map[string]any{"pid": 5678}),
			want:  false,
		},
		{
			name:  "http method equals - match",
			expr:  "http.method == 'GET'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "GET"}}),
			want:  true,
		},
		{
			name:  "http method equals - no match",
			expr:  "http.method == 'POST'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "GET"}}),
			want:  false,
		},
		{
			name:  "http status gte - match",
			expr:  "http.status >= 400",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"status": 500}}),
			want:  true,
		},
		{
			name:  "http status gte - no match",
			expr:  "http.status >= 400",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"status": 200}}),
			want:  false,
		},
		{
			name:  "http status gte - boundary match",
			expr:  "http.status >= 400",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"status": 400}}),
			want:  true,
		},
		{
			name:  "http path contains - match",
			expr:  "http.path contains 'users'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"path": "/api/users/123"}}),
			want:  true,
		},
		{
			name:  "http path contains - no match",
			expr:  "http.path contains 'products'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"path": "/api/users/123"}}),
			want:  false,
		},
		{
			name:  "compound AND - both match",
			expr:  "http.method == 'POST' AND http.status >= 400",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "POST", "status": 500}}),
			want:  true,
		},
		{
			name:  "compound AND - one fails",
			expr:  "http.method == 'POST' AND http.status >= 400",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "POST", "status": 200}}),
			want:  false,
		},
		{
			name:  "compound OR - first matches",
			expr:  "http.method == 'GET' OR http.method == 'POST'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "GET"}}),
			want:  true,
		},
		{
			name:  "compound OR - second matches",
			expr:  "http.method == 'GET' OR http.method == 'POST'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "POST"}}),
			want:  true,
		},
		{
			name:  "compound OR - neither matches",
			expr:  "http.method == 'GET' OR http.method == 'POST'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "DELETE"}}),
			want:  false,
		},
		{
			name:  "NOT - negates match",
			expr:  "http.status != 404",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"status": float64(200)}}),
			want:  true,
		},
		{
			name:  "NOT - negates to false",
			expr:  "http.status != 404",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"status": float64(404)}}),
			want:  false,
		},
		{
			name:  "missing field - no match",
			expr:  "http.method == 'GET'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"status": 200}}),
			want:  false,
		},
		{
			name:  "process container - match",
			expr:  "process.container == 'nginx'",
			event: NewEvent("process.started", map[string]any{"container": map[string]any{"name": "nginx", "id": "abc123"}}),
			want:  true,
		},
		{
			name:  "http domain from url - match",
			expr:  "http.domain == 'api.example.com'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"url": "https://api.example.com/v1/users"}}),
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseFilter(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse filter: %v", err)
			}

			got := filter.Matches(tt.event, nil)
			if got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCrossEntityFiltering(t *testing.T) {
	tests := []struct {
		name  string
		expr  string
		event *Event
		want  bool
	}{
		{
			name: "process filter on connection event - matching pid",
			expr: "process.pid == 1234",
			event: NewEvent("connection.opened", map[string]any{
				"data": map[string]any{
					"source": map[string]any{
						"pid": float64(1234), // JSON numbers decode as float64
					},
				},
			}),
			want: true,
		},
		{
			name: "process filter on connection event - non-matching pid",
			expr: "process.pid == 1234",
			event: NewEvent("connection.opened", map[string]any{
				"data": map[string]any{
					"source": map[string]any{
						"pid": float64(5678),
					},
				},
			}),
			want: false,
		},
		{
			name: "process filter on connection event - matching container",
			expr: "process.container == 'nginx'",
			event: NewEvent("connection.opened", map[string]any{
				"data": map[string]any{
					"source": map[string]any{
						"container": map[string]any{"name": "nginx", "id": "abc123"},
					},
				},
			}),
			want: true,
		},
		{
			name:  "process filter on process event - direct match",
			expr:  "process.pid == 1234",
			event: NewEvent("process.started", map[string]any{"pid": 1234}),
			want:  true,
		},
		{
			name: "connection filter on connection event - direction match",
			expr: "connection.direction == 'egress'",
			event: NewEvent("connection.opened", map[string]any{
				"data": map[string]any{"direction": "egress"},
			}),
			want: true,
		},
		{
			name: "connection filter on connection event - dstPort match",
			expr: "connection.dstPort == 443",
			event: NewEvent("connection.opened", map[string]any{
				"data": map[string]any{
					"destination": map[string]any{
						"address": map[string]any{"ip": "10.0.0.1", "port": 443},
					},
				},
			}),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseFilter(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse filter: %v", err)
			}

			got := filter.Matches(tt.event, nil)
			if got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCrossEntityFilteringWithCache(t *testing.T) {
	// Create a cache with a connection
	cache := NewConnectionCache(100)
	cache.Set("conn-123", map[string]any{
		"connectionId": "conn-123",
		"direction":    "egress",
		"l7Protocol":   "http2",
		"source": map[string]any{
			"pid":      float64(1234), // JSON numbers decode as float64
			"hostname": "web-server",
			"container": map[string]any{
				"name": "nginx",
				"id":   "abc123",
			},
		},
		"destination": map[string]any{
			"address": map[string]any{
				"ip":   "10.0.0.1",
				"port": float64(443), // JSON numbers decode as float64
			},
		},
	})

	tests := []struct {
		name  string
		expr  string
		event *Event
		want  bool
	}{
		{
			name: "process filter on request - matching via cache lookup",
			expr: "process.pid == 1234",
			event: NewEvent("request.created", map[string]any{
				"data": map[string]any{
					"connectionId": "conn-123",
					"method":       "GET",
					"status":       200,
				},
			}),
			want: true,
		},
		{
			name: "process filter on request - non-matching via cache lookup",
			expr: "process.pid == 9999",
			event: NewEvent("request.created", map[string]any{
				"data": map[string]any{
					"connectionId": "conn-123",
					"method":       "GET",
					"status":       200,
				},
			}),
			want: false,
		},
		{
			name: "process container filter on request via cache",
			expr: "process.container == 'nginx'",
			event: NewEvent("request.created", map[string]any{
				"data": map[string]any{
					"connectionId": "conn-123",
					"method":       "POST",
				},
			}),
			want: true,
		},
		{
			name: "connection filter on request - matching dstPort via cache",
			expr: "connection.dstPort == 443",
			event: NewEvent("request.created", map[string]any{
				"data": map[string]any{
					"connectionId": "conn-123",
					"method":       "GET",
				},
			}),
			want: true,
		},
		{
			name: "connection filter on request - matching protocol via cache",
			expr: "connection.protocol == 'http2'",
			event: NewEvent("request.created", map[string]any{
				"data": map[string]any{
					"connectionId": "conn-123",
					"method":       "GET",
				},
			}),
			want: true,
		},
		{
			name: "request with unknown connectionId - cache miss, filter fails",
			expr: "process.pid == 1234",
			event: NewEvent("request.created", map[string]any{
				"data": map[string]any{
					"connectionId": "unknown-conn",
					"method":       "GET",
				},
			}),
			want: false, // Can't verify process.pid without connection data
		},
		{
			name: "combined cross-entity and direct filter - both match",
			expr: "process.pid == 1234 AND http.status >= 400",
			event: NewEvent("request.created", map[string]any{
				"data": map[string]any{
					"connectionId": "conn-123",
					"method":       "GET",
					"status":       500,
				},
			}),
			want: true,
		},
		{
			name: "combined filter - process matches but http fails",
			expr: "process.pid == 1234 AND http.status >= 400",
			event: NewEvent("request.created", map[string]any{
				"data": map[string]any{
					"connectionId": "conn-123",
					"method":       "GET",
					"status":       200,
				},
			}),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseFilter(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse filter: %v", err)
			}

			got := filter.Matches(tt.event, cache)
			if got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConnectionCache(t *testing.T) {
	t.Run("basic set and get", func(t *testing.T) {
		cache := NewConnectionCache(100)
		data := map[string]any{"direction": "egress"}
		cache.Set("conn-1", data)

		got := cache.Get("conn-1")
		if got == nil {
			t.Error("expected to get cached data, got nil")
		}
		if got["direction"] != "egress" {
			t.Errorf("expected direction=egress, got %v", got["direction"])
		}
	})

	t.Run("get non-existent", func(t *testing.T) {
		cache := NewConnectionCache(100)
		got := cache.Get("non-existent")
		if got != nil {
			t.Errorf("expected nil for non-existent key, got %v", got)
		}
	})

	t.Run("delete", func(t *testing.T) {
		cache := NewConnectionCache(100)
		cache.Set("conn-1", map[string]any{"direction": "egress"})
		cache.Delete("conn-1")

		got := cache.Get("conn-1")
		if got != nil {
			t.Errorf("expected nil after delete, got %v", got)
		}
	})

	t.Run("LRU eviction", func(t *testing.T) {
		cache := NewConnectionCache(3) // Small cache

		cache.Set("conn-1", map[string]any{"id": 1})
		cache.Set("conn-2", map[string]any{"id": 2})
		cache.Set("conn-3", map[string]any{"id": 3})

		// Cache is full, adding conn-4 should evict conn-1
		cache.Set("conn-4", map[string]any{"id": 4})

		if cache.Get("conn-1") != nil {
			t.Error("expected conn-1 to be evicted")
		}
		if cache.Get("conn-2") == nil {
			t.Error("expected conn-2 to still exist")
		}
		if cache.Get("conn-4") == nil {
			t.Error("expected conn-4 to exist")
		}
	})

	t.Run("update existing", func(t *testing.T) {
		cache := NewConnectionCache(100)
		cache.Set("conn-1", map[string]any{"version": 1})
		cache.Set("conn-1", map[string]any{"version": 2})

		got := cache.Get("conn-1")
		if got["version"] != 2 {
			t.Errorf("expected version=2, got %v", got["version"])
		}
		if cache.Len() != 1 {
			t.Errorf("expected cache len=1, got %d", cache.Len())
		}
	})
}

func TestBuildEventKV(t *testing.T) {
	t.Run("process event", func(t *testing.T) {
		event := NewEvent("process.started", map[string]any{
			"pid":      1234,
			"binary":   "nginx",
			"path":     "/usr/sbin/nginx",
			"hostname": "web-server",
			"user":     map[string]any{"name": "root", "id": 0},
			"container": map[string]any{
				"name":  "my-container",
				"id":    "abc123",
				"image": "nginx:latest",
			},
			"pod": map[string]any{
				"name":      "web-pod",
				"namespace": "default",
			},
		})

		kv := buildEventKV(event, nil)

		assertKVValue(t, kv, "process.pid", 1234)
		assertKVValue(t, kv, "process.binary", "nginx")
		assertKVValue(t, kv, "process.path", "/usr/sbin/nginx")
		assertKVValue(t, kv, "process.hostname", "web-server")
		assertKVValue(t, kv, "process.user", "root")
		assertKVValue(t, kv, "process.userId", 0)
		assertKVValue(t, kv, "process.container", "my-container")
		assertKVValue(t, kv, "process.containerId", "abc123")
		assertKVValue(t, kv, "process.containerImage", "nginx:latest")
		assertKVValue(t, kv, "process.pod", "web-pod")
		assertKVValue(t, kv, "process.podNamespace", "default")
	})

	t.Run("connection event", func(t *testing.T) {
		event := NewEvent("connection.opened", map[string]any{
			"data": map[string]any{
				"direction":      "egress",
				"l7Protocol":     "http2",
				"socketProtocol": "tcp",
				"source": map[string]any{
					"pid":      float64(1234),
					"hostname": "web-server",
					"exe":      "/usr/bin/curl",
					"address":  map[string]any{"ip": "192.168.1.1", "port": float64(8080)},
				},
				"destination": map[string]any{
					"address": map[string]any{"ip": "10.0.0.1", "port": float64(443)},
				},
			},
		})

		kv := buildEventKV(event, nil)

		assertKVValue(t, kv, "connection.direction", "egress")
		assertKVValue(t, kv, "connection.protocol", "http2")
		assertKVValue(t, kv, "connection.socketProtocol", "tcp")
		assertKVValue(t, kv, "connection.srcIp", "192.168.1.1")
		assertKVValue(t, kv, "connection.srcPort", float64(8080))
		assertKVValue(t, kv, "connection.dstIp", "10.0.0.1")
		assertKVValue(t, kv, "connection.dstPort", float64(443))
		assertKVValue(t, kv, "process.pid", float64(1234))
		assertKVValue(t, kv, "process.hostname", "web-server")
		assertKVValue(t, kv, "process.path", "/usr/bin/curl")
		assertKVValue(t, kv, "process.binary", "curl")
	})

	t.Run("request event", func(t *testing.T) {
		event := NewEvent("request.created", map[string]any{
			"data": map[string]any{
				"method":        "POST",
				"status":        200,
				"path":          "/api/users",
				"url":           "https://api.example.com/api/users",
				"direction":     "egress",
				"duration":      150,
				"bytesSent":     1024,
				"bytesReceived": 2048,
				"connectionId":  "conn-123",
				"requestId":     "req-456",
			},
		})

		kv := buildEventKV(event, nil)

		assertKVValue(t, kv, "http.method", "POST")
		assertKVValue(t, kv, "http.status", 200)
		assertKVValue(t, kv, "http.path", "/api/users")
		assertKVValue(t, kv, "http.url", "https://api.example.com/api/users")
		assertKVValue(t, kv, "http.domain", "api.example.com")
		assertKVValue(t, kv, "http.direction", "egress")
		assertKVValue(t, kv, "http.duration", 150)
		assertKVValue(t, kv, "http.bytesSent", 1024)
		assertKVValue(t, kv, "http.bytesReceived", 2048)
		assertKVValue(t, kv, "http.connectionId", "conn-123")
		assertKVValue(t, kv, "http.requestId", "req-456")
	})

	t.Run("http_transaction summary event", func(t *testing.T) {
		event := NewEvent("request.http_transaction", map[string]any{
			"data": map[string]any{
				"connectionId": "abc123",
				"type":         "http_transaction",
				"summary": map[string]any{
					"request_host":     "api.slack.com",
					"request_method":   "POST",
					"response_status":  float64(200),
					"request_path":     "/api/chat.postMessage",
					"direction":        "egress-external",
					"request_protocol": "http2",
					"request_scheme":   "https",
					"connection_id":    "abc123",
					"container_name":   "myapp",
					"container_image":  "myapp:latest",
					"process_exe":      "/usr/bin/node",
				},
			},
		})

		kv := buildEventKV(event, nil)

		assertKVValue(t, kv, "http.domain", "api.slack.com")
		assertKVValue(t, kv, "http.method", "POST")
		assertKVValue(t, kv, "http.status", float64(200))
		assertKVValue(t, kv, "http.path", "/api/chat.postMessage")
		assertKVValue(t, kv, "http.direction", "egress-external")
		assertKVValue(t, kv, "http.protocol", "http2")
		assertKVValue(t, kv, "http.scheme", "https")
		assertKVValue(t, kv, "process.container", "myapp")
		assertKVValue(t, kv, "process.containerImage", "myapp:latest")
		assertKVValue(t, kv, "process.binary", "/usr/bin/node")
	})
}

func TestRegexMatching(t *testing.T) {
	tests := []struct {
		name  string
		expr  string
		event *Event
		want  bool
	}{
		{
			name:  "path matches regex",
			expr:  "http.path matches /api\\/v[0-9]+/",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"path": "/api/v2/users"}}),
			want:  true,
		},
		{
			name:  "path does not match regex",
			expr:  "http.path matches /api\\/v[0-9]+/",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"path": "/api/users"}}),
			want:  false,
		},
		{
			name:  "domain matches regex",
			expr:  "http.domain matches /\\.example\\.com$/",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"url": "https://api.example.com/test"}}),
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseFilter(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse filter: %v", err)
			}

			got := filter.Matches(tt.event, nil)
			if got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCustomFunctions(t *testing.T) {
	tests := []struct {
		name  string
		expr  string
		event *Event
		want  bool
	}{
		{
			name:  "in_zone - exact match",
			expr:  "in_zone(http.domain, 'example.com')",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"url": "https://example.com/test"}}),
			want:  true,
		},
		{
			name:  "in_zone - subdomain match",
			expr:  "in_zone(http.domain, 'example.com')",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"url": "https://api.example.com/test"}}),
			want:  true,
		},
		{
			name:  "in_zone - no match",
			expr:  "in_zone(http.domain, 'example.com')",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"url": "https://other.com/test"}}),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseFilter(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse filter: %v", err)
			}

			got := filter.Matches(tt.event, nil)
			if got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper function to assert KV values
func assertKVValue(t *testing.T, kv rulekit.KV, key string, want any) {
	t.Helper()
	got, ok := kv[key]
	if !ok {
		t.Errorf("key %q not found in KV", key)
		return
	}
	if got != want {
		t.Errorf("KV[%q] = %v (%T), want %v (%T)", key, got, got, want, want)
	}
}
