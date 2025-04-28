package services

import (
	"fmt"
	"io"
	"sync"
)

// ServiceRegistry holds references to active service instances
type ServiceRegistry struct {
	services map[ServiceType]ServiceFactory
	mu       sync.RWMutex
}

// NewServiceRegistry creates a new service registry
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		services: make(map[ServiceType]ServiceFactory),
	}
}

// Register adds or replaces a service in the registry
func (sr *ServiceRegistry) Register(svc ServiceFactory) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	sr.services[svc.ServiceType()] = svc
}

// Get retrieves a service by type
func (sr *ServiceRegistry) Get(serviceType ServiceType) ServiceFactory {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	return sr.services[serviceType]
}

// Close closes all registered services that implement CloseableService
func (sr *ServiceRegistry) Close() error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	var errs []error
	for _, svc := range sr.services {
		if closer, ok := svc.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing services: %v", errs)
	}

	return nil
}
