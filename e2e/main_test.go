//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/dns"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	ebpfProcess "github.com/qpoint-io/qtap/pkg/ebpf/process"
	"github.com/qpoint-io/qtap/pkg/ebpf/socket"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls/openssl"
	"github.com/qpoint-io/qtap/pkg/ebpf/trace"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/accesslogs"
	"github.com/qpoint-io/qtap/pkg/plugins/logger"
	"github.com/qpoint-io/qtap/pkg/plugins/report"
	"github.com/qpoint-io/qtap/pkg/plugins/wrapper"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/services"
	objectstorenoop "github.com/qpoint-io/qtap/pkg/services/objectstore/noop"
	"github.com/qpoint-io/qtap/pkg/stream"
	"github.com/qpoint-io/qtap/pkg/tags"
	"github.com/rs/xid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
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
		wrapper.Catch(&logger.Factory{}),
		wrapper.Catch(&report.Factory{}),
		wrapper.Catch(accesslogs.NewConsoleJSONFilter()),
		wrapper.Catch(accesslogs.NewConsoleHttpFilter()),
	}
)

func mainSetup() error {
	var err error
	zapconf := zap.NewDevelopmentConfig()
	zapconf.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	zapconf.DisableStacktrace = true
	zapconf.DisableCaller = true
	zapconf.EncoderConfig.EncodeTime = timeElapsedEncoder(e2ectx.start)
	zapconf.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

	e2ectx.ll, err = zapconf.Build()
	if err != nil {
		return fmt.Errorf("creating logger: %w", err)
	}

	if syscall.Getuid() != 0 {
		return fmt.Errorf("please run e2e tests as root to load BPF programs and maps")
	}

	// set up config
	if err := e2ectx.confProvider.Start(); err != nil {
		return fmt.Errorf("starting config provider: %w", err)
	}
	confManager := config.NewConfigManager(e2ectx.ll, e2ectx.confProvider)
	if err := confManager.Run(e2ectx); err != nil {
		return fmt.Errorf("running config manager: %w", err)
	}

	// Load BPF programs and maps
	e2ectx.ll.Info("loading BPF programs and maps")
	spec, err := tap.LoadTap()
	if err != nil {
		return fmt.Errorf("loading BPF programs and maps: %w", err)
	}
	// write the current pid to the bpf program
	err = spec.RewriteConstants(map[string]interface{}{
		"qpid": uint32(os.Getpid()),
	})
	if err != nil {
		return fmt.Errorf("rewriting constants: %w", err)
	}
	tapObjs := tap.TapObjects{}
	err = spec.LoadAndAssign(&tapObjs, nil)
	if err != nil {
		return fmt.Errorf("loading BPF programs and maps: %w", err)
	}
	e2ectx.RegisterErrCloser(tapObjs.Close)

	// Initialize process manager
	procEbpfMan, err := newEbpfProcManager(e2ectx.ll, &tapObjs)
	if err != nil {
		return fmt.Errorf("getting ebpf proc objs: %w", err)
	}

	pm := process.NewProcessManager(e2ectx.ll, procEbpfMan)
	confManager.SubscribeSetter(pm)

	// Initialize container detection
	// containerManager := container.NewManager(e2ectx.ll, dockerSocketEndpoint, containerdSocketEndpoint, criRuntimeSocketEndpoint)
	// if err := containerManager.Start(e2ectx); err != nil {
	// 	return fmt.Errorf("starting container manager: %w", err)
	// }
	// pm.Observe(containerManager)

	// Initialize BPF trace manager
	bpfTraceQuery := "" // TODO(e2e)
	tm, err := trace.NewTraceManager(e2ectx.ll, tapObjs.TraceToggleMap, tapObjs.TraceEvents, pm, bpfTraceQuery)
	if err != nil {
		return fmt.Errorf("creating bpf trace manager: %w", err)
	}

	// start the bpf trace manager
	if err := tm.Start(); err != nil {
		return fmt.Errorf("starting bpf trace manager: %w", err)
	}
	pm.Observe(tm)

	// cleanup the bpf trace manager
	e2ectx.RegisterErrCloser(tm.Stop)

	// Initialize DNS resolver
	resolv := dns.NewDNSManager(e2ectx.ll, pm)
	if err := resolv.Start(); err != nil {
		return fmt.Errorf("starting dns manager: %w", err)
	}
	e2ectx.RegisterErrCloser(resolv.Stop)

	// Initialize service and plugin systems
	svcRegistry := services.NewServiceRegistry()
	svcManager := services.NewServiceManager(e2ectx.ctx, e2ectx.ll, svcRegistry)
	svcManager.RegisterFactory(serviceFactories...)
	confManager.SubscribeSetter(svcManager)

	pluginRegistry := plugins.NewRegistry(pluginFactories...)
	pluginManager := plugins.NewPluginManager(
		e2ectx.ll,
		plugins.SetBufferSize(2*1<<20), // 2MB
		plugins.SetServiceRegistry(svcRegistry),
		plugins.SetPluginRegistry(pluginRegistry),
	)
	confManager.SubscribeSetter(pluginManager)
	if err := pluginManager.Start(); err != nil {
		return fmt.Errorf("starting plugin manager: %w", err)
	}
	e2ectx.RegisterNoErrCloser(pluginManager.Stop)

	// Initialize stream factory
	ds := stream.NewStreamFactory(
		e2ectx.ll,
		stream.SetDnsManager(resolv),
		stream.SetPluginManager(pluginManager),
	)

	//  Initialize connection manager
	connectionManager := connection.NewManager(
		e2ectx.ll,
		connection.SetProcessManager(pm),
		connection.SetDnsManager(resolv),
		connection.SetStreamFactory(ds),
		connection.SetServiceRegistry(svcRegistry),
		connection.SetConfig(confManager.GetConfig()),
		connection.SetDeploymentTags(tags.FromValues(map[string]string{"e2e": "true"})),
	)
	confManager.SubscribeSetter(connectionManager)

	// init a socket settings manager to push config changes down into ebpf land
	socketSettingManager := socket.NewSocketSettingsManager(e2ectx.ll, tapObjs.TapMaps.SocketSettingsMap)
	confManager.SubscribeSetter(socketSettingManager)

	// Initialize socket manager
	socketManager, err := newEbpfSockManager(e2ectx.ll, connectionManager, &tapObjs)
	if err != nil {
		return fmt.Errorf("creating socket event manager: %w", err)
	}

	// Initialize TLS probes
	tlsProbes := "openssl" // TODO(e2e)
	e2ectx.ll.Info("starting TLS Probes", zap.String("probes", tlsProbes))
	tlsManager, err := initTLSProbes(e2ectx.ll, tlsProbes, &tapObjs)
	if err != nil {
		return fmt.Errorf("initializing TLS probes: %w", err)
	}
	if tlsManager != nil {
		// add tls probes as process observers
		pm.Observe(tlsManager)

		e2ectx.RegisterErrCloser(tlsManager.Stop)
	}

	// Start managers
	if err := pm.Start(); err != nil {
		return fmt.Errorf("starting process manager: %w", err)
	}
	e2ectx.RegisterErrCloser(pm.Stop)

	// start the socket manager
	if err := socketManager.Start(); err != nil {
		return fmt.Errorf("starting socket listener: %w", err)
	}
	e2ectx.RegisterErrCloser(socketManager.Stop)

	e2ectx.RegisterNoErrCloser(func() {
		errs, ok := e2ectx.eventstore.Errors()
		if !ok {
			e2ectx.ll.Error("event store exited with errors", zap.Any("errors", errs))
		}
	})

	e2ectx.ll.Info("🥟 completed e2e setup")
	return nil
}

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
	ll           *zap.Logger
	closers      []func()
	start        time.Time
	eventstore   *EventStore
	confProvider *ConfigProvider
}

func (c *e2eContext) RegisterErrCloser(closer func() error) {
	c.closers = append(c.closers, func() {
		if err := closer(); err != nil {
			c.ll.Error("closing resource", zap.Error(err))
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

// TOOD(e2e)
func newEbpfProcManager(logger *zap.Logger, objs *tap.TapObjects) (*ebpfProcess.Manager, error) {
	procManTps := []*common.Tracepoint{
		common.NewTracepoint("syscalls", "sys_enter_execve", objs.TapPrograms.SyscallProbeEntryExecve),
		common.NewTracepoint("syscalls", "sys_exit_execve", objs.TapPrograms.SyscallProbeRetExecve),
		common.NewTracepoint("syscalls", "sys_enter_execveat", objs.TapPrograms.SyscallProbeEntryExecveat),
		common.NewTracepoint("syscalls", "sys_exit_execveat", objs.TapPrograms.SyscallProbeRetExecveat),
		common.NewTracepoint("sched", "sched_process_exit", objs.TapPrograms.TracepointSchedProcessExit),
	}

	procManRB, err := ringbuf.NewReader(objs.TapMaps.ProcEvents)
	if err != nil {
		return nil, fmt.Errorf("failed to create proc event reader: %w", err)
	}

	procMan := ebpfProcess.New(logger, objs.TapMaps.ProcessMetaMap, procManRB, procManTps)

	return procMan, nil
}

// TOOD(e2e)
func initTLSProbes(logger *zap.Logger, tlsProbesStr string, objs *tap.TapObjects) (*tls.TlsManager, error) {
	// Split the string and trim whitespace
	tlsProbesList := strings.Split(tlsProbesStr, ",")
	for i, probe := range tlsProbesList {
		tlsProbesList[i] = strings.TrimSpace(probe)
	}

	enableTLS := true
	probes := make([]tls.TlsProbe, 0, len(tlsProbesList))
	for _, mode := range tlsProbesList {
		mode = strings.ToLower(mode)
		switch mode {
		case "openssl":
			probes = append(probes, openssl.NewOpenSSLManager(logger, newEbpfOpenSSLprobesCreator(objs)))
		case "none", "":
			enableTLS = false
			logger.Info("No TLS probes enabled")
		default:
			logger.Warn("Unknown TLS probe specified", zap.String("probe", mode))
		}
	}

	if enableTLS || len(probes) > 0 {
		// init tls probes manager
		ssl := tls.NewTlsManager(logger, probes...)

		// start the tls probes manager
		if err := ssl.Start(); err != nil {
			logger.Fatal("failed to start tls probes manager", zap.Error(err))
		}

		return ssl, nil
	}

	return nil, nil
}

// TOOD(e2e)
func newEbpfSockManager(logger *zap.Logger, connMan *connection.Manager, objs *tap.TapObjects) (*socket.SocketEventManager, error) {
	// set the tracepoints (⚠️ order is important!)
	tps := []common.Probe{
		// sni tracepoints
		common.NewTracepoint("syscalls", "sys_exit_sendto", objs.TapPrograms.SyscallProbeRetSendtoInit),
		common.NewTracepoint("syscalls", "sys_exit_write", objs.TapPrograms.SyscallProbeRetWriteInit),
		common.NewTracepoint("syscalls", "sys_exit_writev", objs.TapPrograms.SyscallProbeRetWritevInit),
		common.NewTracepoint("syscalls", "sys_exit_recvfrom", objs.TapPrograms.SyscallProbeRetRecvfromInit),
		common.NewTracepoint("syscalls", "sys_exit_read", objs.TapPrograms.SyscallProbeRetReadInit),
		common.NewTracepoint("syscalls", "sys_exit_readv", objs.TapPrograms.SyscallProbeRetReadvInit),

		// syscall socket events
		common.NewTracepoint("syscalls", "sys_enter_accept", objs.TapPrograms.SyscallProbeEntryAccept),
		common.NewTracepoint("syscalls", "sys_exit_accept", objs.TapPrograms.SyscallProbeRetAccept),
		common.NewTracepoint("syscalls", "sys_enter_accept4", objs.TapPrograms.SyscallProbeEntryAccept4),
		common.NewTracepoint("syscalls", "sys_exit_accept4", objs.TapPrograms.SyscallProbeRetAccept4),
		common.NewTracepoint("syscalls", "sys_enter_connect", objs.TapPrograms.SyscallProbeEntryConnect),
		common.NewTracepoint("syscalls", "sys_exit_connect", objs.TapPrograms.SyscallProbeRetConnect),
		common.NewTracepoint("syscalls", "sys_enter_close", objs.TapPrograms.SyscallProbeEntryClose),
		common.NewTracepoint("syscalls", "sys_exit_close", objs.TapPrograms.SyscallProbeRetClose),
		common.NewTracepoint("syscalls", "sys_enter_write", objs.TapPrograms.SyscallProbeEntryWrite),
		common.NewTracepoint("syscalls", "sys_enter_writev", objs.TapPrograms.SyscallProbeEntryWritev),
		common.NewTracepoint("syscalls", "sys_exit_write", objs.TapPrograms.SyscallProbeRetWrite),
		common.NewTracepoint("syscalls", "sys_exit_writev", objs.TapPrograms.SyscallProbeRetWritev),
		common.NewTracepoint("syscalls", "sys_enter_sendto", objs.TapPrograms.SyscallProbeEntrySendto),
		common.NewTracepoint("syscalls", "sys_exit_sendto", objs.TapPrograms.SyscallProbeRetSendto),
		common.NewTracepoint("syscalls", "sys_enter_read", objs.TapPrograms.SyscallProbeEntryRead),
		common.NewTracepoint("syscalls", "sys_enter_readv", objs.TapPrograms.SyscallProbeEntryReadv),
		common.NewTracepoint("syscalls", "sys_exit_read", objs.TapPrograms.SyscallProbeRetRead),
		common.NewTracepoint("syscalls", "sys_exit_readv", objs.TapPrograms.SyscallProbeRetReadv),
		common.NewTracepoint("syscalls", "sys_enter_recvfrom", objs.TapPrograms.SyscallProbeEntryRecvfrom),
		common.NewTracepoint("syscalls", "sys_exit_recvfrom", objs.TapPrograms.SyscallProbeRetRecvfrom),
		common.NewTracepoint("syscalls", "sys_enter_socket", objs.TapPrograms.SyscallProbeEntrySocket),
		common.NewTracepoint("syscalls", "sys_exit_socket", objs.TapPrograms.SyscallProbeRetSocket),

		// pid/fd mapping kprobes
		common.NewKprobe("sock_alloc_file", objs.TapPrograms.TrackSockAllocFileEntry),
		common.NewKretprobe("sock_alloc_file", objs.TapPrograms.TrackSockAllocFileRet),
		common.NewKprobe("fd_install", objs.TapPrograms.TrackFdInstallEntry),
		common.NewKprobe("__fput", objs.TapPrograms.CleanupPidFdFileEntries),
		common.NewKprobe("tcp_close", objs.TapPrograms.TraceTcpClose),

		// ftraces
		common.NewFexit("tcp_v4_connect", objs.TapPrograms.TraceTcpV4ConnectFexit),
		common.NewFexit("tcp_v6_connect", objs.TapPrograms.TraceTcpV6ConnectFexit),
	}

	// open a ring buffer reader
	rb, err := ringbuf.NewReader(objs.TapMaps.SocketEvents)
	if err != nil {
		return nil, fmt.Errorf("creating socket event reader: %w", err)
	}

	return socket.NewSocketEventManager(logger, connMan, rb, tps), nil
}

// TOOD(e2e)
// newEbpfOpenSSLprobesCreator creates a function that returns a list of uprobes for the OpenSSL library
// this is used to create new probes for each many instances.
func newEbpfOpenSSLprobesCreator(objs *tap.TapObjects) func() []*common.Uprobe {
	return func() []*common.Uprobe {
		return []*common.Uprobe{
			// ssl entry uprobes
			common.NewUprobe("SSL_read", objs.TapPrograms.OpensslProbeEntrySSL_read),
			common.NewUprobe("SSL_read_ex", objs.TapPrograms.OpensslProbeEntrySSL_readEx),
			common.NewUprobe("SSL_write", objs.TapPrograms.OpensslProbeEntrySSL_write),
			common.NewUprobe("SSL_write_ex", objs.TapPrograms.OpensslProbeEntrySSL_writeEx),
			common.NewUprobe("SSL_free", objs.TapPrograms.OpensslProbeEntrySSL_free),

			// ssl return uprobes
			common.NewUretprobe("SSL_read", objs.TapPrograms.OpensslProbeRetSSL_read),
			common.NewUretprobe("SSL_read_ex", objs.TapPrograms.OpensslProbeRetSSL_readEx),
			common.NewUretprobe("SSL_write", objs.TapPrograms.OpensslProbeRetSSL_write),
			common.NewUretprobe("SSL_write_ex", objs.TapPrograms.OpensslProbeRetSSL_writeEx),
			common.NewUretprobe("SSL_new", objs.TapPrograms.OpensslProbeRetSSL_new),
		}
	}
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
		ll:  c.ll.With(zap.String("tid", tid)),
	}
}

func (c *e2eContext) SetConfig(conf *config.Config) {
	wait, err := c.confProvider.SetConfig(conf)
	if err != nil {
		c.ll.Fatal("failed to set config", zap.Error(err))
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
