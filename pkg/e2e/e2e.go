package e2e

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/rs/xid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Context struct {
	ctx          context.Context
	closers      []func() error
	Start        time.Time
	Eventstore   *EventStore
	ConfProvider *ConfigProvider
	L            *zap.Logger
}

func NewContext(ctx context.Context) *Context {
	return &Context{
		ctx: ctx,
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
	for _, closer := range c.closers {
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

func TestConfig(mut func(*config.Config)) *config.Config {
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
	tid := NewID()
	return &TestContext{
		TID:    tid,
		e2ectx: c,
		T:      t,
		L:      c.L.With(zap.String("tid", tid)),
	}
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
	TID    string
	T      *testing.T
	L      *zap.Logger
}

type ExecResult struct {
	Output string
	Err    error
	Code   int
	CGID   string
	Events func() Events
}

func (c *TestContext) Exec(name string, args ...string) ExecResult {
	cgid := NewID()
	cmd := exec.CommandContext(c.T.Context(), name, args...)
	cmd.Env = []string{
		fmt.Sprintf("QPOINT_TAGS=cgid:%s,cgid:%s", c.TID, cgid),
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
		CGID:   cgid,
		Events: func() Events {
			return c.e2ectx.Eventstore.GetByCGID(cgid)
		},
	}
}

func (c *TestContext) Events() Events {
	return c.e2ectx.Eventstore.GetByCGID(c.TID)
}

func (c *TestContext) SetConfig(conf *config.Config) {
	c.L.Info("⚙️ setting test config")
	c.e2ectx.SetConfig(conf)
	c.L.Info("✅ new config was propagated")
	c.T.Cleanup(func() {
		c.L.Info("⚙️ restoring default config")
		c.e2ectx.SetConfig(TestConfig(nil))
		c.L.Info("✅ default config restored")
	})
}

func NewLogger(start time.Time) *zap.Logger {
	zapconf := zap.NewDevelopmentConfig()
	zapconf.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	zapconf.DisableStacktrace = true
	zapconf.DisableCaller = true
	zapconf.EncoderConfig.EncodeTime = TimeElapsedEncoder(start)
	zapconf.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	ll, err := zapconf.Build()
	if err != nil {
		panic(err)
	}
	return ll
}
