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

type shutdownMockFactory struct {
	*mockFactory
	shutdownCalled  int
	shutdownErr     error
	shutdownErrFunc func(context.Context) error
	shutdownFunc    func()
}

func (m *shutdownMockFactory) Shutdown(ctx context.Context) error {
	m.shutdownCalled++
	if m.shutdownFunc != nil {
		m.shutdownFunc()
	}
	if m.shutdownErrFunc != nil {
		return m.shutdownErrFunc(ctx)
	}
	return m.shutdownErr
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

	noopDefaultObjectStoreConfig := &serviceConfig{
		isDefault: true,
		factory:   noopObjectStoreFactoryFn(),
		config:    config.ServiceObjectStore{Type: "disabled"},
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
					noopObjectStoreFactoryFn,
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
					noopDefaultObjectStoreConfig,
				}, configs)
			},
		},
		{
			name: "multiple services - first becomes default",
			setupManager: func(t *testing.T) *FactoryManager {
				m := NewFactoryManager(t.Context(), zaptest.NewLogger(t), NewFactoryRegistry())
				m.RegisterFactory(
					newMockFactoryFn("eventstore.console", "eventstore"),
					noopObjectStoreFactoryFn,
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
					noopDefaultObjectStoreConfig,
				}, configs)
			},
		},
		{
			name: "auto-assigns ID to non-default service without ID",
			setupManager: func(t *testing.T) *FactoryManager {
				m := NewFactoryManager(t.Context(), zaptest.NewLogger(t), NewFactoryRegistry())
				m.RegisterFactory(
					newMockFactoryFn("eventstore.console", "eventstore"),
					noopObjectStoreFactoryFn,
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
					noopDefaultObjectStoreConfig,
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
				}, configs)
			},
		},
		{
			name: "extra services appended at end",
			setupManager: func(t *testing.T) *FactoryManager {
				m := NewFactoryManager(t.Context(), zaptest.NewLogger(t), NewFactoryRegistry())
				m.RegisterFactory(
					newMockFactoryFn("eventstore.console", "eventstore"),
					noopObjectStoreFactoryFn,
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
					noopDefaultObjectStoreConfig,
					{
						id:      "service-2",
						factory: newMockFactory("eventstore.console", "eventstore"),
					},
					{
						isDefault: true,
						factory:   newMockFactory("new.type", "new"),
						config:    net.IP{},
					},
					{
						id:      "service-4",
						factory: newMockFactory("new.type", "new"),
					},
				}, configs)
			},
		},
		{
			name: "unknown factory type returns error",
			setupManager: func(t *testing.T) *FactoryManager {
				m := NewFactoryManager(t.Context(), zaptest.NewLogger(t), NewFactoryRegistry())
				m.RegisterFactory(
					newMockFactoryFn("eventstore.console", "eventstore"),
					noopObjectStoreFactoryFn,
				)
				return m
			},
			setupConfig: func() *config.Config {
				return &config.Config{
					Services: config.Services{
						EventStores: []config.ServiceEventStore{
							{Type: "console", EventStoreConfig: testEventStoreConfig()},
						},
					},
				}
			},
			extraServices: []func(*config.Config) *config.ServiceConfig{
				func(*config.Config) *config.ServiceConfig {
					return &config.ServiceConfig{Type: "unknown.factory"}
				},
			},
			wantErr:     true,
			errContains: "no factory registered for service type: unknown.factory",
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
	// Duplicate registrations leave the original factory function in place.
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
	require.Equal(t, 0, eventStoreFactory.closeCalled)
	require.Equal(t, 0, eventStoreFactory.nextCalled)

	// push a bad config with an unknown service type. our existing factories should not be replaced.
	m.AddExtraServices(func(cfg *config.Config) *config.ServiceConfig {
		return &config.ServiceConfig{
			Type: "unknown.factory.type", // unknown factory type
		}
	})
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

	errorLogs := logs.FilterLevelExact(zapcore.ErrorLevel)
	require.Equal(t, 1, errorLogs.Len(), "expected one error log")
	assert.Equal(t, "failed to apply service config, keeping existing config", errorLogs.All()[0].Message)
	require.Equal(t, 1, objectStoreFactory.initCalled)
	require.Equal(t, 0, objectStoreFactory.closeCalled)
	require.Equal(t, 0, objectStoreFactory.nextCalled)
}

func TestFactoryManager_UnknownServiceTypeLeavesRegistryUnchanged(t *testing.T) {
	registry := NewFactoryRegistry()
	manager := NewFactoryManager(t.Context(), zaptest.NewLogger(t), registry)
	active := newMockFactory("eventstore.console", "eventstore")
	manager.RegisterFactory(
		func() Factory { return active },
		newMockFactoryFn("objectstore.noop", "objectstore"),
	)
	valid := &config.Config{Services: config.Services{
		EventStores: []config.ServiceEventStore{{Type: config.EventStoreType_CONSOLE}},
	}}
	require.NoError(t, manager.ApplyConfig(valid))
	require.Same(t, active, registry.Get(ServiceKey{Type: "eventstore"}))

	manager.AddExtraServices(func(*config.Config) *config.ServiceConfig {
		return &config.ServiceConfig{Type: "unknown.factory"}
	})
	err := manager.ApplyConfig(valid)
	require.ErrorContains(t, err, "no factory registered for service type: unknown.factory")
	assert.Same(t, active, registry.Get(ServiceKey{Type: "eventstore"}))
	assert.Equal(t, 1, active.initCalled)
	assert.Equal(t, 0, active.closeCalled)
	assert.Equal(t, 0, active.nextCalled)
}

func TestFactoryManager_ApplyConfigInitFailureIsTransactional(t *testing.T) {
	registry := NewFactoryRegistry()
	manager := NewFactoryManager(t.Context(), zaptest.NewLogger(t), registry)

	oldEvent := newMockFactory("eventstore.console", "eventstore")
	oldObject := newMockFactory("objectstore.console", "objectstore")
	eventCandidate := newMockFactory("eventstore.console", "eventstore")
	objectCandidate := newMockFactory("objectstore.console", "objectstore")
	objectCandidate.initErr = errors.New("object init failed")

	currentEvent := oldEvent
	currentObject := oldObject
	manager.RegisterFactory(
		func() Factory { return currentEvent },
		func() Factory { return currentObject },
	)
	cfg := &config.Config{Services: config.Services{
		EventStores:  []config.ServiceEventStore{{Type: config.EventStoreType_CONSOLE}},
		ObjectStores: []config.ServiceObjectStore{{Type: config.ObjectStoreType_CONSOLE}},
	}}
	require.NoError(t, manager.ApplyConfig(cfg))
	require.Same(t, oldEvent, registry.Get(ServiceKey{Type: "eventstore"}))
	require.Same(t, oldObject, registry.Get(ServiceKey{Type: "objectstore"}))

	currentEvent = eventCandidate
	currentObject = objectCandidate
	err := manager.ApplyConfig(cfg)
	require.ErrorIs(t, err, objectCandidate.initErr)

	assert.Equal(t, 1, eventCandidate.initCalled, "all candidates must be initialized before rollback")
	assert.Equal(t, 1, objectCandidate.initCalled)
	assert.Equal(t, 1, eventCandidate.closeCalled)
	assert.Equal(t, 1, objectCandidate.closeCalled, "a partially initialized failing candidate must be closed")
	assert.Equal(t, 0, oldEvent.closeCalled)
	assert.Equal(t, 0, oldObject.closeCalled)
	assert.Equal(t, 0, oldEvent.nextCalled)
	assert.Equal(t, 0, oldObject.nextCalled)
	assert.Same(t, oldEvent, registry.Get(ServiceKey{Type: "eventstore"}))
	assert.Same(t, oldObject, registry.Get(ServiceKey{Type: "objectstore"}))

	require.NoError(t, manager.Shutdown(t.Context()))
	assert.Equal(t, 1, oldEvent.closeCalled, "the retained event factory remains manager-owned")
	assert.Equal(t, 1, oldObject.closeCalled, "the retained object factory remains manager-owned")
	assert.Equal(t, 1, eventCandidate.closeCalled, "rolled-back candidates are not active")
	assert.Equal(t, 1, objectCandidate.closeCalled, "rolled-back candidates are not active")
}

func TestFactoryManager_ApplyConfigRetainsFailedRollbackCandidateForShutdown(t *testing.T) {
	manager := NewFactoryManager(t.Context(), zaptest.NewLogger(t), NewFactoryRegistry())
	rollbackErr := errors.New("candidate rollback failed")
	eventCandidate := newMockFactory("eventstore.console", "eventstore")
	eventCandidate.closeFunc = func() error {
		if eventCandidate.closeCalled == 1 {
			return rollbackErr
		}
		return nil
	}
	objectCandidate := newMockFactory("objectstore.console", "objectstore")
	objectCandidate.initErr = errors.New("object init failed")
	manager.RegisterFactory(
		func() Factory { return eventCandidate },
		func() Factory { return objectCandidate },
	)
	cfg := &config.Config{Services: config.Services{
		EventStores:  []config.ServiceEventStore{{Type: config.EventStoreType_CONSOLE}},
		ObjectStores: []config.ServiceObjectStore{{Type: config.ObjectStoreType_CONSOLE}},
	}}

	err := manager.ApplyConfig(cfg)
	require.ErrorIs(t, err, objectCandidate.initErr)
	require.ErrorIs(t, err, rollbackErr)
	assert.Equal(t, 1, eventCandidate.closeCalled)
	assert.Equal(t, 1, objectCandidate.closeCalled)

	require.NoError(t, manager.Shutdown(t.Context()))
	assert.Equal(t, 2, eventCandidate.closeCalled, "failed candidate rollback must be retried")
	assert.Equal(t, 1, objectCandidate.closeCalled, "successful candidate rollback must not be retried")
}

func TestFactoryManager_ReplacementDrainsObjectStoreBeforeEventStore(t *testing.T) {
	registry := NewFactoryRegistry()
	manager := NewFactoryManager(t.Context(), zaptest.NewLogger(t), registry)

	var order []string
	oldEvent := newMockFactory("eventstore.console", "eventstore")
	oldEvent.closeFunc = func() error {
		order = append(order, "event")
		return nil
	}
	oldObject := newMockFactory("objectstore.console", "objectstore")
	oldObject.closeFunc = func() error {
		order = append(order, "object")
		return nil
	}
	currentEvent := oldEvent
	currentObject := oldObject
	manager.RegisterFactory(
		func() Factory { return currentEvent },
		func() Factory { return currentObject },
	)
	cfg := &config.Config{Services: config.Services{
		EventStores:  []config.ServiceEventStore{{Type: config.EventStoreType_CONSOLE}},
		ObjectStores: []config.ServiceObjectStore{{Type: config.ObjectStoreType_CONSOLE}},
	}}
	require.NoError(t, manager.ApplyConfig(cfg))

	currentEvent = newMockFactory("eventstore.console", "eventstore")
	currentObject = newMockFactory("objectstore.console", "objectstore")
	require.NoError(t, manager.ApplyConfig(cfg))

	assert.Equal(t, []string{"object", "event"}, order)
}

func TestFactoryManager_ApplyConfigRetainsFailedOldFactoryForShutdown(t *testing.T) {
	core, logs := observer.New(zapcore.ErrorLevel)
	manager := NewFactoryManager(t.Context(), zap.New(core), NewFactoryRegistry())
	closeErr := errors.New("old close failed")
	old := newMockFactory("eventstore.console", "eventstore")
	old.closeFunc = func() error {
		if old.closeCalled == 1 {
			return closeErr
		}
		return nil
	}
	current := old
	manager.RegisterFactory(
		func() Factory { return current },
		newMockFactoryFn("objectstore.noop", "objectstore"),
	)
	cfg := &config.Config{Services: config.Services{
		EventStores: []config.ServiceEventStore{{Type: config.EventStoreType_CONSOLE}},
	}}
	require.NoError(t, manager.ApplyConfig(cfg))

	replacement := newMockFactory("eventstore.console", "eventstore")
	current = replacement
	require.NoError(t, manager.ApplyConfig(cfg), "published topology must not be rejected by an old close failure")
	assert.Same(t, replacement, manager.factories.Get(ServiceKey{Type: "eventstore"}))
	assert.Equal(t, 1, old.closeCalled)
	require.Equal(t, 1, logs.Len())
	assert.Equal(t, closeErr.Error(), logs.All()[0].ContextMap()["error"])

	require.NoError(t, manager.Shutdown(t.Context()))
	assert.Equal(t, 2, old.closeCalled, "failed retired factory must be retried")
	assert.Equal(t, 1, replacement.closeCalled)
}

func TestFactoryManager_ApplyConfigRetainsFailedUnusedCandidateForShutdown(t *testing.T) {
	manager := NewFactoryManager(t.Context(), zaptest.NewLogger(t), NewFactoryRegistry())
	closeErr := errors.New("unused close failed")
	unused := newMockFactory("other.test", "other")
	unused.closeFunc = func() error {
		if unused.closeCalled == 1 {
			return closeErr
		}
		return nil
	}
	active := newMockFactory("other.test", "other")
	factoryCalls := 0
	manager.RegisterFactory(
		func() Factory {
			factoryCalls++
			if factoryCalls <= 2 {
				return unused
			}
			return active
		},
		newMockFactoryFn("eventstore.noop", "eventstore"),
		newMockFactoryFn("objectstore.noop", "objectstore"),
	)
	manager.AddExtraServices(
		func(*config.Config) *config.ServiceConfig {
			return &config.ServiceConfig{Type: "other.test", Default: true}
		},
		func(*config.Config) *config.ServiceConfig {
			return &config.ServiceConfig{Type: "other.test", Default: true}
		},
	)

	require.NoError(t, manager.ApplyConfig(&config.Config{}))
	assert.Same(t, active, manager.factories.Get(ServiceKey{Type: "other"}))
	assert.Equal(t, 1, unused.closeCalled)

	require.NoError(t, manager.Shutdown(t.Context()))
	assert.Equal(t, 2, unused.closeCalled, "failed unused candidate must be retried")
	assert.Equal(t, 1, active.closeCalled)
}

func TestFactoryManager_ShutdownRetainsOnlyFailedFactoriesForRetry(t *testing.T) {
	registry := NewFactoryRegistry()
	manager := NewFactoryManager(t.Context(), zaptest.NewLogger(t), registry)

	var order []string
	eventFactory := &shutdownMockFactory{mockFactory: newMockFactory("eventstore.console", "eventstore")}
	registryClearedBeforeDrain := false
	newServiceRejectedBeforeDrain := false
	freshServices := NewServiceRegistry(registry)
	eventFactory.shutdownFunc = func() {
		order = append(order, "event")
	}
	objectFactory := &shutdownMockFactory{mockFactory: newMockFactory("objectstore.console", "objectstore")}
	objectFactory.shutdownFunc = func() {
		order = append(order, "object")
		_, eventAvailable := registry.Load(ServiceKey{Type: "eventstore"})
		_, objectAvailable := registry.Load(ServiceKey{Type: "objectstore"})
		registryClearedBeforeDrain = !eventAvailable && !objectAvailable && len(registry.AvailableFactoriesForType("objectstore")) == 0
		_, err := freshServices.Get(t.Context(), ServiceKey{Type: "eventstore"})
		newServiceRejectedBeforeDrain = err != nil
	}
	objectFactory.shutdownErrFunc = func(ctx context.Context) error { return ctx.Err() }
	otherFactory := newMockFactory("other.test", "other")
	otherFactory.closeFunc = func() error {
		order = append(order, "other")
		return nil
	}

	manager.RegisterFactory(
		func() Factory { return eventFactory },
		func() Factory { return objectFactory },
		func() Factory { return otherFactory },
	)
	manager.AddExtraServices(func(*config.Config) *config.ServiceConfig {
		return &config.ServiceConfig{Type: "other.test"}
	})
	cfg := &config.Config{Services: config.Services{
		EventStores:  []config.ServiceEventStore{{Type: config.EventStoreType_CONSOLE}},
		ObjectStores: []config.ServiceObjectStore{{Type: config.ObjectStoreType_CONSOLE, ID: "archive"}},
	}}
	require.NoError(t, manager.ApplyConfig(cfg))
	require.Same(t, objectFactory, registry.Get(ServiceKey{Type: "objectstore"}))
	require.Same(t, objectFactory, registry.Get(ServiceKey{Type: "objectstore", ID: "archive"}))

	deadlineCtx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	err := manager.Shutdown(deadlineCtx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, registryClearedBeforeDrain, "registry must be empty before the first factory drain")
	assert.True(t, newServiceRejectedBeforeDrain, "a fresh service registry must not create from a draining factory")
	assert.Nil(t, registry.Get(ServiceKey{Type: "eventstore"}))
	assert.Nil(t, registry.Get(ServiceKey{Type: "objectstore"}))
	assert.Nil(t, registry.Get(ServiceKey{Type: "objectstore", ID: "archive"}))
	assert.Equal(t, []string{"object", "event", "other"}, order)
	assert.Equal(t, 1, objectFactory.shutdownCalled, "default and ID aliases must be shut down once")
	assert.Equal(t, 1, eventFactory.shutdownCalled)
	assert.Equal(t, 1, otherFactory.closeCalled)

	require.NoError(t, manager.Shutdown(t.Context()))
	assert.Equal(t, []string{"object", "event", "other", "object"}, order)
	assert.Equal(t, 2, objectFactory.shutdownCalled, "failed factory must be retried with the fresh context")
	assert.Equal(t, 1, eventFactory.shutdownCalled)
	assert.Equal(t, 1, otherFactory.closeCalled)

	require.NoError(t, manager.Shutdown(t.Context()))
	assert.Equal(t, 2, objectFactory.shutdownCalled)
	assert.Equal(t, 1, eventFactory.shutdownCalled)
	assert.Equal(t, 1, otherFactory.closeCalled)

	replacement := newMockFactory("eventstore.console", "eventstore")
	manager.RegisterFactory(func() Factory { return replacement })
	require.ErrorIs(t, manager.ApplyConfig(cfg), ErrFactoryManagerShutdown)
	assert.Equal(t, 0, replacement.initCalled, "shutdown freezes config updates")
}

var _ ConfigApplier = (*FactoryManager)(nil)

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
