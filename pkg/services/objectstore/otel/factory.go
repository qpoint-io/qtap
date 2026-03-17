package otel

import (
	"context"
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
	"google.golang.org/grpc"
)

// ensure we implement the ObjectStore interface
var _ objectstore.ObjectStore = (*ObjectStore)(nil)

const (
	Type services.ServiceType = "otel"
)

type Factory struct {
	objectstore.BaseObjectStore

	logger             *zap.Logger
	logProvider        *sdklog.LoggerProvider
	serviceName        string
	environment        string
	endpoint           string
	eventStoreSelector config.EventStoreSelector
}

var mustacheRegex = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

func expandEnvVarsInBraces(s string) string {
	return mustacheRegex.ReplaceAllStringFunc(s, func(str string) string {
		matches := mustacheRegex.FindStringSubmatch(str)
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
	f.logger = zap.L().With(zap.String("service_factory", f.FactoryType().String()))

	c, ok := cfg.(config.ServiceObjectStore)
	if !ok {
		return fmt.Errorf("invalid config type: %T wanted config.ServiceObjectStore", cfg)
	}

	if c.Type != config.ObjectStoreType_OTEL {
		return fmt.Errorf("invalid object store type: %s", c.Type)
	}

	f.eventStoreSelector = c.EventStore

	// Extract configuration with protocol-specific defaults
	protocol := "grpc" // Default protocol
	if c.Protocol != "" {
		protocol = c.Protocol
	}

	// Set default endpoint based on protocol
	switch protocol {
	case "http":
		f.endpoint = "localhost:4318" // Default OTLP HTTP endpoint
	case "stdout":
		f.endpoint = "" // stdout doesn't use endpoints
	default:
		f.endpoint = "localhost:4317" // Default OTLP gRPC endpoint
	}

	if c.OTelEndpoint != "" {
		f.endpoint = expandEnvVarsInBraces(c.OTelEndpoint)
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
		exporter, err = f.createHTTPExporter(ctx, f.endpoint, c.ObjectStoreOTelConfig)
	case "grpc":
		exporter, err = f.createGRPCExporter(ctx, f.endpoint, c.ObjectStoreOTelConfig)
	case "stdout":
		exporter, err = f.createStdoutExporter(c.ObjectStoreOTelConfig)
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
				sdklog.WithExportMaxBatchSize(32), // Smaller batches for large payload data
			),
		),
	)

	f.logger.Info("OpenTelemetry objectstore factory initialized",
		zap.String("endpoint", f.endpoint),
		zap.String("protocol", protocol),
		zap.String("service_name", f.serviceName),
		zap.String("environment", f.environment))

	return nil
}

func (f *Factory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	// Resolve optional EventStore from registry
	eventStore, err := services.GetService[eventstore.EventStore](ctx, svcRegistry, eventstore.TypeEventStore, f.eventStoreSelector.ID)
	if err != nil {
		return nil, err
	}

	// Create logger for this service
	logger := f.logProvider.Logger("qtap.objectstore")

	return &ObjectStore{
		logger:      logger,
		endpoint:    f.endpoint,
		eventStore:  eventStore,
		serviceName: f.serviceName,
		environment: f.environment,
	}, nil
}

// FactoryType returns the service factory type
func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", objectstore.TypeObjectStore, Type))
}

// createGRPCExporter creates a gRPC OTLP log exporter
func (f *Factory) createGRPCExporter(ctx context.Context, endpoint string, c config.ObjectStoreOTelConfig) (sdklog.Exporter, error) {
	opts := []otlploggrpc.Option{
		otlploggrpc.WithEndpoint(endpoint),
		// Object store payloads can be large, increase max message size to 32MB
		otlploggrpc.WithDialOption(grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(32 * 1024 * 1024))),
	}

	// Configure TLS
	if c.TLS.Enabled {
		if c.TLS.InsecureSkipVerify {
			opts = append(opts, otlploggrpc.WithTLSCredentials(nil))
		}
	} else {
		opts = append(opts, otlploggrpc.WithInsecure())
	}

	headers := make(map[string]string)
	for k, v := range c.Headers {
		headers[k] = v.String()
	}

	if len(headers) > 0 {
		opts = append(opts, otlploggrpc.WithHeaders(headers))
	}

	return otlploggrpc.New(ctx, opts...)
}

// createHTTPExporter creates an HTTP OTLP log exporter
func (f *Factory) createHTTPExporter(ctx context.Context, endpoint string, c config.ObjectStoreOTelConfig) (sdklog.Exporter, error) {
	var opts []otlploghttp.Option

	// Determine if endpoint is a full URL or just host:port
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		opts = append(opts, otlploghttp.WithEndpointURL(endpoint))
	} else {
		opts = append(opts, otlploghttp.WithEndpoint(endpoint))

		if !c.TLS.Enabled {
			opts = append(opts, otlploghttp.WithInsecure())
		}
	}

	headers := make(map[string]string)
	for k, v := range c.Headers {
		headers[k] = v.String()
	}

	if len(headers) > 0 {
		opts = append(opts, otlploghttp.WithHeaders(headers))
	}

	return otlploghttp.New(ctx, opts...)
}

// createStdoutExporter creates a stdout OTLP log exporter
func (f *Factory) createStdoutExporter(_ config.ObjectStoreOTelConfig) (sdklog.Exporter, error) {
	var opts []stdoutlog.Option

	if term.IsTerminal(int(os.Stdout.Fd())) {
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
