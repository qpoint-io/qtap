package httpcapture

import (
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/rulekitsvc"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"github.com/qpoint-io/rulekit"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

var tracer = telemetry.Tracer()

const (
	PluginTypeHttpCapture plugins.PluginType = "http_capture"
)

type CaptureLevel string

const (
	// CaptureLevelNone signifies that no HTTP transaction data will be captured.
	CaptureLevelNone CaptureLevel = "none"

	// CaptureLevelSummary captures basic HTTP transaction details (e.g., method, URL, status code, duration).
	CaptureLevelSummary CaptureLevel = "summary"

	// CaptureLevelHeaders captures everything in Summary, plus HTTP request and response headers.
	CaptureLevelHeaders CaptureLevel = "headers"

	// CaptureLevelFull captures everything in Headers, plus HTTP request and response bodies (i.e., the complete transaction).
	CaptureLevelFull CaptureLevel = "full"
)

type OutputFormat string

const (
	OutputFormatJSON OutputFormat = "json"
	OutputFormatText OutputFormat = "text"
)

type LogRule struct {
	Name   string       `yaml:"name"`
	Expr   string       `yaml:"expr"`
	Level  CaptureLevel `yaml:"level"`
	Format OutputFormat `yaml:"format,omitempty"`

	rule rulekit.Rule `yaml:"-"`
}

type HttpCaptureConfig struct {
	Level  CaptureLevel `json:"level" yaml:"level"`
	Format OutputFormat `json:"format" yaml:"format"`
	Rules  []LogRule    `json:"rules" yaml:"rules"`
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
		Format: OutputFormatJSON,
		Rules:  []LogRule{},
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

func (f *Factory) NewInstance(conn plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	ctx, _ := tracer.Start(conn.Context(), "Plugin[http_capture]")
	// this span is destroyed by the instance.Destroy() method

	f.logger.Debug("new plugin instance created")
	fi := &instance{
		logger: f.logger,
		ctx:    ctx,
		conn:   conn,
		level:  f.config.Level,
		format: f.config.Format,
		rules:  f.config.Rules,
	}

	if es, ok := svcs.Get(eventstore.TypeEventStore).(eventstore.EventStore); ok {
		fi.eventstore = es
	}
	if rk, ok := svcs.Get(rulekitsvc.TypeRulekit).(rulekitsvc.Service); ok {
		fi.rulekit = rk
	}

	return fi
}

func (f *Factory) RequiredServices() []services.ServiceType {
	return []services.ServiceType{
		eventstore.TypeEventStore,
		rulekitsvc.TypeRulekit,
	}
}

func (f *Factory) Destroy() {
	f.logger.Debug("http_capture plugin destroyed")
}

func (f *Factory) PluginType() plugins.PluginType {
	return PluginTypeHttpCapture
}
