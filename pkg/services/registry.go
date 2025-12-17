package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/qpoint-io/qtap/pkg/synq"
)

/*
	Convenience types
*/

// FactoryRegistry is a convenience type for a registry of service factories.
// Factory registries are keyed by ServiceType() and not FactoryType() so this wrapper exists to avoid any confusion by users.
type FactoryRegistry struct {
	r *Registry[ServiceKey, Factory]

	keysByType map[ServiceType][]ServiceKey
	mu         sync.Mutex
}

func NewFactoryRegistry(factories ...Factory) *FactoryRegistry {
	fr := NewRegistrySize[ServiceKey, Factory](len(factories))
	for _, factory := range factories {
		fr.Register(ServiceKey{Type: factory.ServiceType()}, factory)
	}
	return &FactoryRegistry{
		r:          fr,
		keysByType: make(map[ServiceType][]ServiceKey),
	}
}

// Register registers a default service factory by type and optional ID.
// If the ID is empty, the factory is registered as the default for the type.
func (fr *FactoryRegistry) Register(value Factory, id string) {
	key := ServiceKey{Type: value.ServiceType(), ID: id}
	fr.r.Register(key, value)
	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.keysByType[value.ServiceType()] = append(fr.keysByType[value.ServiceType()], key)
}

// Load retrieves a service factory by type and optional ID.
// If the ID is empty, the default factory for the type is returned.
func (fr *FactoryRegistry) Load(key ServiceKey) (Factory, bool) {
	return fr.r.Load(key)
}

// Get retrieves a service factory by type and optional ID.
// If the ID is empty, the default factory for the type is returned.
func (fr *FactoryRegistry) Get(key ServiceKey) Factory {
	f, ok := fr.Load(key)
	if !ok {
		return nil
	}
	return f
}

func (fr *FactoryRegistry) AvailableFactoriesForType(typ ServiceType) []ServiceKey {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	return fr.keysByType[typ]
}

// ServiceRegistry is a connection-scoped registry that handles creating services from factories.
// This is used to apply adapters and maintain a singleton store for service instances.
type ServiceRegistry struct {
	services     *Registry[ServiceKey, Service]
	factories    *FactoryRegistry
	configurator ServiceConfigurator

	// group ensures that we don't create the same service multiple times concurrently.
	group synq.SingleFlight[ServiceKey, Service]
}

type ServiceConfigurator func(ctx context.Context, service Service) (Service, error)

func NewServiceRegistry(fr *FactoryRegistry) *ServiceRegistry {
	if fr == nil {
		fr = NewFactoryRegistry()
	}
	return &ServiceRegistry{
		services:  NewRegistry[ServiceKey, Service](),
		factories: fr,
	}
}

// SetConfigurator sets a configurator function for any service configuration that needs to be done once it is created.
// This is typically used to apply adapters.
func (sr *ServiceRegistry) SetConfigurator(configurator ServiceConfigurator) {
	sr.configurator = configurator
}

// Get retrieves a service by type and optional ID.
// If a service hasn't been created yet, it will be created using the corresponding factory, configured, and cached
// for future Get calls.
func (sr *ServiceRegistry) Get(ctx context.Context, key ServiceKey) (Service, error) {
	// per-key mutex to allow for resolving of cross-service dependencies
	if service, ok := sr.services.Load(key); ok {
		// the service has already been created
		return service, nil
	}

	// queue a service creation. group ensures that we don't create the same service multiple times concurrently.
	return sr.group.Do(key, func() (Service, error) {
		// the service may have been created by the time our creation is executed.
		// check again:
		if service, ok := sr.services.Load(key); ok {
			return service, nil
		}

		// create the service and store it
		factory, ok := sr.factories.Load(key)
		if !ok {
			return nil, fmt.Errorf("factory not found for service %s", key)
		}
		service, err := factory.Create(ctx, sr)
		if err != nil {
			return nil, fmt.Errorf("creating service: %w", err)
		}

		// configure the service
		if sr.configurator != nil {
			service, err = sr.configurator(ctx, service)
			if err != nil {
				return nil, fmt.Errorf("configuring service: %w", err)
			}
		}

		sr.services.Register(key, service)
		return service, nil
	})
}

func (sr *ServiceRegistry) AvailableServicesForType(typ ServiceType) []ServiceKey {
	return sr.factories.AvailableFactoriesForType(typ)
}

func (sr *ServiceRegistry) Close() error {
	return sr.services.Close()
}

// Has checks if the registry is able to return the given service type and ID.
func (sr *ServiceRegistry) Has(key ServiceKey) bool {
	_, ok := sr.factories.Load(key)
	return ok
}

/*
	Registry pattern implementation
*/

type Registry[K comparable, T any] struct {
	m *synq.Map[K, T]
}

func NewRegistry[K comparable, T any]() *Registry[K, T] {
	return NewRegistrySize[K, T](0)
}

func NewRegistrySize[K comparable, T any](size int) *Registry[K, T] {
	return &Registry[K, T]{
		m: synq.NewMapSize[K, T](size),
	}
}

func (sr *Registry[K, T]) Register(key K, value T) {
	sr.m.Store(key, value)
}

func (sr *Registry[K, T]) Load(key K) (T, bool) {
	return sr.m.Load(key)
}

func (sr *Registry[K, T]) Get(key K) T {
	value, ok := sr.m.Load(key)
	if !ok {
		var zero T
		return zero
	}

	return value
}

func (sr *Registry[K, T]) Close() error {
	var errs []error
	sr.m.Iter(func(key K, value T) bool {
		if closer, ok := any(value).(io.Closer); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		return true
	})
	return errors.Join(errs...)
}

func (sr *Registry[K, T]) Len() int {
	return sr.m.Len()
}
