package reporter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
)

const Type services.ServiceType = "reporter"

type Factory struct {
	factoryRegistry *services.FactoryRegistry
	logger          *zap.Logger
	config          *Config
}

type Config struct {
	EventStoreID string

	// FirstReportDeadline ensures that a report is sent after a connection has been open for at least this duration.
	FirstReportDeadline time.Duration

	// ReportInterval sets an optional reporting interval meant for long-running connections.
	ReportInterval time.Duration
}

func (f *Factory) FactoryType() services.ServiceType {
	return Type
}

func (f *Factory) ServiceType() services.ServiceType {
	return Type
}

// SetFactoryRegistry implements the SetFactoryRegistry interface.
func (f *Factory) SetFactoryRegistry(registry *services.FactoryRegistry) {
	f.factoryRegistry = registry
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

func (f *Factory) Create(ctx context.Context) (services.Service, error) {
	svc, err := f.factoryRegistry.CreateService(ctx, eventstore.TypeEventStore, f.config.EventStoreID)
	if err != nil {
		return nil, fmt.Errorf("creating event store: %w", err)
	}
	es, ok := svc.(eventstore.EventStore)
	if !ok {
		return nil, errors.New("service is not an eventstore.EventStore")
	}
	if l, ok := es.(services.LoggerAdapter); ok {
		l.SetLogger(f.logger)
	}

	ctx, cancel := context.WithCancel(ctx)
	return &service{
		ctx:                 ctx,
		cancel:              cancel,
		firstReportDeadline: f.config.FirstReportDeadline,
		reportInterval:      f.config.ReportInterval,
		eventStore:          es,
		logToConsole:        f.config.EventStoreID == "", // log to the console only if this is the default reporter
	}, nil
}
