package remote

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/qpoint-io/debounce"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/httpclient"
	"go.uber.org/zap"
)

const (
	// ConfigVersion is attached to the config request to indicate
	// the desired version of the config format.
	ConfigVersion = "v2"
)

// ConfigResponse from the remote API
type ConfigResponse struct {
	Config config.Config `json:"config" yaml:"config"`
}

// RemoteConfigProvider watches and loads configuration from a remote API
type RemoteConfigProvider struct {
	apiEndpoint   string
	client        httpclient.HttpClient
	callback      func(*config.Config) (func(), error)
	logger        *zap.Logger
	stopCh        chan struct{}
	watcherStopCh map[string]chan struct{}
	mu            sync.Mutex
}

// RemoteUpdater provides an interface for registering update notifications
type RemoteUpdater interface {
	RegisterForUpdates(channel string, event string, callback func()) (unsubscribe func(), err error)
}

// NewRemoteConfigProvider creates a new provider for remote configuration
func NewRemoteConfigProvider(
	logger *zap.Logger,
	apiEndpoint string,
	client httpclient.HttpClient,
) *RemoteConfigProvider {
	return &RemoteConfigProvider{
		apiEndpoint:   apiEndpoint,
		client:        client,
		logger:        logger,
		stopCh:        make(chan struct{}),
		watcherStopCh: make(map[string]chan struct{}),
	}
}

// Start fetching and watching for config changes
func (p *RemoteConfigProvider) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.callback == nil {
		return errors.New("no callback registered for config changes")
	}

	// Initial config load
	if err := p.fetchAndNotify(); err != nil {
		return fmt.Errorf("initial config fetch failed: %w", err)
	}

	return nil
}

// Stop watching for config changes
func (p *RemoteConfigProvider) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	close(p.stopCh)

	// Close all registered watchers
	for _, ch := range p.watcherStopCh {
		close(ch)
	}
	p.watcherStopCh = make(map[string]chan struct{})
}

// OnConfigChange registers a callback for config changes
func (p *RemoteConfigProvider) OnConfigChange(callback func(*config.Config) (func(), error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callback = callback
}

// Reload forces a configuration reload
func (p *RemoteConfigProvider) Reload() error {
	return p.fetchAndNotify()
}

// RegisterWatcher sets up a watcher for a specific channel and event
func (p *RemoteConfigProvider) RegisterWatcher(
	updater RemoteUpdater,
	channel string,
	event string,
	debounceInterval time.Duration,
	debounceLimit uint64,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if we already have a watcher for this channel
	if _, exists := p.watcherStopCh[channel]; exists {
		return fmt.Errorf("watcher for channel %s already registered", channel)
	}

	// Create debouncer
	debounceFn := debounce.New(debounceInterval, debounceLimit)
	stopCh := make(chan struct{})
	p.watcherStopCh[channel] = stopCh

	// Register update callback
	unsubscribe, err := updater.RegisterForUpdates(channel, event, func() {
		// Use debouncer to prevent update storms
		debounceFn(func() {
			select {
			case <-stopCh:
				return
			default:
				if err := p.fetchAndNotify(); err != nil {
					p.logger.Error("Failed to fetch config after update notification", zap.Error(err))
				}
			}
		})
	})

	if err != nil {
		delete(p.watcherStopCh, channel)
		close(stopCh)
		return fmt.Errorf("failed to register for updates: %w", err)
	}

	// Cleanup on stop
	go func() {
		select {
		case <-p.stopCh:
			unsubscribe()
		case <-stopCh:
			unsubscribe()
		}
	}()

	return nil
}

// fetchAndNotify fetches the config and calls the registered callback
func (p *RemoteConfigProvider) fetchAndNotify() error {
	// Fetch the config from the API
	url := p.apiEndpoint + "/deploy/config?version=" + ConfigVersion

	var payload ConfigResponse
	status, err := p.client.GetYAML(url, &payload)
	if err != nil {
		return fmt.Errorf("fetching config: %w", err)
	}

	// Check if the response was successful
	if status != http.StatusOK {
		return fmt.Errorf("request failed with status \"%s\"", http.StatusText(status))
	}

	if err := payload.Config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	callback := p.callback

	if callback != nil {
		wait, err := callback(&payload.Config)
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
