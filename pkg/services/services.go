package services

import (
	"context"
)

// ServiceType represents a type identifier for services
type ServiceType string

func (s ServiceType) String() string {
	return string(s)
}

// Service is the base interface that all services must implement
type Service interface {
	// ServiceType returns the type of service
	ServiceType() ServiceType
}

// FactoryFactory creates a service factory 🎶
type FactoryFactory func() ServiceFactory

// ServiceFactory creates service instances
type ServiceFactory interface {
	// Init initializes the service factory
	Init(ctx context.Context, config any) error
	// Create creates a new service instance
	Create(ctx context.Context) (Service, error)
	// FactoryType returns the type of factory
	FactoryType() ServiceType
	// ServiceType returns the type of service this factory creates
	ServiceType() ServiceType
}

// SetRegistry sets the registry for the service
type SetRegistry interface {
	SetRegistry(registry RegistryAccessor)
}

// RegistryAccessor is a type that can access the service registry
type RegistryAccessor interface {
	// Get retrieves a service factory by type
	Get(serviceType ServiceType) ServiceFactory
}
