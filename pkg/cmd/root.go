package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unicode"

	"github.com/qpoint-io/qtap/pkg/buildinfo"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/term"
)

var (
	// Global flags
	dataDir               string
	qpointConfig          string
	deploymentTags        string
	logLevel              string
	logEncoding           string
	logCaller             bool
	statusListen          string
	bpfTraceQuery         string
	certInjectionStrategy string
	tlsOkStrategy         string
	httpBufferSize        string

	rootCmd = &cobra.Command{
		Use:     "qtap",
		Short:   "Tap into traffic streams and analyze without a proxy.",
		Long:    "An eBPF agent that captures pre-encrypted network traffic, providing rich context about egress connections and their originating processes.",
		Version: buildinfo.Version(),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Validate http-buffer-size
			size, err := parseSizeString(httpBufferSize)
			if err != nil {
				return err
			}
			maxSize := int64(2 * 1024 * 1024 * 1024) // 2GB
			if size > maxSize {
				return errors.New("http-buffer-size exceeds maximum allowed size of 2GB")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			logger := initLogger()
			defer syncLogger(logger)

			runTapCmd(logger)
		},
	}
)

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Logging options
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level",
		getEnvOr("LOG_LEVEL", "info"),
		"Log level (debug, info, warn, error, dpanic, panic, fatal)")
	rootCmd.PersistentFlags().StringVar(&logEncoding, "log-encoding",
		getEnvOr("LOG_ENCODING", defaultLogEncoding()),
		"Log encoding (console, json)")
	rootCmd.PersistentFlags().BoolVar(&logCaller, "log-caller",
		getEnvBoolOr("LOG_CALLER", false),
		"Log caller")

	// Add commands
	rootCmd.AddCommand(reloadConfigCmd)
}

// defaultLogEncoding determines the default log encoding based on whether stdout is a terminal
func defaultLogEncoding() string {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return "console"
	}
	return "json"
}

// getEnvBoolOr returns environment variable as bool or default if not set
func getEnvBoolOr(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		if value == "true" || value == "1" {
			return true
		}
		if value == "false" || value == "0" {
			return false
		}
	}
	return defaultValue
}

// getEnvOr returns environment variable value or default if not set
func getEnvOr(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// getEnvIntOr returns environment variable as int or default if not set
func getEnvIntOr(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func convertStringToZapLevel(levelStr string) (zapcore.Level, error) {
	var l zapcore.Level
	err := l.UnmarshalText([]byte(levelStr))
	if err != nil {
		return 0, fmt.Errorf("invalid log level: %s", levelStr)
	}
	return l, nil
}

func initLogger() *zap.Logger {
	level, err := convertStringToZapLevel(logLevel)
	if err != nil {
		panic("error: invalid log level: " + err.Error())
	}

	cfg := zap.NewProductionConfig()
	cfg.DisableCaller = !logCaller
	cfg.Level = zap.NewAtomicLevelAt(level)
	cfg.Encoding = logEncoding
	cfg.EncoderConfig.MessageKey = "message"

	if strings.EqualFold(logEncoding, "console") {
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000")
	}

	l, err := cfg.Build()
	if err != nil {
		panic("error: couldn't create a logger: " + err.Error())
	}

	// Replace the global logger with the one we just created
	// This can be accessed via zap.L()
	zap.ReplaceGlobals(l)

	return l
}

func syncLogger(logger *zap.Logger) {
	// The EINVAL error catch is a
	// workaround for https://github.com/uber-go/zap/issues/772
	if err := logger.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		fmt.Fprintf(os.Stderr, "syncing logger: %v\n", err)
		return
	}
}

// parseSizeString converts strings like "1kb", "2mb", "2gb" to bytes
func parseSizeString(size string) (int64, error) {
	size = strings.TrimSpace(strings.ToUpper(size))

	units := map[string]int64{
		"B":  1,
		"KB": 1024,
		"MB": 1024 * 1024,
		"GB": 1024 * 1024 * 1024,
	}

	var value float64
	var unit string
	var err error

	// Find where the numeric part ends and the unit begins
	i := strings.IndexFunc(size, func(r rune) bool {
		return !unicode.IsDigit(r) && r != '.'
	})

	if i == -1 {
		// If no unit found, assume bytes
		value, err = strconv.ParseFloat(size, 64)
		unit = "B"
	} else {
		value, err = strconv.ParseFloat(size[:i], 64)
		unit = strings.TrimSpace(size[i:])
	}

	if err != nil {
		return 0, fmt.Errorf("invalid number format: %w", err)
	}

	multiplier, ok := units[unit]
	if !ok {
		return 0, fmt.Errorf("invalid unit: %s", unit)
	}

	val := int64(value * float64(multiplier))

	return val, nil
}
