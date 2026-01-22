package accesslogs

import (
	"io"
	"os"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/rulekitsvc"

	"github.com/qpoint-io/rulekit"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v3"
)

const (
	pluginTypeAccessLogs plugins.PluginType = "access_logs"
	pluginTypeDebug      plugins.PluginType = "debug"
)

var (
	accessLogsWriter io.Writer = os.Stdout
)

type factory struct {
	pluginType plugins.PluginType
	format     outputFormat
	logger     *zap.Logger
	writer     *zap.Logger
	config     *AccessLogConfig
}

type AccessLogConfig struct {
	Mode   displayMode  `json:"mode" yaml:"mode"`     // summary, details, full
	Format outputFormat `json:"format" yaml:"format"` // json, console
	Rules  []logRule    `json:"rules" yaml:"rules"`
}

func NewConsoleJSONFilter() *factory {
	return &factory{
		pluginType: pluginTypeAccessLogs,
		format:     outputFormatJSON,
	}
}

func NewConsoleHttpFilter() *factory {
	return &factory{
		pluginType: pluginTypeDebug,
		format:     outputFormatConsole,
	}
}

func (f *factory) Init(logger *zap.Logger, config yaml.Node) {
	f.logger = logger
	f.writer = createWriter(accessLogsWriter)
	f.config = &AccessLogConfig{}

	// parse
	var cfg AccessLogConfig
	if err := config.Decode(&cfg); err != nil {
		logger.Error("error decoding config", zap.Error(err))
		return
	}

	if cfg.Format == "" {
		cfg.Format = f.format
	}

	for i := range cfg.Rules {
		name, expr := cfg.Rules[i].Name, cfg.Rules[i].Expr
		if expr == "" {
			logger.Warn("skipping rule with empty expression",
				zap.String("rule_name", name),
			)
			continue
		}

		rule, err := rulekit.Parse(expr)
		if err != nil {
			logger.Error("error parsing log rule",
				zap.String("rule_name", name),
				zap.String("rule_expr", expr),
				zap.Error(err),
			)
			continue
		}

		cfg.Rules[i].rule = rule
	}

	// Adds cross support for headers -> details (from http_capture)
	if cfg.Mode == displayModeHeaders {
		cfg.Mode = displayModeDetails
	}

	f.config = &cfg
}

func (f *factory) NewHttpInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	f.logger.Debug("new HTTP plugin instance created")
	i := &filterInstance{
		ctx: ctx,

		logger: f.logger,
		writer: f.writer,

		mode:   f.config.Mode,
		format: f.config.Format,
		rules:  f.config.Rules,
	}

	if rk, err := services.GetService[rulekitsvc.Service](ctx.Context(), svcs, rulekitsvc.TypeRulekit, ""); err != nil {
		if len(f.config.Rules) > 0 {
			// only log an error if it's relevant
			f.logger.Error("failed to get rulekit, configured rules will be ignored", zap.Error(err))
		}
	} else {
		i.rulekit = rk
	}

	return i
}

func (f *factory) NewRedisInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.RedisPluginInstance {
	var mode displayMode
	var format outputFormat
	if f.config != nil {
		mode = f.config.Mode
		format = f.config.Format
	}

	// fall back to the factory's default format if none was configured
	if format == "" {
		format = f.format
	}

	return &redisFilterInstance{
		ctx:    ctx,
		logger: f.logger,
		writer: f.writer,
		mode:   mode,
		format: format,
	}
}

func (f *factory) Destroy() {
	f.logger.Debug("plugin destroyed")
}

func createWriter(writer io.Writer) *zap.Logger {
	encoderConfig := zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "", // Remove level
		TimeKey:        "", // Remove time
		NameKey:        "", // Remove logger name
		CallerKey:      "", // Remove caller
		StacktraceKey:  "", // Remove stacktrace
		LineEnding:     "",
		EncodeLevel:    func(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {},
		EncodeTime:     func(time.Time, zapcore.PrimitiveArrayEncoder) {},
		EncodeDuration: func(d time.Duration, enc zapcore.PrimitiveArrayEncoder) {},
		EncodeCaller:   func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {},
	}

	// create a core that writes logs to the io.Writer
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(writer),
		zapcore.InfoLevel,
	)

	// create the logger
	logger := zap.New(core)

	return logger
}

func (f *factory) PluginType() plugins.PluginType {
	if f.pluginType == "" {
		return pluginTypeAccessLogs
	}
	return f.pluginType
}
