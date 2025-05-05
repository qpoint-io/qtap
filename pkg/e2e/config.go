package e2e

import (
	"github.com/qpoint-io/qtap/pkg/config"
)

// ConfigProvider loads configuration from an in-memory object
type ConfigProvider struct {
	cfg      *config.Config
	callback func(*config.Config) (func(), error)
}

// NewDefaultConfigProvider creates a new provider for default config
func NewConfigProvider(cfg *config.Config) *ConfigProvider {
	return &ConfigProvider{
		cfg:      cfg,
		callback: func(cfg *config.Config) (func(), error) { return nil, nil },
	}
}

// Start watching for config changes via SIGHUP
func (p *ConfigProvider) Start() error {
	_, err := p.callback(p.cfg)
	return err
}

// Stop watching for config changes
func (p *ConfigProvider) Stop() {}

// SetConfig sets the config
func (p *ConfigProvider) SetConfig(cfg *config.Config) (func(), error) {
	p.cfg = cfg
	return p.callback(p.cfg)
}

// OnConfigChange registers a callback for config changes
func (p *ConfigProvider) OnConfigChange(callback func(*config.Config) (func(), error)) {
	p.callback = callback
}

// Reload forces a configuration reload
func (p *ConfigProvider) Reload() error {
	_, err := p.callback(p.cfg)
	return err
}
