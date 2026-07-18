// Package heartbeat posts periodic liveness pings to a managed control-plane
// endpoint. It consolidates the previously separate "firebase" (/deploy/ping)
// and "pulse" (/api/v1/heartbeat) heartbeats, which differed only in their
// request path, JSON payload shape, and expected success status code.
package heartbeat

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
)

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
	logger       *zap.Logger
	url          string
	token        string
	hostname     string
	instanceID   string
	tickDuration time.Duration
	variant      Variant
	stopChan     chan struct{}
}

func New(logger *zap.Logger, endpoint, token string, dur time.Duration, variant Variant) (*Heartbeat, error) {
	u, err := url.JoinPath(endpoint, variant.Path)
	if err != nil {
		return nil, err
	}

	return &Heartbeat{
		logger:       logger,
		url:          u,
		token:        token,
		hostname:     telemetry.Hostname(),
		instanceID:   telemetry.InstanceID(),
		tickDuration: dur,
		variant:      variant,
	}, nil
}

func (c *Heartbeat) Start() {
	c.logger.Info("starting heartbeat",
		zap.String("endpoint", c.url),
		zap.String("hostname", c.hostname),
		zap.String("instanceId", c.instanceID),
		zap.String("duration", c.tickDuration.String()))

	c.send(StatusStarting)
	c.stopChan = make(chan struct{})

	go func() {
		ticker := time.NewTicker(c.tickDuration)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				c.send(StatusReady)
			case <-c.stopChan:
				return
			}
		}
	}()
}

func (c *Heartbeat) Stop() {
	c.logger.Info("stopping heartbeat",
		zap.String("endpoint", c.url),
		zap.String("hostname", c.hostname),
		zap.String("instanceId", c.instanceID),
		zap.String("duration", c.tickDuration.String()))

	c.send(StatusStopped)
	close(c.stopChan)
}

func (c *Heartbeat) send(status Status) {
	sysInfo, err := telemetry.GetSysInfo()
	if err != nil {
		c.logger.Error("getting system info", zap.Error(err))
		return
	}

	payload := c.variant.Build(c.hostname, c.instanceID, status, sysInfo)
	c.logger.Debug("sending heartbeat", zap.Any("payload", payload))

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		c.logger.Error("marshalling JSON", zap.Error(err))
		return
	}

	req, err := http.NewRequest(http.MethodPatch, c.url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		c.logger.Error("creating request", zap.Error(err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.logger.Error("making request", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != c.variant.OKStatus {
		c.logger.Error("non-OK HTTP", zap.String("status", resp.Status))
	}
}
