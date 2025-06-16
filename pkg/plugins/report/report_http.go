package report

import (
	"context"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/tools"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
)

type filterInstance struct {
	logger *zap.Logger
	ctx    plugins.PluginContext

	eventstore eventstore.EventStore

	reqheaders plugins.Headers
	resheaders plugins.Headers

	startTime  time.Time
	finishTime time.Time
}

func (h *filterInstance) RequestHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	// set the start time
	h.startTime = time.Now()
	h.reqheaders = headers

	return plugins.HeadersStatusContinue
}

func (h *filterInstance) RequestBody(body plugins.BodyBuffer, endStream bool) plugins.BodyStatus {
	return plugins.BodyStatusContinue
}

func (h *filterInstance) ResponseHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	// set the finish time
	h.finishTime = time.Now()
	h.resheaders = headers

	return plugins.HeadersStatusContinue
}

func (h *filterInstance) ResponseBody(body plugins.BodyBuffer, endStream bool) plugins.BodyStatus {
	return plugins.BodyStatusContinue
}

func (h *filterInstance) Destroy() {
	ctx := context.TODO()

	if h.finishTime.IsZero() {
		h.finishTime = time.Now()
	}

	meta := h.ctx.Meta()
	hm := tools.NewHeaderMap(h.reqheaders)

	userAgent, _ := hm.UserAgent()
	path, _ := hm.Path()
	method, _ := hm.Method()
	endpoint := meta.Endpoint()
	url, _ := hm.URL()
	direction := meta.Direction()

	// ----- Response
	rhm := tools.NewHeaderMap(h.resheaders)

	status, _ := rhm.Status()
	contentType, _ := rhm.Get("Content-Type")

	category := tools.MimeCategory(contentType)

	// ------ Duration
	var durationMS int64
	if !h.startTime.IsZero() && !h.finishTime.IsZero() {
		durationMS = h.finishTime.Sub(h.startTime).Milliseconds()
		if durationMS == 0 {
			durationMS = 1
		}
	}

	r := eventstore.Request{
		WrBytes: meta.WriteBytes(),
		RdBytes: meta.ReadBytes(),

		Timestamp: time.Now().UTC(),
		Duration:  durationMS,
		Direction: direction,
		Category:  category,

		// From Request/Response headers
		Url:         url,
		URLPath:     path,
		Method:      method,
		Status:      status,
		ContentType: contentType,
		Agent:       userAgent,
	}

	r.SetRequestID(meta.RequestID())
	r.SetEndpointID(endpoint)

	// Scan for auth tokens
	authTokenSource, authToken, authTokenFound := h.scanForAuthTokens()
	if authTokenFound {
		authTokenType := detectTokenType(authTokenSource, authToken)
		authTokenMask := maskToken(authToken, authTokenType)
		r.RequestAuthToken = eventstore.RequestAuthToken{
			AuthTokenMask:   authTokenMask,
			AuthTokenHash:   hashToken(authToken),
			AuthTokenSource: authTokenSource,
			AuthTokenType:   string(authTokenType),
		}
	}

	h.eventstore.Save(ctx, &r)

	h.logger.Debug("filter instance destroyed")
}

func (h *filterInstance) scanForAuthTokens() (string, string, bool) {
	// Check Authorization header
	if authHeader, ok := h.reqheaders.Get("Authorization"); ok {
		return "Authorization", authHeader.String(), true
	}

	// Check common API key headers
	commonAPIKeyHeaders := []string{"X-API-Key", "Api-Key", "APIKey", "X-Auth-Token"}
	for _, header := range commonAPIKeyHeaders {
		if apiKey, ok := h.reqheaders.Get(header); ok {
			return header, apiKey.String(), true
		}
	}

	return "", "", false
}
