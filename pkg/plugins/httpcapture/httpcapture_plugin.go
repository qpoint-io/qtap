package httpcapture

import (
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
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
	CaptureLevelDetails CaptureLevel = "details" // Legacy from `access_logs`

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
	Level               CaptureLevel               `json:"level" yaml:"level"`
	Format              OutputFormat               `json:"format" yaml:"format"`
	ObjectStoreSelector config.ObjectStoreSelector `json:"object_store" yaml:"object_store"`
	Rules               []LogRule                  `json:"rules" yaml:"rules"`
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

	// Legacy support for `details` level
	if cfg.Level == CaptureLevelDetails {
		logger.Warn("`details` level is deprecated, using `headers` level instead")
		cfg.Level = CaptureLevelHeaders
	}

	f.config = &cfg
	f.logger.Debug("http_capture plugin initialized",
		zap.String("default_level", string(cfg.Level)),
		zap.String("default_format", string(cfg.Format)),
		zap.Int("rules", len(cfg.Rules)))
}

func (f *Factory) NewHttpInstance(conn plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
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

	os, err := services.GetService[objectstore.ObjectStore](ctx, svcs, objectstore.TypeObjectStore, f.config.ObjectStoreSelector.ID)
	if err != nil {
		f.logger.Error("failed to get object store", zap.Error(err))
		// object store is a hard requirement for this plugin
		return nil
	}
	fi.objectstore = os

	if rk, err := services.GetService[rulekitsvc.Service](ctx, svcs, rulekitsvc.TypeRulekit, ""); err != nil {
		if len(f.config.Rules) > 0 {
			// only log an error if it's relevant
			f.logger.Error("failed to get rulekit, configured rules will be ignored", zap.Error(err))
		}
	} else {
		fi.rulekit = rk
	}

	return fi
}

func (f *Factory) Destroy() {
	f.logger.Debug("http_capture plugin destroyed")
}

func (f *Factory) PluginType() plugins.PluginType {
	return PluginTypeHttpCapture
}
