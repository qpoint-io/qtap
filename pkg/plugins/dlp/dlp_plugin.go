package dlp

import (
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

const (
	pluginTypeDLP plugins.PluginType = "data_loss_protection"
)

type Factory struct {
	logger *zap.Logger

	config *DLPConfig
	engine *DLPEngine
}

func (f *Factory) Init(logger *zap.Logger, config yaml.Node) {
	f.logger = logger

	// parse
	var cfg DLPConfig
	if err := config.Decode(&cfg); err != nil {
		logger.Error("error decoding config", zap.Error(err))
		return
	}

	f.config = &cfg

	f.engine = NewDLPEngine(f.config.Rules)
}

func (f *Factory) NewHttpInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	f.logger.Debug("new plugin instance created")
	fi := &filterInstance{
		ctx: ctx,

		logger: f.logger,
		config: f.config,
		engine: f.engine,
		rules:  f.config.Rules,
	}

	if es, err := services.GetService[eventstore.EventStore](ctx.Context(), svcs, eventstore.TypeEventStore, ""); err != nil {
		f.logger.Error("failed to get event store", zap.Error(err))
		return nil
	} else {
		fi.eventstore = es
	}
	if os, err := services.GetService[objectstore.ObjectStore](ctx.Context(), svcs, objectstore.TypeObjectStore, ""); err != nil {
		f.logger.Error("failed to get object store", zap.Error(err))
		return nil
	} else {
		fi.objectstore = os
	}

	return fi
}

func (f *Factory) Destroy() {
	f.logger.Debug("plugin destroyed")
}

func (f *Factory) PluginType() plugins.PluginType {
	return pluginTypeDLP
}
