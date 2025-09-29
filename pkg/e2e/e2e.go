package e2e

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var AwaitEventsTimeout = 10 * time.Second

type Context struct {
	ctx          context.Context
	closers      []func() error
	machineIP    net.IP
	Start        time.Time
	EventStore   *EventStoreFactory
	ConfProvider *ConfigProvider
	L            *zap.Logger
	TestConfig   func(mut func(*config.Config)) *config.Config
}

func NewContext(ctx context.Context) *Context {
	return &Context{
		ctx:        ctx,
		TestConfig: DefaultTestConfig,
	}
}

func (c *Context) RegisterErrCloser(closer func() error) {
	c.closers = append(c.closers, closer)
}

func (c *Context) RegisterNoErrCloser(closer func()) {
	c.closers = append(c.closers, func() error {
		closer()
		return nil
	})
}

func (c *Context) Close() error {
	for i := len(c.closers) - 1; i >= 0; i-- {
		closer := c.closers[i]
		if err := closer(); err != nil {
			return err
		}
	}
	return nil
}

// Deadline implements context.Context for convenience
func (c *Context) Deadline() (deadline time.Time, ok bool) {
	return c.ctx.Deadline()
}

// Done implements context.Context for convenience
func (c *Context) Done() <-chan struct{} {
	return c.ctx.Done()
}

// Err implements context.Context for convenience
func (c *Context) Err() error {
	return c.ctx.Err()
}

// Value implements context.Context for convenience
func (c *Context) Value(key any) any {
	return c.ctx.Value(key)
}

func NewID() string {
	return "e2e_" + xid.New().String()
}

func humanDuration(d time.Duration) string {
	var s strings.Builder
	s.WriteString(fmt.Sprintf("% 2ds", d/time.Second))
	d %= time.Second
	s.WriteString(fmt.Sprintf("% 3d.%03dms", d/time.Millisecond, d%time.Millisecond/time.Microsecond))
	return s.String()
}

func TimeElapsedEncoder(start time.Time) zapcore.TimeEncoder {
	return func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(fmt.Sprintf("% 10s", humanDuration(time.Since(start))))
	}
}

func DefaultTestConfig(mut func(*config.Config)) *config.Config {
	conf := &config.Config{
		Services: config.Services{
			EventStores:  []config.ServiceEventStore{{Type: "e2e"}},
			ObjectStores: []config.ServiceObjectStore{{Type: "disabled"}},
		},
		Stacks: map[string]config.Stack{
			"e2e": {
				Plugins: []config.Plugin{
					{Type: "report_usage"},
				},
			},
		},
		Tap: &config.TapConfig{
			Direction:       config.TrafficDirection_EGRESS,
			IgnoreLoopback:  true,
			AuditIncludeDNS: false,
			Http: config.TapHttpConfig{
				Stack: "e2e",
			},
		},
		Control: &config.Control{
			Default: config.AccessControlAction_ALLOW,
			Rules:   []config.Rule{},
		},
	}
	if mut != nil {
		mut(conf)
	}
	return conf
}

func (c *Context) TestCtx(t *testing.T) *TestContext {
	id := NewID()
	ctx := &TestContext{
		ID:     id,
		e2ectx: c,
		T:      t,
		L:      c.L.With(zap.String("ctxid", id)),
	}
	ctx.L.Info("🔧 test context created")
	return ctx
}

func (c *Context) SetConfig(conf *config.Config) {
	wait, err := c.ConfProvider.SetConfig(conf)
	if err != nil {
		c.L.Fatal("failed to set config", zap.Error(err))
	}
	wait()
}

type TestContext struct {
	e2ectx *Context
	ID     string
	T      *testing.T
	L      *zap.Logger
}

type ExecResult struct {
	Output      string
	Err         error
	Code        int
	ID          string
	AwaitEvents func(expectedConnections int) *Events
}

func (c *TestContext) Exec(name string, args ...string) ExecResult {
	id := NewID()
	cmd := exec.CommandContext(c.T.Context(), name, args...)
	cmd.Env = []string{
		fmt.Sprintf("QPOINT_TAGS=ctxid:%s,ctxid:%s", c.ID, id),
	}
	c.L.Info("🕹️ executing command", zap.String("cmd", strings.Join(append([]string{name}, args...), " ")))
	out, err := cmd.CombinedOutput()
	var code int
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	}
	return ExecResult{
		Output: string(out),
		Err:    err,
		Code:   code,
		ID:     id,
		AwaitEvents: func(expectedConnections int) *Events {
			events, err := c.e2ectx.EventStore.AwaitByCtxID(id, expectedConnections, AwaitEventsTimeout)
			require.NoError(c.T, err)
			return events
		},
	}
}

// func (c *TestContext) Do(request *HTTPRequest) ExecResult {
// 	id := NewID()
// 	request.WithExtraEnvVar("QPOINT_TAGS", fmt.Sprintf("ctxid:%s,ctxid:%s", c.ID, id))
// 	c.L.Info("🕹️ executing babel http request", zap.String("image", request.ImageURL))

// 	c.L.Debug("✅ babel http request starting", zap.String("image", request.ImageURL))
// 	container := request.Run(c.T.Context(), c.L)

// 	c.L.Debug("⏱️ Waiting for babel http container to exit", zap.String("image", request.ImageURL))
// 	result, err := container.WaitForExit(c.T.Context())
// 	require.NoError(c.T, err)
// 	c.L.Debug("✅ babel http request finished", zap.String("image", request.ImageURL), zap.String("stdout", result.Stdout()), zap.String("stderr", result.Stderr()))

// 	return ExecResult{
// 		Output: result.Combined(),
// 		Err:    result.Error,
// 		Code:   result.ExitCode,
// 		ID:     id,
// 		AwaitEvents: func(expectedConnections int) *Events {
// 			events, err := c.e2ectx.EventStore.AwaitByCtxID(id, expectedConnections, AwaitEventsTimeout)
// 			require.NoError(c.T, err)
// 			return events
// 		},
// 	}
// }

func (c *TestContext) Events(expectedConnections int) *Events {
	events, err := c.e2ectx.EventStore.AwaitByCtxID(c.ID, expectedConnections, AwaitEventsTimeout)
	require.NoError(c.T, err)
	return events
}

func (c *TestContext) WithConfig(t *testing.T, mut ConfigMutator, fn func(*testing.T)) {
	t.Helper()
	conf := c.e2ectx.TestConfig(mut)
	c.L.Info("⚙️ setting test config")
	c.e2ectx.SetConfig(conf)
	c.L.Info("✅ new config was propagated")

	defer func() {
		c.L.Info("⚙️ restoring default config")
		c.e2ectx.SetConfig(c.e2ectx.TestConfig(nil))
		c.L.Info("✅ default config restored")
	}()

	fn(t)
}

func (c *TestContext) MachineIP() net.IP {
	return c.e2ectx.MachineIP()
}

func (c *Context) SetMachineIP(ip net.IP) {
	c.machineIP = ip
}

func (c *Context) MachineIP() net.IP {
	return c.machineIP
}

func NewLogger(start time.Time) *zap.Logger {
	zapconf := zap.NewDevelopmentConfig()
	level := zap.InfoLevel
	if l, err := zap.ParseAtomicLevel(os.Getenv("LOG_LEVEL")); err == nil {
		level = l.Level()
	}
	zapconf.Level = zap.NewAtomicLevelAt(level)
	zapconf.DisableStacktrace = level > zap.DebugLevel
	zapconf.DisableCaller = level > zap.DebugLevel
	zapconf.EncoderConfig.EncodeTime = TimeElapsedEncoder(start)
	zapconf.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	ll, err := zapconf.Build()
	if err != nil {
		panic(err)
	}
	return ll
}
