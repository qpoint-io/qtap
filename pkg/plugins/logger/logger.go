package logger

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

const (
	pluginTypeLogger plugins.PluginType = "logger"
)

type Factory struct {
	logger *zap.Logger
	prefix string

	instancesCreated  atomic.Uint64
	egressReqBodySize atomic.Uint64
	egressResBodySize atomic.Uint64
}

func (f *Factory) Init(logger *zap.Logger, config yaml.Node) {
	f.logger = logger
	f.prefix = "🧬"
}

func (f *Factory) NewInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	instances := f.instancesCreated.Add(1)
	f.logger.Info(f.prefix+": new instance created.", zap.Uint64("instances created", instances))

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

func (f *Factory) Destroy() {
	instances := f.instancesCreated.Load()
	totalEgressReqBodySize := f.egressReqBodySize.Load()
	totalEgressResBodySize := f.egressResBodySize.Load()
	f.logger.Info(f.prefix+": plugin destroyed",
		zap.Uint64("instances created", instances),
		zap.Uint64("total egress request body size", totalEgressReqBodySize),
		zap.Uint64("total egress response body size", totalEgressResBodySize))
}

type filterInstance struct {
	logger    *zap.Logger
	ctx       plugins.PluginContext
	filter    *Factory
	prefix    string
	startTime time.Time
}

func (h *filterInstance) RequestHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.startTime = time.Now()
	h.logger.Info(fmt.Sprintf("%s: request headers received. endstream: %v", h.prefix, endStream))

	return plugins.HeadersStatusContinue
}

func (h *filterInstance) RequestBody(body plugins.BodyBuffer, endStream bool) plugins.BodyStatus {
	totalSize := h.filter.egressReqBodySize.Add(uint64(body.Length()))

	h.logger.Info(h.prefix+": request body received", zap.Int("body_size", body.Length()), zap.String("body", string(body.Copy())), zap.Bool("endstream", endStream))
	h.logger.Info(h.prefix+": total egress request body size", zap.Uint64("size", totalSize))

	return plugins.BodyStatusContinue
}

func (h *filterInstance) ResponseHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.logger.Info(h.prefix+": response headers received", zap.Bool("endstream", endStream))

	return plugins.HeadersStatusContinue
}

func (h *filterInstance) ResponseBody(body plugins.BodyBuffer, endStream bool) plugins.BodyStatus {
	totalSize := h.filter.egressResBodySize.Add(uint64(body.Length()))

	h.logger.Info(h.prefix+": response body received", zap.Int("body_size", body.Length()), zap.String("body", string(body.Copy())), zap.Bool("endstream", endStream))
	h.logger.Info(h.prefix+": total egress response body size", zap.Uint64("size", totalSize))

	return plugins.BodyStatusContinue
}

func (h *filterInstance) Destroy() {
	h.logger.Info(h.prefix + ": plugin instance destroyed")
}

func (f *Factory) PluginType() plugins.PluginType {
	return pluginTypeLogger
}
