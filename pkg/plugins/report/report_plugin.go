package report

import (
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"gopkg.in/yaml.v3"

	"go.uber.org/zap"
)

const (
	PluginTypeReport plugins.PluginType = "report_usage"
)

type Config struct {
	Tags    []string `yaml:"tags" json:"tags"`
	Headers []string `yaml:"headers" json:"headers"`
}

type Factory struct {
	logger *zap.Logger
	config Config
}

func (f *Factory) Init(logger *zap.Logger, config yaml.Node) {
	f.logger = logger

	if err := config.Decode(&f.config); err != nil {
		logger.Error("error decoding config", zap.Error(err))
	}
}

func (f *Factory) NewHttpInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	f.logger.Debug("new plugin instance created")
	fi := &filterInstance{
		logger:         f.logger,
		ctx:            ctx,
		promoteHeaders: f.config.Headers,
	}

	if es, err := services.GetService[eventstore.EventStore](ctx.Context(), svcs, eventstore.TypeEventStore, ""); err != nil {
		f.logger.Error("failed to get event store", zap.Error(err))
		return nil
	} else {
		fi.eventstore = es
	}

	return fi
}

func (f *Factory) NewGrpcInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.GrpcPluginInstance {
	f.logger.Debug("new gRPC plugin instance created")

	es, err := services.GetService[eventstore.EventStore](ctx.Context(), svcs, eventstore.TypeEventStore, "")
	if err != nil {
		f.logger.Error("failed to get event store", zap.Error(err))
		return nil
	}

	return &grpcFilterInstance{
		logger:     f.logger,
		ctx:        ctx,
		eventstore: es,
	}
}

func (f *Factory) NewRedisInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.RedisPluginInstance {
	f.logger.Debug("new Redis plugin instance created")
	ri := &redisFilterInstance{
		logger: f.logger,
		ctx:    ctx,
	}

	if es, err := services.GetService[eventstore.EventStore](ctx.Context(), svcs, eventstore.TypeEventStore, ""); err != nil {
		f.logger.Error("failed to get event store", zap.Error(err))
		return nil
	} else {
		ri.eventstore = es
	}

	return ri
}

func (f *Factory) NewKafkaInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.KafkaPluginInstance {
	f.logger.Debug("new Kafka plugin instance created")
	ki := &kafkaFilterInstance{
		logger: f.logger,
		ctx:    ctx,
	}

	if es, err := services.GetService[eventstore.EventStore](ctx.Context(), svcs, eventstore.TypeEventStore, ""); err != nil {
		f.logger.Error("failed to get event store", zap.Error(err))
		return nil
	} else {
		ki.eventstore = es
	}

	return ki
}

func (f *Factory) Destroy() {
	f.logger.Debug("plugin destroyed")
}

func (f *Factory) PluginType() plugins.PluginType {
	return PluginTypeReport
}
