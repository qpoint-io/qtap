package devtools

import (
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/httpcapture"
	"github.com/qpoint-io/qtap/pkg/services"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

/*
	The devtools plugin uses the `http_capture` plugin to broadcast all http transactions to any subscribed clients.
*/

const (
	PluginTypeDevTools plugins.PluginType = "devtools"
)

var httpCaptureConfig yaml.Node

func init() {
	err := httpCaptureConfig.Encode(&httpcapture.HttpCaptureConfig{
		Level:  httpcapture.CaptureLevelFull,
		Format: httpcapture.OutputFormatJSON,
		ObjectStoreSelector: config.ObjectStoreSelector{
			ID: "devtools",
		},
	})
	if err != nil {
		panic(err)
	}
}

type PluginFactory struct {
	httpCapture *httpcapture.Factory
	logger      *zap.Logger
}

func (f *PluginFactory) Init(logger *zap.Logger, config yaml.Node) {
	// initialize the http_capture plugin and pass our own custom config
	f.httpCapture.Init(
		logger.With(zap.Stringer("plugin", httpcapture.PluginTypeHttpCapture)),
		httpCaptureConfig,
	)
}

func (f *PluginFactory) NewHttpInstance(conn plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	f.logger.Debug("new plugin instance created")

	// TODO: should we return a no-op plugin instance if there are no connected devtools clients?
	return f.httpCapture.NewHttpInstance(conn, svcs)
}

func (f *PluginFactory) Destroy() {}

func (f *PluginFactory) PluginType() plugins.PluginType {
	return PluginTypeDevTools
}
