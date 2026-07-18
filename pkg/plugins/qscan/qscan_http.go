package qscan

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/tools"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"go.uber.org/zap"
)

// defaultCaptureByteLimit is the default byte limit for capturing
// request and response bodies.
//
// Note(Jon): For every 100,000 characters Qscan requires approximately
// 1GiB of memory for scanning. This limit is a conservative estimate.
const defaultCaptureByteLimit = 1024 * 1024 // 1MB

type filterInstance struct {
	logger *zap.Logger
	ctx    plugins.PluginContext

	objectstore objectstore.ObjectStore

	config  *QscanConfig
	factory *Factory
	sample  bool

	requestID string

	method      string
	path        string
	url         string
	headers     map[string]string
	requestBody string

	responseHeaders map[string]string
	responseBody    string

	shouldCaptureRequestBody  bool
	shouldCaptureResponseBody bool

	requestStart time.Time
}

// isTextBasedContentType checks if a content type is text-based (not binary)
func isTextBasedContentType(contentType string) bool {
	if contentType == "" {
		// If no content type is specified, assume it might be text
		return true
	}

	// Normalize to lowercase for comparison
	contentType = strings.ToLower(contentType)
	// Remove any parameters (e.g., charset=utf-8)
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = contentType[:idx]
	}
	contentType = strings.TrimSpace(contentType)

	// Binary content types to exclude
	// Note: compression types (zip, gzip, tar) are not excluded since decompression
	// happens before this code runs
	binaryTypes := []string{
		"application/octet-stream",
		"application/pdf",
		"image/",
		"audio/",
		"video/",
	}

	for _, binaryType := range binaryTypes {
		if strings.HasPrefix(contentType, binaryType) {
			return false
		}
	}

	// Text-based content types
	textTypes := []string{
		"text/",
		"application/json",
		"application/xml",
		"application/x-www-form-urlencoded",
		"application/javascript",
		"application/x-javascript",
		"application/ecmascript",
		"application/x-ecmascript",
	}

	for _, textType := range textTypes {
		if strings.HasPrefix(contentType, textType) {
			return true
		}
	}

	// Default: if it doesn't match known binary types, assume it's text-based
	// This is a conservative approach - we'll include it unless we're sure it's binary
	return true
}

// getContentType extracts the Content-Type header value from a Headers interface
func getContentType(headers plugins.Headers) string {
	if headers == nil {
		return ""
	}
	ct, ok := headers.Get("Content-Type")
	if !ok {
		// Try case-insensitive lookup
		ct, ok = headers.Get("content-type")
	}
	if ok {
		return ct.String()
	}
	return ""
}

func (h *filterInstance) RequestHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.requestStart = time.Now()
	scansAttemptedTotal.Inc()

	hm := tools.NewHeaderMap(headers)

	// grab payload headers
	h.url, _ = hm.URL()
	h.path, _ = hm.Path()
	h.method, _ = hm.Method()
	h.headers = headers.All()

	// Get sampling result with reason
	var sampleReason string
	h.sample, sampleReason = h.factory.shouldSample(h.url)

	// Track sampling metrics
	if h.sample {
		scansSampledTotal.WithLabelValues(sampleReason).Inc()
	} else {
		scansNotSampledTotal.Inc()
	}

	h.requestID = h.ctx.Meta().RequestID()

	// Check if request body should be captured based on content type
	h.shouldCaptureRequestBody = isTextBasedContentType(getContentType(headers))

	return plugins.HeadersStatusContinue
}

func (h *filterInstance) RequestBody(frame plugins.BodyBuffer, endOfStream bool) plugins.BodyStatus {
	if !h.shouldCaptureRequestBody {
		return plugins.BodyStatusContinue
	}

	if !endOfStream {
		return plugins.BodyStatusStopIterationAndBuffer
	}

	if body := frame.Copy(); len(body) > 0 {
		h.requestBody = string(body)
	}

	return plugins.BodyStatusContinue
}

func (h *filterInstance) ResponseHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.responseHeaders = headers.All()

	// Check if response body should be captured based on content type
	h.shouldCaptureResponseBody = isTextBasedContentType(getContentType(headers))

	return plugins.HeadersStatusContinue
}

func (h *filterInstance) ResponseBody(frame plugins.BodyBuffer, endOfStream bool) plugins.BodyStatus {
	if !h.shouldCaptureResponseBody {
		return plugins.BodyStatusContinue
	}

	if !endOfStream {
		return plugins.BodyStatusStopIterationAndBuffer
	}

	if body := frame.Copy(); len(body) > 0 {
		h.responseBody = string(body)
	}

	return plugins.BodyStatusContinue
}

func (h *filterInstance) Destroy() {
	if !h.sample {
		return
	}

	// Create a single transaction request with both request and response data
	txnRequest := &HTTPTransactionRequest{
		URL:       h.url,
		Method:    h.method,
		RequestID: h.requestID,
	}

	// Add request data if available
	if len(h.headers) > 0 || h.requestBody != "" {
		requestData := &HTTPRequestData{
			Headers: h.headers,
		}
		if h.requestBody != "" {
			requestData.Body = &h.requestBody
		}
		txnRequest.Request = requestData
	}

	// Add response data if available
	if len(h.responseHeaders) > 0 || h.responseBody != "" {
		responseData := &HTTPResponseData{
			Headers: h.responseHeaders,
		}
		if h.responseBody != "" {
			responseData.Body = &h.responseBody
		}
		if status, ok := h.responseHeaders[":status"]; ok {
			if s, err := strconv.ParseInt(status, 10, 32); err == nil {
				var v int = int(s)
				responseData.Status = &v
			}
		}
		txnRequest.Response = responseData
	}

	if txnRequest.Request == nil && txnRequest.Response == nil {
		h.logger.Debug("no request or response data to scan")
		return
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(txnRequest)
	if err != nil {
		h.logger.Error("failed to marshal qscan document", zap.Error(err))
		scansStoredTotal.WithLabelValues("error").Inc()
		return
	}

	if len(jsonData) > defaultCaptureByteLimit {
		// TODO(Jon): Earlier evaluations could be done to reduce the
		// memory footprint of this if we cross the boundary with a
		// large request body.
		h.logger.Warn("qscan request is too large, skipping", zap.Int("size", len(jsonData)))
		return
	}

	// Track document size
	scansStoredSize.Observe(float64(len(jsonData)))

	a := eventstore.Artifact{
		Type:        eventstore.ArtifactType_QscanRequest,
		Data:        jsonData,
		ContentType: "application/json",
		Summary:     h.config.Summary(),
	}
	a.SetEndpointID(h.ctx.Meta().Endpoint())
	a.SetRequestID(h.requestID)

	if h.objectstore != nil {
		h.objectstore.Put(h.ctx.Context(), &a)
		scansStoredTotal.WithLabelValues("success").Inc()
	} else {
		scansStoredTotal.WithLabelValues("error").Inc()
	}
}
