package otel

import (
	"testing"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactory_Init_ValidConfig_GRPC(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceEventStore{
		Type: "otel",
		EventStoreConfig: config.EventStoreConfig{
			EventStoreOTelConfig: config.EventStoreOTelConfig{
				Endpoint:    "localhost:4317",
				Protocol:    "grpc",
				ServiceName: "test-qtap",
				Environment: "test",
				Headers: map[string]config.ValueSource{
					"api-key": {
						Type:  "text",
						Value: "test-key",
					},
				},
				TLS: config.EventStoreOTelTLS{
					Enabled:            false,
					InsecureSkipVerify: false,
				},
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "test-qtap", f.serviceName)
	assert.Equal(t, "test", f.environment)
	assert.NotNil(t, f.logProvider)
	assert.NotNil(t, f.logger)
}

func TestFactory_Init_ValidConfig_HTTP(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceEventStore{
		Type: "otel",
		EventStoreConfig: config.EventStoreConfig{
			EventStoreOTelConfig: config.EventStoreOTelConfig{
				Endpoint:    "localhost:4318",
				Protocol:    "http",
				ServiceName: "test-qtap-http",
				Environment: "test",
				Headers: map[string]config.ValueSource{
					"authorization": {
						Type:  "text",
						Value: "Bearer test-token",
					},
				},
				TLS: config.EventStoreOTelTLS{
					Enabled:            false,
					InsecureSkipVerify: false,
				},
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "test-qtap-http", f.serviceName)
	assert.Equal(t, "test", f.environment)
	assert.NotNil(t, f.logProvider)
	assert.NotNil(t, f.logger)
}

func TestFactory_Init_DefaultValues_GRPC(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceEventStore{
		Type: "otel",
		EventStoreConfig: config.EventStoreConfig{
			EventStoreOTelConfig: config.EventStoreOTelConfig{
				// Using defaults - should default to gRPC
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "qtap", f.serviceName)
	assert.Equal(t, "production", f.environment)
}

func TestFactory_Init_DefaultValues_HTTP(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceEventStore{
		Type: "otel",
		EventStoreConfig: config.EventStoreConfig{
			EventStoreOTelConfig: config.EventStoreOTelConfig{
				Protocol: "http", // Explicitly set to HTTP
				// Other fields use defaults
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "qtap", f.serviceName)
	assert.Equal(t, "production", f.environment)
}

func TestFactory_Init_ValidConfig_Stdout(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceEventStore{
		Type: "otel",
		EventStoreConfig: config.EventStoreConfig{
			EventStoreOTelConfig: config.EventStoreOTelConfig{
				Protocol:    "stdout",
				ServiceName: "test-qtap-stdout",
				Environment: "test",
				// Note: endpoint, headers, and TLS are ignored for stdout
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "test-qtap-stdout", f.serviceName)
	assert.Equal(t, "test", f.environment)
	assert.NotNil(t, f.logProvider)
	assert.NotNil(t, f.logger)
}

func TestFactory_Init_DefaultValues_Stdout(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceEventStore{
		Type: "otel",
		EventStoreConfig: config.EventStoreConfig{
			EventStoreOTelConfig: config.EventStoreOTelConfig{
				Protocol: "stdout", // Explicitly set to stdout
				// Other fields use defaults
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "qtap", f.serviceName)
	assert.Equal(t, "production", f.environment)
}

func TestFactory_Init_InvalidConfig(t *testing.T) {
	f := &Factory{}
	err := f.Init(t.Context(), "invalid config")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid config type")
}

func TestFactory_Create(t *testing.T) {
	f := &Factory{}

	// Initialize with valid config first
	cfg := config.ServiceEventStore{
		Type: "otel",
		EventStoreConfig: config.EventStoreConfig{
			EventStoreOTelConfig: config.EventStoreOTelConfig{
				Endpoint:    "localhost:4317",
				ServiceName: "test-qtap",
				Environment: "test",
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)

	svcRegistry := services.NewServiceRegistry(nil)

	// Now test Create
	svc, err := f.Create(t.Context(), svcRegistry)
	require.NoError(t, err)
	require.NotNil(t, svc)

	// Verify it's the correct type
	es, ok := svc.(*EventStore)
	require.True(t, ok)
	assert.Equal(t, "test-qtap", es.serviceName)
	assert.Equal(t, "test", es.environment)
	assert.NotNil(t, es.logger)
}

func TestFactory_FactoryType(t *testing.T) {
	f := &Factory{}
	expected := services.ServiceType("eventstore.otel")
	assert.Equal(t, expected, f.FactoryType())
}

func TestFactory_Close(t *testing.T) {
	f := &Factory{}

	// Test close without initialization
	err := f.Close()
	require.NoError(t, err)

	// Test close after initialization
	cfg := config.ServiceEventStore{
		Type: "otel",
		EventStoreConfig: config.EventStoreConfig{
			EventStoreOTelConfig: config.EventStoreOTelConfig{},
		},
	}
	err = f.Init(t.Context(), cfg)
	require.NoError(t, err)

	err = f.Close()
	assert.NoError(t, err)
}

func TestFactory_Init_UnsupportedProtocol(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceEventStore{
		Type: "otel",
		EventStoreConfig: config.EventStoreConfig{
			EventStoreOTelConfig: config.EventStoreOTelConfig{
				Protocol: "websocket", // Unsupported protocol
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported protocol: websocket")
	require.Contains(t, err.Error(), "supported protocols are 'grpc', 'http', and 'stdout'")
}

func TestFactory_ProtocolDefaults(t *testing.T) {
	testCases := []struct {
		name                string
		protocol            string
		endpoint            string
		expectedDefaultPort string
	}{
		{
			name:                "grpc_default",
			protocol:            "grpc",
			endpoint:            "", // Empty to test default
			expectedDefaultPort: "4317",
		},
		{
			name:                "http_default",
			protocol:            "http",
			endpoint:            "", // Empty to test default
			expectedDefaultPort: "4318",
		},
		{
			name:                "explicit_endpoint_grpc",
			protocol:            "grpc",
			endpoint:            "custom.example.com:9999",
			expectedDefaultPort: "9999", // Should use explicit endpoint
		},
		{
			name:                "explicit_endpoint_http",
			protocol:            "http",
			endpoint:            "custom.example.com:8888",
			expectedDefaultPort: "8888", // Should use explicit endpoint
		},
		{
			name:                "stdout_default",
			protocol:            "stdout",
			endpoint:            "", // Empty to test default
			expectedDefaultPort: "", // stdout doesn't use ports
		},
		{
			name:                "stdout_with_endpoint_ignored",
			protocol:            "stdout",
			endpoint:            "ignored.example.com:9999", // Should be ignored
			expectedDefaultPort: "",                         // stdout doesn't use ports
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := &Factory{}
			cfg := config.ServiceEventStore{
				Type: "otel",
				EventStoreConfig: config.EventStoreConfig{
					EventStoreOTelConfig: config.EventStoreOTelConfig{
						Protocol: tc.protocol,
						Endpoint: tc.endpoint,
					},
				},
			}
			err := f.Init(t.Context(), cfg)
			require.NoError(t, err)
			// We can't easily test the internal endpoint selection without exposing it
			// but we can verify the factory initializes successfully
			assert.NotNil(t, f.logProvider)

			// Cleanup
			err = f.Close()
			assert.NoError(t, err)
		})
	}
}

func TestExpandEnvVarsInBraces(t *testing.T) {
	t.Setenv("FOO", "bar")
	t.Setenv("BAZ", "qux")

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no vars", "hello world", "hello world"},
		{"single var", "foo={{FOO}}", "foo=bar"},
		{"single var with spaces", "foo={{ FOO }}", "foo=bar"},
		{"var with spaces", "foo={{   FOO   }}", "foo=bar"},
		{"multiple vars", "a={{FOO}},b={{BAZ}}", "a=bar,b=qux"},
		{"unknown var", "foo={{UNKNOWN}}", "foo={{UNKNOWN}}"},
		{"mixed known/unknown", "foo={{FOO}},x={{UNKNOWN}}", "foo=bar,x={{UNKNOWN}}"},
		{"nested braces", "foo={{{FOO}}}", "foo={bar}"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandEnvVarsInBraces(tt.in)
			if got != tt.want {
				t.Errorf("expandEnvVarsInBraces(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
