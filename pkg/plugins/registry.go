package plugins

import (
	"fmt"
	"io"
	"sync"
)

// PluginRegistry holds references to active plugin instances
type PluginRegistry struct {
	plugins map[PluginType]Plugin
	mu      sync.RWMutex
}

// NewRegistry creates a new service registry
func NewRegistry(plugins ...Plugin) *PluginRegistry {
	registry := &PluginRegistry{
		plugins: make(map[PluginType]Plugin),
	}

	for _, p := range plugins {
		registry.Register(p)
	}

	return registry
}

// Register adds or replaces a service in the registry
func (sr *PluginRegistry) Register(svc Plugin) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	// Close old plugin if it implements Closer
	if old, exists := sr.plugins[svc.PluginType()]; exists {
		if closer, ok := old.(io.Closer); ok {
			closer.Close()
		}
	}

	sr.plugins[svc.PluginType()] = svc
}

// Get retrieves a plugin by type
func (sr *PluginRegistry) Get(pluginType PluginType) Plugin {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	return sr.plugins[pluginType]
}

// Close closes all registered services that implement CloseableService
func (sr *PluginRegistry) Close() error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	var errs []error
	for _, svc := range sr.plugins {
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
