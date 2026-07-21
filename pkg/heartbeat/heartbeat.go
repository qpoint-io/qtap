// Package heartbeat posts periodic liveness pings to a managed control-plane
// endpoint. It consolidates the previously separate "firebase" (/deploy/ping)
// and "pulse" (/api/v1/heartbeat) heartbeats, which differed only in their
// request path, JSON payload shape, and expected success status code.
package heartbeat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
)

const defaultRequestTimeout = 10 * time.Second

type Status string

var (
	StatusStarting Status = "starting"
	StatusReady    Status = "ready"
	StatusStopped  Status = "stopped"
)

// Variant captures the only differences between the two heartbeat flavors.
type Variant struct {
	// Path is appended to the endpoint to form the request URL.
	Path string
	// OKStatus is the HTTP status code that indicates a successful ping.
	OKStatus int
	// Build produces the JSON request body for a given status.
	Build func(hostname, instanceID string, status Status, sysInfo map[string]map[string]string) any
}

// Deploy is the registration/deploy API heartbeat (api.qpoint.io/deploy/ping).
var Deploy = Variant{
	Path:     "/deploy/ping",
	OKStatus: http.StatusOK,
	Build: func(hostname, instanceID string, status Status, sysInfo map[string]map[string]string) any {
		return struct {
			Name       string                       `json:"name"`
			Status     string                       `json:"status"`
			InstanceID string                       `json:"instanceId"`
			SysInfo    map[string]map[string]string `json:"sysInfo"`
		}{hostname, string(status), instanceID, sysInfo}
	},
}

// Pulse is the pulse API heartbeat (api-pulse.qpoint.io/api/v1/heartbeat).
var Pulse = Variant{
	Path:     "/api/v1/heartbeat",
	OKStatus: http.StatusCreated,
	Build: func(hostname, instanceID string, status Status, sysInfo map[string]map[string]string) any {
		return struct {
			Hostname      string `json:"hostname"`
			Status        string `json:"status"`
			InstanceId    string `json:"instanceId"`
			KernelRelease string `json:"kernelRelease"`
			Architecture  string `json:"architecture"`
		}{hostname, string(status), instanceID, sysInfo["kernel"]["release"], sysInfo["system"]["architecture"]}
	},
}

type Heartbeat struct {
	logger         *zap.Logger
	client         *http.Client
	url            string
	token          string
	hostname       string
	instanceID     string
	tickDuration   time.Duration
	requestTimeout time.Duration
	variant        Variant

	mu        sync.Mutex
	lifecycle *lifecycle
}

func New(logger *zap.Logger, endpoint, token string, dur time.Duration, variant Variant) (*Heartbeat, error) {
	return NewWithClient(logger, endpoint, token, dur, variant, &http.Client{Timeout: defaultRequestTimeout})
}

// NewWithClient constructs a heartbeat using client. A copy of client is used,
// and a finite timeout is applied if it does not already have one.
func NewWithClient(logger *zap.Logger, endpoint, token string, dur time.Duration, variant Variant, client *http.Client) (*Heartbeat, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("heartbeat token is required")
	}
	if dur <= 0 {
		return nil, errors.New("heartbeat duration must be positive")
	}
	if client == nil {
		return nil, errors.New("heartbeat HTTP client is required")
	}

	u, err := url.JoinPath(endpoint, variant.Path)
	if err != nil {
		return nil, err
	}
	clientCopy := *client
	if clientCopy.Timeout <= 0 {
		clientCopy.Timeout = defaultRequestTimeout
	}

	return &Heartbeat{
		logger:         logger,
		client:         &clientCopy,
		url:            u,
		token:          token,
		hostname:       telemetry.Hostname(),
		instanceID:     telemetry.InstanceID(),
		tickDuration:   dur,
		requestTimeout: clientCopy.Timeout,
		variant:        variant,
	}, nil
}

type lifecycle struct {
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	startDone chan struct{}
	startErr  error
	stopDone  chan struct{}
	stopErr   error
	stopping  bool
}

// Start sends the initial heartbeat and starts periodic heartbeats. Repeated
// calls return the result of the first call without starting another worker.
func (c *Heartbeat) Start() error {
	c.mu.Lock()
	if c.lifecycle != nil {
		lifecycle := c.lifecycle
		c.mu.Unlock()
		<-lifecycle.startDone
		c.mu.Lock()
		err := lifecycle.startErr
		c.mu.Unlock()
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	lifecycle := &lifecycle{
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
		startDone: make(chan struct{}),
		stopDone:  make(chan struct{}),
	}
	c.lifecycle = lifecycle
	c.mu.Unlock()

	c.logger.Info("starting heartbeat",
		zap.String("endpoint", c.url),
		zap.String("hostname", c.hostname),
		zap.String("instanceId", c.instanceID),
		zap.String("duration", c.tickDuration.String()))

	err := c.send(lifecycle.ctx, StatusStarting)
	c.mu.Lock()
	lifecycle.startErr = err
	close(lifecycle.startDone)
	c.mu.Unlock()
	if err != nil {
		lifecycle.cancel()
		close(lifecycle.done)
		return err
	}

	go c.run(lifecycle)
	return nil
}

func (c *Heartbeat) run(lifecycle *lifecycle) {
	ticker := time.NewTicker(c.tickDuration)
	defer ticker.Stop()
	defer close(lifecycle.done)

	for {
		select {
		case <-ticker.C:
			if err := c.send(lifecycle.ctx, StatusReady); err != nil && lifecycle.ctx.Err() == nil {
				c.logger.Error("sending heartbeat", zap.Error(err))
			}
		case <-lifecycle.ctx.Done():
			return
		}
	}
}

// Stop retains the original no-error API and bounds shutdown with the default
// request timeout. Call StopContext when the shutdown error must be observed.
func (c *Heartbeat) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	defer cancel()
	if err := c.StopContext(ctx); err != nil {
		c.logger.Error("stopping heartbeat", zap.Error(err))
	}
}

// StopContext stops and joins the periodic worker, then sends one final
// heartbeat. Repeated calls wait for and return the first stop result.
func (c *Heartbeat) StopContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("stop context is required")
	}

	c.mu.Lock()
	lifecycle := c.lifecycle
	if lifecycle == nil {
		c.mu.Unlock()
		return nil
	}
	if !lifecycle.stopping {
		lifecycle.stopping = true
		go c.stopLifecycle(ctx, lifecycle)
	}
	stopDone := lifecycle.stopDone
	c.mu.Unlock()

	select {
	case <-stopDone:
		c.mu.Lock()
		err := lifecycle.stopErr
		c.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Heartbeat) stopLifecycle(ctx context.Context, lifecycle *lifecycle) {
	c.logger.Info("stopping heartbeat",
		zap.String("endpoint", c.url),
		zap.String("hostname", c.hostname),
		zap.String("instanceId", c.instanceID),
		zap.String("duration", c.tickDuration.String()))

	lifecycle.cancel()
	<-lifecycle.done
	err := c.send(ctx, StatusStopped)

	c.mu.Lock()
	lifecycle.stopErr = err
	close(lifecycle.stopDone)
	c.mu.Unlock()
}

func (c *Heartbeat) send(ctx context.Context, status Status) error {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	sysInfo, err := telemetry.GetSysInfo()
	if err != nil {
		return fmt.Errorf("get system information: %w", err)
	}

	payload := c.variant.Build(c.hostname, c.instanceID, status, sysInfo)
	c.logger.Debug("sending heartbeat", zap.Any("payload", payload))

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal heartbeat: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.url, bytes.NewReader(jsonPayload))
	if err != nil {
		return fmt.Errorf("create heartbeat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("send heartbeat request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != c.variant.OKStatus {
		return fmt.Errorf("heartbeat response status %s, want %d", resp.Status, c.variant.OKStatus)
	}
	return nil
}
