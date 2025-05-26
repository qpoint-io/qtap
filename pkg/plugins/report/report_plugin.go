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

func (f *Factory) NewInstance(ctx plugins.PluginContext, svcs ...services.Service) plugins.HttpPluginInstance {
	f.logger.Debug("new plugin instance created")
	fi := &filterInstance{
		logger: f.logger,
		ctx:    ctx,
	}

	for _, s := range svcs {
		if i, ok := s.(eventstore.EventStore); ok {
			fi.eventstore = i
		}
	}

	return fi
}

func (f *Factory) RequiredServices() []services.ServiceType {
	return []services.ServiceType{eventstore.TypeEventStore}
}

func (f *Factory) Destroy() {
	f.logger.Debug("filter destroyed")
}

func (f *Factory) PluginType() plugins.PluginType {
	return PluginTypeReport
}
