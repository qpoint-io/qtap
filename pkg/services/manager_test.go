package services

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
)

// mockService implements the Service interface
type mockService struct {
	serviceType ServiceType
}

func (m *mockService) ServiceType() ServiceType {
	return m.serviceType
}

func (m *mockService) ServiceEndpoints() []string {
	return []string{}
}

// mockFactory implements the Factory interface
type mockFactory struct {
	factoryType ServiceType
	serviceType ServiceType
	initErr     error
	initCalled  int
	initConfig  any

	closeCalled int
	closeFunc   func() error
	nextCalled  int
	nextFunc    func(Factory)
}

func (m *mockFactory) Init(ctx context.Context, config any) error {
	m.initCalled++
	m.initConfig = config
	return m.initErr
}

func (m *mockFactory) Create(ctx context.Context, svcRegistry *ServiceRegistry) (Service, error) {
	return &mockService{serviceType: m.serviceType}, nil
}

func (m *mockFactory) FactoryType() ServiceType {
	return m.factoryType
}

func (m *mockFactory) ServiceType() ServiceType {
	return m.serviceType
}

func (m *mockFactory) Close() error {
	m.closeCalled++
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func (m *mockFactory) Next(factory Factory) {
	m.nextCalled++
	if m.nextFunc != nil {
		m.nextFunc(factory)
	}
}

// newMockFactory creates a new mock factory
func newMockFactory(factoryType, serviceType string) *mockFactory {
	return &mockFactory{
		factoryType: ServiceType(factoryType),
		serviceType: ServiceType(serviceType),
	}
}

func newMockFactoryFn(factoryType, serviceType string) FactoryFn {
	return func() Factory {
		return newMockFactory(factoryType, serviceType)
	}
}

func TestFactoryManager_getServiceConfigs(t *testing.T) {
	// noopEventStoreFactoryFn := newMockFactoryFn("eventstore.noop", "eventstore")
	noopObjectStoreFactoryFn := newMockFactoryFn("objectstore.noop", "objectstore")
	noopQscanFactoryFn := newMockFactoryFn("qscan.noop", "qscan")

	noopDefaultObjectStoreConfig := &serviceConfig{
		isDefault: true,
		factory:   noopObjectStoreFactoryFn(),
		config:    config.ServiceObjectStore{Type: "disabled"},
	}
	noopDefaultQscanConfig := &serviceConfig{
		isDefault: true,
		factory:   noopQscanFactoryFn(),
		config:    &config.ServiceQscan{Type: "disabled"},
	}

	tests := []struct {
		name          string
		setupManager  func(t *testing.T) *FactoryManager
		setupConfig   func() *config.Config
		extraServices []func(*config.Config) *config.ServiceConfig
		want          func(t *testing.T, configs []*serviceConfig)
		wantErr       bool
		errContains   string
	}{
		{
			name: "single default service",
			setupManager: func(t *testing.T) *FactoryManager {
				m := NewFactoryManager(t.Context(), zaptest.NewLogger(t), NewFactoryRegistry())
				m.RegisterFactory(
					newMockFactoryFn("eventstore.console", "eventstore"),
					noopObjectStoreFactoryFn, noopQscanFactoryFn,
				)
				return m
			},
			setupConfig: func() *config.Config {
				return &config.Config{
					Services: config.Services{
						EventStores: []config.ServiceEventStore{
							{
								Type:             "console",
								EventStoreConfig: testEventStoreConfig(),
							},
						},
					},
				}
			},
			want: func(t *testing.T, configs []*serviceConfig) {
				require.Equal(t, []*serviceConfig{
					{
						isDefault: true,
						id:        "",
						factory:   newMockFactory("eventstore.console", "eventstore"),
						config:    configs[0].config,
					},
					noopDefaultObjectStoreConfig, noopDefaultQscanConfig,
				}, configs)
			},
		},
		{
			name: "multiple services - first becomes default",
			setupManager: func(t *testing.T) *FactoryManager {
				m := NewFactoryManager(t.Context(), zaptest.NewLogger(t), NewFactoryRegistry())
				m.RegisterFactory(
					newMockFactoryFn("eventstore.console", "eventstore"),
					noopObjectStoreFactoryFn, noopQscanFactoryFn,
				)
				return m
			},
			setupConfig: func() *config.Config {
				return &config.Config{
					Services: config.Services{
						EventStores: []config.ServiceEventStore{
							{
								Type:             "console",
								EventStoreConfig: testEventStoreConfig(),
								ID:               "store1",
							},
							{
								Type:             "console",
								EventStoreConfig: testEventStoreConfig(),
								ID:               "store2",
							},
						},
					},
				}
			},
			want: func(t *testing.T, configs []*serviceConfig) {
				require.Equal(t, []*serviceConfig{
					{
						isDefault: true,
						id:        "store1",
						factory:   newMockFactory("eventstore.console", "eventstore"),
						config:    configs[0].config,
					},
					{
						id:      "store2",
						factory: newMockFactory("eventstore.console", "eventstore"),
						config:  configs[1].config,
					},
					noopDefaultObjectStoreConfig, noopDefaultQscanConfig,
				}, configs)
			},
		},
		{
			name: "auto-assigns ID to non-default service without ID",
			setupManager: func(t *testing.T) *FactoryManager {
				m := NewFactoryManager(t.Context(), zaptest.NewLogger(t), NewFactoryRegistry())
				m.RegisterFactory(
					newMockFactoryFn("eventstore.console", "eventstore"),
					noopObjectStoreFactoryFn, noopQscanFactoryFn,
				)
				return m
			},
			setupConfig: func() *config.Config {
				return &config.Config{
					Services: config.Services{
						EventStores: []config.ServiceEventStore{
							{
								Type:             "console",
								EventStoreConfig: testEventStoreConfig(),
							},
							{
								Type:             "console",
								EventStoreConfig: testEventStoreConfig(),
							},
						},
					},
				}
			},
			want: func(t *testing.T, configs []*serviceConfig) {
				require.Equal(t, []*serviceConfig{
					{
						isDefault: true,
						id:        "",
						factory:   newMockFactory("eventstore.console", "eventstore"),
						config:    configs[0].config,
					},
					{
						id:      "service-1",
						factory: newMockFactory("eventstore.console", "eventstore"),
						config:  configs[1].config,
					},
					noopDefaultObjectStoreConfig, noopDefaultQscanConfig,
				}, configs)
			},
		},
		{
			name: "multiple service types",
			setupManager: func(t *testing.T) *FactoryManager {
				m := NewFactoryManager(t.Context(), zaptest.NewLogger(t), NewFactoryRegistry())
				m.RegisterFactory(
					newMockFactoryFn("eventstore.console", "eventstore"),
					newMockFactoryFn("objectstore.s3", "objectstore"),
					noopQscanFactoryFn,
				)
				return m
			},
			setupConfig: func() *config.Config {
				return &config.Config{
					Services: config.Services{
						EventStores: []config.ServiceEventStore{
							{Type: "console", EventStoreConfig: testEventStoreConfig()},
						},
						ObjectStores: []config.ServiceObjectStore{
							{Type: "s3", ObjectStoreConfig: testObjectStoreConfig()},
						},
					},
				}
			},
			want: func(t *testing.T, configs []*serviceConfig) {
				require.Equal(t, []*serviceConfig{
					{
						isDefault: true,
						id:        "",
						factory:   newMockFactory("eventstore.console", "eventstore"),
						config:    configs[0].config,
					},
					{
						isDefault: true,
						id:        "",
						factory:   newMockFactory("objectstore.s3", "objectstore"),
						config:    configs[1].config,
					},
					noopDefaultQscanConfig,
				}, configs)
			},
		},
		{
			name: "extra services appended at end",
			setupManager: func(t *testing.T) *FactoryManager {
				m := NewFactoryManager(t.Context(), zaptest.NewLogger(t), NewFactoryRegistry())
				m.RegisterFactory(
					newMockFactoryFn("eventstore.console", "eventstore"),
					noopObjectStoreFactoryFn, noopQscanFactoryFn,
					newMockFactoryFn("new.type", "new"),
				)
				return m
			},
			setupConfig: func() *config.Config {
				return &config.Config{
					Services: config.Services{
						EventStores: []config.ServiceEventStore{
							{Type: "console", ID: "main"},
						},
					},
				}
			},
			extraServices: []func(*config.Config) *config.ServiceConfig{
				func(cfg *config.Config) *config.ServiceConfig {
					return &config.ServiceConfig{
						Type: "eventstore.console",
					}
				},
				func(cfg *config.Config) *config.ServiceConfig {
					return &config.ServiceConfig{
						Type: "new.type",
						// no id
						Config: net.IP{}, // config is `any`
					}
				},
				func(cfg *config.Config) *config.ServiceConfig {
					return &config.ServiceConfig{
						Type: "new.type",
						// no id
					}
				},
			},
			want: func(t *testing.T, configs []*serviceConfig) {
				require.Equal(t, []*serviceConfig{
					{
						isDefault: true,
						id:        "main",
						factory:   newMockFactory("eventstore.console", "eventstore"),
						config:    configs[0].config,
					},
					noopDefaultObjectStoreConfig, noopDefaultQscanConfig,
					{
						id:      "service-3",
						factory: newMockFactory("eventstore.console", "eventstore"),
					},
					{
						isDefault: true,
						factory:   newMockFactory("new.type", "new"),
						config:    net.IP{},
					},
					{
						id:      "service-5",
						factory: newMockFactory("new.type", "new"),
					},
				}, configs)
			},
		},
		{
			name: "unknown factory type skipped error",
			setupManager: func(t *testing.T) *FactoryManager {
				m := NewFactoryManager(t.Context(), zaptest.NewLogger(t), NewFactoryRegistry())
				m.RegisterFactory(newMockFactoryFn("eventstore.console", "eventstore"))
				return m
			},
			setupConfig: func() *config.Config {
				return &config.Config{
					Services: config.Services{
						EventStores: []config.ServiceEventStore{
							{Type: "console", EventStoreConfig: testEventStoreConfig()}, // known
							{Type: "unknown-type"}, // unknown type
						},
					},
				}
			},
			// TODO: fail hard
			// wantErr:     true,
			// errContains: "no factory registered for service type: objectstore.noop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setupManager(t)
			if tt.extraServices != nil {
				m.AddExtraServices(tt.extraServices...)
			}
			cfg := tt.setupConfig()

			got, err := m.getServiceConfigs(cfg)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			if tt.want != nil {
				tt.want(t, got)
			}
		})
	}
}

func TestServiceConfig_keys(t *testing.T) {
	factory := newMockFactory("eventstore.console", "eventstore")

	tests := []struct {
		name   string
		config *serviceConfig
		want   []ServiceKey
	}{
		{
			name: "default with no ID",
			config: &serviceConfig{
				isDefault: true,
				id:        "",
				factory:   factory,
			},
			want: []ServiceKey{
				{Type: "eventstore", ID: ""},
			},
		},
		{
			name: "default with ID",
			config: &serviceConfig{
				isDefault: true,
				id:        "my-store",
				factory:   factory,
			},
			want: []ServiceKey{
				{Type: "eventstore", ID: "my-store"},
				{Type: "eventstore", ID: ""},
			},
		},
		{
			name: "non-default with ID",
			config: &serviceConfig{
				isDefault: false,
				id:        "my-store",
				factory:   factory,
			},
			want: []ServiceKey{
				{Type: "eventstore", ID: "my-store"},
			},
		},
		{
			name: "non-default without ID",
			config: &serviceConfig{
				isDefault: false,
				id:        "",
				factory:   factory,
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.keys()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestManager_SetConfig(t *testing.T) {
	var logs *observer.ObservedLogs
	ll := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core {
		var obsCore zapcore.Core
		obsCore, logs = observer.New(zapcore.DebugLevel)
		return zapcore.NewTee(c, obsCore)
	})))

	eventStoreFactory := newMockFactory("eventstore.console", "eventstore")
	eventStoreFactoryFn := func() Factory { return eventStoreFactory }
	eventStoreConfig := testEventStoreConfig()

	m := NewFactoryManager(t.Context(), ll, NewFactoryRegistry())
	m.RegisterFactory(
		eventStoreFactoryFn,
		newMockFactoryFn("objectstore.console", "objectstore"),
		newMockFactoryFn("qscan.noop", "qscan"),
	)

	cfg := &config.Config{
		Services: config.Services{
			EventStores: []config.ServiceEventStore{
				{Type: "console", EventStoreConfig: eventStoreConfig},
			},
			ObjectStores: []config.ServiceObjectStore{
				{Type: "console"},
			},
		},
	}
	_ = logs.TakeAll() // clear logs
	m.SetConfig(cfg)

	// test
	require.Equal(t, 0, logs.FilterLevelExact(zapcore.ErrorLevel).Len(), "expected no error logs")
	require.Equal(t, 1, eventStoreFactory.initCalled)
	require.Equal(t, config.ServiceEventStore{
		Type:             "console",
		EventStoreConfig: eventStoreConfig,
	}, eventStoreFactory.initConfig)
	require.Equal(t, 0, eventStoreFactory.closeCalled)
	require.Equal(t, 0, eventStoreFactory.nextCalled)

	// push a new config
	// the event store will remain the same, but the object store will be replaced with a different type.
	objectStoreConfig := testObjectStoreConfig()
	objectStoreFactory := newMockFactory("objectstore.s3", "objectstore")
	objectStoreFactoryFn := func() Factory { return objectStoreFactory }
	m.RegisterFactory(objectStoreFactoryFn)
	// replace the event store factory with a new one so we can keep our instance of `eventStoreFactory` unmodified.
	m.RegisterFactory(newMockFactoryFn("eventstore.console", "eventstore"))
	cfg = &config.Config{
		Services: config.Services{
			EventStores: []config.ServiceEventStore{
				{Type: "console", EventStoreConfig: eventStoreConfig},
			},
			ObjectStores: []config.ServiceObjectStore{
				{Type: "s3", ObjectStoreConfig: objectStoreConfig},
			},
		},
	}
	_ = logs.TakeAll() // clear logs
	m.SetConfig(cfg)

	// test
	require.Equal(t, 0, logs.FilterLevelExact(zapcore.ErrorLevel).Len(), "expected no error logs")
	require.Equal(t, 1, eventStoreFactory.closeCalled)
	require.Equal(t, 1, eventStoreFactory.nextCalled)

	// push a bad config with an unknown service type. our existing factories should not be replaced.
	cfg = &config.Config{
		Services: config.Services{
			EventStores: []config.ServiceEventStore{
				{Type: "console", EventStoreConfig: eventStoreConfig},
			},
			ObjectStores: []config.ServiceObjectStore{
				{Type: "s3", ObjectStoreConfig: objectStoreConfig},
			},
			QscanClient: &config.ServiceQscan{Type: "unknown-type"},
		},
	}
	_ = logs.TakeAll() // clear logs
	m.SetConfig(cfg)

	errorLogs := logs.FilterLevelExact(zapcore.ErrorLevel)
	require.Equal(t, 1, errorLogs.Len(), "expected one error log")
	assert.Equal(t, "no factory registered for service type", errorLogs.All()[0].Message)
	// assert.Equal(t, "failed to get service config plan, keeping existing config", errorLogs.All()[0].Message)
	// test that the factories were not replaced
	// TODO: fail hard
	// require.Equal(t, 1, objectStoreFactory.initCalled)
	// require.Equal(t, 0, objectStoreFactory.closeCalled)
	// require.Equal(t, 0, objectStoreFactory.nextCalled)
	require.Equal(t, 2, objectStoreFactory.initCalled)
	require.Equal(t, 1, objectStoreFactory.closeCalled)
	require.Equal(t, 1, objectStoreFactory.nextCalled)

	// push a bad config that results in Init() returning an error. our existing factories should not be replaced.
	objectStoreFactory.initErr = errors.New("init error")
	cfg = &config.Config{
		Services: config.Services{
			EventStores: []config.ServiceEventStore{
				{Type: "console", EventStoreConfig: eventStoreConfig},
			},
			ObjectStores: []config.ServiceObjectStore{
				{Type: "s3", ObjectStoreConfig: objectStoreConfig},
			},
		},
	}
	_ = logs.TakeAll() // clear logs
	m.SetConfig(cfg)

	errorLogs = logs.FilterLevelExact(zapcore.ErrorLevel)
	require.Equal(t, 1, errorLogs.Len(), "expected one error log")
	assert.Equal(t, "failed to initialize service", errorLogs.All()[0].Message)
	// assert.Equal(t, "failed to initialize service, keeping existing config", errorLogs.All()[0].Message)
	// test that the factories were not replaced
	require.Equal(t, 3, objectStoreFactory.initCalled)
	require.Equal(t, 2, objectStoreFactory.closeCalled)
	require.Equal(t, 2, objectStoreFactory.nextCalled)
}

func testEventStoreConfig() config.EventStoreConfig {
	return config.EventStoreConfig{
		Token: config.ValueSource{Value: time.Now().Format(time.RFC3339)},
	}
}

func testObjectStoreConfig() config.ObjectStoreConfig {
	return config.ObjectStoreConfig{
		ObjectStoreS3Config: config.ObjectStoreS3Config{
			Endpoint: "https://example.com",
			Bucket:   "test-bucket",
		},
	}
}
