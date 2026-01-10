package devtools

import (
	"net/url"
	"testing"
)

func TestParseFilters(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantErr    bool
		wantEmpty  bool
		wantEntity string
		wantCount  int
	}{
		{
			name:      "empty query",
			query:     "",
			wantEmpty: true,
		},
		{
			name:       "single filter",
			query:      "process.pid.eq=1234",
			wantEntity: "process",
			wantCount:  1,
		},
		{
			name:       "multiple filters same entity",
			query:      "process.pid.eq=1234&process.container.eq=nginx",
			wantEntity: "process",
			wantCount:  2,
		},
		{
			name:      "multiple filters different entities",
			query:     "process.pid.eq=1234&http.method.eq=POST",
			wantCount: 2, // 1 per entity
		},
		{
			name:    "invalid operator",
			query:   "process.pid.invalid=1234",
			wantErr: true,
		},
		{
			name:      "non-filter query params ignored",
			query:     "foo=bar&baz=qux",
			wantEmpty: true,
		},
		{
			name:       "mixed filter and non-filter params",
			query:      "foo=bar&process.pid.eq=1234",
			wantEntity: "process",
			wantCount:  1,
		},
		{
			name:       "all operators",
			query:      "http.status.eq=200&http.status.neq=404&http.status.gt=100&http.status.gte=200&http.status.lt=500&http.status.lte=299&http.path.prefix=/api&http.path.contains=users&http.method.in=GET,POST",
			wantEntity: "http",
			wantCount:  9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatalf("failed to parse query: %v", err)
			}

			filter, err := ParseFilters(values)
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
					t.Errorf("expected empty filter, got %+v", filter.Conditions)
				}
				return
			}

			if tt.wantEntity != "" {
				conditions, ok := filter.Conditions[tt.wantEntity]
				if !ok {
					t.Errorf("expected conditions for entity %q", tt.wantEntity)
					return
				}
				if len(conditions) != tt.wantCount {
					t.Errorf("expected %d conditions, got %d", tt.wantCount, len(conditions))
				}
			} else if tt.wantCount > 0 {
				// Count total conditions across all entities
				total := 0
				for _, conds := range filter.Conditions {
					total += len(conds)
				}
				if total != tt.wantCount {
					t.Errorf("expected %d total conditions, got %d", tt.wantCount, total)
				}
			}
		})
	}
}

func TestEventFilter_Matches(t *testing.T) {
	tests := []struct {
		name   string
		filter *EventFilter
		event  *Event
		want   bool
	}{
		{
			name:   "nil filter matches everything",
			filter: nil,
			event:  NewEvent("request.created", map[string]any{"data": map[string]any{"method": "GET"}}),
			want:   true,
		},
		{
			name:   "empty filter matches everything",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{}},
			event:  NewEvent("request.created", map[string]any{"data": map[string]any{"method": "GET"}}),
			want:   true,
		},
		{
			name: "system events always pass",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {{Entity: "http", Attribute: "method", Operator: OpEq, Value: "POST"}},
			}},
			event: NewEvent("system.connected", map[string]any{"topics": []string{}}),
			want:  true,
		},
		{
			name: "unfiltered entity passes through",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {{Entity: "http", Attribute: "method", Operator: OpEq, Value: "POST"}},
			}},
			event: NewEvent("process.started", map[string]any{"pid": 1234}),
			want:  true,
		},
		{
			name: "matching eq filter",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {{Entity: "http", Attribute: "method", Operator: OpEq, Value: "GET"}},
			}},
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "GET"}}),
			want:  true,
		},
		{
			name: "non-matching eq filter",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {{Entity: "http", Attribute: "method", Operator: OpEq, Value: "POST"}},
			}},
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "GET"}}),
			want:  false,
		},
		{
			name: "matching neq filter",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {{Entity: "http", Attribute: "method", Operator: OpNeq, Value: "POST"}},
			}},
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "GET"}}),
			want:  true,
		},
		{
			name: "matching in filter",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {{Entity: "http", Attribute: "method", Operator: OpIn, Value: "GET,POST,PUT"}},
			}},
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "POST"}}),
			want:  true,
		},
		{
			name: "non-matching in filter",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {{Entity: "http", Attribute: "method", Operator: OpIn, Value: "GET,POST,PUT"}},
			}},
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "DELETE"}}),
			want:  false,
		},
		{
			name: "matching gte filter",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {{Entity: "http", Attribute: "status", Operator: OpGte, Value: "400"}},
			}},
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"status": 500}}),
			want:  true,
		},
		{
			name: "non-matching gte filter",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {{Entity: "http", Attribute: "status", Operator: OpGte, Value: "400"}},
			}},
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"status": 200}}),
			want:  false,
		},
		{
			name: "matching prefix filter",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {{Entity: "http", Attribute: "path", Operator: OpPrefix, Value: "/api/"}},
			}},
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"path": "/api/users/123"}}),
			want:  true,
		},
		{
			name: "matching contains filter",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {{Entity: "http", Attribute: "path", Operator: OpContains, Value: "users"}},
			}},
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"path": "/api/users/123"}}),
			want:  true,
		},
		{
			name: "multiple conditions AND logic - all match",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {
					{Entity: "http", Attribute: "method", Operator: OpEq, Value: "POST"},
					{Entity: "http", Attribute: "status", Operator: OpGte, Value: "400"},
				},
			}},
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "POST", "status": 500}}),
			want:  true,
		},
		{
			name: "multiple conditions AND logic - one fails",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {
					{Entity: "http", Attribute: "method", Operator: OpEq, Value: "POST"},
					{Entity: "http", Attribute: "status", Operator: OpGte, Value: "400"},
				},
			}},
			event: NewEvent("request.created", map[string]any{"data": map[string]any{"method": "POST", "status": 200}}),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.Matches(tt.event, nil)
			if got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCrossEntityFiltering(t *testing.T) {
	tests := []struct {
		name   string
		filter *EventFilter
		event  *Event
		cache  *ConnectionCache
		want   bool
	}{
		{
			name: "process filter on connection event - matching pid",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "1234"}},
			}},
			event: NewEvent("connection.opened", map[string]any{
				"data": map[string]any{
					"source": map[string]any{
						"pid": uint32(1234),
					},
				},
			}),
			want: true,
		},
		{
			name: "process filter on connection event - non-matching pid",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "1234"}},
			}},
			event: NewEvent("connection.opened", map[string]any{
				"data": map[string]any{
					"source": map[string]any{
						"pid": uint32(5678),
					},
				},
			}),
			want: false,
		},
		{
			name: "process filter on connection event - matching container",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "container", Operator: OpEq, Value: "nginx"}},
			}},
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
			name: "process filter on process event - still works directly",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "1234"}},
			}},
			event: NewEvent("process.started", map[string]any{"pid": 1234}),
			want:  true,
		},
		{
			name: "http filter does not apply to connection",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {{Entity: "http", Attribute: "method", Operator: OpEq, Value: "POST"}},
			}},
			event: NewEvent("connection.opened", map[string]any{
				"data": map[string]any{"direction": "egress"},
			}),
			want: true, // passes through - http filter doesn't apply to connection
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.Matches(tt.event, tt.cache)
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
		"source": map[string]any{
			"pid":      uint32(1234),
			"hostname": "web-server",
			"container": map[string]any{
				"name": "nginx",
				"id":   "abc123",
			},
		},
		"destination": map[string]any{
			"address": map[string]any{
				"ip":   "10.0.0.1",
				"port": 443,
			},
		},
	})

	tests := []struct {
		name   string
		filter *EventFilter
		event  *Event
		want   bool
	}{
		{
			name: "process filter on request - matching via cache lookup",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "1234"}},
			}},
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
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "9999"}},
			}},
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
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "container", Operator: OpEq, Value: "nginx"}},
			}},
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
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"connection": {{Entity: "connection", Attribute: "dstPort", Operator: OpEq, Value: "443"}},
			}},
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
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "1234"}},
			}},
			event: NewEvent("request.created", map[string]any{
				"data": map[string]any{
					"connectionId": "unknown-conn",
					"method":       "GET",
				},
			}),
			want: false, // Can't verify process.pid without connection data
		},
		{
			name: "combined cross-entity and direct filter",
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "1234"}},
				"http":    {{Entity: "http", Attribute: "status", Operator: OpGte, Value: "400"}},
			}},
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
			filter: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "1234"}},
				"http":    {{Entity: "http", Attribute: "status", Operator: OpGte, Value: "400"}},
			}},
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
			got := tt.filter.Matches(tt.event, cache)
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

func TestExtractProcessAttribute(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		attr string
		want any
	}{
		{
			name: "pid",
			data: map[string]any{"pid": 1234},
			attr: "pid",
			want: 1234,
		},
		{
			name: "binary",
			data: map[string]any{"binary": "nginx"},
			attr: "binary",
			want: "nginx",
		},
		{
			name: "container name",
			data: map[string]any{"container": map[string]any{"name": "web-container", "id": "abc123"}},
			attr: "container",
			want: "web-container",
		},
		{
			name: "container id",
			data: map[string]any{"container": map[string]any{"name": "web-container", "id": "abc123"}},
			attr: "containerId",
			want: "abc123",
		},
		{
			name: "pod name",
			data: map[string]any{"pod": map[string]any{"name": "web-pod", "namespace": "default"}},
			attr: "pod",
			want: "web-pod",
		},
		{
			name: "pod namespace",
			data: map[string]any{"pod": map[string]any{"name": "web-pod", "namespace": "default"}},
			attr: "podNamespace",
			want: "default",
		},
		{
			name: "user name",
			data: map[string]any{"user": map[string]any{"name": "root", "id": uint(0)}},
			attr: "user",
			want: "root",
		},
		{
			name: "missing attribute",
			data: map[string]any{"pid": 1234},
			attr: "nonexistent",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractProcessAttribute(tt.data, tt.attr)
			if got != tt.want {
				t.Errorf("extractProcessAttribute() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractConnectionAttribute(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		attr string
		want any
	}{
		{
			name: "direction",
			data: map[string]any{"data": map[string]any{"direction": "egress"}},
			attr: "direction",
			want: "egress",
		},
		{
			name: "protocol",
			data: map[string]any{"data": map[string]any{"l7Protocol": "http2"}},
			attr: "protocol",
			want: "http2",
		},
		{
			name: "srcIp",
			data: map[string]any{"data": map[string]any{
				"source": map[string]any{
					"address": map[string]any{"ip": "192.168.1.1", "port": 8080},
				},
			}},
			attr: "srcIp",
			want: "192.168.1.1",
		},
		{
			name: "dstPort",
			data: map[string]any{"data": map[string]any{
				"destination": map[string]any{
					"address": map[string]any{"ip": "10.0.0.1", "port": 443},
				},
			}},
			attr: "dstPort",
			want: 443,
		},
		{
			name: "container from source",
			data: map[string]any{"data": map[string]any{
				"source": map[string]any{
					"container": map[string]any{"name": "web-app"},
				},
			}},
			attr: "container",
			want: "web-app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractConnectionAttribute(tt.data, tt.attr)
			if got != tt.want {
				t.Errorf("extractConnectionAttribute() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompareEqual(t *testing.T) {
	tests := []struct {
		name        string
		value       any
		filterValue string
		want        bool
	}{
		{"string match", "hello", "hello", true},
		{"string no match", "hello", "world", false},
		{"int match", 42, "42", true},
		{"int no match", 42, "43", false},
		{"int64 match", int64(1234567890), "1234567890", true},
		{"float64 match", float64(200), "200", true},
		{"float64 decimal", float64(3.14), "3.14", true},
		{"uint32 match", uint32(443), "443", true},
		{"uint16 match", uint16(8080), "8080", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareEqual(tt.value, tt.filterValue)
			if got != tt.want {
				t.Errorf("compareEqual(%v, %q) = %v, want %v", tt.value, tt.filterValue, got, tt.want)
			}
		})
	}
}

func TestCompareNumeric(t *testing.T) {
	tests := []struct {
		name        string
		value       any
		filterValue string
		op          func(a, b int64) bool
		want        bool
	}{
		{"int gt true", 100, "50", func(a, b int64) bool { return a > b }, true},
		{"int gt false", 50, "100", func(a, b int64) bool { return a > b }, false},
		{"int gte equal", 100, "100", func(a, b int64) bool { return a >= b }, true},
		{"float64 lt", float64(50), "100", func(a, b int64) bool { return a < b }, true},
		{"uint32 lte", uint32(100), "100", func(a, b int64) bool { return a <= b }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareNumeric(tt.value, tt.filterValue, tt.op)
			if got != tt.want {
				t.Errorf("compareNumeric(%v, %q) = %v, want %v", tt.value, tt.filterValue, got, tt.want)
			}
		})
	}
}

func TestExtractHttpDomain(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want any
	}{
		{
			name: "extract domain from url",
			data: map[string]any{"data": map[string]any{"url": "https://api.example.com/v1/users"}},
			want: "api.example.com",
		},
		{
			name: "extract domain with port",
			data: map[string]any{"data": map[string]any{"url": "http://localhost:8080/api"}},
			want: "localhost",
		},
		{
			name: "missing url",
			data: map[string]any{"data": map[string]any{"method": "GET"}},
			want: nil,
		},
		{
			name: "invalid url",
			data: map[string]any{"data": map[string]any{"url": "not-a-url"}},
			want: "",
		},
		{
			name: "http_transaction summary - domain from request_host",
			data: map[string]any{"data": map[string]any{
				"summary": map[string]any{
					"request_host":   "api.slack.com",
					"request_method": "POST",
				},
			}},
			want: "api.slack.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractHttpAttribute(tt.data, "domain")
			if got != tt.want {
				t.Errorf("extractHttpAttribute(domain) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractFromHttpTransactionSummary(t *testing.T) {
	// Simulates the structure of request.http_transaction events from ObjectStore
	data := map[string]any{
		"data": map[string]any{
			"connectionId": "abc123",
			"type":         "http_transaction",
			"summary": map[string]any{
				"request_host":     "api.slack.com",
				"request_method":   "POST",
				"response_status":  float64(200), // JSON numbers are float64
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
	}

	tests := []struct {
		attr string
		want any
	}{
		{"domain", "api.slack.com"},
		{"method", "POST"},
		{"status", float64(200)},
		{"path", "/api/chat.postMessage"},
		{"direction", "egress-external"},
		{"protocol", "http2"},
		{"scheme", "https"},
		{"connectionId", "abc123"},
		{"container", "myapp"},
		{"containerImage", "myapp:latest"},
		{"binary", "/usr/bin/node"},
	}

	for _, tt := range tests {
		t.Run(tt.attr, func(t *testing.T) {
			got := extractHttpAttribute(data, tt.attr)
			if got != tt.want {
				t.Errorf("extractHttpAttribute(%q) = %v, want %v", tt.attr, got, tt.want)
			}
		})
	}
}

func TestParseFiltersFromBody(t *testing.T) {
	tests := []struct {
		name       string
		filters    map[string]string
		wantErr    bool
		wantNil    bool
		wantEntity string
		wantCount  int
	}{
		{
			name:    "nil map",
			filters: nil,
			wantNil: true,
		},
		{
			name:    "empty map",
			filters: map[string]string{},
			wantNil: true,
		},
		{
			name:       "single filter",
			filters:    map[string]string{"process.pid.eq": "1234"},
			wantEntity: "process",
			wantCount:  1,
		},
		{
			name: "multiple filters same entity",
			filters: map[string]string{
				"process.pid.eq":       "1234",
				"process.container.eq": "nginx",
			},
			wantEntity: "process",
			wantCount:  2,
		},
		{
			name: "multiple filters different entities",
			filters: map[string]string{
				"process.pid.eq": "1234",
				"http.method.eq": "POST",
			},
			wantCount: 2, // 1 per entity
		},
		{
			name:    "invalid operator",
			filters: map[string]string{"process.pid.invalid": "1234"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseFiltersFromBody(tt.filters)
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

			if tt.wantNil {
				if filter != nil {
					t.Errorf("expected nil filter, got %+v", filter)
				}
				return
			}

			if tt.wantEntity != "" {
				conditions, ok := filter.Conditions[tt.wantEntity]
				if !ok {
					t.Errorf("expected conditions for entity %q", tt.wantEntity)
					return
				}
				if len(conditions) != tt.wantCount {
					t.Errorf("expected %d conditions, got %d", tt.wantCount, len(conditions))
				}
			} else if tt.wantCount > 0 {
				// Count total conditions across all entities
				total := 0
				for _, conds := range filter.Conditions {
					total += len(conds)
				}
				if total != tt.wantCount {
					t.Errorf("expected %d total conditions, got %d", tt.wantCount, total)
				}
			}
		})
	}
}

func TestMergeFilters(t *testing.T) {
	tests := []struct {
		name     string
		base     *EventFilter
		override *EventFilter
		wantNil  bool
		check    func(*EventFilter) bool
	}{
		{
			name:     "both nil",
			base:     nil,
			override: nil,
			wantNil:  true,
		},
		{
			name: "nil base, non-nil override",
			base: nil,
			override: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "1234"}},
			}},
			check: func(f *EventFilter) bool {
				return len(f.Conditions["process"]) == 1 && f.Conditions["process"][0].Value == "1234"
			},
		},
		{
			name: "non-nil base, nil override",
			base: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "1234"}},
			}},
			override: nil,
			check: func(f *EventFilter) bool {
				return len(f.Conditions["process"]) == 1 && f.Conditions["process"][0].Value == "1234"
			},
		},
		{
			name: "non-nil base, empty override",
			base: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "1234"}},
			}},
			override: &EventFilter{Conditions: map[string][]FilterCondition{}},
			check: func(f *EventFilter) bool {
				return len(f.Conditions["process"]) == 1 && f.Conditions["process"][0].Value == "1234"
			},
		},
		{
			name: "override replaces same entity",
			base: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "1234"}},
			}},
			override: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "5678"}},
			}},
			check: func(f *EventFilter) bool {
				return len(f.Conditions["process"]) == 1 && f.Conditions["process"][0].Value == "5678"
			},
		},
		{
			name: "merge different entities",
			base: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "1234"}},
			}},
			override: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {{Entity: "http", Attribute: "method", Operator: OpEq, Value: "POST"}},
			}},
			check: func(f *EventFilter) bool {
				return len(f.Conditions["process"]) == 1 &&
					f.Conditions["process"][0].Value == "1234" &&
					len(f.Conditions["http"]) == 1 &&
					f.Conditions["http"][0].Value == "POST"
			},
		},
		{
			name: "override one entity, keep another",
			base: &EventFilter{Conditions: map[string][]FilterCondition{
				"process": {{Entity: "process", Attribute: "pid", Operator: OpEq, Value: "1234"}},
				"http":    {{Entity: "http", Attribute: "method", Operator: OpEq, Value: "GET"}},
			}},
			override: &EventFilter{Conditions: map[string][]FilterCondition{
				"http": {{Entity: "http", Attribute: "method", Operator: OpEq, Value: "POST"}},
			}},
			check: func(f *EventFilter) bool {
				return len(f.Conditions["process"]) == 1 &&
					f.Conditions["process"][0].Value == "1234" &&
					len(f.Conditions["http"]) == 1 &&
					f.Conditions["http"][0].Value == "POST"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeFilters(tt.base, tt.override)
			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Error("expected non-nil result")
				return
			}
			if tt.check != nil && !tt.check(result) {
				t.Errorf("check failed for result: %+v", result.Conditions)
			}
		})
	}
}
