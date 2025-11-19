package services

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/rulekit/set"
	"go.uber.org/zap"
)

// FactoryManager handles factory creation and configuration
type FactoryManager struct {
	ctx           context.Context
	logger        *zap.Logger
	factories     *FactoryRegistry
	factoryFns    *Registry[ServiceType, FactoryFn]
	extraServices []func(*config.Config) *config.ServiceConfig
	mu            sync.Mutex
}

// NewFactoryManager creates a new factory manager
func NewFactoryManager(ctx context.Context, logger *zap.Logger, registry *FactoryRegistry) *FactoryManager {
	return &FactoryManager{
		ctx:        ctx,
		logger:     logger,
		factories:  registry,
		factoryFns: NewRegistry[ServiceType, FactoryFn](),
	}
}

// AddExtraServices adds services that are always added to the config.
func (m *FactoryManager) AddExtraServices(services ...func(*config.Config) *config.ServiceConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.extraServices = append(m.extraServices, services...)
}

// RegisterFactory registers a service factory
func (m *FactoryManager) RegisterFactory(fns ...FactoryFn) {
	for _, fn := range fns {
		factory := fn()
		if _, exists := m.factoryFns.Load(factory.FactoryType()); !exists {
			m.logger.Debug("registering factory", zap.String("factory_type", factory.FactoryType().String()))
			m.factoryFns.Register(factory.FactoryType(), fn)
		}
	}
}

// SetConfig processes a config update and creates/updates services
func (m *FactoryManager) SetConfig(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		// TODO: should we clear all existing factories in this case?
		return
	}

	plan, err := m.getServiceConfigs(cfg)
	if err != nil {
		m.logger.Error("failed to get service config plan, keeping existing config", zap.Error(err))
		return
	}

	// initialize all factories
	// bail out if any factory fails to initialize
	for _, cfg := range plan {
		if err := cfg.factory.Init(m.ctx, cfg.config); err != nil {
			// TODO: fail hard
			m.logger.Error("failed to initialize service", zap.Stringer("service", cfg.factory.FactoryType()), zap.Error(err))
			// m.logger.Error("failed to initialize service, keeping existing config", zap.Stringer("service", cfg.factory.FactoryType()), zap.Error(err))
			// return // keep the existing config
		}
	}

	// keep track of factories that need to be replaced with new instances
	replacements := make(map[Factory]Factory)
	for _, cfg := range plan {
		keys := cfg.keys()
		for _, key := range keys {
			// check if there is an old factory for this key.
			// keep the old factory reference in the replacements map as
			// the registry entry will be replaced with the new factory
			old := m.factories.Get(key)
			if old != nil {
				replacements[old] = cfg.factory
			}

			// register the new factory for this key
			m.factories.Register(cfg.factory, key.ID)
		}
	}

	// replace any old factories
	for old, new := range replacements {
		m.replaceFactory(old, new)
	}
}

func (m *FactoryManager) replaceFactory(old Factory, new Factory) {
	m.logger.Debug("replacing factory",
		zap.Stringer("old_factory_type", old.FactoryType()),
		zap.Stringer("new_factory_type", new.FactoryType()))

	// send replacement factory to services that support it
	if next, ok := old.(NextFactory); ok {
		next.Next(new)
	}

	// close old factory if it implements Closer
	if closer, ok := old.(io.Closer); ok {
		closer.Close()
	}
}

type serviceConfig struct {
	isDefault bool
	id        string
	config    any
	factory   Factory
}

func (c *serviceConfig) keys() []ServiceKey {
	var keys []ServiceKey
	if c.id != "" {
		keys = append(keys, ServiceKey{Type: c.factory.ServiceType(), ID: c.id})
	}
	if c.isDefault {
		keys = append(keys, ServiceKey{Type: c.factory.ServiceType()})
	}
	return keys
}

// getServiceConfigs creates a config plan given a user config.
//
// The following guarantees are made:
//   - Each ServiceType has a default factory assigned.
//     If one must be assigned by this function, the default factory will be the first instance we see of that ServiceType.
//   - Each non-default service has an ID.
//   - The order of the services in the config plan is the same as the order of the services in the user config, with extra services added at the end.
func (m *FactoryManager) getServiceConfigs(cfg *config.Config) ([]*serviceConfig, error) {
	// get all service configs from the user config + append any extra services
	svcs := cfg.Services.All()
	for _, fn := range m.extraServices {
		svcs = append(svcs, fn(cfg))
	}

	// hasDefault keeps track of the service types that have a default factory assigned.
	hasDefault := set.NewSet[ServiceType]()
	cfgs := make([]*serviceConfig, 0, len(svcs))
	for _, svc := range svcs {
		fn := m.factoryFns.Get(ServiceType(svc.Type))
		if fn == nil {
			// TODO: fail hard
			m.logger.Error("no factory registered for service type", zap.String("service_type", svc.Type))
			continue
			// return nil, fmt.Errorf("no factory registered for service type: %s", svc.Type)
		}
		factory := fn()

		if svc.Default {
			hasDefault.Add(factory.ServiceType())
		}
		cfgs = append(cfgs, &serviceConfig{
			isDefault: svc.Default,
			id:        svc.ID,
			factory:   factory,
			config:    svc.Config,
		})
	}

	// ensure that each ServiceType has a default factory assigned.
	// services that are not default will be assigned an ID.
	for i, cfg := range cfgs {
		if cfg.isDefault {
			hasDefault.Add(cfg.factory.ServiceType())
			continue
		}

		if !hasDefault.Contains(cfg.factory.ServiceType()) {
			// assigning the first instance we see of this service type as the default
			hasDefault.Add(cfg.factory.ServiceType())
			cfg.isDefault = true
		}

		if !cfg.isDefault && cfg.id == "" {
			// ensure that all non-default services have an ID
			cfg.id = fmt.Sprintf("service-%d", i)
		}
	}

	return cfgs, nil
}
