// pkg/services/eventstore/otel/factory.go
package otel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/qpoint-io/qtap/pkg/buildinfo"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.uber.org/zap"
	"golang.org/x/term"
)

// Sample config:
// eventstore:
//   - type: otel
//     endpoint: "localhost:4317"  # OTLP endpoint (gRPC: 4317, HTTP: 4318)
//     protocol: grpc              # "grpc", "http", or "stdout"
//     headers:                    # Optional headers for auth
//       api-key: "${OTEL_API_KEY}"
//     service_name: "qtap"
//     environment: "production"
//     tls:
//       enabled: true
//       insecure_skip_verify: false

const (
	Type services.ServiceType = "otel"
)

type Factory struct {
	eventstore.BaseEventStore

	logger      *zap.Logger
	logProvider *sdklog.LoggerProvider
	serviceName string
	environment string
}

func expandEnvVarsInBraces(s string) string {
	re := regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)
	return re.ReplaceAllStringFunc(s, func(str string) string {
		matches := re.FindStringSubmatch(str)
		if len(matches) == 2 {
			key := matches[1]
			if val, ok := os.LookupEnv(key); ok {
				return val
			}
		}
		return str
	})
}

func (f *Factory) Init(ctx context.Context, cfg any) error {
	c, ok := cfg.(config.ServiceEventStore)
	if !ok {
		return fmt.Errorf("invalid config type: %T wanted config.ServiceEventStore", cfg)
	}

	f.logger = zap.L().With(zap.String("service_factory", f.FactoryType().String()))

	// Extract configuration with protocol-specific defaults
	protocol := "grpc" // Default protocol
	if c.Protocol != "" {
		protocol = c.Protocol
	}

	// Set default endpoint based on protocol
	var endpoint string
	switch protocol {
	case "http":
		endpoint = "localhost:4318" // Default OTLP HTTP endpoint
	case "stdout":
		endpoint = "" // stdout doesn't use endpoints
	default:
		endpoint = "localhost:4317" // Default OTLP gRPC endpoint
	}

	if c.Endpoint != "" {
		endpoint = expandEnvVarsInBraces(c.Endpoint)
	}

	// Service name and environment from config
	f.serviceName = "qtap"
	if c.ServiceName != "" {
		f.serviceName = c.ServiceName
	}

	f.environment = "production"
	if c.Environment != "" {
		f.environment = c.Environment
	}

	// Create resource without merging to avoid schema conflicts
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(f.serviceName),
		semconv.ServiceVersion(buildinfo.Version()),
		attribute.String("environment", f.environment),
	)

	// Create exporter based on protocol
	var exporter sdklog.Exporter
	var err error
	switch protocol {
	case "http":
		exporter, err = f.createHTTPExporter(ctx, endpoint, c.EventStoreOTelConfig)
	case "grpc":
		exporter, err = f.createGRPCExporter(ctx, endpoint, c.EventStoreOTelConfig)
	case "stdout":
		exporter, err = f.createStdoutExporter(c.EventStoreOTelConfig)
	default:
		return fmt.Errorf("unsupported protocol: %s, supported protocols are 'grpc', 'http', and 'stdout'", protocol)
	}
	if err != nil {
		return fmt.Errorf("failed to create OTLP log exporter: %w", err)
	}

	// log provider
	f.logProvider = sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(
			sdklog.NewBatchProcessor(exporter,
				sdklog.WithExportInterval(time.Second),
				sdklog.WithExportMaxBatchSize(512),
			),
		),
	)

	f.logger.Info("OpenTelemetry service factory initialized",
		zap.String("endpoint", endpoint),
		zap.String("protocol", protocol),
		zap.String("service_name", f.serviceName),
		zap.String("environment", f.environment))

	return nil
}

func (f *Factory) Create(ctx context.Context) (services.Service, error) {
	of := f.Registry.Get(objectstore.TypeObjectStore)
	s, err := of.Create(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating object store: %w", err)
	}
	o, ok := s.(objectstore.ObjectStore)
	if !ok {
		return nil, errors.New("object store service is not an objectstore.ObjectStore")
	}

	// Create logger for this service
	logger := f.logProvider.Logger("qtap.eventstore")

	es := &EventStore{
		logger:      logger,
		objectStore: o,
		serviceName: f.serviceName,
		environment: f.environment,
	}

	// Initialize LogHelper
	es.SetLogger(f.logger)

	return es, nil
}

// FactoryType returns the service factory type
func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", eventstore.TypeEventStore, Type))
}

// createGRPCExporter creates a gRPC OTLP log exporter
func (f *Factory) createGRPCExporter(ctx context.Context, endpoint string, c config.EventStoreOTelConfig) (sdklog.Exporter, error) {
	// Create exporter options
	opts := []otlploggrpc.Option{
		otlploggrpc.WithEndpoint(endpoint),
	}

	// Configure TLS
	if c.TLS.Enabled {
		if c.TLS.InsecureSkipVerify {
			opts = append(opts, otlploggrpc.WithTLSCredentials(nil))
		}
		// For production, you'd configure proper TLS here
	} else {
		opts = append(opts, otlploggrpc.WithInsecure())
	}

	headers := make(map[string]string)
	for k, v := range c.Headers {
		headers[k] = v.String()
	}

	// Add headers if configured
	if len(headers) > 0 {
		opts = append(opts, otlploggrpc.WithHeaders(headers))
	}

	return otlploggrpc.New(ctx, opts...)
}

// createHTTPExporter creates an HTTP OTLP log exporter
func (f *Factory) createHTTPExporter(ctx context.Context, endpoint string, c config.EventStoreOTelConfig) (sdklog.Exporter, error) {
	// Create exporter options
	var opts []otlploghttp.Option

	// Determine if endpoint is a full URL or just host:port
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		// Full URL provided
		opts = append(opts, otlploghttp.WithEndpointURL(endpoint))
	} else {
		// Host:port provided
		opts = append(opts, otlploghttp.WithEndpoint(endpoint))

		// Configure TLS only when using WithEndpoint
		if !c.TLS.Enabled {
			opts = append(opts, otlploghttp.WithInsecure())
		}
	}
	// Note: When using WithEndpointURL, TLS is determined by the URL scheme

	headers := make(map[string]string)
	for k, v := range c.Headers {
		headers[k] = v.String()
	}

	// Add headers if configured
	if len(headers) > 0 {
		opts = append(opts, otlploghttp.WithHeaders(headers))
	}

	return otlploghttp.New(ctx, opts...)
}

// createStdoutExporter creates a stdout OTLP log exporter
func (f *Factory) createStdoutExporter(_ config.EventStoreOTelConfig) (sdklog.Exporter, error) {
	// Create exporter options
	var opts []stdoutlog.Option

	if term.IsTerminal(int(os.Stdout.Fd())) {
		// Enable pretty printing by default for better readability
		opts = append(opts, stdoutlog.WithPrettyPrint())
	}

	return stdoutlog.New(opts...)
}

// Close cleans up resources
func (f *Factory) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if f.logProvider != nil {
		return f.logProvider.Shutdown(ctx)
	}
	return nil
}
