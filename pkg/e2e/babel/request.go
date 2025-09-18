package babel

import (
	"context"
	"errors"
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
func (m *HTTPRequestBuilder) WithMethod(method string) *HTTPRequestBuilder {
	m.req.Method = method
	return m
}

// WithURL sets the target URL for the HTTP request
func (m *HTTPRequestBuilder) WithURL(url string) *HTTPRequestBuilder {
	m.req.URL = url
	return m
}

// WithImageURL sets the image URL for the HTTP request
func (m *HTTPRequestBuilder) WithImageURL(imageURL string) *HTTPRequestBuilder {
	m.req.ImageURL = imageURL
	return m
}

// WithHeaders sets the HTTP headers
func (m *HTTPRequestBuilder) WithHeaders(headers map[string]string) *HTTPRequestBuilder {
	m.req.Headers = headers
	return m
}

// WithHeader adds a single HTTP header
func (m *HTTPRequestBuilder) WithHeader(key, value string) *HTTPRequestBuilder {
	m.req.Headers[key] = value
	return m
}

// WithBody sets the request body content
func (m *HTTPRequestBuilder) WithBody(body string) *HTTPRequestBuilder {
	m.req.Body = body
	return m
}

// WithBodyFile sets the path to file containing request body
func (m *HTTPRequestBuilder) WithBodyFile(bodyFile string) *HTTPRequestBuilder {
	m.req.BodyFile = bodyFile
	return m
}

// WithHTTPVersion sets the HTTP version ("1.0", "1.1", "2")
func (m *HTTPRequestBuilder) WithHTTPVersion(version string) *HTTPRequestBuilder {
	m.req.HTTPVersion = version
	return m
}

// WithKeepAlive sets the connection keep-alive setting
func (m *HTTPRequestBuilder) WithKeepAlive(keepAlive bool) *HTTPRequestBuilder {
	m.req.KeepAlive = keepAlive
	return m
}

// WithTimeout sets the request timeout
func (m *HTTPRequestBuilder) WithTimeout(timeout time.Duration) *HTTPRequestBuilder {
	m.req.Timeout = timeout
	return m
}

// WithClient sets the HTTP client
func (m *HTTPRequestBuilder) WithClient(client string) *HTTPRequestBuilder {
	m.req.Client = client
	return m
}

// WithRequests sets the number of requests to make sequentially
func (m *HTTPRequestBuilder) WithRequests(requests int) *HTTPRequestBuilder {
	m.req.Requests = requests
	return m
}

// WithConcurrentRequests sets the number of concurrent requests
func (m *HTTPRequestBuilder) WithConcurrentRequests(concurrent int) *HTTPRequestBuilder {
	m.req.ConcurrentRequests = concurrent
	return m
}

// WithDelayBetweenRequests sets the delay between requests
func (m *HTTPRequestBuilder) WithDelayBetweenRequests(delay time.Duration) *HTTPRequestBuilder {
	m.req.DelayBetweenRequests = delay
	return m
}

// WithStartupDelay sets the startup delay
func (m *HTTPRequestBuilder) WithStartupDelay(delay time.Duration) *HTTPRequestBuilder {
	m.req.StartupDelay = delay
	return m
}

// WithOutputFormat sets the output format ("json" or other for human-readable)
func (m *HTTPRequestBuilder) WithOutputFormat(format string) *HTTPRequestBuilder {
	m.req.OutputFormat = format
	return m
}

// WithVerbose enables verbose logging
func (m *HTTPRequestBuilder) WithVerbose() *HTTPRequestBuilder {
	m.req.Verbose = true
	return m
}

// WithExtraEnvVars adds additional environment variables
func (m *HTTPRequestBuilder) WithExtraEnvVars(vars map[string]string) *HTTPRequestBuilder {
	m.req.WithExtraEnvVars(vars)
	return m
}

// WithExtraEnvVar adds a single additional environment variable
func (m *HTTPRequestBuilder) WithExtraEnvVar(key, value string) *HTTPRequestBuilder {
	m.req.WithExtraEnvVar(key, value)
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

	if m.Client != "" {
		envVars["HTTP_CLIENT"] = m.Client
	}

	// Execution control
	envVars["REQUESTS"] = strconv.Itoa(m.Requests)
	envVars["CONCURRENT_REQUESTS"] = strconv.Itoa(m.ConcurrentRequests)
	if m.DelayBetweenRequests > 0 {
		envVars["DELAY_BETWEEN_REQUESTS"] = fmt.Sprintf("%.3f", m.DelayBetweenRequests.Seconds())
	}
	if m.StartupDelay > 0 {
		envVars["STARTUP_DELAY"] = strconv.FormatInt(m.StartupDelay.Milliseconds(), 10)
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
	Client      string

	// Execution control
	Requests             int
	ConcurrentRequests   int
	DelayBetweenRequests time.Duration
	StartupDelay         time.Duration // Milliseconds

	// Output control
	OutputFormat string // "json" or other for human-readable
	Verbose      bool

	// Extension point for any additional env vars
	ExtraEnvVars map[string]string

	// Container configuration
	ImageURL string
}

type HTTPRequestBuilder struct {
	req *HTTPRequest
}

func (m *HTTPRequestBuilder) Build() (*HTTPRequest, error) {
	if m.req.URL == "" {
		return nil, errors.New("URL is required")
	}
	if m.req.ImageURL == "" {
		return nil, errors.New("ImageURL is required")
	}

	// Create a deep copy of the request to allow multiple builds
	reqCopy := &HTTPRequest{
		Method:   m.req.Method,
		URL:      m.req.URL,
		Headers:  make(map[string]string),
		Body:     m.req.Body,
		BodyFile: m.req.BodyFile,

		HTTPVersion: m.req.HTTPVersion,
		KeepAlive:   m.req.KeepAlive,
		Timeout:     m.req.Timeout,

		Requests:             m.req.Requests,
		ConcurrentRequests:   m.req.ConcurrentRequests,
		DelayBetweenRequests: m.req.DelayBetweenRequests,
		StartupDelay:         m.req.StartupDelay,

		OutputFormat: m.req.OutputFormat,
		Verbose:      m.req.Verbose,

		ExtraEnvVars: make(map[string]string),
		ImageURL:     m.req.ImageURL,
	}

	// Deep copy the headers map
	for k, v := range m.req.Headers {
		reqCopy.Headers[k] = v
	}

	// Deep copy the extra env vars map
	for k, v := range m.req.ExtraEnvVars {
		reqCopy.ExtraEnvVars[k] = v
	}

	return reqCopy, nil
}

// BuildHTTPRequest creates a new HTTPRequestBuilder with default values
func BuildHTTPRequest() *HTTPRequestBuilder {
	return &HTTPRequestBuilder{
		req: &HTTPRequest{
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
		},
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

	// TODO(Jon): test this
	// if m.BodyFile != "" {
	// 	req.Files = []testcontainers.ContainerFile{
	// 		{
	// 			HostFilePath:      m.BodyFile,
	// 			ContainerFilePath: m.BodyFile,
	// 		},
	// 	}
	// }

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
