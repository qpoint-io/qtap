package httpcapture

import (
	"encoding/json"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/tools"
)

// GrpcTransaction represents a gRPC request-response transaction
type GrpcTransaction struct {
	// Process and connection metadata
	Metadata Metadata `json:"metadata"`

	// gRPC-specific fields
	Service        string `json:"service"`
	Method         string `json:"method"`
	GrpcStatus     string `json:"grpc_status"`
	GrpcStatusName string `json:"grpc_status_name"`
	GrpcMessage    string `json:"grpc_message,omitempty"`

	// Request information
	Request GrpcMessage `json:"request"`

	// Response information
	Response GrpcResponse `json:"response"`

	// Transaction metadata
	TransactionTime time.Time `json:"transaction_time"`
	DurationMs      int64     `json:"duration_ms,omitempty"`
	Direction       string    `json:"direction,omitempty"`
}

// GrpcMessage represents a gRPC request message (metadata + body)
type GrpcMessage struct {
	ContentType string            `json:"content_type,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        []byte            `json:"body,omitempty"`
}

// GrpcResponse represents a gRPC response (metadata + body + grpc status fields)
type GrpcResponse struct {
	ContentType    string            `json:"content_type,omitempty"`
	GrpcStatus     string            `json:"grpc_status"`
	GrpcStatusName string            `json:"grpc_status_name"`
	GrpcMessage    string            `json:"grpc_message,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Body           []byte            `json:"body,omitempty"`
}

// NewGrpcTransaction creates a new gRPC transaction with metadata
func NewGrpcTransaction(ctx plugins.PluginContext, reqHeaders, resHeaders plugins.Headers, startTime, endTime time.Time) *GrpcTransaction {
	tx := &GrpcTransaction{
		TransactionTime: time.Now(),
	}

	// Calculate duration
	if !startTime.IsZero() && !endTime.IsZero() {
		tx.DurationMs = endTime.Sub(startTime).Milliseconds()
		if tx.DurationMs == 0 {
			tx.DurationMs = 1
		}
	}

	meta := ctx.Meta()
	tx.Direction = meta.Direction()
	tx.Metadata = getMetadata(meta)

	// Extract gRPC service/method from :path header
	reqHM := tools.NewHeaderMap(reqHeaders)
	tx.Service, tx.Method = reqHM.GRPCServiceMethod()

	// Extract gRPC status fields from response headers
	resHM := tools.NewHeaderMap(resHeaders)
	tx.GrpcStatus, _ = resHM.Get("Grpc-Status")
	tx.GrpcStatusName, _ = resHM.Get("Grpc-Status-Name")
	tx.GrpcMessage, _ = resHM.Get("Grpc-Message")

	// Request
	reqCT, _ := reqHM.ContentType()
	tx.Request = GrpcMessage{
		ContentType: reqCT,
	}
	if reqHeaders != nil {
		tx.Request.Headers = reqHeaders.All()
	}

	// Response
	resCT, _ := resHM.ContentType()
	tx.Response = GrpcResponse{
		ContentType:    resCT,
		GrpcStatus:     tx.GrpcStatus,
		GrpcStatusName: tx.GrpcStatusName,
		GrpcMessage:    tx.GrpcMessage,
	}
	if resHeaders != nil {
		tx.Response.Headers = resHeaders.All()
	}

	return tx
}

// Summary returns a map of key transaction fields for artifact metadata
func (t *GrpcTransaction) Summary() map[string]any {
	summary := map[string]any{}
	if t.Service != "" {
		summary["grpc_service"] = t.Service
	}
	if t.Method != "" {
		summary["grpc_method"] = t.Method
	}
	if t.GrpcStatus != "" {
		summary["grpc_status"] = t.GrpcStatus
	}
	if t.GrpcStatusName != "" {
		summary["grpc_status_name"] = t.GrpcStatusName
	}
	if t.DurationMs > 0 {
		summary["duration_ms"] = t.DurationMs
	}
	if t.Direction != "" {
		summary["direction"] = t.Direction
	}
	maps.Copy(summary, t.Metadata.Summary())
	return summary
}

// ToJSON converts the GrpcTransaction to JSON
func (t *GrpcTransaction) ToJSON() ([]byte, error) {
	return json.Marshal(t)
}

// ToString converts the GrpcTransaction to a formatted text string
func (t *GrpcTransaction) ToString() string {
	var text strings.Builder
	text.WriteString("gRPC Transaction\n")
	text.WriteString("================\n\n")

	// Metadata
	text.WriteString("Metadata:\n")
	text.WriteString("  Transaction Time: " + t.TransactionTime.Format(time.RFC3339) + "\n")
	if t.DurationMs > 0 {
		text.WriteString("  Duration: " + strconv.FormatInt(t.DurationMs, 10) + "ms\n")
	}
	text.WriteString("  Direction: " + t.Direction + "\n")
	if t.Metadata.RequestID != "" {
		text.WriteString("  Request ID: " + t.Metadata.RequestID + "\n")
	}
	if t.Metadata.ProcessExe != "" {
		text.WriteString("  Process: " + t.Metadata.ProcessExe + "\n")
	}

	// RPC
	text.WriteString("\nRPC:\n")
	text.WriteString("  Service: " + t.Service + "\n")
	text.WriteString("  Method: " + t.Method + "\n")
	text.WriteString("  Status: " + t.GrpcStatusName + " (" + t.GrpcStatus + ")\n")
	if t.GrpcMessage != "" {
		text.WriteString("  Message: " + t.GrpcMessage + "\n")
	}

	// Request
	text.WriteString("\nRequest:\n")
	if t.Request.ContentType != "" {
		text.WriteString("  Content-Type: " + t.Request.ContentType + "\n")
	}
	if len(t.Request.Headers) > 0 {
		text.WriteString("  Headers:\n")
		for k, v := range t.Request.Headers {
			text.WriteString("    " + k + ": " + v + "\n")
		}
	}
	if len(t.Request.Body) > 0 {
		text.WriteString("  Body:\n")
		text.WriteString(string(t.Request.Body) + "\n")
	}

	// Response
	text.WriteString("\nResponse:\n")
	if t.Response.ContentType != "" {
		text.WriteString("  Content-Type: " + t.Response.ContentType + "\n")
	}
	if len(t.Response.Headers) > 0 {
		text.WriteString("  Headers:\n")
		for k, v := range t.Response.Headers {
			text.WriteString("    " + k + ": " + v + "\n")
		}
	}
	if len(t.Response.Body) > 0 {
		text.WriteString("  Body:\n")
		text.WriteString(string(t.Response.Body) + "\n")
	}

	return text.String()
}
