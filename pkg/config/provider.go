package config

import (
	"context"
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
	subscribers []func(*Config)
	mu          sync.RWMutex
	logger      *zap.Logger
}

// NewConfigManager creates a config manager with a specific provider
func NewConfigManager(logger *zap.Logger, provider ConfigProvider) *ConfigManager {
	cm := &ConfigManager{
		logger:   logger,
		provider: provider,
	}

	// Register for config updates from provider
	provider.OnConfigChange(func(cfg *Config) (wait func(), err error) {
		return cm.updateConfig(cfg), nil
	})

	return cm
}

// Subscribe to config changes
func (cm *ConfigManager) Subscribe(callback func(*Config)) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.subscribers = append(cm.subscribers, callback)

	// Immediately call with current config if available
	if cm.config != nil {
		go callback(cm.config)
	}
}

type ConfigSetter interface {
	SetConfig(cfg *Config)
}

func (cm *ConfigManager) SubscribeSetter(setter ConfigSetter) {
	cm.Subscribe(func(cfg *Config) {
		setter.SetConfig(cfg)
	})
}

// GetConfig returns the current configuration
func (cm *ConfigManager) GetConfig() *Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config
}

// updateConfig updates the config and notifies subscribers
func (cm *ConfigManager) updateConfig(cfg *Config) func() {
	cm.mu.Lock()
	cm.config = cfg
	subscribers := make([]func(*Config), len(cm.subscribers))
	copy(subscribers, cm.subscribers)
	cm.mu.Unlock()

	cm.logger.Info("configuration updated, notifying subscribers")

	// Notify subscribers
	var wg sync.WaitGroup
	for _, sub := range subscribers {
		wg.Go(func() {
			sub(cfg)
		})
	}
	return wg.Wait
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
