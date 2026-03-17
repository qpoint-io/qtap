package otel

import (
	"testing"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
)

func TestFactory_Init_ValidConfig_GRPC(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_OTEL,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreOTelConfig: config.ObjectStoreOTelConfig{
				OTelEndpoint: "localhost:4317",
				Protocol:     "grpc",
				ServiceName:  "test-qtap",
				Environment:  "test",
				Headers: map[string]config.ValueSource{
					"api-key": {
						Type:  "text",
						Value: "test-key",
					},
				},
				TLS: config.ObjectStoreOTelTLS{
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
	assert.Equal(t, "localhost:4317", f.endpoint)
	assert.NotNil(t, f.logProvider)
	assert.NotNil(t, f.logger)
}

func TestFactory_Init_ValidConfig_HTTP(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_OTEL,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreOTelConfig: config.ObjectStoreOTelConfig{
				OTelEndpoint: "localhost:4318",
				Protocol:     "http",
				ServiceName:  "test-qtap-http",
				Environment:  "test",
				Headers: map[string]config.ValueSource{
					"authorization": {
						Type:  "text",
						Value: "Bearer test-token",
					},
				},
				TLS: config.ObjectStoreOTelTLS{
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
	assert.Equal(t, "localhost:4318", f.endpoint)
	assert.NotNil(t, f.logProvider)
	assert.NotNil(t, f.logger)
}

func TestFactory_Init_ValidConfig_Stdout(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_OTEL,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreOTelConfig: config.ObjectStoreOTelConfig{
				Protocol:    "stdout",
				ServiceName: "test-qtap-stdout",
				Environment: "test",
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

func TestFactory_Init_DefaultValues_GRPC(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_OTEL,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreOTelConfig: config.ObjectStoreOTelConfig{},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "qtap", f.serviceName)
	assert.Equal(t, "production", f.environment)
	assert.Equal(t, "localhost:4317", f.endpoint)
}

func TestFactory_Init_DefaultValues_HTTP(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_OTEL,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreOTelConfig: config.ObjectStoreOTelConfig{
				Protocol: "http",
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "qtap", f.serviceName)
	assert.Equal(t, "production", f.environment)
	assert.Equal(t, "localhost:4318", f.endpoint)
}

func TestFactory_Init_DefaultValues_Stdout(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_OTEL,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreOTelConfig: config.ObjectStoreOTelConfig{
				Protocol: "stdout",
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "qtap", f.serviceName)
	assert.Equal(t, "production", f.environment)
	assert.Empty(t, f.endpoint)
}

func TestFactory_Init_InvalidConfigType(t *testing.T) {
	f := &Factory{}
	err := f.Init(t.Context(), "invalid config")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config type")
}

func TestFactory_Init_WrongObjectStoreType(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_S3,
	}
	err := f.Init(t.Context(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid object store type")
}

func TestFactory_Init_UnsupportedProtocol(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_OTEL,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreOTelConfig: config.ObjectStoreOTelConfig{
				Protocol: "websocket",
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported protocol: websocket")
	assert.Contains(t, err.Error(), "supported protocols are 'grpc', 'http', and 'stdout'")
}

func TestFactory_Create(t *testing.T) {
	f := &Factory{}
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_OTEL,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreOTelConfig: config.ObjectStoreOTelConfig{
				OTelEndpoint: "localhost:4317",
				ServiceName:  "test-qtap",
				Environment:  "test",
			},
		},
	}
	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)

	// Register a mock eventstore so Create can resolve it
	ctrl := gomock.NewController(t)
	mockES := eventstore.NewMockEventStore(ctrl)
	mockES.EXPECT().ServiceType().Return(eventstore.TypeEventStore).AnyTimes()
	fr := services.NewFactoryRegistry()
	fr.Register(services.StaticFactory(
		services.ServiceType("eventstore"),
		mockES,
	), "")
	svcRegistry := services.NewServiceRegistry(fr)

	svc, err := f.Create(t.Context(), svcRegistry)
	require.NoError(t, err)
	require.NotNil(t, svc)

	os, ok := svc.(*ObjectStore)
	require.True(t, ok)
	assert.Equal(t, "test-qtap", os.serviceName)
	assert.Equal(t, "test", os.environment)
	assert.Equal(t, "localhost:4317", os.endpoint)
	assert.NotNil(t, os.logger)
	assert.NotNil(t, os.eventStore)
}

func TestFactory_FactoryType(t *testing.T) {
	f := &Factory{}
	expected := services.ServiceType("objectstore.otel")
	assert.Equal(t, expected, f.FactoryType())
}

func TestFactory_Close(t *testing.T) {
	f := &Factory{}

	// Test close without initialization
	err := f.Close()
	require.NoError(t, err)

	// Test close after initialization
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_OTEL,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreOTelConfig: config.ObjectStoreOTelConfig{},
		},
	}
	err = f.Init(t.Context(), cfg)
	require.NoError(t, err)

	err = f.Close()
	assert.NoError(t, err)
}

func TestFactory_ProtocolDefaults(t *testing.T) {
	testCases := []struct {
		name             string
		protocol         string
		endpoint         string
		expectedEndpoint string
	}{
		{
			name:             "grpc_default",
			protocol:         "grpc",
			endpoint:         "",
			expectedEndpoint: "localhost:4317",
		},
		{
			name:             "http_default",
			protocol:         "http",
			endpoint:         "",
			expectedEndpoint: "localhost:4318",
		},
		{
			name:             "explicit_endpoint_grpc",
			protocol:         "grpc",
			endpoint:         "custom.example.com:9999",
			expectedEndpoint: "custom.example.com:9999",
		},
		{
			name:             "explicit_endpoint_http",
			protocol:         "http",
			endpoint:         "custom.example.com:8888",
			expectedEndpoint: "custom.example.com:8888",
		},
		{
			name:             "stdout_default",
			protocol:         "stdout",
			endpoint:         "",
			expectedEndpoint: "",
		},
		{
			name:             "stdout_with_endpoint",
			protocol:         "stdout",
			endpoint:         "custom.example.com:9999",
			expectedEndpoint: "custom.example.com:9999",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := &Factory{}
			cfg := config.ServiceObjectStore{
				Type: config.ObjectStoreType_OTEL,
				ObjectStoreConfig: config.ObjectStoreConfig{
					ObjectStoreOTelConfig: config.ObjectStoreOTelConfig{
						Protocol:     tc.protocol,
						OTelEndpoint: tc.endpoint,
					},
				},
			}
			err := f.Init(t.Context(), cfg)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedEndpoint, f.endpoint)
			assert.NotNil(t, f.logProvider)

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
			assert.Equal(t, tt.want, got)
		})
	}
}
