package errordetection

import (
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"gopkg.in/yaml.v3"

	"go.uber.org/zap"
)

const (
	pluginTypeErrordetection plugins.PluginType = "detect_errors"
)

type Factory struct {
	logger *zap.Logger

	config *DetectErrorConfig

	stopChan  chan struct{}
	closeOnce sync.Once
}

func (f *Factory) Init(logger *zap.Logger, config yaml.Node) {
	f.logger = logger
	f.stopChan = make(chan struct{})

	var cfg DetectErrorConfig
	if err := config.Decode(&cfg); err != nil {
		f.logger.Error("error decoding config", zap.Error(err))
		return
	}

	f.config = &cfg
}

func (f *Factory) NewHttpInstance(conn plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	ctx, _ := tracer.Start(conn.Context(), "Plugin[detect_errors]")
	// this span is destroyed by the filterInstance.Destroy() method

	f.logger.Debug("new plugin instance created")
	fi := &filterInstance{
		logger: f.logger,
		conn:   conn,
		ctx:    ctx,
		rules:  f.config.Rules,
		// only record req body if a rule requires it
		shouldRecordReqBody: rulesRequireReqBody(f.config.Rules),
		tags: func() []string {
			return conn.Meta().Tags().List()
		},
		now: time.Now,
	}

	if es, err := services.GetService[eventstore.EventStore](conn.Context(), svcs, eventstore.TypeEventStore, ""); err != nil {
		f.logger.Error("failed to get event store", zap.Error(err))
		return nil
	} else {
		fi.eventstore = es
	}
	if os, err := services.GetService[objectstore.ObjectStore](conn.Context(), svcs, objectstore.TypeObjectStore, ""); err != nil {
		f.logger.Error("failed to get object store", zap.Error(err))
	} else {
		fi.objectstore = os
	}

	return fi
}

func (f *Factory) Destroy() {
	// TODO(Jon): this is likely a deeper problem and should be investigated
	f.closeOnce.Do(func() {
		close(f.stopChan)
		f.logger.Debug("plugin destroyed")
	})
}

func rulesRequireReqBody(rules []Rule) bool {
	for _, rule := range rules {
		if rule.RecordReqBody {
			return true
		}
	}
	return false
}

func (f *Factory) PluginType() plugins.PluginType {
	return pluginTypeErrordetection
}
