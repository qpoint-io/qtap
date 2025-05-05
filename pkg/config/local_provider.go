package config

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"go.uber.org/zap"
)

// LocalConfigProvider loads configuration from a local file and reloads on SIGHUP
type LocalConfigProvider struct {
	logger     *zap.Logger
	configPath string
	callback   func(*Config) (func(), error)
	sigChan    chan os.Signal
	done       chan struct{}
	mu         sync.Mutex
}

// NewLocalConfigProvider creates a new provider for local config files
func NewLocalConfigProvider(logger *zap.Logger, configPath string) *LocalConfigProvider {
	return &LocalConfigProvider{
		logger:     logger,
		configPath: configPath,
		done:       make(chan struct{}),
	}
}

// Start watching for config changes via SIGHUP
func (p *LocalConfigProvider) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.callback == nil {
		return errors.New("no callback registered for config changes")
	}

	// Initial config load
	if err := p.loadAndNotify(); err != nil {
		return fmt.Errorf("initial config load failed: %w", err)
	}

	// Set up signal handler for SIGHUP
	p.sigChan = make(chan os.Signal, 1)
	signal.Notify(p.sigChan, syscall.SIGHUP)

	// Start watching for SIGHUP
	go p.watchSignals()

	return nil
}

// Stop watching for config changes
func (p *LocalConfigProvider) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.sigChan == nil {
		return
	}

	signal.Stop(p.sigChan)
	close(p.done)
	close(p.sigChan)
	p.sigChan = nil
}

// OnConfigChange registers a callback for config changes
func (p *LocalConfigProvider) OnConfigChange(callback func(*Config) (func(), error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callback = callback
}

// Reload forces a configuration reload
func (p *LocalConfigProvider) Reload() error {
	return p.loadAndNotify()
}

// watchSignals monitors for SIGHUP signals to reload config
func (p *LocalConfigProvider) watchSignals() {
	for {
		select {
		case <-p.sigChan:
			p.logger.Info("SIGHUP received, reloading configuration")
			if err := p.loadAndNotify(); err != nil {
				p.logger.Error("Failed to reload config after SIGHUP", zap.Error(err))
			}
		case <-p.done:
			return
		}
	}
}

// loadAndNotify loads the config and calls the registered callback
func (p *LocalConfigProvider) loadAndNotify() error {
	data, err := os.ReadFile(p.configPath)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	conf, err := UnmarshalConfig(data)
	if err != nil {
		return fmt.Errorf("unmarshalling config: %w", err)
	}

	if err := conf.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	callback := p.callback

	if callback != nil {
		wait, err := callback(conf)
		if err != nil {
			return fmt.Errorf("config callback failed: %w", err)
		}
		go func() {
			wait()
			p.logger.Info("config load completed")
		}()
	}

	return nil
}
