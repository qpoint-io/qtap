package reporter

import (
	"context"
	"fmt"
	"time"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
)

const Type services.ServiceType = "reporter"

type Factory struct {
	svcRegistry *services.ServiceRegistry
	logger      *zap.Logger
	config      *Config
}

type Config struct {
	EventStoreID string

	// FirstReportDeadline ensures that a report is sent after a connection has been open for at least this duration.
	FirstReportDeadline time.Duration

	// ReportInterval sets an optional interval for recurring reports following the first report.
	ReportInterval time.Duration
}

func (f *Factory) FactoryType() services.ServiceType {
	return Type
}

func (f *Factory) ServiceType() services.ServiceType {
	return Type
}

// SetServiceRegistry implements the SetServiceRegistry interface.
func (f *Factory) SetServiceRegistry(registry *services.ServiceRegistry) {
	f.svcRegistry = registry
}

func (f *Factory) Init(ctx context.Context, cfg any) error {
	c, ok := cfg.(*Config)
	if !ok {
		return fmt.Errorf("invalid config type: %T wanted Config", cfg)
	}
	f.config = c
	f.logger = zap.L().With(zap.String("service_factory", f.FactoryType().String()))

	return nil
}

func (f *Factory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	es, err := services.GetService[eventstore.EventStore](ctx, svcRegistry, eventstore.TypeEventStore, f.config.EventStoreID)
	if err != nil {
		return nil, fmt.Errorf("getting event store: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	return &service{
		ctx:        ctx,
		cancel:     cancel,
		eventStore: es,
		logger:     f.logger,

		firstReportDeadline: f.config.FirstReportDeadline,
		reportInterval:      f.config.ReportInterval,
		logToConsole:        f.config.EventStoreID == "", // log to the console only if this is the default reporter
	}, nil
}
