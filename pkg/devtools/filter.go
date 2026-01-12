package devtools

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/qpoint-io/qtap/pkg/rulekitext"
	"github.com/qpoint-io/rulekit"
)

// EventFilter holds a compiled rulekit expression for filtering events
type EventFilter struct {
	expr string       // original expression string
	rule rulekit.Rule // compiled rule
}

// ParseFilter parses a rulekit expression string into an EventFilter
func ParseFilter(expr string) (*EventFilter, error) {
	if expr == "" {
		return nil, nil
	}

	rule, err := rulekit.Parse(expr)
	if err != nil {
		return nil, err
	}

	return &EventFilter{
		expr: expr,
		rule: rule,
	}, nil
}

// IsEmpty returns true if no filter is defined
func (f *EventFilter) IsEmpty() bool {
	return f == nil || f.rule == nil
}

// Expression returns the original expression string
func (f *EventFilter) Expression() string {
	if f == nil {
		return ""
	}
	return f.expr
}

// Matches checks if an event matches the filter expression.
// The cache parameter enables cross-entity filtering by looking up connection data
// for request events.
func (f *EventFilter) Matches(event *Event, cache *ConnectionCache) bool {
	if f.IsEmpty() {
		return true
	}

	// System events always pass through
	if event.TopLevelTopic() == "system" {
		return true
	}

	// Build KV map from event data
	kv := buildEventKV(event, cache)

	// Evaluate rule
	res := f.rule.Eval(&rulekit.Ctx{
		Functions: rulekitext.Functions,
		KV:        kv,
	})

	// Missing fields are not errors - they just don't match
	if !res.Ok() {
		// Critical errors (invalid syntax, etc.) should not match
		// Missing fields also don't match
		return false
	}

	return res.Pass()
}

// buildEventKV creates a flattened key-value map from event data for rulekit evaluation
func buildEventKV(event *Event, cache *ConnectionCache) rulekit.KV {
	kv := make(rulekit.KV)

	data, ok := event.Data.(map[string]any)
	if !ok {
		return kv
	}

	eventEntity := event.TopLevelTopic()

	switch eventEntity {
	case "process":
		addProcessKV(kv, data)

	case "connection":
		addConnectionKV(kv, data)
		// Connection events include process data from source endpoint
		addProcessFromConnectionKV(kv, data)

	case "request":
		addHttpKV(kv, data)
		// Request events need cache lookups for connection/process data
		addConnectionFromCacheKV(kv, data, cache)
		addProcessFromCacheKV(kv, data, cache)
	}

	return kv
}

// addProcessKV adds process.* keys to the KV map from process event data
func addProcessKV(kv rulekit.KV, data map[string]any) {
	setIfNotNil(kv, "process.pid", data["pid"])
	setIfNotNil(kv, "process.binary", data["binary"])
	setIfNotNil(kv, "process.path", data["path"])
	setIfNotNil(kv, "process.hostname", data["hostname"])

	if user, ok := data["user"].(map[string]any); ok {
		setIfNotNil(kv, "process.user", user["name"])
		setIfNotNil(kv, "process.userId", user["id"])
	}

	if container, ok := data["container"].(map[string]any); ok {
		setIfNotNil(kv, "process.container", container["name"])
		setIfNotNil(kv, "process.containerId", container["id"])
		setIfNotNil(kv, "process.containerImage", container["image"])
	}

	if pod, ok := data["pod"].(map[string]any); ok {
		setIfNotNil(kv, "process.pod", pod["name"])
		setIfNotNil(kv, "process.podNamespace", pod["namespace"])
	}
}

// addConnectionKV adds connection.* keys to the KV map from connection event data
func addConnectionKV(kv rulekit.KV, data map[string]any) {
	// Connection data is nested under "data" key
	connData := toMapAny(data["data"])
	if connData == nil {
		connData = data
	}

	setIfNotNil(kv, "connection.direction", connData["direction"])
	setIfNotNil(kv, "connection.protocol", connData["l7Protocol"])
	setIfNotNil(kv, "connection.socketProtocol", connData["socketProtocol"])
	setIfNotNil(kv, "connection.vendorId", connData["vendorId"])
	setIfNotNil(kv, "connection.tlsVersion", connData["tlsVersion"])

	// Source endpoint
	if src := toMapAny(connData["source"]); src != nil {
		if addr := toMapAny(src["address"]); addr != nil {
			setIfNotNil(kv, "connection.srcIp", addr["ip"])
			setIfNotNil(kv, "connection.srcPort", addr["port"])
		}
	}

	// Destination endpoint
	if dst := toMapAny(connData["destination"]); dst != nil {
		if addr := toMapAny(dst["address"]); addr != nil {
			setIfNotNil(kv, "connection.dstIp", addr["ip"])
			setIfNotNil(kv, "connection.dstPort", addr["port"])
		}
	}
}

// addProcessFromConnectionKV extracts process.* keys from a connection's source endpoint
func addProcessFromConnectionKV(kv rulekit.KV, data map[string]any) {
	// Connection data is nested under "data" key
	connData := toMapAny(data["data"])
	if connData == nil {
		connData = data
	}

	// Get source endpoint (local process info)
	source := toMapAny(connData["source"])
	if source == nil {
		return
	}

	setIfNotNil(kv, "process.pid", source["pid"])
	setIfNotNil(kv, "process.hostname", source["hostname"])
	setIfNotNil(kv, "process.user", source["user"])
	setIfNotNil(kv, "process.userId", source["userId"])
	setIfNotNil(kv, "process.path", source["exe"])

	// Extract binary name from exe path
	if exe, ok := source["exe"].(string); ok {
		parts := strings.Split(exe, "/")
		if len(parts) > 0 {
			kv["process.binary"] = parts[len(parts)-1]
		}
	}

	if container := toMapAny(source["container"]); container != nil {
		setIfNotNil(kv, "process.container", container["name"])
		setIfNotNil(kv, "process.containerId", container["id"])
		setIfNotNil(kv, "process.containerImage", container["image"])

		if pod := toMapAny(container["pod"]); pod != nil {
			setIfNotNil(kv, "process.pod", pod["name"])
			setIfNotNil(kv, "process.podNamespace", pod["namespace"])
		}
	}
}

// addHttpKV adds http.* keys to the KV map from request event data
func addHttpKV(kv rulekit.KV, data map[string]any) {
	httpData := toMapAny(data["data"])
	if httpData == nil {
		httpData = data
	}

	// Check for http_transaction summary format
	if summary := toMapAny(httpData["summary"]); summary != nil {
		addHttpFromSummaryKV(kv, summary)
		return
	}

	// Standard request.created format
	setIfNotNil(kv, "http.method", httpData["method"])
	setIfNotNil(kv, "http.status", httpData["status"])
	setIfNotNil(kv, "http.path", httpData["path"])
	setIfNotNil(kv, "http.url", httpData["url"])
	setIfNotNil(kv, "http.direction", httpData["direction"])
	setIfNotNil(kv, "http.category", httpData["category"])
	setIfNotNil(kv, "http.contentType", httpData["contentType"])
	setIfNotNil(kv, "http.agent", httpData["agent"])
	setIfNotNil(kv, "http.duration", httpData["duration"])
	setIfNotNil(kv, "http.bytesSent", httpData["bytesSent"])
	setIfNotNil(kv, "http.bytesReceived", httpData["bytesReceived"])
	setIfNotNil(kv, "http.connectionId", httpData["connectionId"])
	setIfNotNil(kv, "http.requestId", httpData["requestId"])

	// Extract domain from URL
	if urlStr, ok := httpData["url"].(string); ok {
		if parsed, err := url.Parse(urlStr); err == nil {
			kv["http.domain"] = parsed.Hostname()
		}
	}
}

// addHttpFromSummaryKV extracts http.* keys from http_transaction summary data
func addHttpFromSummaryKV(kv rulekit.KV, summary map[string]any) {
	setIfNotNil(kv, "http.method", summary["request_method"])
	setIfNotNil(kv, "http.status", summary["response_status"])
	setIfNotNil(kv, "http.path", summary["request_path"])
	setIfNotNil(kv, "http.domain", summary["request_host"])
	setIfNotNil(kv, "http.direction", summary["direction"])
	setIfNotNil(kv, "http.protocol", summary["request_protocol"])
	setIfNotNil(kv, "http.scheme", summary["request_scheme"])
	setIfNotNil(kv, "http.requestContentType", summary["request_content_type"])
	setIfNotNil(kv, "http.responseContentType", summary["response_content_type"])
	setIfNotNil(kv, "http.duration", summary["duration_ms"])
	setIfNotNil(kv, "http.connectionId", summary["connection_id"])
	setIfNotNil(kv, "http.requestId", summary["request_id"])

	// Also add process info from summary if available
	setIfNotNil(kv, "process.container", summary["container_name"])
	setIfNotNil(kv, "process.containerImage", summary["container_image"])
	setIfNotNil(kv, "process.pod", summary["pod_name"])
	setIfNotNil(kv, "process.podNamespace", summary["pod_namespace"])
	setIfNotNil(kv, "process.binary", summary["process_exe"])
}

// addConnectionFromCacheKV looks up connection data from cache and adds connection.* keys
func addConnectionFromCacheKV(kv rulekit.KV, data map[string]any, cache *ConnectionCache) {
	if cache == nil {
		return
	}

	// Request data is nested under "data" key
	reqData := toMapAny(data["data"])
	if reqData == nil {
		reqData = data
	}

	// Get connectionId from request
	connId, ok := reqData["connectionId"].(string)
	if !ok {
		return
	}

	// Lookup connection from cache
	connData := cache.Get(connId)
	if connData == nil {
		return
	}

	// Add connection attributes
	setIfNotNil(kv, "connection.direction", connData["direction"])
	setIfNotNil(kv, "connection.protocol", connData["l7Protocol"])
	setIfNotNil(kv, "connection.socketProtocol", connData["socketProtocol"])
	setIfNotNil(kv, "connection.vendorId", connData["vendorId"])
	setIfNotNil(kv, "connection.tlsVersion", connData["tlsVersion"])

	if src := toMapAny(connData["source"]); src != nil {
		if addr := toMapAny(src["address"]); addr != nil {
			setIfNotNil(kv, "connection.srcIp", addr["ip"])
			setIfNotNil(kv, "connection.srcPort", addr["port"])
		}
	}

	if dst := toMapAny(connData["destination"]); dst != nil {
		if addr := toMapAny(dst["address"]); addr != nil {
			setIfNotNil(kv, "connection.dstIp", addr["ip"])
			setIfNotNil(kv, "connection.dstPort", addr["port"])
		}
	}
}

// addProcessFromCacheKV looks up connection data from cache and adds process.* keys
func addProcessFromCacheKV(kv rulekit.KV, data map[string]any, cache *ConnectionCache) {
	if cache == nil {
		return
	}

	// Request data is nested under "data" key
	reqData := toMapAny(data["data"])
	if reqData == nil {
		reqData = data
	}

	// Get connectionId from request
	connId, ok := reqData["connectionId"].(string)
	if !ok {
		return
	}

	// Lookup connection from cache
	connData := cache.Get(connId)
	if connData == nil {
		return
	}

	// Extract process data from connection's source
	addProcessFromConnectionKV(kv, map[string]any{"data": connData})
}

// setIfNotNil sets a key in the KV map only if the value is not nil
func setIfNotNil(kv rulekit.KV, key string, value any) {
	if value != nil {
		kv[key] = value
	}
}

// toMapAny converts a value to map[string]any.
// If the value is already a map[string]any, it returns it directly.
// If the value is a struct or pointer to struct, it JSON marshals/unmarshals to convert.
// Returns nil if conversion fails.
func toMapAny(v any) map[string]any {
	if v == nil {
		return nil
	}

	// Already a map
	if m, ok := v.(map[string]any); ok {
		return m
	}

	// Convert struct via JSON marshal/unmarshal
	jsonBytes, err := json.Marshal(v)
	if err != nil {
		return nil
	}

	var result map[string]any
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return nil
	}

	return result
}
