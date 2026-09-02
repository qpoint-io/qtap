package otel

import (
	"context"
	"testing"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	gomock "go.uber.org/mock/gomock"
)

func TestObjectStore_Put_BasicArtifact(t *testing.T) {
	f := newTestFactory(t)
	svc := newTestObjectStore(t, f, nil)

	artifact := &eventstore.Artifact{
		Type:        eventstore.ArtifactType_RequestBody,
		ContentType: "application/json",
		Data:        []byte(`{"hello":"world"}`),
	}

	// Should not panic
	svc.Put(t.Context(), artifact)
}

func TestObjectStore_Put_WithSummary(t *testing.T) {
	f := newTestFactory(t)
	svc := newTestObjectStore(t, f, nil)

	artifact := &eventstore.Artifact{
		Type:        eventstore.ArtifactType_HTTPTransaction,
		ContentType: "application/json",
		Data:        []byte(`{"request":"data"}`),
		Summary: map[string]any{
			"method": "GET",
			"status": 200,
			"url":    "https://example.com",
		},
	}

	svc.Put(t.Context(), artifact)
}

func TestObjectStore_Put_WithEventStore(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockES := eventstore.NewMockEventStore(ctrl)

	f := newTestFactory(t)
	svc := newTestObjectStore(t, f, mockES)

	artifact := &eventstore.Artifact{
		Type:        eventstore.ArtifactType_RequestBody,
		ContentType: "text/plain",
		Data:        []byte("hello"),
	}

	// The eventStore.Save is called in a goroutine, so we use a channel to wait
	done := make(chan struct{})
	mockES.EXPECT().Save(gomock.Any(), gomock.Any()).Do(func(_ context.Context, item any) {
		record, ok := item.(*eventstore.ArtifactRecord)
		if !ok {
			t.Errorf("expected *eventstore.ArtifactRecord, got %T", item)
		}
		if record.URL == "" {
			t.Error("expected non-empty URL")
		}
		close(done)
	})

	svc.Put(t.Context(), artifact)
	<-done
}

func TestObjectStore_Put_WithoutEventStore(t *testing.T) {
	f := newTestFactory(t)
	svc := newTestObjectStore(t, f, nil)

	artifact := &eventstore.Artifact{
		Type:        eventstore.ArtifactType_ResponseBody,
		ContentType: "text/html",
		Data:        []byte("<html>test</html>"),
	}

	// Should not panic when eventStore is nil
	svc.Put(t.Context(), artifact)
}

func TestObjectStore_Put_AllArtifactTypes(t *testing.T) {
	types := []struct {
		name string
		typ  eventstore.ArtifactType
	}{
		{"request_body", eventstore.ArtifactType_RequestBody},
		{"request_headers", eventstore.ArtifactType_RequestHeaders},
		{"response_body", eventstore.ArtifactType_ResponseBody},
		{"response_headers", eventstore.ArtifactType_ResponseHeaders},
		{"http_transaction", eventstore.ArtifactType_HTTPTransaction},
		{"llm_conversation", eventstore.ArtifactType_LLMConversation},
		{"dlp_matches", eventstore.ArtifactType_DLPMatches},
	}

	for _, tc := range types {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestFactory(t)
			svc := newTestObjectStore(t, f, nil)

			artifact := &eventstore.Artifact{
				Type:        tc.typ,
				ContentType: "application/octet-stream",
				Data:        []byte("test data"),
			}

			svc.Put(t.Context(), artifact)
		})
	}
}

// newTestFactory creates a Factory initialized with stdout protocol for testing.
func newTestFactory(t *testing.T) *Factory {
	t.Helper()
	f := &Factory{}
	cfg := newTestConfig("stdout", "")
	if err := f.Init(t.Context(), cfg); err != nil {
		t.Fatalf("failed to init factory: %v", err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("failed to close factory: %v", err)
		}
	})
	return f
}

// newTestObjectStore creates an ObjectStore from the factory for testing.
func newTestObjectStore(t *testing.T, f *Factory, es eventstore.EventStore) *ObjectStore {
	t.Helper()
	logger := f.logProvider.Logger("qtap.objectstore.test")
	return &ObjectStore{
		logger:      logger,
		endpoint:    f.endpoint,
		eventStore:  es,
		serviceName: f.serviceName,
		environment: f.environment,
	}
}

func newTestConfig(protocol, endpoint string) any {
	return config.ServiceObjectStore{
		Type:         config.ObjectStoreType_OTEL,
		Protocol:     protocol,
		OTelEndpoint: endpoint,
		ServiceName:  "test-qtap",
		Environment:  "test",
	}
}
