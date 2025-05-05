//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/accesslogs"
	loggerPlugin "github.com/qpoint-io/qtap/pkg/plugins/logger"
	"github.com/qpoint-io/qtap/pkg/plugins/report"
	"github.com/qpoint-io/qtap/pkg/plugins/wrapper"
	"github.com/qpoint-io/qtap/pkg/services"
	objectstorenoop "github.com/qpoint-io/qtap/pkg/services/objectstore/noop"
	"github.com/rs/xid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	logger = testLogger()

	e2ectx = &e2eContext{
		ctx:          context.Background(),
		start:        time.Now(),
		eventstore:   &EventStore{},
		confProvider: NewConfigProvider(testConfig(nil)),
	}
	serviceFactories = []services.FactoryFactory{
		// Eventstore services
		func() services.ServiceFactory { return e2ectx.eventstore },

		// Objectstore services
		func() services.ServiceFactory { return &objectstorenoop.Factory{} },
	}

	pluginFactories = []plugins.HttpPlugin{
		wrapper.Catch(&loggerPlugin.Factory{}),
		wrapper.Catch(&report.Factory{}),
		wrapper.Catch(accesslogs.NewConsoleJSONFilter()),
		wrapper.Catch(accesslogs.NewConsoleHttpFilter()),
	}
)

func TestMain(m *testing.M) {
	if err := mainSetup(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	code := m.Run()
	for _, closer := range e2ectx.closers {
		closer()
	}
	os.Exit(code)
}

type e2eContext struct {
	ctx          context.Context
	closers      []func()
	start        time.Time
	eventstore   *EventStore
	confProvider *ConfigProvider
}

func (c *e2eContext) RegisterErrCloser(closer func() error) {
	c.closers = append(c.closers, func() {
		if err := closer(); err != nil {
			logger.Error("closing resource", zap.Error(err))
		}
	})
}

func (c *e2eContext) RegisterNoErrCloser(closer func()) {
	c.closers = append(c.closers, closer)
}

// Deadline implements context.Context for convenience
func (c *e2eContext) Deadline() (deadline time.Time, ok bool) {
	return c.ctx.Deadline()
}

// Done implements context.Context for convenience
func (c *e2eContext) Done() <-chan struct{} {
	return c.ctx.Done()
}

// Err implements context.Context for convenience
func (c *e2eContext) Err() error {
	return c.ctx.Err()
}

// Value implements context.Context for convenience
func (c *e2eContext) Value(key any) any {
	return c.ctx.Value(key)
}

func testID() string {
	return "e2e_" + xid.New().String()
}

func timeElapsedEncoder(start time.Time) zapcore.TimeEncoder {
	return func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		// TODO(e2e): use a better format
		enc.AppendString(fmt.Sprintf("% 10s", time.Since(start).Truncate(time.Microsecond).String()))
	}
}

func testConfig(mut func(*config.Config)) *config.Config {
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

func (c *e2eContext) TestCtx(t *testing.T) *testContext {
	tid := testID()
	return &testContext{
		tid: tid,
		ctx: t.Context(),
		t:   t,
		ll:  logger.With(zap.String("tid", tid)),
	}
}

func (c *e2eContext) SetConfig(conf *config.Config) {
	wait, err := c.confProvider.SetConfig(conf)
	if err != nil {
		logger.Fatal("failed to set config", zap.Error(err))
	}
	wait()
}

type testContext struct {
	tid string
	ctx context.Context
	t   *testing.T
	ll  *zap.Logger
}

type execResult struct {
	output string
	err    error
	cgid   string
	events func() Events
}

func (c *testContext) exec(name string, args ...string) execResult {
	cgid := testID()
	cmd := exec.CommandContext(c.ctx, name, args...)
	cmd.Env = []string{
		fmt.Sprintf("QPOINT_TAGS=cgid:%s,cgid:%s", c.tid, cgid),
	}
	out, err := cmd.CombinedOutput()
	return execResult{
		output: string(out),
		err:    err,
		cgid:   cgid,
		events: func() Events {
			return e2ectx.eventstore.GetByCGID(cgid)
		},
	}
}

func (c *testContext) events() Events {
	return e2ectx.eventstore.GetByCGID(c.tid)
}

func (c *testContext) SetConfig(conf *config.Config) {
	c.ll.Info("⚙️ setting test config")
	e2ectx.SetConfig(conf)
	c.ll.Info("✅ new config was propagated")
	c.t.Cleanup(func() {
		c.ll.Info("⚙️ restoring default config")
		e2ectx.SetConfig(testConfig(nil))
		c.ll.Info("✅ default config restored")
	})
}

func testLogger() *zap.Logger {
	zapconf := zap.NewDevelopmentConfig()
	zapconf.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	zapconf.DisableStacktrace = true
	zapconf.DisableCaller = true
	zapconf.EncoderConfig.EncodeTime = timeElapsedEncoder(e2ectx.start)
	zapconf.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	ll, err := zapconf.Build()
	if err != nil {
		panic(err)
	}
	return ll
}
