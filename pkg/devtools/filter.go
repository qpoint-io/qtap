package devtools

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/qpoint-io/qtap/pkg/rulekitext"
	"github.com/qpoint-io/rulekit"
)

/*
Filter Fields Per Topic

This package supports filtering events using rulekit expressions. Each topic has
its own set of available fields. Native properties (belonging to the event type)
have no prefix. Relational properties (from associated entities) are prefixed.

Process Topic:
  Native properties - no prefix needed.
    binary              Process executable name
    path                Full executable path
    hostname            Process hostname
    user.name           Username
    user.id             User ID
    container.name      Container name
    container.id        Container ID
    container.image     Container image
    container.labels.*  Container labels (e.g., container.labels.app)
    pod.name            Pod name
    pod.namespace       Pod namespace
    pod.labels.*        Pod labels (e.g., pod.labels.app)

Connection Topic:
  Native connection properties + relational process, container, and pod fields.
    direction           Connection direction (egress, ingress)
    protocol            L7 protocol (http, http2, etc.)
    type                Socket type (tcp, udp)
    src.ip              Source IP address
    src.port            Source port
    dst.ip              Destination IP address
    dst.port            Destination port
    dst.domain          Destination domain
    tls.enabled         TLS enabled
    tls.version         TLS version
    tls.sni             TLS SNI
    process.binary      Process executable name
    process.path        Full executable path
    process.hostname    Process hostname
    process.user.name   Username
    process.user.id     User ID
    container.name      Container name
    container.id        Container ID
    container.image     Container image
    container.labels.*  Container labels
    pod.name            Pod name
    pod.namespace       Pod namespace
    pod.labels.*        Pod labels

HTTP Topic:
  Native HTTP properties (req and res) + relational connection/process fields.
    req.method          HTTP method (GET, POST, etc.)
    req.path            Request path
    req.host            Request host
    req.url             Full URL
    res.status          Response status code
    direction           Connection direction
    protocol            L7 protocol
    src.ip              Source IP address
    src.port            Source port
    dst.ip              Destination IP address
    dst.port            Destination port
    dst.domain          Destination domain
    tls.enabled         TLS enabled
    tls.version         TLS version
    tls.sni             TLS SNI
    process.binary      Process executable name
    process.path        Full executable path
    process.hostname    Process hostname
    process.user.name   Username
    process.user.id     User ID
    container.name      Container name
    container.id        Container ID
    container.image     Container image
    container.labels.*  Container labels
    pod.name            Pod name
    pod.namespace       Pod namespace
    pod.labels.*        Pod labels

Example Filters:
  Process:    binary == 'nginx'
  Process:    container.name == 'web' AND pod.namespace == 'production'
  Connection: dst.port == 443 AND process.binary == 'curl'
  Connection: direction == 'egress' AND dst.domain contains 'example.com'
  HTTP:       res.status >= 400 AND req.method == 'POST'
  HTTP:       in_zone(req.host, 'example.com') AND container.name == 'api'
*/

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

// buildEventKV creates a nested key-value map from event data for rulekit evaluation.
// The structure follows the principle: native properties have no prefix, relational properties are prefixed.
func buildEventKV(event *Event, cache *ConnectionCache) rulekit.KV {
	kv := make(rulekit.KV)

	data, ok := event.Data.(map[string]any)
	if !ok {
		return kv
	}

	switch event.TopLevelTopic() {
	case "process":
		// Native properties - no prefix
		addProcessFieldsNative(kv, data)
		addContainerFieldsNative(kv, data)
		addPodFieldsNative(kv, data)

	case "connection":
		connData := toMapAny(data["data"])
		if connData == nil {
			connData = data
		}
		// Native connection properties - no prefix
		addConnectionFieldsNative(kv, connData)
		// Relational properties - prefixed
		addProcessFieldsRelational(kv, connData)
		// Container/pod stay unprefixed
		addContainerFieldsFromConnection(kv, connData)
		addPodFieldsFromConnection(kv, connData)

	case "request":
		// Native HTTP properties - req.*/res.* (no http. prefix)
		addHttpFieldsNative(kv, data)
		// Lookup connection data from cache for relational fields
		if connData := lookupConnection(data, cache); connData != nil {
			addConnectionFieldsNative(kv, connData)
			addProcessFieldsRelational(kv, connData)
			addContainerFieldsFromConnection(kv, connData)
			addPodFieldsFromConnection(kv, connData)
		}
	}

	return kv
}

// addProcessFieldsNative adds process fields at root level (no prefix) for process topic
func addProcessFieldsNative(kv rulekit.KV, data map[string]any) {
	setIfNotNil(kv, "binary", data["binary"])
	setIfNotNil(kv, "path", data["path"])
	setIfNotNil(kv, "hostname", data["hostname"])
	if user := toMapAny(data["user"]); user != nil {
		kv["user"] = user
	}
}

// addProcessFieldsRelational adds process fields with process.* prefix for connection/http topics
func addProcessFieldsRelational(kv rulekit.KV, connData map[string]any) {
	source := toMapAny(connData["source"])
	if source == nil {
		return
	}

	process := make(map[string]any)
	setIfNotNil(process, "hostname", source["hostname"])
	if exe, ok := source["exe"].(string); ok {
		process["path"] = exe
		parts := strings.Split(exe, "/")
		process["binary"] = parts[len(parts)-1]
	}
	if user := toMapAny(source["user"]); user != nil {
		process["user"] = user
	} else {
		// Handle flat user fields
		user := make(map[string]any)
		setIfNotNil(user, "name", source["user"])
		setIfNotNil(user, "id", source["userId"])
		if len(user) > 0 {
			process["user"] = user
		}
	}
	if len(process) > 0 {
		kv["process"] = process
	}
}

// addContainerFieldsNative adds container fields at root level for process topic
func addContainerFieldsNative(kv rulekit.KV, data map[string]any) {
	if container := toMapAny(data["container"]); container != nil {
		kv["container"] = container
	}
}

// addContainerFieldsFromConnection adds container fields at root level from connection source
func addContainerFieldsFromConnection(kv rulekit.KV, connData map[string]any) {
	source := toMapAny(connData["source"])
	if source == nil {
		return
	}
	if container := toMapAny(source["container"]); container != nil {
		kv["container"] = container
	}
}

// addPodFieldsNative adds pod fields at root level for process topic
func addPodFieldsNative(kv rulekit.KV, data map[string]any) {
	if pod := toMapAny(data["pod"]); pod != nil {
		kv["pod"] = pod
	}
}

// addPodFieldsFromConnection adds pod fields at root level from connection source
func addPodFieldsFromConnection(kv rulekit.KV, connData map[string]any) {
	source := toMapAny(connData["source"])
	if source == nil {
		return
	}
	// Pod may be nested under container
	if container := toMapAny(source["container"]); container != nil {
		if pod := toMapAny(container["pod"]); pod != nil {
			kv["pod"] = pod
		}
	}
}

// addConnectionFieldsNative adds connection fields (direction, protocol, src.*, dst.*, tls.*)
func addConnectionFieldsNative(kv rulekit.KV, connData map[string]any) {
	setIfNotNil(kv, "direction", connData["direction"])
	setIfNotNil(kv, "protocol", connData["l7Protocol"])
	setIfNotNil(kv, "type", connData["socketProtocol"])

	// src (network address only - process info goes in process.*)
	if source := toMapAny(connData["source"]); source != nil {
		src := make(map[string]any)
		if addr := toMapAny(source["address"]); addr != nil {
			setIfNotNil(src, "ip", addr["ip"])
			setIfNotNil(src, "port", addr["port"])
		}
		if len(src) > 0 {
			kv["src"] = src
		}
	}

	// dst (network address + domain)
	if dest := toMapAny(connData["destination"]); dest != nil {
		dst := make(map[string]any)
		if addr := toMapAny(dest["address"]); addr != nil {
			setIfNotNil(dst, "ip", addr["ip"])
			setIfNotNil(dst, "port", addr["port"])
		}
		setIfNotNil(dst, "domain", dest["domain"])
		if len(dst) > 0 {
			kv["dst"] = dst
		}
	}

	// tls
	if tls := toMapAny(connData["tls"]); tls != nil {
		kv["tls"] = tls
	}
}

// addHttpFieldsNative adds HTTP fields (req.*, res.* at root - no http. prefix)
func addHttpFieldsNative(kv rulekit.KV, data map[string]any) {
	httpData := toMapAny(data["data"])
	if httpData == nil {
		return
	}

	req := make(map[string]any)
	res := make(map[string]any)

	// Check for summary format (http_transaction)
	if summary := toMapAny(httpData["summary"]); summary != nil {
		setIfNotNil(req, "method", summary["request_method"])
		setIfNotNil(req, "path", summary["request_path"])
		setIfNotNil(req, "host", summary["request_host"])
		setIfNotNil(res, "status", summary["response_status"])
	} else {
		// Standard request format
		setIfNotNil(req, "method", httpData["method"])
		setIfNotNil(req, "path", httpData["path"])
		setIfNotNil(req, "url", httpData["url"])
		// Extract host from URL
		if urlStr, ok := httpData["url"].(string); ok {
			if parsed, err := url.Parse(urlStr); err == nil {
				req["host"] = parsed.Hostname()
			}
		}
		setIfNotNil(res, "status", httpData["status"])
	}

	if len(req) > 0 {
		kv["req"] = req
	}
	if len(res) > 0 {
		kv["res"] = res
	}
}

// lookupConnection retrieves connection data from cache by connectionId
func lookupConnection(data map[string]any, cache *ConnectionCache) map[string]any {
	if cache == nil {
		return nil
	}
	reqData := toMapAny(data["data"])
	if reqData == nil {
		return nil
	}
	connId, ok := reqData["connectionId"].(string)
	if !ok {
		return nil
	}
	return cache.Get(connId)
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
