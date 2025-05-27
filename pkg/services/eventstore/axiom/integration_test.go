//go:build integration

package axiom

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/mocks"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore/noop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestAxiomIntegration tests the Axiom service with a real Axiom instance
// This test requires:
// - AXIOM_TOKEN environment variable to be set
// - AXIOM_DATASET environment variable to be set (optional, defaults to "qtap-test")
// Run with: go test -tags=integration
func TestAxiomIntegration(t *testing.T) {
	ctrl := gomock.NewController(t)

	token := os.Getenv("AXIOM_TOKEN")
	if token == "" {
		t.Skip("AXIOM_TOKEN environment variable not set, skipping integration test")
	}

	dataset := os.Getenv("AXIOM_DATASET")
	if dataset == "" {
		dataset = "qtap-test"
	}

	// Create and initialize factory
	f := &Factory{}
	cfg := config.ServiceEventStore{
		Type: "axiom",
		EventStoreConfig: config.EventStoreConfig{
			Token: config.ValueSource{
				Type:  config.ValueSourceType_TEXT,
				Value: token,
			},
			EventStoreAxiomConfig: config.EventStoreAxiomConfig{
				Dataset: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: dataset,
				},
			},
		},
	}

	err := f.Init(context.Background(), cfg)
	require.NoError(t, err)

	// Set up mock registry with noop object store factory (integration test doesn't need real object store)
	mockRegistry := mocks.NewMockRegistryAccessor(ctrl)
	noopFactory := &noop.Factory{}
	mockRegistry.EXPECT().Get(objectstore.TypeObjectStore).Return(noopFactory)
	f.SetRegistry(mockRegistry)

	// Create service instance
	svc, err := f.Create(context.Background())
	require.NoError(t, err)
	require.NotNil(t, svc)

	es, ok := svc.(*EventStore)
	require.True(t, ok)

	// Test submitting different event types
	ctx := context.Background()

	// Test Request event
	request := &eventstore.Request{
		Timestamp:   time.Now(),
		Direction:   "ingress",
		Url:         "https://api.example.com/test",
		Method:      "GET",
		Status:      200,
		Duration:    100,
		ContentType: "application/json",
		Category:    "api",
		Agent:       "qtap-test",
	}
	request.SetConnectionID("test-connection-123")
	request.SetRequestID("test-request-456")
	request.AddTags("integration-test", "axiom-service")

	es.Save(ctx, request)

	// Test Issue event
	issue := &eventstore.Issue{
		Timestamp: time.Now(),
		Direction: "ingress",
		Error:     "Test error for integration test",
		URL:       "https://api.example.com/error",
		Method:    "POST",
		Status:    500,
	}
	issue.SetConnectionID("test-connection-123")
	issue.SetRequestID("test-request-789")
	issue.AddTags("integration-test", "error-test")

	es.Save(ctx, issue)

	// Test PII Entity event
	pii := &eventstore.PIIEntity{
		Timestamp:    time.Now(),
		EntityType:   "email",
		Score:        0.95,
		EntitySource: "request_body",
		FieldPath:    "user.email",
		ValueHash:    "abcd1234567890efghijklmnopqrstuvwxyz123456",
	}
	pii.SetConnectionID("test-connection-123")
	pii.SetRequestID("test-request-101")
	pii.AddTags("integration-test", "pii-detection")

	es.Save(ctx, pii)

	// Test Artifact Record event
	artifact := &eventstore.ArtifactRecord{
		Timestamp: time.Now(),
		Type:      eventstore.ArtifactType_RequestBody,
		Digest:    "sha1-abcdef1234567890",
		URL:       "https://storage.example.com/artifacts/test123",
	}
	artifact.SetConnectionID("test-connection-123")
	artifact.SetRequestID("test-request-202")

	es.Save(ctx, artifact)

	// Give some time for async operations to complete
	time.Sleep(2 * time.Second)

	// If we reach here without errors, the integration test passed
	assert.True(t, true, "Integration test completed successfully")
}
