package otel

import (
	"context"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore/noop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	noopotel "go.opentelemetry.io/otel/log/noop"
	"go.uber.org/zap"
)

// TestOTelEventStore_Integration tests the complete flow without network dependencies
func TestOTelEventStore_Integration(t *testing.T) {
	// Create EventStore directly for integration testing
	es := &EventStore{
		logger:      noopotel.NewLoggerProvider().Logger("integration-test"),
		objectStore: &noop.ObjectStore{},
		serviceName: "integration-test",
		environment: "test",
	}

	// Initialize LogHelper to avoid nil pointer issues
	es.SetLogger(zap.L().With(zap.String("service", "eventstore.otel")))

	// Verify service type
	assert.Equal(t, services.ServiceType("eventstore"), es.ServiceType())

	// Test saving various event types
	ctx := context.Background()

	// Test Request event
	req := &eventstore.Request{
		Timestamp:   time.Now(),
		Direction:   "ingress",
		Url:         "https://api.example.com/users",
		URLPath:     "/users",
		Method:      "GET",
		Status:      200,
		Duration:    150,
		ContentType: "application/json",
		Category:    "api",
		Agent:       "curl/7.68.0",
		WrBytes:     0,
		RdBytes:     2048,
	}
	req.ConnectionID = "conn-integration-test"
	req.EndpointId = "endpoint-integration-test"
	req.RequestId = "req-integration-test"
	req.Tags = []string{"integration", "test"}

	assert.NotPanics(t, func() {
		es.Save(ctx, req)
	})

	// Test Issue event
	issue := &eventstore.Issue{
		Timestamp: time.Now(),
		Direction: "ingress",
		Error:     "Integration test error",
		URL:       "https://api.example.com/error",
		URLPath:   "/error",
		Method:    "POST",
		Status:    500,
		TriggerConditions: []eventstore.IssueTriggerCondition{
			{Plugin: eventstore.PluginNameDetectErrors, Condition: "http_5xx"},
		},
		TriggerReasons: []string{"server_error", "integration_test"},
	}
	issue.ConnectionID = "conn-integration-test"
	issue.Tags = []string{"integration", "error"}

	assert.NotPanics(t, func() {
		es.Save(ctx, issue)
	})

	// Test Connection event
	conn := &eventstore.Connection{
		Timestamp:      time.Now(),
		Finalized:      true,
		Direction:      eventstore.Direction_Ingress,
		VendorID:       "vendor-integration",
		Part:           1,
		SocketProtocol: eventstore.SocketProtocol_TCP,
		L7Protocol:     eventstore.L7Protocol_HTTP1,
		BytesSent:      2048,
		BytesReceived:  4096,
		System: &eventstore.ConnectionSystem{
			Hostname:      "integration-host",
			Agent:         "qtap",
			AgentInstance: "qtap-integration",
		},
		Tags: map[string][]string{
			"test": {"integration"},
			"env":  {"test"},
		},
	}
	conn.ConnectionID = "conn-integration-test"

	assert.NotPanics(t, func() {
		es.Save(ctx, conn)
	})
}

// TestOTelEventStore_ConfigTypes tests that different configuration values are handled correctly
func TestOTelEventStore_ConfigTypes(t *testing.T) {
	testCases := []struct {
		name   string
		config config.EventStoreOTelConfig
	}{
		{
			name:   "minimal_config",
			config: config.EventStoreOTelConfig{
				// All defaults
			},
		},
		{
			name: "full_config_grpc",
			config: config.EventStoreOTelConfig{
				Endpoint:    "https://otel.example.com:4317",
				Protocol:    "grpc",
				ServiceName: "custom-service",
				Environment: "staging",
				Headers: map[string]config.ValueSource{
					"authorization": {
						Type:  "text",
						Value: "Bearer token123",
					},
					"x-custom": {
						Type:  "text",
						Value: "custom-value",
					},
				},
				TLS: config.EventStoreOTelTLS{
					Enabled:            true,
					InsecureSkipVerify: false,
				},
			},
		},
		{
			name: "full_config_http",
			config: config.EventStoreOTelConfig{
				Endpoint:    "otel.example.com:4318",
				Protocol:    "http",
				ServiceName: "custom-service-http",
				Environment: "staging",
				Headers: map[string]config.ValueSource{
					"authorization": {
						Type:  "text",
						Value: "Bearer token456",
					},
					"x-custom": {
						Type:  "text",
						Value: "custom-http-value",
					},
				},
				TLS: config.EventStoreOTelTLS{
					Enabled:            true,
					InsecureSkipVerify: false,
				},
			},
		},
		{
			name: "insecure_config_http",
			config: config.EventStoreOTelConfig{
				Endpoint:    "localhost:4318",
				Protocol:    "http",
				ServiceName: "insecure-test-http",
				TLS: config.EventStoreOTelTLS{
					Enabled:            false,
					InsecureSkipVerify: true,
				},
			},
		},
		{
			name: "full_url_http",
			config: config.EventStoreOTelConfig{
				Endpoint:    "https://otel.example.com:4318/custom/path",
				Protocol:    "http",
				ServiceName: "full-url-test",
				Environment: "test",
				Headers: map[string]config.ValueSource{
					"x-api-key": {
						Type:  "text",
						Value: "test-key",
					},
				},
				TLS: config.EventStoreOTelTLS{
					Enabled:            true,
					InsecureSkipVerify: false,
				},
			},
		},
		{
			name: "insecure_config",
			config: config.EventStoreOTelConfig{
				Endpoint:    "localhost:4317",
				ServiceName: "insecure-test",
				TLS: config.EventStoreOTelTLS{
					Enabled:            false,
					InsecureSkipVerify: true,
				},
			},
		},
		{
			name: "stdout_config",
			config: config.EventStoreOTelConfig{
				Protocol:    "stdout",
				ServiceName: "stdout-test",
				Environment: "test",
				// Note: endpoint, headers, and TLS are ignored for stdout
			},
		},
		{
			name: "stdout_minimal",
			config: config.EventStoreOTelConfig{
				Protocol: "stdout",
				// Use all defaults with stdout protocol
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			factory := &Factory{}
			cfg := config.ServiceEventStore{
				Type:             "otel",
				EventStoreConfig: config.EventStoreConfig{EventStoreOTelConfig: tc.config},
			}

			err := factory.Init(context.Background(), cfg)
			require.NoError(t, err)

			// Verify factory state
			if tc.config.ServiceName != "" {
				assert.Equal(t, tc.config.ServiceName, factory.serviceName)
			} else {
				assert.Equal(t, "qtap", factory.serviceName)
			}

			if tc.config.Environment != "" {
				assert.Equal(t, tc.config.Environment, factory.environment)
			} else {
				assert.Equal(t, "production", factory.environment)
			}

			// Cleanup
			err = factory.Close()
			assert.NoError(t, err)
		})
	}
}
