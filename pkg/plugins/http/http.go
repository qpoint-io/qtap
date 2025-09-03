package http

import (
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

const (
	pluginTypeHTTPMetrics plugins.PluginType = "http_metrics"
)

type Factory struct {
	logger *zap.Logger
	prefix string
}

func (f *Factory) Init(logger *zap.Logger, config yaml.Node) {
	f.logger = logger
}

func (f *Factory) NewInstance(ctx plugins.PluginContext, svcs ...services.Service) plugins.HttpPluginInstance {
	return &filterInstance{
		logger: f.logger,
		ctx:    ctx,

		filter: f,
		prefix: f.prefix,
	}
}

func (f *Factory) RequiredServices() []services.ServiceType {
	return nil
}

func (f *Factory) Destroy() {}

type filterInstance struct {
	logger        *zap.Logger
	ctx           plugins.PluginContext
	filter        *Factory
	prefix        string
	requestStart  time.Time
	responseStart time.Time
	method        string
	host          string
	statusCode    string
}

func (h *filterInstance) RequestHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.requestStart = time.Now()

	// Extract HTTP method and host from headers
	if method, ok := headers.Get(":method"); ok {
		h.method = method.String()
	}
	if host, ok := headers.Get(":authority"); ok {
		h.host = host.String()
	}

	return plugins.HeadersStatusContinue
}

func (h *filterInstance) RequestBody(body plugins.BodyBuffer, endStream bool) plugins.BodyStatus {
	return plugins.BodyStatusContinue
}

func (h *filterInstance) ResponseHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.responseStart = time.Now()

	// Extract status code from response headers
	if status, ok := headers.Get(":status"); ok {
		h.statusCode = status.String()
	} else {
		h.statusCode = "000"
	}

	h.recordRequestMetrics()

	if endStream {
		h.recordResponseMetrics()
	}

	return plugins.HeadersStatusContinue
}

func (h *filterInstance) ResponseBody(body plugins.BodyBuffer, endStream bool) plugins.BodyStatus {
	if endStream {
		h.recordResponseMetrics()
	}

	return plugins.BodyStatusContinue
}

func (h *filterInstance) recordRequestMetrics() {
	protocol := h.ctx.Meta().Protocol()

	host := h.host
	if host == "" {
		host = h.ctx.Meta().Endpoint()
	}

	requestsTotal.WithLabelValues(h.method, host, h.statusCode, protocol).Inc()
	requestsDuration.WithLabelValues(h.method, host, h.statusCode, protocol).Observe(float64(h.responseStart.Sub(h.requestStart).Milliseconds()))
}

func (h *filterInstance) recordResponseMetrics() {
	protocol := h.ctx.Meta().Protocol()

	host := h.host
	if host == "" {
		host = h.ctx.Meta().Endpoint()
	}

	responsesTotal.WithLabelValues(h.method, host, h.statusCode, protocol).Inc()
	responsesDuration.WithLabelValues(h.method, host, h.statusCode, protocol).Observe(float64(time.Since(h.responseStart).Milliseconds()))

	// record the combined duration of the request and response
	duration.Observe(float64(time.Since(h.requestStart).Milliseconds()))
}

func (h *filterInstance) Destroy() {
	protocol := h.ctx.Meta().Protocol()

	host := h.host
	if host == "" {
		host = h.ctx.Meta().Endpoint()
	}

	requestsSize.WithLabelValues(h.method, host, h.statusCode, protocol).Observe(float64(h.ctx.Meta().WriteBytes()))
	responsesSize.WithLabelValues(h.method, host, h.statusCode, protocol).Observe(float64(h.ctx.Meta().ReadBytes()))
}

func (f *Factory) PluginType() plugins.PluginType {
	return pluginTypeHTTPMetrics
}
