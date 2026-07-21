package config

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// ConfigProvider defines a unified interface for watching and loading configurations
type ConfigProvider interface {
	// Start monitoring for configuration changes
	Start() error

	// Stop monitoring for configuration changes
	Stop()

	// OnConfigChange registers a callback for configuration changes
	OnConfigChange(callback func(*Config) (wait func(), err error))

	// Reload forces a configuration reload
	Reload() error
}

// ConfigManager handles all configuration needs with a unified approach
type ConfigManager struct {
	config      *Config
	provider    ConfigProvider
	subscribers []configSubscriber
	mu          sync.RWMutex
	applyMu     sync.Mutex
	logger      *zap.Logger
}

type configSubscriber struct {
	apply    func(*Config) error
	fallible bool
}

// NewConfigManager creates a config manager with a specific provider
func NewConfigManager(logger *zap.Logger, provider ConfigProvider) *ConfigManager {
	cm := &ConfigManager{
		logger:   logger,
		provider: provider,
	}

	// Register for config updates from provider
	provider.OnConfigChange(func(cfg *Config) (wait func(), err error) {
		return cm.updateConfig(cfg)
	})

	return cm
}

// Subscribe to config changes
func (cm *ConfigManager) Subscribe(callback func(*Config)) {
	_ = cm.subscribe(func(cfg *Config) error {
		callback(cfg)
		return nil
	}, false)
}

func (cm *ConfigManager) subscribe(callback func(*Config) error, fallible bool) error {
	cm.applyMu.Lock()
	defer cm.applyMu.Unlock()

	cm.mu.Lock()
	cm.subscribers = append(cm.subscribers, configSubscriber{apply: callback, fallible: fallible})
	cfg := cm.config
	cm.mu.Unlock()

	// Immediately call with current config if available
	if cfg != nil {
		return callback(cfg)
	}
	return nil
}

type ConfigSetter interface {
	SetConfig(cfg *Config)
}

type ConfigApplier interface {
	ApplyConfig(cfg *Config) error
}

func (cm *ConfigManager) SubscribeSetter(setter ConfigSetter) error {
	if applier, ok := setter.(ConfigApplier); ok {
		return cm.subscribe(applier.ApplyConfig, true)
	}

	return cm.subscribe(func(cfg *Config) error {
		setter.SetConfig(cfg)
		return nil
	}, false)
}

// GetConfig returns the current configuration
func (cm *ConfigManager) GetConfig() *Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config
}

// updateConfig applies a validated generation before publishing it.
func (cm *ConfigManager) updateConfig(cfg *Config) (func(), error) {
	cm.applyMu.Lock()
	defer cm.applyMu.Unlock()

	if cfg == nil {
		return nil, errors.New("invalid config: config is nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	cm.mu.RLock()
	subscribers := make([]configSubscriber, len(cm.subscribers))
	copy(subscribers, cm.subscribers)
	cm.mu.RUnlock()

	for _, sub := range subscribers {
		if !sub.fallible {
			continue
		}
		if err := sub.apply(cfg); err != nil {
			return nil, fmt.Errorf("applying config subscriber: %w", err)
		}
	}
	for _, sub := range subscribers {
		if sub.fallible {
			continue
		}
		if err := sub.apply(cfg); err != nil {
			return nil, fmt.Errorf("applying config subscriber: %w", err)
		}
	}

	cm.mu.Lock()
	cm.config = cfg
	cm.mu.Unlock()

	cm.logger.Info("configuration updated, notified subscribers")
	return func() {}, nil
}

// Reload forces a configuration reload
func (cm *ConfigManager) Reload() error {
	return cm.provider.Reload()
}

// Run starts the config manager
func (cm *ConfigManager) Run(ctx context.Context) error {
	// Start the provider
	if err := cm.provider.Start(); err != nil {
		return err
	}

	// Wait for context cancellation
	go func() {
		<-ctx.Done()
		cm.provider.Stop()
	}()

	return nil
}
