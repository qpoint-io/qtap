package axiom

import (
	"testing"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactory_Init_ValidConfig(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceEventStore{
		Type: "axiom",
		EventStoreConfig: config.EventStoreConfig{
			Token: config.ValueSource{
				Type:  config.ValueSourceType_TEXT,
				Value: "xaat-12345678-1234-1234-1234-123456789012", // Valid-looking Axiom token format
			},
			EventStoreAxiomConfig: config.EventStoreAxiomConfig{
				Dataset: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: "test-dataset",
				},
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "test-dataset", f.dataset)
	assert.NotNil(t, f.axiomClient)
	assert.NotNil(t, f.logger)
}

func TestFactory_Init_DefaultDataset(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceEventStore{
		Type: "axiom",
		EventStoreConfig: config.EventStoreConfig{
			Token: config.ValueSource{
				Type:  config.ValueSourceType_TEXT,
				Value: "xaat-12345678-1234-1234-1234-123456789012", // Valid-looking Axiom token format
			},
			EventStoreAxiomConfig: config.EventStoreAxiomConfig{
				// Dataset not set - should use default
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "qtap-events", f.dataset)
}

func TestFactory_Create(t *testing.T) {
	f := &Factory{}
	// Initialize with valid config first
	cfg := config.ServiceEventStore{
		Type: "axiom",
		EventStoreConfig: config.EventStoreConfig{
			Token: config.ValueSource{
				Type:  config.ValueSourceType_TEXT,
				Value: "xaat-12345678-1234-1234-1234-123456789012", // Valid-looking Axiom token format
			},
			EventStoreAxiomConfig: config.EventStoreAxiomConfig{
				Dataset: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: "test-dataset",
				},
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)

	svcs := services.NewServiceRegistry(nil)

	// Now test Create
	svc, err := f.Create(t.Context(), svcs)
	require.NoError(t, err)
	require.NotNil(t, svc)

	// Verify it's the correct type
	axes, ok := svc.(*EventStore)
	require.True(t, ok)
	assert.Equal(t, "test-dataset", axes.dataset)
	assert.NotNil(t, axes.axiomClient)
}

func TestFactory_ServiceType(t *testing.T) {
	f := &Factory{}
	expected := services.ServiceType("eventstore.axiom")
	assert.Equal(t, expected, f.FactoryType())
}

func TestFactory_Close(t *testing.T) {
	f := &Factory{}
	err := f.Close()
	assert.NoError(t, err)
}
