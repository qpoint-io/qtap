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
//
//go:generate go tool go.uber.org/mock/mockgen -destination ./mocks/service_factory.go -package mocks . ServiceFactory
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

// SetFactoryRegistry sets the factory registry for the service
type SetFactoryRegistry interface {
	SetFactoryRegistry(registry FactoryRegistryAccessor)
}

// FactoryRegistryAccessor is a type that can access the service registry
type FactoryRegistryAccessor interface {
	// CreateService creates a service by type
	CreateService(ctx context.Context, serviceType ServiceType) (Service, error)
}

// NextFactory indicates that a factory will handle graceful replacements
// of itself to the replacement factory
type NextFactory interface {
	Next(ServiceFactory)
}
