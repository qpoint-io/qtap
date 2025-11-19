package axiom

import (
	"context"
	"errors"
	"fmt"

	"github.com/axiomhq/axiom-go/axiom"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
)

// ensure we implement the EventStore interface
var _ eventstore.EventStore = (*EventStore)(nil)

var tracer = telemetry.Tracer()

const (
	Type services.ServiceType = "axiom"
)

type Factory struct {
	eventstore.BaseEventStore

	logger      *zap.Logger
	axiomClient *axiom.Client
	dataset     string
}

// Init initializes the Axiom service factory
func (f *Factory) Init(ctx context.Context, cfg any) error {
	c, ok := cfg.(config.ServiceEventStore)
	if !ok {
		return fmt.Errorf("invalid config type: %T wanted config.ServiceEventStore", cfg)
	}

	f.logger = zap.L().With(zap.String("service_factory", f.FactoryType().String()))

	// Extract Axiom configuration from the service config
	token := c.Token.String()
	if token == "" {
		return errors.New("Axiom API token is required")
	}

	// Use URL as dataset name if provided, otherwise use a default
	f.dataset = "qtap-events"
	if c.Dataset.String() != "" {
		f.dataset = c.Dataset.String() // URL field can be repurposed as dataset name
	}

	// Create Axiom client
	client, err := axiom.NewClient(
		axiom.SetToken(token),
	)
	if err != nil {
		return fmt.Errorf("failed to create Axiom client: %w", err)
	}

	f.axiomClient = client

	f.logger.Info("Axiom service factory initialized",
		zap.String("dataset", f.dataset))

	return nil
}

// Create creates a new Axiom EventStore service instance
func (f *Factory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	return &EventStore{
		axiomClient: f.axiomClient,
		dataset:     f.dataset,
		endpoint:    "",
	}, nil
}

// FactoryType returns the service factory type
func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", eventstore.TypeEventStore, Type))
}

// Close cleans up resources (Axiom client handles its own cleanup)
func (f *Factory) Close() error {
	return nil
}
