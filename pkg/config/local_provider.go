package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/qpoint-io/qtap/pkg/services/client"
	"go.uber.org/zap"
)

// LocalConfigProvider loads configuration from a local file or URL and reloads on SIGHUP
type LocalConfigProvider struct {
	logger     *zap.Logger
	configPath string
	callback   func(*Config) (func(), error)
	mu         sync.Mutex
	// URL-specific fields
	isURL     bool
	cacheFile string // temp file for caching downloaded configs
}

// NewLocalConfigProvider creates a new provider for local config files or URLs.
// When a URL is provided (http:// or https://), the config will be downloaded
// and cached locally for SIGHUP reloads. If the download fails during reload,
// it will fall back to the cached version.
func NewLocalConfigProvider(logger *zap.Logger, configPath string) *LocalConfigProvider {
	isURL := isURL(configPath)
	var cacheFile string

	if isURL {
		// Create a temp file for caching downloaded configs
		cacheFile = filepath.Join(os.TempDir(), fmt.Sprintf("qtap-config-%s.yaml", generateCacheKey(configPath)))
		logger.Debug("URL config detected, will cache to temp file",
			zap.String("url", configPath),
			zap.String("cache_file", cacheFile))
	}

	return &LocalConfigProvider{
		logger:     logger,
		configPath: configPath,
		isURL:      isURL,
		cacheFile:  cacheFile,
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

	return nil
}

// Stop watching for config changes
func (p *LocalConfigProvider) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Clean up cache file if it exists
	if p.isURL && p.cacheFile != "" {
		if err := os.Remove(p.cacheFile); err != nil && !os.IsNotExist(err) {
			p.logger.Warn("Failed to remove cache file",
				zap.String("cache_file", p.cacheFile),
				zap.Error(err))
		}
	}
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

// loadAndNotify loads the config and calls the registered callback
func (p *LocalConfigProvider) loadAndNotify() error {
	var data []byte
	var err error

	if p.isURL {
		// Download config from URL and cache it
		data, err = p.downloadAndCache()
		if err != nil {
			// Try to fall back to cached file if download fails
			if p.cacheFile != "" {
				p.logger.Warn("Failed to download config, trying cached version", zap.Error(err))
				data, err = os.ReadFile(p.cacheFile)
				if err != nil {
					return fmt.Errorf("downloading config and reading cached file failed: %w", err)
				}
			} else {
				return fmt.Errorf("downloading config: %w", err)
			}
		}
	} else {
		// Read from local file
		data, err = os.ReadFile(p.configPath)
		if err != nil {
			return fmt.Errorf("reading config file: %w", err)
		}
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
		if wait != nil {
			wait()
		}
		p.logger.Info("config load completed")
	}

	return nil
}

// isURL checks if the given path is a URL (starts with http:// or https://)
func isURL(path string) bool {
	u, err := url.Parse(strings.TrimSpace(path))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

// generateCacheKey creates a safe filename from a URL
func generateCacheKey(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		// Fallback to a simple hash-like approach
		return hex.EncodeToString([]byte(urlStr))
	}

	// Create a safe filename from host and path
	safeHost := strings.ReplaceAll(u.Host, ":", "-")
	safePath := strings.ReplaceAll(strings.ReplaceAll(u.Path, "/", "-"), ".", "-")
	// Handle empty path or root path
	switch u.Path {
	case "":
		safePath = "index"
	case "/":
		safePath = "-"
	default:
		if safePath == "" || safePath == "-" {
			safePath = "index"
		}
	}

	return fmt.Sprintf("%s%s", safeHost, safePath)
}

// downloadAndCache downloads the config from URL and caches it to a temp file
func (p *LocalConfigProvider) downloadAndCache() ([]byte, error) {
	p.logger.Info("Downloading config from URL", zap.String("url", p.configPath))

	// Create HTTP client
	httpClient := client.NewHttpClient()

	// Make the request
	resp, err := httpClient.Get(p.configPath)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, resp.Status)
	}

	// Read response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	// Cache the downloaded config to temp file for SIGHUP reloads
	if p.cacheFile != "" {
		// Ensure the directory exists
		if err := os.MkdirAll(filepath.Dir(p.cacheFile), 0755); err != nil {
			p.logger.Warn("Failed to create cache directory", zap.Error(err))
		} else {
			if err := os.WriteFile(p.cacheFile, data, 0644); err != nil {
				p.logger.Warn("Failed to cache config file", zap.Error(err))
			} else {
				p.logger.Debug("Config cached successfully", zap.String("cache_file", p.cacheFile))
			}
		}
	}

	p.logger.Info("Config downloaded successfully",
		zap.String("url", p.configPath),
		zap.Int("size_bytes", len(data)))

	return data, nil
}
