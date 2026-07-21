package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/rulekit/set"
	"go.uber.org/zap"
)

var ErrFactoryManagerShutdown = errors.New("factory manager is shut down")

// ConfigApplier applies a configuration and reports whether it was accepted.
type ConfigApplier interface {
	ApplyConfig(*config.Config) error
}

// FactoryManager handles factory creation and configuration
type FactoryManager struct {
	ctx           context.Context
	logger        *zap.Logger
	factories     *FactoryRegistry
	factoryFns    *Registry[ServiceType, FactoryFn]
	extraServices []func(*config.Config) *config.ServiceConfig
	active        []Factory
	activeKeys    map[ServiceKey]Factory
	pendingDrains []Factory
	shutdown      bool
	mu            sync.Mutex
}

// NewFactoryManager creates a new factory manager
func NewFactoryManager(ctx context.Context, logger *zap.Logger, registry *FactoryRegistry) *FactoryManager {
	return &FactoryManager{
		ctx:        ctx,
		logger:     logger,
		factories:  registry,
		factoryFns: NewRegistry[ServiceType, FactoryFn](),
		activeKeys: make(map[ServiceKey]Factory),
	}
}

// AddExtraServices adds services that are always added to the config.
func (m *FactoryManager) AddExtraServices(services ...func(*config.Config) *config.ServiceConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shutdown {
		return
	}
	m.extraServices = append(m.extraServices, services...)
}

// RegisterFactory registers a service factory
func (m *FactoryManager) RegisterFactory(fns ...FactoryFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shutdown {
		return
	}
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
	if err := m.ApplyConfig(cfg); err != nil {
		m.logger.Error("failed to apply service config, keeping existing config", zap.Error(err))
	}
}

// ApplyConfig initializes a complete candidate topology before publishing it.
func (m *FactoryManager) ApplyConfig(cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shutdown {
		return ErrFactoryManagerShutdown
	}
	if cfg == nil {
		return nil
	}

	plan, err := m.getServiceConfigs(cfg)
	if err != nil {
		return fmt.Errorf("getting service config plan: %w", err)
	}

	var initErrs []error
	for _, candidate := range plan {
		if err := candidate.factory.Init(m.ctx, candidate.config); err != nil {
			initErrs = append(initErrs, fmt.Errorf("initializing service %s: %w", candidate.factory.FactoryType(), err))
		}
	}
	if len(initErrs) > 0 {
		for _, candidate := range uniqueFactories(plan) {
			if containsFactory(m.active, candidate) {
				continue
			}
			if err := closeFactory(m.ctx, candidate); err != nil {
				m.pendingDrains = appendUniqueFactory(m.pendingDrains, candidate)
				initErrs = append(initErrs, fmt.Errorf("closing candidate %s: %w", candidate.FactoryType(), err))
			}
		}
		return errors.Join(initErrs...)
	}

	nextKeys := make(map[ServiceKey]Factory)
	var replacements []factoryReplacement
	for _, candidate := range plan {
		for _, key := range candidate.keys() {
			nextKeys[key] = candidate.factory
			if old := m.activeKeys[key]; old != nil && !sameFactory(old, candidate.factory) && !hasReplacement(replacements, old) {
				replacements = append(replacements, factoryReplacement{old: old, new: candidate.factory})
			}
		}
	}

	m.factories.Replace(nextKeys)

	nextActive := uniqueFactoryValues(plan, nextKeys)
	oldActive := m.active
	m.active = nextActive
	m.activeKeys = nextKeys

	var drains []Factory
	for _, old := range oldActive {
		if containsFactory(nextActive, old) {
			continue
		}
		var replacement Factory
		for _, item := range replacements {
			if sameFactory(item.old, old) {
				replacement = item.new
				break
			}
		}
		if replacement != nil {
			m.notifyReplacement(old, replacement)
		}
		drains = appendUniqueFactory(drains, old)
	}

	for _, candidate := range uniqueFactories(plan) {
		if !containsFactory(nextActive, candidate) {
			drains = appendUniqueFactory(drains, candidate)
		}
	}
	for _, factory := range orderFactoriesForDrain(drains) {
		if err := closeFactory(m.ctx, factory); err != nil {
			m.pendingDrains = appendUniqueFactory(m.pendingDrains, factory)
			m.logger.Error("failed to drain retired factory; will retry during shutdown",
				zap.Stringer("factory_type", factory.FactoryType()),
				zap.Error(err))
		}
	}
	return nil
}

func (m *FactoryManager) notifyReplacement(old Factory, new Factory) {
	m.logger.Debug("replacing factory",
		zap.Stringer("old_factory_type", old.FactoryType()),
		zap.Stringer("new_factory_type", new.FactoryType()))

	// send replacement factory to services that support it
	if next, ok := old.(NextFactory); ok {
		next.Next(new)
	}
}

// Shutdown freezes configuration updates and drains all active factories.
func (m *FactoryManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.shutdown {
		m.shutdown = true
		m.factories.Replace(map[ServiceKey]Factory{})
		for _, factory := range m.active {
			m.pendingDrains = appendUniqueFactory(m.pendingDrains, factory)
		}
		m.active = nil
		m.activeKeys = make(map[ServiceKey]Factory)
	}

	var errs []error
	failed := make([]Factory, 0, len(m.pendingDrains))
	for _, factory := range orderFactoriesForDrain(m.pendingDrains) {
		if err := closeFactory(ctx, factory); err != nil {
			errs = append(errs, fmt.Errorf("shutting down factory %s: %w", factory.FactoryType(), err))
			failed = append(failed, factory)
		}
	}
	m.pendingDrains = failed
	return errors.Join(errs...)
}

type factoryShutdowner interface {
	Shutdown(context.Context) error
}

func closeFactory(ctx context.Context, factory Factory) error {
	if shutdowner, ok := factory.(factoryShutdowner); ok {
		return shutdowner.Shutdown(ctx)
	}
	if closer, ok := factory.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type factoryReplacement struct {
	old Factory
	new Factory
}

func hasReplacement(replacements []factoryReplacement, factory Factory) bool {
	for _, replacement := range replacements {
		if sameFactory(replacement.old, factory) {
			return true
		}
	}
	return false
}

func uniqueFactories(plan []*serviceConfig) []Factory {
	factories := make([]Factory, 0, len(plan))
	for _, candidate := range plan {
		if !containsFactory(factories, candidate.factory) {
			factories = append(factories, candidate.factory)
		}
	}
	return factories
}

func uniqueFactoryValues(plan []*serviceConfig, byKey map[ServiceKey]Factory) []Factory {
	factories := make([]Factory, 0, len(plan))
	for _, candidate := range plan {
		for _, factory := range byKey {
			if sameFactory(candidate.factory, factory) && !containsFactory(factories, factory) {
				factories = append(factories, factory)
				break
			}
		}
	}
	return factories
}

func containsFactory(factories []Factory, target Factory) bool {
	for _, factory := range factories {
		if sameFactory(factory, target) {
			return true
		}
	}
	return false
}

func appendUniqueFactory(factories []Factory, factory Factory) []Factory {
	if containsFactory(factories, factory) {
		return factories
	}
	return append(factories, factory)
}

func orderFactoriesForDrain(factories []Factory) []Factory {
	ordered := make([]Factory, 0, len(factories))
	for _, serviceType := range []ServiceType{"objectstore", "eventstore"} {
		for _, factory := range factories {
			if factory.ServiceType() == serviceType {
				ordered = appendUniqueFactory(ordered, factory)
			}
		}
	}
	for _, factory := range factories {
		if factory.ServiceType() != "objectstore" && factory.ServiceType() != "eventstore" {
			ordered = appendUniqueFactory(ordered, factory)
		}
	}
	return ordered
}

func sameFactory(a, b Factory) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	aValue := reflect.ValueOf(a)
	bValue := reflect.ValueOf(b)
	if aValue.Type() != bValue.Type() {
		return false
	}
	if aValue.Type().Comparable() {
		return aValue.Interface() == bValue.Interface()
	}
	switch aValue.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Ptr, reflect.Slice, reflect.UnsafePointer:
		return aValue.Pointer() == bValue.Pointer()
	default:
		return false
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
			return nil, fmt.Errorf("no factory registered for service type: %s", svc.Type)
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
