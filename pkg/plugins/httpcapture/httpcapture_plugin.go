package httpcapture

import (
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"github.com/qpoint-io/rulekit"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

var tracer = telemetry.Tracer()

const (
	pluginTypeHttpCapture plugins.PluginType = "http_capture"
)

type captureLevel string

const (
	// CaptureLevelNone signifies that no HTTP transaction data will be captured.
	CaptureLevelNone captureLevel = "none"

	// CaptureLevelSummary captures basic HTTP transaction details (e.g., method, URL, status code, duration).
	CaptureLevelSummary captureLevel = "summary"

	// CaptureLevelHeaders captures everything in Summary, plus HTTP request and response headers.
	CaptureLevelHeaders captureLevel = "headers"

	// CaptureLevelFull captures everything in Headers, plus HTTP request and response bodies (i.e., the complete transaction).
	CaptureLevelFull captureLevel = "full"
)

type outputFormat string

const (
	outputFormatJSON outputFormat = "json"
	outputFormatText outputFormat = "text"
)

type logRule struct {
	Name   string       `yaml:"name"`
	Expr   string       `yaml:"expr"`
	Level  captureLevel `yaml:"level"`
	Format outputFormat `yaml:"format,omitempty"`

	rule rulekit.Rule `yaml:"-"`
}

type HttpCaptureConfig struct {
	Level  captureLevel `json:"level" yaml:"level"`
	Format outputFormat `json:"format" yaml:"format"`
	Rules  []logRule    `json:"rules" yaml:"rules"`
}

type Factory struct {
	logger *zap.Logger

	config *HttpCaptureConfig
}

func (f *Factory) Init(logger *zap.Logger, config yaml.Node) {
	f.logger = logger

	// Default configuration
	var cfg HttpCaptureConfig = HttpCaptureConfig{
		Level:  CaptureLevelNone,
		Format: outputFormatJSON,
		Rules:  []logRule{},
	}

	// Parse provided configuration
	if err := config.Decode(&cfg); err != nil {
		logger.Error("error decoding config", zap.Error(err))
	}

	// Compile rule expressions
	for i := range cfg.Rules {
		// If rule doesn't specify a format, use the global format
		if cfg.Rules[i].Format == "" {
			cfg.Rules[i].Format = cfg.Format
		}

		var err error
		cfg.Rules[i].rule, err = rulekit.Parse(cfg.Rules[i].Expr)
		if err != nil {
			logger.Error("error parsing log rule", zap.Error(err), zap.String("rule", cfg.Rules[i].Name))
		}
	}

	f.config = &cfg
	f.logger.Debug("http_capture plugin initialized",
		zap.String("default_level", string(cfg.Level)),
		zap.String("default_format", string(cfg.Format)),
		zap.Int("rules", len(cfg.Rules)))
}

func (f *Factory) NewInstance(conn plugins.PluginContext, svcs ...services.Service) plugins.HttpPluginInstance {
	ctx, _ := tracer.Start(conn.Context(), "Plugin[http_capture]")
	// this span is destroyed by the instance.Destroy() method

	f.logger.Debug("new plugin instance created")
	fi := &instance{
		logger: f.logger,
		conn:   conn,
		ctx:    ctx,
		level:  f.config.Level,
		format: f.config.Format,
		rules:  f.config.Rules,
	}

	// Get the eventstore service
	for _, s := range svcs {
		if i, ok := s.(eventstore.EventStore); ok {
			fi.eventstore = i
			break
		}
	}

	return fi
}

func (f *Factory) RequiredServices() []services.ServiceType {
	return []services.ServiceType{eventstore.TypeEventStore}
}

func (f *Factory) Destroy() {
	f.logger.Debug("http_capture plugin destroyed")
}

func (f *Factory) PluginType() plugins.PluginType {
	return pluginTypeHttpCapture
}
