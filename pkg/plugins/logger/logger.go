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

	httpInstancesCreated  atomic.Uint64
	redisInstancesCreated atomic.Uint64
	egressReqBodySize     atomic.Uint64
	egressResBodySize     atomic.Uint64
	redisCommands         atomic.Uint64
	redisResults          atomic.Uint64
}

func (f *Factory) Init(logger *zap.Logger, config yaml.Node) {
	f.logger = logger
	f.prefix = "🧬"
}

func (f *Factory) NewHttpInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	instances := f.httpInstancesCreated.Add(1)
	f.logger.Debug(f.prefix+": new HTTP instance created", zap.Uint64("http_instances", instances))

	return &httpInstance{
		logger: f.logger,
		ctx:    ctx,
		filter: f,
		prefix: f.prefix,
	}
}

func (f *Factory) NewRedisInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.RedisPluginInstance {
	instances := f.redisInstancesCreated.Add(1)
	f.logger.Debug(f.prefix+": new Redis instance created", zap.Uint64("redis_instances", instances))

	return &redisInstance{
		logger: f.logger,
		ctx:    ctx,
		filter: f,
		prefix: f.prefix,
	}
}

func (f *Factory) Destroy() {
	f.logger.Debug(f.prefix+": plugin destroyed",
		zap.Uint64("http_instances", f.httpInstancesCreated.Load()),
		zap.Uint64("redis_instances", f.redisInstancesCreated.Load()),
		zap.Uint64("egress_req_body_size", f.egressReqBodySize.Load()),
		zap.Uint64("egress_res_body_size", f.egressResBodySize.Load()),
		zap.Uint64("redis_commands", f.redisCommands.Load()),
		zap.Uint64("redis_results", f.redisResults.Load()))
}

// httpInstance handles HTTP traffic logging
type httpInstance struct {
	logger    *zap.Logger
	ctx       plugins.PluginContext
	filter    *Factory
	prefix    string
	startTime time.Time
}

func (h *httpInstance) RequestHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.startTime = time.Now()
	h.logger.Debug(fmt.Sprintf("%s: request headers received. endstream: %v", h.prefix, endStream))

	return plugins.HeadersStatusContinue
}

func (h *httpInstance) RequestBody(body plugins.BodyBuffer, endStream bool) plugins.BodyStatus {
	totalSize := h.filter.egressReqBodySize.Add(uint64(body.Length()))

	h.logger.Debug(h.prefix+": request body received", zap.Int("body_size", body.Length()), zap.String("body", string(body.Copy())), zap.Bool("endstream", endStream))
	h.logger.Debug(h.prefix+": total egress request body size", zap.Uint64("size", totalSize))

	return plugins.BodyStatusContinue
}

func (h *httpInstance) ResponseHeaders(headers plugins.Headers, endStream bool) plugins.HeadersStatus {
	h.logger.Debug(h.prefix+": response headers received", zap.Bool("endstream", endStream))

	return plugins.HeadersStatusContinue
}

func (h *httpInstance) ResponseBody(body plugins.BodyBuffer, endStream bool) plugins.BodyStatus {
	totalSize := h.filter.egressResBodySize.Add(uint64(body.Length()))

	h.logger.Debug(h.prefix+": response body received", zap.Int("body_size", body.Length()), zap.String("body", string(body.Copy())), zap.Bool("endstream", endStream))
	h.logger.Debug(h.prefix+": total egress response body size", zap.Uint64("size", totalSize))

	return plugins.BodyStatusContinue
}

func (h *httpInstance) Destroy() {
	h.logger.Debug(h.prefix + ": HTTP instance destroyed")
}

// redisInstance handles Redis traffic logging
type redisInstance struct {
	logger *zap.Logger
	ctx    plugins.PluginContext
	filter *Factory
	prefix string
}

func (r *redisInstance) OnRedisCommand(cmd *plugins.RedisCommand) plugins.RedisStatus {
	r.filter.redisCommands.Add(1)
	r.logger.Debug(r.prefix+": redis command",
		zap.String("cmd", cmd.Name),
		zap.Strings("args", cmd.Args),
		zap.Time("timestamp", cmd.Timestamp))

	return plugins.RedisStatusContinue
}

func (r *redisInstance) OnRedisResult(res *plugins.RedisResult) plugins.RedisStatus {
	r.filter.redisResults.Add(1)
	r.logger.Debug(r.prefix+": redis result",
		zap.String("type", res.Type),
		zap.Any("value", res.Value),
		zap.Bool("is_error", res.IsError))

	return plugins.RedisStatusContinue
}

func (r *redisInstance) Destroy() {
	r.logger.Debug(r.prefix + ": Redis instance destroyed")
}

func (f *Factory) PluginType() plugins.PluginType {
	return pluginTypeLogger
}
