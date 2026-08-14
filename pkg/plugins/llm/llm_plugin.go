package llm

import (
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/conversations"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

var tracer = telemetry.Tracer()

const (
	PluginTypeLLM plugins.PluginType = "llm"
)

// LLMConfig represents the configuration for the LLM plugin
//
// Example YAML configuration:
// ```yaml
//
// ```
type LLMConfig struct {
}

type Factory struct {
	logger *zap.Logger

	config        *LLMConfig
	conversations *conversations.Tracker
}

func (f *Factory) Init(logger *zap.Logger, config yaml.Node) {
	f.logger = logger

	// Parse provided configuration
	var cfg LLMConfig
	if err := config.Decode(&cfg); err != nil {
		logger.Error("error decoding config", zap.Error(err))
	}
	f.config = &cfg

	f.conversations = conversations.NewTracker()

	f.logger.Debug("llm plugin initialized")
}

func (f *Factory) NewHttpInstance(conn plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	ctx, _ := tracer.Start(conn.Context(), "Plugin[llm]")
	// this span is destroyed by the instance.Destroy() method

	f.logger.Debug("new plugin instance created")
	fi := &instance{
		logger:        f.logger,
		conn:          conn,
		ctx:           ctx,
		conversations: f.conversations,
	}

	if os, err := services.GetService[objectstore.ObjectStore](ctx, svcs, objectstore.TypeObjectStore, ""); err != nil {
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
	return PluginTypeLLM
}
