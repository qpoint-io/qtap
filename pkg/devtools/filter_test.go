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
			name: "simple equality - process binary",
			expr: "binary == 'nginx'",
		},
		{
			name: "string equality - http method",
			expr: "req.method == 'GET'",
		},
		{
			name: "numeric comparison - http status",
			expr: "res.status >= 400",
		},
		{
			name: "compound AND",
			expr: "binary == 'curl' AND res.status >= 400",
		},
		{
			name: "compound OR",
			expr: "req.method == 'GET' OR req.method == 'POST'",
		},
		{
			name: "negation",
			expr: "NOT res.status == 404",
		},
		{
			name: "contains",
			expr: "req.path contains '/api/'",
		},
		{
			name: "regex match",
			expr: "req.path matches /api\\/v[0-9]+/",
		},
		{
			name: "function call",
			expr: "in_zone(req.host, 'example.com')",
		},
		{
			name:    "invalid syntax - incomplete",
			expr:    "binary ==",
			wantErr: true,
		},
		{
			name:    "invalid syntax - bad operator",
			expr:    "binary === 'nginx'",
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
		name  string
		expr  string
		event *Event
		want  bool
	}{
		{
			name:  "nil filter matches everything",
			expr:  "",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "GET"}}),
			want:  true,
		},
		{
			name:  "system events always pass",
			expr:  "req.method == 'POST'",
			event: NewEvent("system.connected", map[string]any{"topics": []string{}}),
			want:  true,
		},
		{
			name:  "process binary equals - match",
			expr:  "binary == 'nginx'",
			event: NewEvent("process.started", map[string]any{"binary": "nginx"}),
			want:  true,
		},
		{
			name:  "process binary equals - no match",
			expr:  "binary == 'nginx'",
			event: NewEvent("process.started", map[string]any{"binary": "curl"}),
			want:  false,
		},
		{
			name:  "http method equals - match",
			expr:  "req.method == 'GET'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "GET"}}),
			want:  true,
		},
		{
			name:  "http method equals - no match",
			expr:  "req.method == 'POST'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "GET"}}),
			want:  false,
		},
		{
			name:  "http status gte - match",
			expr:  "res.status >= 400",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"status": 500}}),
			want:  true,
		},
		{
			name:  "http status gte - no match",
			expr:  "res.status >= 400",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"status": 200}}),
			want:  false,
		},
		{
			name:  "http status gte - boundary match",
			expr:  "res.status >= 400",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"status": 400}}),
			want:  true,
		},
		{
			name:  "http path contains - match",
			expr:  "req.path contains 'users'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"path": "/api/users/123"}}),
			want:  true,
		},
		{
			name:  "http path contains - no match",
			expr:  "req.path contains 'products'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"path": "/api/users/123"}}),
			want:  false,
		},
		{
			name:  "compound AND - both match",
			expr:  "req.method == 'POST' AND res.status >= 400",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "POST", "status": 500}}),
			want:  true,
		},
		{
			name:  "compound AND - one fails",
			expr:  "req.method == 'POST' AND res.status >= 400",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "POST", "status": 200}}),
			want:  false,
		},
		{
			name:  "compound OR - first matches",
			expr:  "req.method == 'GET' OR req.method == 'POST'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "GET"}}),
			want:  true,
		},
		{
			name:  "compound OR - second matches",
			expr:  "req.method == 'GET' OR req.method == 'POST'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "POST"}}),
			want:  true,
		},
		{
			name:  "compound OR - neither matches",
			expr:  "req.method == 'GET' OR req.method == 'POST'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "DELETE"}}),
			want:  false,
		},
		{
			name:  "NOT - negates match",
			expr:  "res.status != 404",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"status": float64(200)}}),
			want:  true,
		},
		{
			name:  "NOT - negates to false",
			expr:  "res.status != 404",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"status": float64(404)}}),
			want:  false,
		},
		{
			name:  "missing field - no match",
			expr:  "req.method == 'GET'",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"status": 200}}),
			want:  false,
		},
		{
			name:  "process container name - match",
			expr:  "container.name == 'nginx'",
			event: NewEvent("process.started", map[string]any{"container": map[string]any{"name": "nginx", "id": "abc123"}}),
			want:  true,
		},
		{
			name:  "http host from url - match",
			expr:  "req.host == 'api.example.com'",
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
			name: "process filter on connection event - matching binary",
			expr: "process.binary == 'curl'",
			event: NewEvent("connection.opened", map[string]any{
				"data": map[string]any{
					"source": map[string]any{
						"exe": "/usr/bin/curl",
					},
				},
			}),
			want: true,
		},
		{
			name: "process filter on connection event - non-matching binary",
			expr: "process.binary == 'curl'",
			event: NewEvent("connection.opened", map[string]any{
				"data": map[string]any{
					"source": map[string]any{
						"exe": "/usr/bin/wget",
					},
				},
			}),
			want: false,
		},
		{
			name: "container filter on connection event - matching container name",
			expr: "container.name == 'nginx'",
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
			expr:  "binary == 'nginx'",
			event: NewEvent("process.started", map[string]any{"binary": "nginx"}),
			want:  true,
		},
		{
			name: "connection filter on connection event - direction match",
			expr: "direction == 'egress'",
			event: NewEvent("connection.opened", map[string]any{
				"data": map[string]any{"direction": "egress"},
			}),
			want: true,
		},
		{
			name: "connection filter on connection event - dst.port match",
			expr: "dst.port == 443",
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
			"hostname": "web-server",
			"exe":      "/usr/bin/curl",
			"container": map[string]any{
				"name": "nginx",
				"id":   "abc123",
			},
		},
		"destination": map[string]any{
			"address": map[string]any{
				"ip":   "10.0.0.1",
				"port": float64(443),
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
			expr: "process.binary == 'curl'",
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
			expr: "process.binary == 'wget'",
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
			name: "container filter on request via cache",
			expr: "container.name == 'nginx'",
			event: NewEvent("request.created", map[string]any{
				"data": map[string]any{
					"connectionId": "conn-123",
					"method":       "POST",
				},
			}),
			want: true,
		},
		{
			name: "connection filter on request - matching dst.port via cache",
			expr: "dst.port == 443",
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
			expr: "protocol == 'http2'",
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
			expr: "process.binary == 'curl'",
			event: NewEvent("request.created", map[string]any{
				"data": map[string]any{
					"connectionId": "unknown-conn",
					"method":       "GET",
				},
			}),
			want: false, // Can't verify process.binary without connection data
		},
		{
			name: "combined cross-entity and direct filter - both match",
			expr: "process.binary == 'curl' AND res.status >= 400",
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
			expr: "process.binary == 'curl' AND res.status >= 400",
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
	t.Run("process event - native fields at root", func(t *testing.T) {
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

		// Native process fields - no prefix
		assertKVValue(t, kv, "binary", "nginx")
		assertKVValue(t, kv, "path", "/usr/sbin/nginx")
		assertKVValue(t, kv, "hostname", "web-server")

		// User nested under user.*
		assertNestedKVValue(t, kv, "user", "name", "root")
		assertNestedKVValue(t, kv, "user", "id", 0)

		// Container nested under container.*
		assertNestedKVValue(t, kv, "container", "name", "my-container")
		assertNestedKVValue(t, kv, "container", "id", "abc123")
		assertNestedKVValue(t, kv, "container", "image", "nginx:latest")

		// Pod nested under pod.*
		assertNestedKVValue(t, kv, "pod", "name", "web-pod")
		assertNestedKVValue(t, kv, "pod", "namespace", "default")
	})

	t.Run("connection event - native + relational process", func(t *testing.T) {
		event := NewEvent("connection.opened", map[string]any{
			"data": map[string]any{
				"direction":      "egress",
				"l7Protocol":     "http2",
				"socketProtocol": "tcp",
				"source": map[string]any{
					"hostname": "web-server",
					"exe":      "/usr/bin/curl",
					"address":  map[string]any{"ip": "192.168.1.1", "port": float64(8080)},
					"container": map[string]any{
						"name": "my-container",
						"id":   "abc123",
					},
				},
				"destination": map[string]any{
					"address": map[string]any{"ip": "10.0.0.1", "port": float64(443)},
					"domain":  "api.example.com",
				},
			},
		})

		kv := buildEventKV(event, nil)

		// Native connection fields
		assertKVValue(t, kv, "direction", "egress")
		assertKVValue(t, kv, "protocol", "http2")
		assertKVValue(t, kv, "type", "tcp")

		// src.* nested
		assertNestedKVValue(t, kv, "src", "ip", "192.168.1.1")
		assertNestedKVValue(t, kv, "src", "port", float64(8080))

		// dst.* nested
		assertNestedKVValue(t, kv, "dst", "ip", "10.0.0.1")
		assertNestedKVValue(t, kv, "dst", "port", float64(443))
		assertNestedKVValue(t, kv, "dst", "domain", "api.example.com")

		// Relational process.* from source
		assertNestedKVValue(t, kv, "process", "hostname", "web-server")
		assertNestedKVValue(t, kv, "process", "path", "/usr/bin/curl")
		assertNestedKVValue(t, kv, "process", "binary", "curl")

		// Container at root (native to connection via source)
		assertNestedKVValue(t, kv, "container", "name", "my-container")
		assertNestedKVValue(t, kv, "container", "id", "abc123")
	})

	t.Run("request event - native http fields", func(t *testing.T) {
		event := NewEvent("request.created", map[string]any{
			"data": map[string]any{
				"method":       "POST",
				"status":       200,
				"path":         "/api/users",
				"url":          "https://api.example.com/api/users",
				"connectionId": "conn-123",
			},
		})

		kv := buildEventKV(event, nil)

		// Native HTTP fields - req.* and res.*
		assertNestedKVValue(t, kv, "req", "method", "POST")
		assertNestedKVValue(t, kv, "req", "path", "/api/users")
		assertNestedKVValue(t, kv, "req", "url", "https://api.example.com/api/users")
		assertNestedKVValue(t, kv, "req", "host", "api.example.com")
		assertNestedKVValue(t, kv, "res", "status", 200)
	})

	t.Run("http_transaction summary event", func(t *testing.T) {
		event := NewEvent("request.http_transaction", map[string]any{
			"data": map[string]any{
				"connectionId": "abc123",
				"type":         "http_transaction",
				"summary": map[string]any{
					"request_host":    "api.slack.com",
					"request_method":  "POST",
					"response_status": float64(200),
					"request_path":    "/api/chat.postMessage",
				},
			},
		})

		kv := buildEventKV(event, nil)

		// HTTP fields from summary format
		assertNestedKVValue(t, kv, "req", "host", "api.slack.com")
		assertNestedKVValue(t, kv, "req", "method", "POST")
		assertNestedKVValue(t, kv, "req", "path", "/api/chat.postMessage")
		assertNestedKVValue(t, kv, "res", "status", float64(200))
	})

	t.Run("request event with cache - relational fields populated", func(t *testing.T) {
		cache := NewConnectionCache(100)
		cache.Set("conn-123", map[string]any{
			"direction":  "egress",
			"l7Protocol": "http2",
			"source": map[string]any{
				"hostname": "web-server",
				"exe":      "/usr/bin/curl",
				"container": map[string]any{
					"name": "nginx",
					"pod": map[string]any{
						"name":      "web-pod",
						"namespace": "production",
					},
				},
			},
			"destination": map[string]any{
				"address": map[string]any{"ip": "10.0.0.1", "port": float64(443)},
				"domain":  "api.example.com",
			},
		})

		event := NewEvent("request.created", map[string]any{
			"data": map[string]any{
				"method":       "GET",
				"status":       200,
				"connectionId": "conn-123",
			},
		})

		kv := buildEventKV(event, cache)

		// Native HTTP fields
		assertNestedKVValue(t, kv, "req", "method", "GET")
		assertNestedKVValue(t, kv, "res", "status", 200)

		// Relational connection fields from cache
		assertKVValue(t, kv, "direction", "egress")
		assertKVValue(t, kv, "protocol", "http2")
		assertNestedKVValue(t, kv, "dst", "port", float64(443))
		assertNestedKVValue(t, kv, "dst", "domain", "api.example.com")

		// Relational process fields from cache
		assertNestedKVValue(t, kv, "process", "hostname", "web-server")
		assertNestedKVValue(t, kv, "process", "binary", "curl")

		// Container and pod from cache
		assertNestedKVValue(t, kv, "container", "name", "nginx")
		assertNestedKVValue(t, kv, "pod", "name", "web-pod")
		assertNestedKVValue(t, kv, "pod", "namespace", "production")
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
			expr:  "req.path matches /api\\/v[0-9]+/",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"path": "/api/v2/users"}}),
			want:  true,
		},
		{
			name:  "path does not match regex",
			expr:  "req.path matches /api\\/v[0-9]+/",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"path": "/api/users"}}),
			want:  false,
		},
		{
			name:  "host matches regex",
			expr:  "req.host matches /\\.example\\.com$/",
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
			expr:  "in_zone(req.host, 'example.com')",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"url": "https://example.com/test"}}),
			want:  true,
		},
		{
			name:  "in_zone - subdomain match",
			expr:  "in_zone(req.host, 'example.com')",
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"url": "https://api.example.com/test"}}),
			want:  true,
		},
		{
			name:  "in_zone - no match",
			expr:  "in_zone(req.host, 'example.com')",
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

// Helper function to assert KV values at root level
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

// Helper function to assert nested KV values (e.g., req.method, container.name)
func assertNestedKVValue(t *testing.T, kv rulekit.KV, parent, key string, want any) {
	t.Helper()
	parentVal, ok := kv[parent]
	if !ok {
		t.Errorf("parent key %q not found in KV", parent)
		return
	}
	parentMap, ok := parentVal.(map[string]any)
	if !ok {
		t.Errorf("KV[%q] is not a map, got %T", parent, parentVal)
		return
	}
	got, ok := parentMap[key]
	if !ok {
		t.Errorf("key %q not found in KV[%q]", key, parent)
		return
	}
	if got != want {
		t.Errorf("KV[%q][%q] = %v (%T), want %v (%T)", parent, key, got, got, want, want)
	}
}
