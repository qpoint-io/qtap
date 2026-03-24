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

// HttpTransaction represents an HTTP request-response transaction
type HttpTransaction struct {
	// Process and connection metadata
	Metadata Metadata `json:"metadata"`

	// Request information
	Request Request `json:"request"`

	// Response information
	Response Response `json:"response"`

	// Transaction metadata
	TransactionTime time.Time `json:"transaction_time"`
	DurationMs      int64     `json:"duration_ms,omitempty"`
	Direction       string    `json:"direction,omitempty"`
}

// Metadata contains process and connection information
type Metadata struct {
	ProcessID      string   `json:"process_id,omitempty"`
	ProcessExe     string   `json:"process_exe,omitempty"`
	ContainerName  string   `json:"container_name,omitempty"`
	ContainerImage string   `json:"container_image,omitempty"`
	PodName        string   `json:"pod_name,omitempty"`
	PodNamespace   string   `json:"pod_namespace,omitempty"`
	BytesSent      int64    `json:"bytes_sent,omitempty"`
	BytesReceived  int64    `json:"bytes_received,omitempty"`
	RequestID      string   `json:"request_id,omitempty"`
	ConnectionID   string   `json:"connection_id,omitempty"`
	EndpointID     string   `json:"endpoint_id,omitempty"`
	Tags           []string `json:"tags,omitempty"`
}

// Request contains HTTP request information
type Request struct {
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Scheme      string            `json:"scheme,omitempty"`
	Path        string            `json:"path,omitempty"`
	Authority   string            `json:"authority,omitempty"`
	Protocol    string            `json:"protocol,omitempty"`
	RequestID   string            `json:"request_id,omitempty"`
	UserAgent   string            `json:"user_agent,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        []byte            `json:"body,omitempty"`
}

// Response contains HTTP response information
type Response struct {
	Status      int               `json:"status"`
	ContentType string            `json:"content_type,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        []byte            `json:"body,omitempty"`
}

// NewHttpTransaction creates a new HTTP transaction with metadata
func NewHttpTransaction(ctx plugins.PluginContext, reqHeaders, resHeaders plugins.Headers, startTime, endTime time.Time) *HttpTransaction {
	// Create transaction
	tx := &HttpTransaction{
		TransactionTime: time.Now(),
	}

	// Calculate duration if both start and end times are set
	if !startTime.IsZero() && !endTime.IsZero() {
		tx.DurationMs = endTime.Sub(startTime).Milliseconds()
		if tx.DurationMs == 0 {
			tx.DurationMs = 1
		}
	}

	meta := ctx.Meta()
	// Set direction
	tx.Direction = meta.Direction()

	// Get process and connection metadata
	tx.Metadata = getMetadata(meta)

	// Get request info
	tx.Request = getRequestInfo(reqHeaders, meta)

	// Get response info
	tx.Response = getResponseInfo(resHeaders, meta)

	return tx
}

// getMetadata extracts process and connection metadata from plugin context
func getMetadata(meta plugins.Meta) Metadata {
	m := Metadata{
		BytesSent:     meta.WriteBytes(),
		BytesReceived: meta.ReadBytes(),
		EndpointID:    meta.Endpoint(),
		ConnectionID:  meta.ConnectionID(),
	}

	if p := meta.Process(); p != nil {
		m.ProcessID = strconv.Itoa(p.Pid)
		m.ProcessExe = p.Exe

		if c, _ := p.Container(); c != nil {
			m.ContainerName = c.Name
			m.ContainerImage = c.Image
		}
		if p, _ := p.Pod(); p != nil {
			m.PodName = p.Name
			m.PodNamespace = p.Namespace
		}
	}

	if t := meta.Tags(); t != nil {
		m.Tags = t.List()
	}

	return m
}

// getRequestInfo extracts HTTP request information
func getRequestInfo(headers plugins.Headers, meta plugins.Meta) Request {
	r := Request{}

	// Use HeaderMap to easily extract header information
	headerMap := tools.NewHeaderMap(headers)

	// Extract basic request info
	r.Method, _ = headerMap.Method()
	r.URL, _ = headerMap.URL()
	r.Path, _ = headerMap.Path()
	r.Authority, _ = headerMap.Authority()
	r.Scheme, _ = headerMap.Scheme()
	r.UserAgent, _ = headerMap.UserAgent()
	r.ContentType, _ = headerMap.ContentType()
	r.Protocol = meta.Protocol()
	r.RequestID = meta.RequestID()

	// Get request headers
	if headers != nil {
		r.Headers = headers.All()
	}

	return r
}

// getResponseInfo extracts HTTP response information
func getResponseInfo(headers plugins.Headers, _ plugins.Meta) Response {
	r := Response{}

	// Use HeaderMap to easily extract header information
	headerMap := tools.NewHeaderMap(headers)

	// Get status code
	r.Status, _ = headerMap.Status()

	// Get content type
	r.ContentType, _ = headerMap.ContentType()

	// Get all response headers
	if headers != nil {
		r.Headers = headers.All()
	}

	return r
}

func (m Metadata) Summary() map[string]any {
	summary := map[string]any{}
	if m.ProcessExe != "" {
		summary["process_exe"] = m.ProcessExe
	}
	if m.ContainerName != "" {
		summary["container_name"] = m.ContainerName
	}
	if m.ContainerImage != "" {
		summary["container_image"] = m.ContainerImage
	}
	if m.PodName != "" {
		summary["pod_name"] = m.PodName
	}
	if m.PodNamespace != "" {
		summary["pod_namespace"] = m.PodNamespace
	}
	if m.RequestID != "" {
		summary["request_id"] = m.RequestID
	}
	if m.ConnectionID != "" {
		summary["connection_id"] = m.ConnectionID
	}
	return summary
}

func (r Request) Summary() map[string]any {
	summary := map[string]any{}
	if r.Method != "" {
		summary["request_method"] = r.Method
	}
	if r.Scheme != "" {
		summary["request_scheme"] = r.Scheme
	}
	if r.Authority != "" {
		summary["request_host"] = r.Authority
	}
	if r.Protocol != "" {
		summary["request_protocol"] = r.Protocol
	}
	if r.ContentType != "" {
		summary["request_content_type"] = r.ContentType
	}
	return summary
}

func (r Response) Summary() map[string]any {
	summary := map[string]any{}
	summary["response_status"] = r.Status
	if r.ContentType != "" {
		summary["response_content_type"] = r.ContentType
	}
	return summary
}

func (t *HttpTransaction) Summary() map[string]any {
	summary := map[string]any{}
	// Transaction-level fields
	if t.DurationMs > 0 {
		summary["duration_ms"] = t.DurationMs
	}
	if t.Direction != "" {
		summary["direction"] = t.Direction
	}
	// Merge metadata, request, response summaries
	maps.Copy(summary, t.Metadata.Summary())
	maps.Copy(summary, t.Request.Summary())
	maps.Copy(summary, t.Response.Summary())
	return summary
}

// ToJSON converts the HttpTransaction to JSON
func (t *HttpTransaction) ToJSON() ([]byte, error) {
	return json.Marshal(t)
}

// ToString converts the HttpTransaction to a formatted text string
func (t *HttpTransaction) ToString() string {
	// Create a text representation of the transaction
	var text strings.Builder
	text.WriteString("HTTP Transaction\n")
	text.WriteString("================\n\n")

	// Add metadata
	text.WriteString("Metadata:\n")
	text.WriteString("  Transaction Time: " + t.TransactionTime.Format(time.RFC3339) + "\n")
	if t.DurationMs > 0 {
		text.WriteString("  Duration: " + strconv.FormatInt(t.DurationMs, 10) + "ms\n")
	}
	text.WriteString("  Direction: " + t.Direction + "\n")
	if t.Metadata.RequestID != "" {
		text.WriteString("  Request ID: " + t.Metadata.RequestID + "\n")
	}
	if t.Metadata.ProcessID != "" {
		text.WriteString("  Process ID: " + t.Metadata.ProcessID + "\n")
	}
	if t.Metadata.ProcessExe != "" {
		text.WriteString("  Process: " + t.Metadata.ProcessExe + "\n")
	}
	text.WriteString("  Bytes Sent: " + strconv.FormatInt(t.Metadata.BytesSent, 10) + "\n")
	text.WriteString("  Bytes Received: " + strconv.FormatInt(t.Metadata.BytesReceived, 10) + "\n")
	if tags := t.Metadata.Tags; len(tags) > 0 {
		text.WriteString("  Tags: " + strings.Join(tags, ", ") + "\n")
	}
	text.WriteString("\n")

	// Add request info
	text.WriteString("Request:\n")
	text.WriteString("  Method: " + t.Request.Method + "\n")
	text.WriteString("  URL: " + t.Request.URL + "\n")
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
	text.WriteString("\n")

	// Add response info
	text.WriteString("Response:\n")
	text.WriteString("  Status: " + strconv.Itoa(t.Response.Status) + "\n")
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
