package devtools

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// FilterOperator defines comparison operations for event filtering
type FilterOperator string

const (
	OpEq       FilterOperator = "eq"
	OpNeq      FilterOperator = "neq"
	OpIn       FilterOperator = "in"
	OpGt       FilterOperator = "gt"
	OpGte      FilterOperator = "gte"
	OpLt       FilterOperator = "lt"
	OpLte      FilterOperator = "lte"
	OpPrefix   FilterOperator = "prefix"
	OpContains FilterOperator = "contains"
)

// validOperators is the set of recognized operators
var validOperators = map[FilterOperator]struct{}{
	OpEq:       {},
	OpNeq:      {},
	OpIn:       {},
	OpGt:       {},
	OpGte:      {},
	OpLt:       {},
	OpLte:      {},
	OpPrefix:   {},
	OpContains: {},
}

// FilterCondition represents a single filter condition
type FilterCondition struct {
	Entity    string         // e.g., "process", "request", "connection"
	Attribute string         // e.g., "pid", "method", "container"
	Operator  FilterOperator // e.g., "eq", "gte", "prefix"
	Value     string         // raw value from query param
}

// EventFilter holds parsed filter conditions grouped by entity
type EventFilter struct {
	Conditions map[string][]FilterCondition // entity -> conditions
}

// ParseFilters parses query parameters into an EventFilter.
// Query params should be in the format: entity.attribute.operator=value
// Example: process.pid.eq=1234, request.status.gte=400
func ParseFilters(query url.Values) (*EventFilter, error) {
	filter := &EventFilter{
		Conditions: make(map[string][]FilterCondition),
	}

	for key, values := range query {
		// Skip empty values
		if len(values) == 0 || values[0] == "" {
			continue
		}

		// Parse the key: entity.attribute.operator
		parts := strings.Split(key, ".")
		if len(parts) != 3 {
			// Not a filter param, skip (could be other query params)
			continue
		}

		entity := parts[0]
		attribute := parts[1]
		operator := FilterOperator(parts[2])

		// Validate operator
		if _, ok := validOperators[operator]; !ok {
			return nil, fmt.Errorf("invalid operator %q in filter %q", operator, key)
		}

		// Create condition (use first value if multiple provided)
		condition := FilterCondition{
			Entity:    entity,
			Attribute: attribute,
			Operator:  operator,
			Value:     values[0],
		}

		filter.Conditions[entity] = append(filter.Conditions[entity], condition)
	}

	return filter, nil
}

// IsEmpty returns true if no filter conditions are defined
func (f *EventFilter) IsEmpty() bool {
	return f == nil || len(f.Conditions) == 0
}

// entityAppliesTo defines which event types each filter entity applies to.
// This enables cross-entity filtering (e.g., process.* filters apply to connection events).
// Note: "http" filter entity applies to "request" topic events (request.created, request.http_transaction)
var entityAppliesTo = map[string]map[string]bool{
	"process":    {"process": true, "connection": true, "request": true, "issue": true, "pii": true},
	"connection": {"connection": true, "request": true, "issue": true, "pii": true},
	"http":       {"request": true},
	"issue":      {"issue": true},
	"pii":        {"pii": true},
}

// Matches checks if an event matches the filter conditions.
// The cache parameter enables cross-entity filtering by looking up connection data
// for request/issue/pii events.
// Returns true if:
// - No applicable conditions exist for this event's entity type (pass through)
// - All applicable conditions match (AND logic)
func (f *EventFilter) Matches(event *Event, cache *ConnectionCache) bool {
	if f.IsEmpty() {
		return true
	}

	// Extract entity from topic (e.g., "request.created" -> "request")
	eventEntity := event.TopLevelTopic()

	// System events always pass through
	if eventEntity == "system" {
		return true
	}

	// Extract the data from the event
	data, ok := event.Data.(map[string]any)
	if !ok {
		// Can't extract data, pass through
		return true
	}

	// Check each filter entity's conditions
	for filterEntity, conditions := range f.Conditions {
		// Check if this filter entity applies to the event's entity type
		appliesTo, exists := entityAppliesTo[filterEntity]
		if !exists || !appliesTo[eventEntity] {
			// This filter entity doesn't apply to this event type, skip
			continue
		}

		// All conditions for this filter entity must match
		for _, cond := range conditions {
			attrValue := extractAttributeWithCache(filterEntity, eventEntity, data, cond.Attribute, cache)
			if !matchCondition(attrValue, cond) {
				return false
			}
		}
	}

	return true
}

// extractAttributeWithCache extracts an attribute value, using cache for cross-entity lookups
func extractAttributeWithCache(filterEntity, eventEntity string, data map[string]any, attr string, cache *ConnectionCache) any {
	// Same entity - direct extraction
	if filterEntity == eventEntity {
		return extractAttribute(filterEntity, data, attr)
	}

	// "http" filter entity maps to "request" event entity
	if filterEntity == "http" && eventEntity == "request" {
		return extractAttribute("http", data, attr)
	}

	// Cross-entity extraction
	switch {
	case filterEntity == "process" && eventEntity == "connection":
		// Connection events have process data embedded in source
		return extractProcessFromConnection(data, attr)

	case filterEntity == "process" && (eventEntity == "request" || eventEntity == "issue" || eventEntity == "pii"):
		// Need to lookup connection, then extract process data
		return extractProcessFromRequest(data, cache, attr)

	case filterEntity == "connection" && (eventEntity == "request" || eventEntity == "issue" || eventEntity == "pii"):
		// Need to lookup connection data from cache
		return extractConnectionFromRequest(data, cache, attr)
	}

	return nil
}

// extractProcessFromConnection extracts process attributes from connection event data
// Connection events have process info embedded in the source endpoint
func extractProcessFromConnection(data map[string]any, attr string) any {
	// Connection data is nested under "data" key
	connData := toMapAny(data["data"])
	if connData == nil {
		connData = data
	}

	// Get source endpoint (local process info)
	source := toMapAny(connData["source"])
	if source == nil {
		return nil
	}

	switch attr {
	case "pid":
		return source["pid"]
	case "hostname":
		return source["hostname"]
	case "user":
		return source["user"]
	case "userId":
		return source["userId"]
	case "path":
		return source["exe"]
	case "binary":
		// Extract basename from exe path
		if exe, ok := source["exe"].(string); ok {
			parts := strings.Split(exe, "/")
			if len(parts) > 0 {
				return parts[len(parts)-1]
			}
		}
		return nil
	case "container":
		if container := toMapAny(source["container"]); container != nil {
			return container["name"]
		}
		return nil
	case "containerId":
		if container := toMapAny(source["container"]); container != nil {
			return container["id"]
		}
		return nil
	case "containerImage":
		if container := toMapAny(source["container"]); container != nil {
			return container["image"]
		}
		return nil
	case "pod":
		if container := toMapAny(source["container"]); container != nil {
			if pod := toMapAny(container["pod"]); pod != nil {
				return pod["name"]
			}
		}
		return nil
	case "podNamespace":
		if container := toMapAny(source["container"]); container != nil {
			if pod := toMapAny(container["pod"]); pod != nil {
				return pod["namespace"]
			}
		}
		return nil
	}

	return nil
}

// extractConnectionFromRequest extracts connection attributes from request data via cache lookup
func extractConnectionFromRequest(data map[string]any, cache *ConnectionCache, attr string) any {
	if cache == nil {
		return nil
	}

	// Request data is nested under "data" key
	reqData := toMapAny(data["data"])
	if reqData == nil {
		reqData = data
	}

	// Get connectionId from request
	connId, ok := reqData["connectionId"].(string)
	if !ok {
		return nil
	}

	// Lookup connection from cache
	connData := cache.Get(connId)
	if connData == nil {
		return nil
	}

	// Extract the attribute from connection data
	return extractConnectionAttribute(map[string]any{"data": connData}, attr)
}

// extractProcessFromRequest extracts process attributes from request data via cache lookup
func extractProcessFromRequest(data map[string]any, cache *ConnectionCache, attr string) any {
	if cache == nil {
		return nil
	}

	// Request data is nested under "data" key
	reqData := toMapAny(data["data"])
	if reqData == nil {
		reqData = data
	}

	// Get connectionId from request
	connId, ok := reqData["connectionId"].(string)
	if !ok {
		return nil
	}

	// Lookup connection from cache
	connData := cache.Get(connId)
	if connData == nil {
		return nil
	}

	// Extract process data from connection's source
	return extractProcessFromConnection(map[string]any{"data": connData}, attr)
}

// extractAttribute extracts an attribute value from event data based on entity type and attribute name
func extractAttribute(entity string, data map[string]any, attr string) any {
	switch entity {
	case "process":
		return extractProcessAttribute(data, attr)
	case "http":
		return extractHttpAttribute(data, attr)
	case "connection":
		return extractConnectionAttribute(data, attr)
	case "issue":
		return extractIssueAttribute(data, attr)
	case "pii":
		return extractPIIAttribute(data, attr)
	default:
		return nil
	}
}

// extractProcessAttribute extracts attributes from process event data
func extractProcessAttribute(data map[string]any, attr string) any {
	switch attr {
	case "pid":
		return data["pid"]
	case "binary":
		return data["binary"]
	case "path":
		return data["path"]
	case "hostname":
		return data["hostname"]
	case "user":
		if user, ok := data["user"].(map[string]any); ok {
			return user["name"]
		}
		return nil
	case "userId":
		if user, ok := data["user"].(map[string]any); ok {
			return user["id"]
		}
		return nil
	case "container":
		if container, ok := data["container"].(map[string]any); ok {
			return container["name"]
		}
		return nil
	case "containerId":
		if container, ok := data["container"].(map[string]any); ok {
			return container["id"]
		}
		return nil
	case "containerImage":
		if container, ok := data["container"].(map[string]any); ok {
			return container["image"]
		}
		return nil
	case "pod":
		if pod, ok := data["pod"].(map[string]any); ok {
			return pod["name"]
		}
		return nil
	case "podNamespace":
		if pod, ok := data["pod"].(map[string]any); ok {
			return pod["namespace"]
		}
		return nil
	default:
		return nil
	}
}

// extractHttpAttribute extracts attributes from HTTP event data.
// Handles both request.created events (from EventStore) and request.http_transaction
// events (from ObjectStore/Artifact) which have different data structures.
func extractHttpAttribute(data map[string]any, attr string) any {
	// HTTP data is nested under "data" key from eventstore
	httpData := toMapAny(data["data"])
	if httpData == nil {
		// Try direct access (in case structure varies)
		httpData = data
	}

	// Check if this is an http_transaction artifact (has "summary" field)
	// These come from ObjectStore and have a different structure
	if summary := toMapAny(httpData["summary"]); summary != nil {
		return extractFromHttpSummary(summary, attr)
	}

	// Standard request.created event structure
	switch attr {
	case "method":
		return httpData["method"]
	case "status":
		return httpData["status"]
	case "path":
		return httpData["path"]
	case "url":
		return httpData["url"]
	case "domain":
		// Extract domain from URL
		if urlStr, ok := httpData["url"].(string); ok {
			if parsed, err := url.Parse(urlStr); err == nil {
				return parsed.Hostname()
			}
		}
		return nil
	case "direction":
		return httpData["direction"]
	case "category":
		return httpData["category"]
	case "contentType":
		return httpData["contentType"]
	case "agent":
		return httpData["agent"]
	case "duration":
		return httpData["duration"]
	case "bytesSent":
		return httpData["bytesSent"]
	case "bytesReceived":
		return httpData["bytesReceived"]
	case "connectionId":
		return httpData["connectionId"]
	case "requestId":
		return httpData["requestId"]
	default:
		return nil
	}
}

// extractFromHttpSummary extracts attributes from http_transaction summary data.
// Summary fields use snake_case naming like request_host, request_method, etc.
func extractFromHttpSummary(summary map[string]any, attr string) any {
	switch attr {
	case "method":
		return summary["request_method"]
	case "status":
		return summary["response_status"]
	case "path":
		return summary["request_path"]
	case "domain":
		return summary["request_host"]
	case "direction":
		return summary["direction"]
	case "protocol":
		return summary["request_protocol"]
	case "scheme":
		return summary["request_scheme"]
	case "requestContentType":
		return summary["request_content_type"]
	case "responseContentType":
		return summary["response_content_type"]
	case "duration":
		return summary["duration_ms"]
	case "connectionId":
		return summary["connection_id"]
	case "requestId":
		return summary["request_id"]
	case "container":
		return summary["container_name"]
	case "containerImage":
		return summary["container_image"]
	case "pod":
		return summary["pod_name"]
	case "podNamespace":
		return summary["pod_namespace"]
	case "binary":
		return summary["process_exe"]
	default:
		return nil
	}
}

// extractConnectionAttribute extracts attributes from connection event data
func extractConnectionAttribute(data map[string]any, attr string) any {
	// Connection data is nested under "data" key from eventstore
	connData := toMapAny(data["data"])
	if connData == nil {
		connData = data
	}

	switch attr {
	case "direction":
		return connData["direction"]
	case "protocol":
		return connData["l7Protocol"]
	case "socketProtocol":
		return connData["socketProtocol"]
	case "vendorId":
		return connData["vendorId"]
	case "tlsVersion":
		return connData["tlsVersion"]
	case "pid":
		return extractEndpointField(connData, "source", "pid")
	case "container":
		if src := toMapAny(connData["source"]); src != nil {
			if container := toMapAny(src["container"]); container != nil {
				return container["name"]
			}
		}
		return nil
	case "hostname":
		return extractEndpointField(connData, "source", "hostname")
	case "exe":
		return extractEndpointField(connData, "source", "exe")
	case "srcIp":
		return extractEndpointIP(connData, "source")
	case "srcPort":
		return extractEndpointPort(connData, "source")
	case "dstIp":
		return extractEndpointIP(connData, "destination")
	case "dstPort":
		return extractEndpointPort(connData, "destination")
	default:
		return nil
	}
}

// extractEndpointField extracts a field from a connection endpoint
func extractEndpointField(data map[string]any, endpoint, field string) any {
	if ep := toMapAny(data[endpoint]); ep != nil {
		return ep[field]
	}
	return nil
}

// extractEndpointIP extracts the IP address from a connection endpoint
func extractEndpointIP(data map[string]any, endpoint string) any {
	if ep := toMapAny(data[endpoint]); ep != nil {
		if addr := toMapAny(ep["address"]); addr != nil {
			return addr["ip"]
		}
	}
	return nil
}

// extractEndpointPort extracts the port from a connection endpoint
func extractEndpointPort(data map[string]any, endpoint string) any {
	if ep := toMapAny(data[endpoint]); ep != nil {
		if addr := toMapAny(ep["address"]); addr != nil {
			return addr["port"]
		}
	}
	return nil
}

// extractIssueAttribute extracts attributes from issue event data
func extractIssueAttribute(data map[string]any, attr string) any {
	issueData := toMapAny(data["data"])
	if issueData == nil {
		issueData = data
	}

	switch attr {
	case "method":
		return issueData["method"]
	case "status":
		return issueData["status"]
	case "path":
		return issueData["path"]
	case "url":
		return issueData["url"]
	case "direction":
		return issueData["direction"]
	case "error":
		return issueData["error"]
	default:
		return nil
	}
}

// extractPIIAttribute extracts attributes from PII event data
func extractPIIAttribute(data map[string]any, attr string) any {
	piiData := toMapAny(data["data"])
	if piiData == nil {
		piiData = data
	}

	switch attr {
	case "entityType":
		return piiData["entityType"]
	case "entitySource":
		return piiData["entitySource"]
	case "fieldPath":
		return piiData["fieldPath"]
	case "requestMethod":
		return piiData["requestMethod"]
	case "requestPath":
		return piiData["requestPath"]
	default:
		return nil
	}
}

// matchCondition checks if a value matches a filter condition
func matchCondition(value any, cond FilterCondition) bool {
	if value == nil {
		// If the attribute doesn't exist, the condition doesn't match
		// unless we're using neq (not equals)
		return cond.Operator == OpNeq
	}

	switch cond.Operator {
	case OpEq:
		return compareEqual(value, cond.Value)
	case OpNeq:
		return !compareEqual(value, cond.Value)
	case OpIn:
		return compareIn(value, cond.Value)
	case OpGt:
		return compareNumeric(value, cond.Value, func(a, b int64) bool { return a > b })
	case OpGte:
		return compareNumeric(value, cond.Value, func(a, b int64) bool { return a >= b })
	case OpLt:
		return compareNumeric(value, cond.Value, func(a, b int64) bool { return a < b })
	case OpLte:
		return compareNumeric(value, cond.Value, func(a, b int64) bool { return a <= b })
	case OpPrefix:
		return comparePrefix(value, cond.Value)
	case OpContains:
		return compareContains(value, cond.Value)
	default:
		return false
	}
}

// compareEqual compares a value for equality with a filter value
func compareEqual(value any, filterValue string) bool {
	switch v := value.(type) {
	case string:
		return v == filterValue
	case int:
		if fv, err := strconv.Atoi(filterValue); err == nil {
			return v == fv
		}
	case int64:
		if fv, err := strconv.ParseInt(filterValue, 10, 64); err == nil {
			return v == fv
		}
	case float64:
		// JSON numbers are often float64
		if fv, err := strconv.ParseFloat(filterValue, 64); err == nil {
			return v == fv
		}
		// Also try int comparison for integer values
		if fv, err := strconv.ParseInt(filterValue, 10, 64); err == nil {
			return int64(v) == fv
		}
	case uint32:
		if fv, err := strconv.ParseUint(filterValue, 10, 32); err == nil {
			return v == uint32(fv)
		}
	case uint16:
		if fv, err := strconv.ParseUint(filterValue, 10, 16); err == nil {
			return v == uint16(fv)
		}
	}
	// Fallback: convert both to string
	return fmt.Sprintf("%v", value) == filterValue
}

// compareIn checks if a value is in a comma-separated list
func compareIn(value any, filterValue string) bool {
	parts := strings.Split(filterValue, ",")
	for _, part := range parts {
		if compareEqual(value, strings.TrimSpace(part)) {
			return true
		}
	}
	return false
}

// compareNumeric performs a numeric comparison
func compareNumeric(value any, filterValue string, cmp func(a, b int64) bool) bool {
	fv, err := strconv.ParseInt(filterValue, 10, 64)
	if err != nil {
		return false
	}

	var v int64
	switch val := value.(type) {
	case int:
		v = int64(val)
	case int64:
		v = val
	case float64:
		v = int64(val)
	case uint32:
		v = int64(val)
	case uint16:
		v = int64(val)
	default:
		return false
	}

	return cmp(v, fv)
}

// comparePrefix checks if a string value starts with the filter value
func comparePrefix(value any, filterValue string) bool {
	if s, ok := value.(string); ok {
		return strings.HasPrefix(s, filterValue)
	}
	return false
}

// compareContains checks if a string value contains the filter value
func compareContains(value any, filterValue string) bool {
	if s, ok := value.(string); ok {
		return strings.Contains(s, filterValue)
	}
	return false
}

// ParseFiltersFromBody parses filters from a map (from JSON body).
// The map keys should be in the format: entity.attribute.operator
// Example: {"process.pid.eq": "1234", "request.status.gte": "400"}
func ParseFiltersFromBody(filters map[string]string) (*EventFilter, error) {
	if len(filters) == 0 {
		return nil, nil
	}

	// Convert map to url.Values format and reuse ParseFilters
	values := make(url.Values)
	for key, val := range filters {
		values.Set(key, val)
	}
	return ParseFilters(values)
}

// MergeFilters combines two filters, with override taking precedence.
// Returns override if base is nil, base if override is nil/empty.
// When both have conditions for the same entity, override's conditions replace base's.
func MergeFilters(base, override *EventFilter) *EventFilter {
	if override == nil || override.IsEmpty() {
		return base
	}
	if base == nil || base.IsEmpty() {
		return override
	}

	// Merge conditions: override replaces base for same entity
	merged := &EventFilter{
		Conditions: make(map[string][]FilterCondition),
	}

	// Copy base conditions
	for entity, conds := range base.Conditions {
		merged.Conditions[entity] = conds
	}

	// Override with override's conditions
	for entity, conds := range override.Conditions {
		merged.Conditions[entity] = conds
	}

	return merged
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
