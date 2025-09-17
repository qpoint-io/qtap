package babel

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
)

// WithMethod sets the HTTP method (GET, POST, PUT, DELETE, etc.)
func (m *HTTPRequest) WithMethod(method string) *HTTPRequest {
	m.Method = method
	return m
}

// WithURL sets the target URL for the HTTP request
func (m *HTTPRequest) WithURL(url string) *HTTPRequest {
	m.URL = url
	return m
}

// WithHeaders sets the HTTP headers
func (m *HTTPRequest) WithHeaders(headers map[string]string) *HTTPRequest {
	m.Headers = headers
	return m
}

// WithHeader adds a single HTTP header
func (m *HTTPRequest) WithHeader(key, value string) *HTTPRequest {
	m.Headers[key] = value
	return m
}

// WithBody sets the request body content
func (m *HTTPRequest) WithBody(body string) *HTTPRequest {
	m.Body = body
	return m
}

// WithBodyFile sets the path to file containing request body
func (m *HTTPRequest) WithBodyFile(bodyFile string) *HTTPRequest {
	m.BodyFile = bodyFile
	return m
}

// WithHTTPVersion sets the HTTP version ("1.0", "1.1", "2")
func (m *HTTPRequest) WithHTTPVersion(version string) *HTTPRequest {
	m.HTTPVersion = version
	return m
}

// WithKeepAlive sets the connection keep-alive setting
func (m *HTTPRequest) WithKeepAlive(keepAlive bool) *HTTPRequest {
	m.KeepAlive = keepAlive
	return m
}

// WithTimeout sets the request timeout
func (m *HTTPRequest) WithTimeout(timeout time.Duration) *HTTPRequest {
	m.Timeout = timeout
	return m
}

// WithRequests sets the number of requests to make sequentially
func (m *HTTPRequest) WithRequests(requests int) *HTTPRequest {
	m.Requests = requests
	return m
}

// WithConcurrentRequests sets the number of concurrent requests
func (m *HTTPRequest) WithConcurrentRequests(concurrent int) *HTTPRequest {
	m.ConcurrentRequests = concurrent
	return m
}

// WithDelayBetweenRequests sets the delay between requests
func (m *HTTPRequest) WithDelayBetweenRequests(delay time.Duration) *HTTPRequest {
	m.DelayBetweenRequests = delay
	return m
}

// WithOutputFormat sets the output format ("json" or other for human-readable)
func (m *HTTPRequest) WithOutputFormat(format string) *HTTPRequest {
	m.OutputFormat = format
	return m
}

// WithVerbose enables verbose logging
func (m *HTTPRequest) WithVerbose(verbose bool) *HTTPRequest {
	m.Verbose = verbose
	return m
}

// WithExtraEnvVars adds additional environment variables
func (m *HTTPRequest) WithExtraEnvVars(vars map[string]string) *HTTPRequest {
	for k, v := range vars {
		m.ExtraEnvVars[k] = v
	}
	return m
}

// WithExtraEnvVar adds a single additional environment variable
func (m *HTTPRequest) WithExtraEnvVar(key, value string) *HTTPRequest {
	m.ExtraEnvVars[key] = value
	return m
}

// toEnvVars converts the module configuration to environment variables
func (m *HTTPRequest) toEnvVars() map[string]string {
	envVars := make(map[string]string)

	// Core configuration
	if m.URL != "" {
		envVars["URL"] = m.URL
	}
	envVars["HTTP_METHOD"] = m.Method

	// Headers - convert map to comma-separated key:value pairs
	if len(m.Headers) > 0 {
		var headerPairs []string
		for key, value := range m.Headers {
			headerPairs = append(headerPairs, fmt.Sprintf("%s:%s", key, value))
		}
		envVars["HTTP_HEADERS"] = strings.Join(headerPairs, ",")
	}

	// Body
	if m.Body != "" {
		envVars["HTTP_BODY"] = m.Body
	}
	if m.BodyFile != "" {
		envVars["HTTP_BODY_FILE"] = m.BodyFile
	}

	// Protocol settings
	envVars["HTTP_VERSION"] = m.HTTPVersion
	if m.KeepAlive {
		envVars["HTTP_KEEP_ALIVE"] = "on"
	} else {
		envVars["HTTP_KEEP_ALIVE"] = "off"
	}
	envVars["HTTP_TIMEOUT"] = strconv.Itoa(int(m.Timeout.Seconds()))

	// Execution control
	envVars["REQUESTS"] = strconv.Itoa(m.Requests)
	envVars["CONCURRENT_REQUESTS"] = strconv.Itoa(m.ConcurrentRequests)
	if m.DelayBetweenRequests > 0 {
		envVars["DELAY_BETWEEN_REQUESTS"] = fmt.Sprintf("%.3f", m.DelayBetweenRequests.Seconds())
	}

	// Output control
	envVars["OUTPUT_FORMAT"] = m.OutputFormat
	if m.Verbose {
		envVars["VERBOSE"] = "true"
	} else {
		envVars["VERBOSE"] = "false"
	}

	// Add any extra environment variables
	for key, value := range m.ExtraEnvVars {
		envVars[key] = value
	}

	return envVars
}

// HTTPRequest represents a generic HTTP client testcontainers module
type HTTPRequest struct {
	// Request configuration
	Method   string
	URL      string
	Headers  map[string]string
	Body     string
	BodyFile string

	// Protocol settings
	HTTPVersion string // "1.0", "1.1", "2"
	KeepAlive   bool
	Timeout     time.Duration

	// Execution control
	Requests             int
	ConcurrentRequests   int
	DelayBetweenRequests time.Duration

	// Output control
	OutputFormat string // "json" or other for human-readable
	Verbose      bool

	// Extension point for any additional env vars
	ExtraEnvVars map[string]string

	// Container configuration
	ImageURL string
}

// NewHTTPRequest creates a new HTTPClientModule with default values
func NewHTTPRequest(image HTTPRequestImage, targetURL string) *HTTPRequest {
	return &HTTPRequest{
		// Default request configuration
		Method:   "GET",
		Headers:  make(map[string]string),
		Body:     "",
		BodyFile: "",

		// Default protocol settings
		HTTPVersion: "1.1",
		KeepAlive:   false,
		Timeout:     10 * time.Second,

		// Default execution control
		Requests:             1,
		ConcurrentRequests:   1,
		DelayBetweenRequests: 0,

		// Default output control
		OutputFormat: "json",
		Verbose:      false,

		// Extension point
		ExtraEnvVars: make(map[string]string),

		// Container configuration
		ImageURL: image.String(),
		URL:      targetURL,
	}
}

func (m *HTTPRequest) Run(ctx context.Context, l *zap.Logger) *ContainerResult {
	result := &ContainerResult{}
	var err error

	envVars := m.toEnvVars()

	// Create container request
	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: m.ImageURL,
			Env:   envVars,
			HostConfigModifier: func(hc *container.HostConfig) {
				hc.NetworkMode = network.NetworkHost
			},
			LogConsumerCfg: &testcontainers.LogConsumerConfig{
				Opts:      []testcontainers.LogProductionOption{testcontainers.WithLogProductionTimeout(10 * time.Second)},
				Consumers: []testcontainers.LogConsumer{result},
			},
			WaitingFor: wait.ForExit().WithPollInterval(10 * time.Millisecond),
			AutoRemove: true, // Clean up container when it exits
		},
		Started: true,
	}

	// Start the container
	l.Info("🕹️ starting container", zap.String("image", m.ImageURL))
	container, err := testcontainers.GenericContainer(ctx, req)
	if err != nil {
		l.Error("start container error", zap.Error(err))
		result.Error = fmt.Errorf("starting container %s: %w", m.ImageURL, err)
		result.ExitCode = -1
		return result
	}

	state, err := container.State(ctx)
	if err != nil {
		l.Error("get container state error", zap.Error(err))
		result.Error = fmt.Errorf("getting container state: %w", err)
		result.ExitCode = -1
		return result
	}

	l.Debug("container state", zap.Any("state", state))
	result.ExitCode = state.ExitCode
	if state.Error != "" {
		result.Error = fmt.Errorf("container error: %s", state.Error)
	}

	return result
}
