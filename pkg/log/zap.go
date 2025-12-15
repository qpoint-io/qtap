package log

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/term"
)

var (
	colorMuted = lipgloss.AdaptiveColor{Light: "#b0b0b0", Dark: "#737373"}
	muteText   = lipgloss.NewStyle().Foreground(colorMuted).Render
)

// Log levels
const (
	// QoL aliases for native zap levels
	DebugLevel  = zapcore.DebugLevel
	InfoLevel   = zapcore.InfoLevel
	WarnLevel   = zapcore.WarnLevel
	ErrorLevel  = zapcore.ErrorLevel
	DPanicLevel = zapcore.DPanicLevel
	PanicLevel  = zapcore.PanicLevel
	FatalLevel  = zapcore.FatalLevel

	// Custom log levels
	//
	// Use these on a *zap.Logger like so:
	//		zap.L().Log(log.TraceLevel, "trace msg")
	TraceLevel = zapcore.DebugLevel - 1
)

// Encoding types
const (
	EncodingConsole = "console"
	EncodingJSON    = "json"
)

// NewLogger creates a new zap.Logger with qtap defaults:
//
//   - level: info
//   - encoding: console if running in an interactive terminal, json otherwise
//
// You may pass a mutator function to modify the config.
// Use log.SetEncoding() to set the encoding type.
func NewLogger(mut func(cfg *zap.Config)) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.DisableCaller = true
	cfg.Level = zap.NewAtomicLevelAt(InfoLevel)
	cfg.Encoding = EncodingJSON
	cfg.EncoderConfig.MessageKey = "message"

	if term.IsTerminal(int(os.Stdout.Fd())) {
		SetEncoding(&cfg, EncodingConsole)
	} else {
		SetEncoding(&cfg, EncodingJSON)
	}

	if cfg.Level.Level() <= DebugLevel {
		cfg.Sampling = nil // disable sampling in debug mode
	}

	if mut != nil {
		mut(&cfg)
	}

	return cfg.Build()
}

func SetEncoding(cfg *zap.Config, encoding string) {
	switch strings.ToLower(encoding) {
	case EncodingConsole:
		cfg.Encoding = EncodingConsole
		cfg.EncoderConfig.EncodeLevel = ConsoleLevelEncoder
		cfg.EncoderConfig.EncodeTime = ConsoleTimeEncoder
		cfg.EncoderConfig.EncodeDuration = ConsoleDurationEncoder
	case EncodingJSON:
		cfg.Encoding = EncodingJSON
		cfg.EncoderConfig.EncodeLevel = LevelEncoder
		cfg.EncoderConfig.EncodeTime = zapcore.EpochTimeEncoder
		cfg.EncoderConfig.EncodeDuration = zapcore.SecondsDurationEncoder
	}
}

func SetLevel(cfg *zap.Config, level string) {
	l, err := StringToLevel(level)
	if err == nil {
		cfg.Level.SetLevel(l)
	}
}

func StringToLevel(levelStr string) (zapcore.Level, error) {
	if strings.EqualFold(levelStr, "trace") {
		return TraceLevel, nil
	}

	var l zapcore.Level
	err := l.UnmarshalText([]byte(levelStr))
	if err != nil {
		return 0, fmt.Errorf("invalid log level: %s", levelStr)
	}
	return l, nil
}

func LevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	if level == TraceLevel {
		enc.AppendString("trace")
		return
	}
	zapcore.LowercaseLevelEncoder(level, enc)
}

func ConsoleLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	if level == TraceLevel {
		enc.AppendString(muteText("TRACE"))
		return
	}
	zapcore.CapitalColorLevelEncoder(level, enc)
}

func ConsoleTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	mutedColon := muteText(":")
	enc.AppendString(muteText(t.Format("2006-01-02 ")) + t.Format("15") + mutedColon + t.Format("04") + mutedColon + t.Format("05") + muteText(".000"))
}

func ConsoleDurationEncoder(d time.Duration, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(d.String())
}

/*

TODO: add dev vs prod configs

	- dev:  `zapcore.FullCallerEncoder` /full/path/to/package/file:line
	  prod: `zapcore.ShortCallerEncoder` package/file:line

	- dev: DPanic should panic()
	  prod: DPanic should log (this is built into zap)

*/
