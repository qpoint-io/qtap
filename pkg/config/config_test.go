package config

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	validator "github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestValidConfig tests the unmarshaling and validation of a valid YAML configuration
func TestValidConfig(t *testing.T) {
	yamlBlob := `
services:
  event_store:
    type: stdout
  object_store:
    type: stdout
tap:
  audit_logs:
    type: disabled
  default_stack: complete
proxy:
  tcp_listen_list: "0.0.0.0:10080:80,0.0.0.0:10443:443"
  dns_lookup_family: "ALL"
  default_domain_action: "ALLOW"
  default_ip_address_action: "ALLOW"
  endpoints:
  - domain: "example.com"
    action: "ALLOW"
`

	config, err := UnmarshalConfig([]byte(yamlBlob))
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	err = config.Validate()
	if err != nil {
		t.Errorf("Validation failed: %v", err)
	}
}

// TestInvalidConfig tests the unmarshaling and validation of an invalid YAML configuration
// func TestInvalidConfig(t *testing.T) {
// 	yamlBlob := `
// proxy:
//   tcp_listen_list: "0.0.0.0:10080:80,0.0.0.0:10443:443"
//   dns_lookup_family: "ALL"
//   default_domain_action: "ALLOW"
//   default_ip_address_action: "ALLOW"
//   endpoints:
//     - domain: "example.com"
//       action: "DENY"
// `

// 	config, err := UnmarshalConfig([]byte(yamlBlob))
// 	if err != nil {
// 		t.Fatalf("Failed to unmarshal: %v", err)
// 	}

// 	err = config.Validate()
// 	if err == nil {
// 		t.Errorf("Expected validation to fail, but it passed")
// 	}
// }

// TestValidConfig tests the unmarshaling and validation of a valid YAML configuration
func TestValidConfigStacks(t *testing.T) {
	yamlBlob := `
services:
  event_store:
    type: stdout
  object_store:
    type: stdout
stacks:
  default:
    middlewares:
      - name: report
        config: "{<JSON>}"
        wasm: "https://some-location-to.wasm"
  deep-scan:
    middlewares:
      - name: report
        config: "{<JSON>}"
        wasm: "https://some-location-to.wasm"
      - name: scan
        config: "{<JSON>}"
        wasm: "https://some-location-to.wasm"
tap:
  audit_logs:
    type: disabled
  default_stack: complete
proxy:
  tcp_listen_list: "0.0.0.0:10080:80,0.0.0.0:10443:443"
  dns_lookup_family: "ALL"
  default_domain_action: "ALLOW"
  default_ip_address_action: "ALLOW"
  endpoints:
    - domain: "example.com"
      action: "ALLOW"
`

	config, err := UnmarshalConfig([]byte(yamlBlob))
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	err = config.Validate()
	if err != nil {
		t.Errorf("Validation failed: %v", err)
	}
}

func TestValidate(t *testing.T) {
	tcs := []struct {
		name   string
		config *Config
		errors []string
		test   func(*testing.T, *Config, validator.ValidationErrors) // use validatorErrors if needed
	}{
		{
			name: "rulekit valid macros",
			config: &Config{
				Rulekit: &Rulekit{
					Macros: []*RulekitMacro{{
						Name: "test",
						Expr: "src.ip == 192.168.1.1",
					}},
				},
			},
		},
		{
			name: "rulekit macro invalid syntax",
			config: &Config{
				Rulekit: &Rulekit{
					Macros: []*RulekitMacro{{
						Name: "test",
						Expr: "+!&3",
					}},
				},
			},
			errors: []string{
				`parsing rulekit macros: "test": syntax error at line 1:1:
+!&3
^
syntax error: unexpected symbol`,
			},
		},
		{
			name: "rulekit macro name conflict with function",
			config: &Config{
				Rulekit: &Rulekit{
					Macros: []*RulekitMacro{{
						Name: "zone",
						Expr: "src.ip == 192.168.1.1",
					}},
				},
			},
			errors: []string{
				`validating rulekit macros: macro "zone": name conflicts with a custom function`,
			},
		},
		{
			name: "rulekit duplicate macros",
			config: &Config{
				Rulekit: &Rulekit{
					Macros: []*RulekitMacro{
						{
							Name: "zone",
							Expr: "src.ip == 192.168.1.1",
						},
						{
							Name: "zone",
							Expr: "src.ip == duplicate",
						},
					},
				},
			},
			errors: []string{
				`parsing rulekit macros: "zone": name must be unique`,
			},
		},
		{
			name: "object store event store selector - valid",
			config: &Config{
				Services: Services{
					EventStores: []ServiceEventStore{{
						ID:   "test",
						Type: EventStoreType_CONSOLE,
					}},
					ObjectStores: []ServiceObjectStore{{
						ObjectStoreConfig: ObjectStoreConfig{
							EventStore: EventStoreSelector{ID: "test"},
						},
					}},
				},
			},
			errors: []string{},
		},
		{
			name: "object store event store selector - inexistent",
			config: &Config{
				Services: Services{
					ObjectStores: []ServiceObjectStore{{
						ObjectStoreConfig: ObjectStoreConfig{
							EventStore: EventStoreSelector{ID: "test"},
						},
					}},
				},
			},
			errors: []string{
				"object store 0: event store id=\"test\" does not exist",
			},
		},
		{
			name: "object store event store selector - disabled and ID",
			config: &Config{
				Services: Services{
					ObjectStores: []ServiceObjectStore{{
						ObjectStoreConfig: ObjectStoreConfig{
							EventStore: EventStoreSelector{
								Disabled: true,
								ID:       "test",
							},
						},
					}},
				},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if len(tc.errors) == 0 {
				require.NoError(t, err)
				if tc.test != nil {
					tc.test(t, tc.config, nil)
				}
				return
			}

			var verrs validator.ValidationErrors
			if errors.As(err, &verrs) {
				require.ElementsMatch(t, tc.errors, validatorErrors(verrs))
				if tc.test != nil {
					tc.test(t, tc.config, verrs)
				}
			} else {
				require.Len(t, tc.errors, 1, "got non-validator.ValidationErrors, test case errors must have exactly one element")
				require.EqualError(t, err, tc.errors[0])
			}
		})
	}
}

func validatorErrors(errs validator.ValidationErrors) []string {
	errors := []string{}
	for _, err := range errs {
		errors = append(errors, err.Error())
	}
	return errors
}

// Tests for LocalConfigProvider URL functionality

func TestIsURL(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"http URL", "http://example.com/config.yaml", true},
		{"https URL", "https://example.com/config.yaml", true},
		{"http URL case insensitive", "HTTP://example.com/config.yaml", true},
		{"https URL case insensitive", "HTTPS://example.com/config.yaml", true},
		{"local file", "/path/to/config.yaml", false},
		{"relative file", "config.yaml", false},
		{"ftp URL", "ftp://example.com/config.yaml", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isURL(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"simple URL", "https://example.com/config.yaml", "example.com-config-yaml"},
		{"URL with port", "https://example.com:8080/config.yaml", "example.com-8080-config-yaml"},
		{"URL with path", "https://example.com/api/v1/config.yaml", "example.com-api-v1-config-yaml"},
		{"URL with no path", "https://example.com", "example.comindex"},
		{"URL with root path", "https://example.com/", "example.com-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateCacheKey(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLocalConfigProviderWithURL(t *testing.T) {
	// Create a test HTTP server that serves a config
	testConfig := `
services:
  event_stores:
  - type: stdout
  object_stores:
  - type: stdout
tap:
  default_stack: complete
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(testConfig)); err != nil {
			t.Error("failed to write response:", err)
		}
	}))
	defer server.Close()

	// Create a logger for testing
	logger := zap.NewNop()

	// Create the provider with the test server URL
	provider := NewLocalConfigProvider(logger, server.URL)

	// Verify it detected it as a URL
	assert.True(t, provider.isURL)
	assert.NotEmpty(t, provider.cacheFile)

	// Set up a callback to capture the loaded config
	var loadedConfig *Config
	provider.OnConfigChange(func(cfg *Config) (func(), error) {
		loadedConfig = cfg
		return func() {}, nil
	})

	// Start the provider (this should download and load the config)
	err := provider.Start()
	require.NoError(t, err)

	// Verify the config was loaded correctly
	assert.NotNil(t, loadedConfig)
	assert.NotNil(t, loadedConfig.Services)
	assert.Len(t, loadedConfig.Services.EventStores, 1)
	assert.Equal(t, EventStoreType_CONSOLE, loadedConfig.Services.EventStores[0].Type)

	// Verify the cache file was created
	_, err = os.Stat(provider.cacheFile)
	require.NoError(t, err)

	// Test reload functionality
	err = provider.Reload()
	require.NoError(t, err)

	// Clean up
	provider.Stop()

	// Verify the cache file was cleaned up
	_, err = os.Stat(provider.cacheFile)
	require.True(t, os.IsNotExist(err))
}

func TestLocalConfigProviderURLFallbackToCache(t *testing.T) {
	testConfig := `
services:
  event_stores:
  - type: stdout
  object_stores:
  - type: stdout
tap:
  default_stack: complete
`

	// Create a server that initially works, then fails
	var serverResponds bool = true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serverResponds {
			w.Header().Set("Content-Type", "application/yaml")
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte(testConfig)); err != nil {
				t.Error("failed to write response:", err)
			}
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			if _, err := w.Write([]byte("Server error")); err != nil {
				t.Error("failed to write error response:", err)
			}
		}
	}))
	defer server.Close()

	logger := zap.NewNop()
	provider := NewLocalConfigProvider(logger, server.URL)

	var loadedConfig *Config
	provider.OnConfigChange(func(cfg *Config) (func(), error) {
		loadedConfig = cfg
		return func() {}, nil
	})

	// First load should succeed and create cache
	err := provider.Start()
	require.NoError(t, err)
	assert.NotNil(t, loadedConfig)

	// Verify cache file exists
	_, err = os.Stat(provider.cacheFile)
	require.NoError(t, err)

	// Now make the server fail
	serverResponds = false

	// Reload should fall back to cache
	err = provider.Reload()
	require.NoError(t, err) // Should succeed using cached config

	// Verify config is still available
	assert.NotNil(t, loadedConfig)
	assert.Equal(t, EventStoreType_CONSOLE, loadedConfig.Services.EventStores[0].Type)

	provider.Stop()
}

func TestServiceConfigs(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected []*ServiceConfig
	}{
		{
			name: "no services",
			config: &Config{
				Services: Services{},
			},
			expected: []*ServiceConfig{
				{
					Type:    "eventstore.noop",
					Default: true,
					Config: ServiceEventStore{
						Type: EventStoreType_DISABLED,
					},
				},
				{
					Type:    "objectstore.noop",
					Default: true,
					Config: ServiceObjectStore{
						Type: ObjectStoreType_DISABLED,
					},
				},
			},
		},
		{
			name: "happy path",
			config: &Config{
				Services: Services{
					EventStores: []ServiceEventStore{
						{
							ID:   "pulse",
							Type: EventStoreType_PULSE,
						},
						// no id
						{
							Type: EventStoreType_AXIOM,
						},
					},
					ObjectStores: []ServiceObjectStore{
						{
							ID:   "qpoint",
							Type: ObjectStoreType_QPOINT,
						},
						// no id
						{
							Type: ObjectStoreType_S3,
						},
					},
				},
			},
			expected: []*ServiceConfig{
				// event stores
				{
					Type:    "eventstore.eventstorev1_nonstreaming",
					ID:      "pulse",
					Default: true,
					Config: ServiceEventStore{
						ID:   "pulse",
						Type: EventStoreType_PULSE,
					},
				},
				{
					Type: "eventstore.axiom",
					Config: ServiceEventStore{
						Type: EventStoreType_AXIOM,
					},
				},
				// object stores
				{
					ID:      "qpoint",
					Type:    "objectstore.warehouse",
					Default: true,
					Config: ServiceObjectStore{
						ID:   "qpoint",
						Type: ObjectStoreType_QPOINT,
					},
				},
				{
					Type: "objectstore.s3",
					Config: ServiceObjectStore{
						Type: ObjectStoreType_S3,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.Services.All()
			assert.Equal(t, tt.expected, result)
		})
	}
}
