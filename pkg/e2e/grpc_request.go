package e2e

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"go.uber.org/zap"
)

// GRPCRequest represents a gRPC client testcontainers module
type GRPCRequest struct {
	Server             string
	Service            string
	Method             string
	Message            string
	Metadata           map[string]string
	RPCType            string // unary, server-streaming, client-streaming, bidi-streaming
	TLS                bool
	InsecureSkipVerify bool
	ServerCert         string
	Timeout            time.Duration
	OutputFormat       string
	Verbose            bool
	ExtraEnvVars       map[string]string
	ImageURL           string

	// Readiness handshake
	ReadinessFile    string
	ReadinessTimeout time.Duration
}

type GRPCRequestBuilder struct {
	req *GRPCRequest
}

// BuildGRPCRequest creates a new GRPCRequestBuilder with default values
func BuildGRPCRequest() *GRPCRequestBuilder {
	return &GRPCRequestBuilder{
		req: &GRPCRequest{
			Metadata:     make(map[string]string),
			RPCType:      "unary",
			Timeout:      30 * time.Second,
			OutputFormat: "json",
			ExtraEnvVars: make(map[string]string),
		},
	}
}

func (b *GRPCRequestBuilder) WithServer(server string) *GRPCRequestBuilder {
	b.req.Server = server
	return b
}

func (b *GRPCRequestBuilder) WithService(service string) *GRPCRequestBuilder {
	b.req.Service = service
	return b
}

func (b *GRPCRequestBuilder) WithMethod(method string) *GRPCRequestBuilder {
	b.req.Method = method
	return b
}

func (b *GRPCRequestBuilder) WithMessage(message string) *GRPCRequestBuilder {
	b.req.Message = message
	return b
}

func (b *GRPCRequestBuilder) WithMetadata(metadata map[string]string) *GRPCRequestBuilder {
	maps.Copy(b.req.Metadata, metadata)
	return b
}

func (b *GRPCRequestBuilder) WithRPCType(rpcType string) *GRPCRequestBuilder {
	b.req.RPCType = rpcType
	return b
}

func (b *GRPCRequestBuilder) WithTLS(tls bool) *GRPCRequestBuilder {
	b.req.TLS = tls
	return b
}

func (b *GRPCRequestBuilder) WithInsecureSkipVerify(skip bool) *GRPCRequestBuilder {
	b.req.InsecureSkipVerify = skip
	return b
}

func (b *GRPCRequestBuilder) WithServerCert(cert string) *GRPCRequestBuilder {
	b.req.ServerCert = cert
	return b
}

func (b *GRPCRequestBuilder) WithTimeout(timeout time.Duration) *GRPCRequestBuilder {
	b.req.Timeout = timeout
	return b
}

func (b *GRPCRequestBuilder) WithOutputFormat(format string) *GRPCRequestBuilder {
	b.req.OutputFormat = format
	return b
}

func (b *GRPCRequestBuilder) WithVerbose() *GRPCRequestBuilder {
	b.req.Verbose = true
	return b
}

func (b *GRPCRequestBuilder) WithImageURL(imageURL string) *GRPCRequestBuilder {
	b.req.ImageURL = imageURL
	return b
}

func (b *GRPCRequestBuilder) WithReadinessHandshake(file string, timeout time.Duration) *GRPCRequestBuilder {
	b.req.ReadinessFile = file
	b.req.ReadinessTimeout = timeout
	return b
}

func (b *GRPCRequestBuilder) WithExtraEnvVar(key, value string) *GRPCRequestBuilder {
	b.req.ExtraEnvVars[key] = value
	return b
}

func (b *GRPCRequestBuilder) Build() (*GRPCRequest, error) {
	if b.req.ImageURL == "" {
		return nil, errors.New("ImageURL is required")
	}

	reqCopy := &GRPCRequest{
		Server:             b.req.Server,
		Service:            b.req.Service,
		Method:             b.req.Method,
		Message:            b.req.Message,
		Metadata:           make(map[string]string),
		RPCType:            b.req.RPCType,
		TLS:                b.req.TLS,
		InsecureSkipVerify: b.req.InsecureSkipVerify,
		ServerCert:         b.req.ServerCert,
		Timeout:            b.req.Timeout,
		OutputFormat:       b.req.OutputFormat,
		Verbose:            b.req.Verbose,
		ExtraEnvVars:       make(map[string]string),
		ImageURL:           b.req.ImageURL,
		ReadinessFile:      b.req.ReadinessFile,
		ReadinessTimeout:   b.req.ReadinessTimeout,
	}

	maps.Copy(reqCopy.Metadata, b.req.Metadata)
	maps.Copy(reqCopy.ExtraEnvVars, b.req.ExtraEnvVars)

	return reqCopy, nil
}

// WithExtraEnvVar adds a single additional environment variable
func (m *GRPCRequest) WithExtraEnvVar(key, value string) *GRPCRequest {
	m.ExtraEnvVars[key] = value
	return m
}

func (m *GRPCRequest) toEnvVars() map[string]string {
	envVars := make(map[string]string)

	if m.Server != "" {
		envVars["GRPC_SERVER"] = m.Server
	}
	if m.Service != "" {
		envVars["GRPC_SERVICE"] = m.Service
	}
	if m.Method != "" {
		envVars["GRPC_METHOD"] = m.Method
	}
	if m.Message != "" {
		envVars["GRPC_MESSAGE"] = m.Message
	}
	if len(m.Metadata) > 0 {
		var pairs []string
		for k, v := range m.Metadata {
			pairs = append(pairs, fmt.Sprintf("%s:%s", k, v))
		}
		envVars["GRPC_METADATA"] = strings.Join(pairs, ",")
	}
	if m.RPCType != "" {
		envVars["GRPC_RPC_TYPE"] = m.RPCType
	}
	if m.TLS {
		envVars["GRPC_TLS"] = "true"
	} else {
		envVars["GRPC_TLS"] = "false"
	}
	if m.InsecureSkipVerify {
		envVars["GRPC_INSECURE_SKIP_VERIFY"] = "true"
	}
	if m.ServerCert != "" {
		envVars["GRPC_SERVER_CERT"] = m.ServerCert
	}
	if m.Timeout > 0 {
		envVars["GRPC_TIMEOUT"] = strconv.Itoa(int(m.Timeout.Seconds()))
	}
	if m.OutputFormat != "" {
		envVars["OUTPUT_FORMAT"] = m.OutputFormat
	}
	envVars["VERBOSE"] = "true"

	if m.ReadinessFile != "" {
		envVars["READINESS_HANDSHAKE"] = fmt.Sprintf("%s.ready:%s.pid:%d", m.ReadinessFile, m.ReadinessFile, m.ReadinessTimeout.Milliseconds())
	}

	maps.Copy(envVars, m.ExtraEnvVars)

	return envVars
}

func (m *GRPCRequest) lifecycleTimeout() time.Duration {
	timeout := m.ReadinessTimeout + m.Timeout + containerLifecycleGracePeriod
	if timeout <= 0 {
		return containerLifecycleGracePeriod
	}
	return timeout
}

func (m *GRPCRequest) readinessTimeout() time.Duration {
	if m.ReadinessTimeout > 0 {
		return m.ReadinessTimeout
	}
	return containerLifecycleGracePeriod
}

// Run starts a gRPC babel container and returns a Container with result channel.
func (m *GRPCRequest) Run(ctx context.Context, l *zap.Logger) *Container {
	if l == nil {
		l = zap.NewNop()
	}
	result := newContainerResult()
	c := &Container{
		resultCh:   make(chan ContainerResult, 1),
		processPID: make(chan int, 1),
	}

	envVars := m.toEnvVars()

	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: m.ImageURL,
			Env:   envVars,
			HostConfigModifier: func(hc *container.HostConfig) {
				hc.NetworkMode = network.NetworkHost
				// pid mode host is required for the readiness handshake
				hc.PidMode = "host"
			},
			LogConsumerCfg: &testcontainers.LogConsumerConfig{
				Opts:      []testcontainers.LogProductionOption{testcontainers.WithLogProductionTimeout(10 * time.Second)},
				Consumers: []testcontainers.LogConsumer{&result},
			},
			AutoRemove: true,
		},
	}

	l.Info("starting gRPC container", zap.String("image", m.ImageURL), zap.Any("env", envVars))
	creationCtx, creationCancel := context.WithTimeout(ctx, m.lifecycleTimeout())
	cntnr, err := genericContainer(creationCtx, req)
	creationCancel()
	if err != nil || cntnr == nil {
		if err == nil {
			err = errors.New("container provider returned a nil container")
		}
		l.Error("start container error", zap.Error(err))
		result.Error = fmt.Errorf("starting container %s: %w", m.ImageURL, err)
		result.ExitCode = -1
		if cntnr != nil {
			terminateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			_ = cntnr.Terminate(terminateCtx)
			cancel()
		}
		c.resultCh <- result
		return c
	}

	c.Container = cntnr
	go func() {
		workerCtx, cancel := context.WithTimeout(ctx, m.lifecycleTimeout())
		defer cancel()
		defer func() {
			if result.Error == nil && workerCtx.Err() != nil {
				result.Error = fmt.Errorf("container lifecycle: %w", workerCtx.Err())
				result.ExitCode = -1
			}
			c.resultCh <- result
		}()

		err := cntnr.Start(workerCtx)
		if err != nil {
			l.Error("start container error", zap.Error(err))
			result.Error = fmt.Errorf("starting container %s: %w", m.ImageURL, err)
			result.ExitCode = -1
			return
		}

		if m.ReadinessFile != "" {
			readinessCtx, readinessCancel := context.WithTimeout(workerCtx, m.readinessTimeout())
			defer readinessCancel()
			var pid int
			for {
				if readinessCtx.Err() != nil {
					l.Warn("readiness handshake timed out", zap.Error(readinessCtx.Err()))
					result.Error = fmt.Errorf("readiness handshake: %w", readinessCtx.Err())
					result.ExitCode = -1
					return
				}
				pidFile, err := cntnr.CopyFileFromContainer(readinessCtx, m.ReadinessFile+".pid")
				if err == nil {
					pidTxt, err := io.ReadAll(pidFile)
					_ = pidFile.Close()
					if err == nil {
						pid, err = strconv.Atoi(string(pidTxt))
						if err == nil {
							break
						}
						l.Warn("failed to parse pid from file", zap.String("pid", string(pidTxt)), zap.Error(err))
					} else {
						l.Warn("read pid file error", zap.Error(err))
					}
				}
				l.Debug("waiting for pid file", zap.Error(err))
				select {
				case <-time.After(100 * time.Millisecond):
				case <-readinessCtx.Done():
				}
			}
			l.Debug("got pid", zap.Int("pid", pid))
			select {
			case c.processPID <- pid:
			case <-workerCtx.Done():
				result.Error = fmt.Errorf("publishing readiness process: %w", workerCtx.Err())
				result.ExitCode = -1
				return
			}
		}

		// Poll for container exit
		for {
			if workerCtx.Err() != nil {
				return
			}
			state, err := cntnr.State(workerCtx)
			if err != nil {
				l.Error("get container state error", zap.Error(err))
				result.Error = fmt.Errorf("getting container state: %w", err)
				result.ExitCode = -1
				return
			}
			if state == nil {
				result.Error = errors.New("getting container state: container provider returned nil state")
				result.ExitCode = -1
				return
			}
			if state.Running {
				select {
				case <-time.After(10 * time.Millisecond):
				case <-workerCtx.Done():
				}
				continue
			}

			l.Debug("container state", zap.Any("state", state))
			result.ExitCode = state.ExitCode
			if state.Error != "" {
				result.Error = fmt.Errorf("container error: %s", state.Error)
			}
			return
		}
	}()

	return c
}
