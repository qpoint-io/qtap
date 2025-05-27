package axiom

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactory_FactoryType(t *testing.T) {
	f := &Factory{}
	expected := services.ServiceType("eventstore.axiom")
	assert.Equal(t, expected, f.FactoryType())
}

func TestFactory_Init_InvalidConfig(t *testing.T) {
	f := &Factory{}
	err := f.Init(context.Background(), "invalid config")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid config type")
}

func TestFactory_Init_MissingToken(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceEventStore{
		Type: "axiom",
		EventStoreConfig: config.EventStoreConfig{
			Token: config.ValueSource{},
			EventStoreAxiomConfig: config.EventStoreAxiomConfig{
				Dataset: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: "test-dataset",
				},
			},
		},
	}
	err := f.Init(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Axiom API token is required")
}

func TestEventStore_Save_UnsupportedType(t *testing.T) {
	es := &EventStore{}

	// This should not panic and should log a warning
	es.Save(context.Background(), "unsupported type")
	// Since we can't easily test the log output, we just ensure it doesn't panic
}

func TestEventStore_SetConnection(t *testing.T) {
	es := &EventStore{}

	// Test that SetConnection doesn't panic
	es.SetConnection(nil)
	assert.Nil(t, es.conn)
}

func TestEventStore_ServiceType(t *testing.T) {
	es := &EventStore{}
	assert.Equal(t, eventstore.TypeEventStore, es.ServiceType())
}

// TestEventStore_Save_SupportedTypes tests that supported event types are handled
func TestEventStore_Save_SupportedTypes(t *testing.T) {
	// Note: This test would require mocking the Axiom client for full testing
	// For now, we test the structure and ensure no panics
	es := &EventStore{}

	testCases := []any{
		&eventstore.Request{
			Timestamp: time.Now(),
			Direction: "ingress",
			Url:       "https://example.com",
			Method:    "GET",
			Status:    200,
		},
		&eventstore.Issue{
			Timestamp: time.Now(),
			Direction: "ingress",
			Error:     "test error",
			URL:       "https://example.com",
		},
		&eventstore.PIIEntity{
			Timestamp:    time.Now(),
			EntityType:   "email",
			Score:        0.9,
			EntitySource: "request_body",
		},
		&eventstore.ArtifactRecord{
			Timestamp: time.Now(),
			Type:      eventstore.ArtifactType_RequestBody,
			Digest:    "abc123",
			URL:       "https://storage.example.com/artifact",
		},
	}

	for i, testCase := range testCases {
		t.Run(fmt.Sprintf("save_case_%d", i), func(t *testing.T) {
			// This should not panic even without a real Axiom client
			// The actual submission will fail, but the structure should be sound
			require.NotPanics(t, func() {
				es.Save(context.Background(), testCase)
			})
		})
	}
}
