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
	Tags []string `json:"tags"`
}

type Factory struct {
	logger *zap.Logger
}

func (f *Factory) Init(logger *zap.Logger, config yaml.Node) {
	f.logger = logger
}

func (f *Factory) NewHttpInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	f.logger.Debug("new plugin instance created")
	fi := &filterInstance{
		logger: f.logger,
		ctx:    ctx,
	}

	if es, err := services.GetService[eventstore.EventStore](ctx.Context(), svcs, eventstore.TypeEventStore, ""); err != nil {
		f.logger.Error("failed to get event store", zap.Error(err))
		return nil
	} else {
		fi.eventstore = es
	}

	return fi
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

func (f *Factory) NewMySQLInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.MySQLPluginInstance {
	f.logger.Debug("new MySQL plugin instance created")
	mi := &mysqlFilterInstance{
		logger: f.logger,
		ctx:    ctx,
	}

	if es, err := services.GetService[eventstore.EventStore](ctx.Context(), svcs, eventstore.TypeEventStore, ""); err != nil {
		f.logger.Error("failed to get event store", zap.Error(err))
		return nil
	} else {
		mi.eventstore = es
	}

	return mi
}

func (f *Factory) NewPostgresInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.PostgresPluginInstance {
	f.logger.Debug("new Postgres plugin instance created")
	pi := &postgresFilterInstance{
		logger: f.logger,
		ctx:    ctx,
	}

	if es, err := services.GetService[eventstore.EventStore](ctx.Context(), svcs, eventstore.TypeEventStore, ""); err != nil {
		f.logger.Error("failed to get event store", zap.Error(err))
		return nil
	} else {
		pi.eventstore = es
	}

	return pi
}

func (f *Factory) Destroy() {
	f.logger.Debug("plugin destroyed")
}

func (f *Factory) PluginType() plugins.PluginType {
	return PluginTypeReport
}
