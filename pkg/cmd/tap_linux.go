//go:build linux

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/auditlog"
	"github.com/qpoint-io/qtap/pkg/buildinfo"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/container"
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
	eventstoreconsole "github.com/qpoint-io/qtap/pkg/services/eventstore/console"
	eventstorenoop "github.com/qpoint-io/qtap/pkg/services/eventstore/noop"
	objectstoreconsole "github.com/qpoint-io/qtap/pkg/services/objectstore/console"
	objectstorenoop "github.com/qpoint-io/qtap/pkg/services/objectstore/noop"
	"github.com/qpoint-io/qtap/pkg/status"
	"github.com/qpoint-io/qtap/pkg/stream"
	"github.com/qpoint-io/qtap/pkg/tags"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/propagators/autoprop"
	"go.opentelemetry.io/otel"
	oteltracesdk "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
)

var (
	tlsProbes                string
	sanCertMaxSize           int
	dockerSocketEndpoint     string
	containerdSocketEndpoint string
	criRuntimeSocketEndpoint string
)

var (
	serviceFactories = []services.ServiceFactory{
		// Eventstore services
		&eventstoreconsole.Factory{},
		&eventstorenoop.Factory{},

		// Objectstore services
		&objectstoreconsole.Factory{},
		&objectstorenoop.Factory{},

		// Add more services here...
	}

	pluginFactories = []plugins.HttpPlugin{
		wrapper.Catch(&logger.Factory{}),
		wrapper.Catch(&report.Factory{}),
		wrapper.Catch(accesslogs.NewConsoleJSONFilter()),
		wrapper.Catch(accesslogs.NewConsoleHttpFilter()),

		// Add more plugins here...
	}
)

func init() {
	// Common options
	rootCmd.Flags().StringVar(&qpointConfig, "config",
		getEnvOr("QPOINT_CONFIG", ""),
		"Configuration file path")
	rootCmd.Flags().IntVar(&auditLogBufferSize, "audit-log-buffer-size",
		getEnvIntOr("AUDIT_LOG_BUFFER_SIZE", 1000),
		"Buffer size for audit logs")
	rootCmd.Flags().StringVar(&deploymentTags, "tags",
		getEnvOr("QPOINT_DEPLOYMENT_TAGS", ""),
		"Tags to add to the node")

	// Data directory options
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir",
		getEnvOr("DATA_DIR", "/tmp/qpoint"),
		"Directory to store state")

	// BPF trace options
	rootCmd.Flags().StringVar(&bpfTraceQuery, "bpf-trace",
		getEnvOr("BPF_TRACE", ""),
		"BPF trace query")

	// Certificate injection options
	rootCmd.Flags().StringVar(&certInjectionStrategy, "cert-injection",
		getEnvOr("CERT_INJECTION", "inline"),
		"How should CA certificates be injected for forwarding traffic (inline, ebpf, manual)")
	rootCmd.Flags().StringVar(&tlsOkStrategy, "set-tls-ok",
		getEnvOr("SET_TLS_OK", "on-cert-inject"),
		"When to mark forwarded traffic as OK for TLS termination (on-cert-inject, on-cert-read)")

	// Initialize flags with environment variable fallbacks
	rootCmd.Flags().StringVar(&tlsProbes, "tls-probes",
		getEnvOr("TLS_PROBES", "openssl"),
		"Comma-separated list of TLS probes to use")

	rootCmd.Flags().StringVar(&httpBufferSize, "http-buffer-size",
		getEnvOr("HTTP_BUFFER_SIZE", "2mb"),
		"HTTP buffer size (max 2gb)")

	rootCmd.Flags().IntVar(&sanCertMaxSize, "san-cert-max-size",
		getEnvIntOr("SAN_CERT_MAX_SIZE", 100),
		"Maximum size for SAN certificates")

	rootCmd.Flags().StringVar(&dockerSocketEndpoint, "docker-socket-endpoint",
		getEnvOr("DOCKER_SOCKET", "/var/run/docker.sock"),
		"Docker socket endpoint")

	rootCmd.Flags().StringVar(&containerdSocketEndpoint, "containerd-socket-endpoint",
		getEnvOr("CONTAINERD_SOCKET", "/run/containerd/containerd.sock"),
		"Containerd socket endpoint")

	rootCmd.Flags().StringVar(&criRuntimeSocketEndpoint, "cri-runtime-socket-endpoint",
		getEnvOr("CRI_RUNTIME_SOCKET", ""),
		"CRI runtime socket endpoint")

	// Status options
	rootCmd.Flags().StringVar(&statusListen, "status-listen",
		getEnvOr("STATUS_LISTEN", "0.0.0.0:10001"),
		"IP:PORT of status server to listen on")
}

// This skeleton version of runrootCmd provides the basic structure
// but will need to be fleshed out with actual implementation
func runTapCmd(logger *zap.Logger) {
	ctx := context.Background()

	shutdownTelemetry, err := setupTelemetry(ctx, "tap")
	if err != nil {
		logger.Fatal("unable to setup telemetry", zap.Error(err))
	}
	defer func() {
		if err := shutdownTelemetry(ctx); err != nil {
			logger.Error("unable to shutdown tracer provider", zap.Error(err))
		}
	}()

	// Log startup information
	logger.Info("Starting Qtap",
		zap.String("version", buildinfo.Version()),
		zap.Strings("tags", strings.Split(deploymentTags, ",")),
		telemetry.GetSysInfoAsFields(),
	)

	// Check if running as root (required for eBPF)
	if syscall.Getuid() != 0 {
		logger.Error("This program requires root privileges to load BPF programs and maps. Please run as root or with sudo.")
		defer func() {
			os.Exit(1)
		}()
		return
	}

	// Parse deployment tags if provided
	var dTags tags.List
	if deploymentTags != "" {
		var err error
		dTags, err = parseDeploymentTags()
		if err != nil {
			logger.Error("failed to parse deployment tags", zap.Error(err))
		}
	}

	// Create config provider based on command line flags
	var provider config.ConfigProvider

	// Setup configuration context
	configCtx, configCancel := context.WithCancel(ctx)
	defer configCancel()

	// Initialize a local config provider
	if qpointConfig != "" {
		provider = config.NewLocalConfigProvider(logger, qpointConfig)
	} else {
		logger.Warn("no config file provided, using default config")
		provider = config.NewDefaultConfigProvider(logger)
	}

	// Create and start config manager
	configManager := config.NewConfigManager(logger, provider)
	if err := configManager.Run(configCtx); err != nil {
		logger.Fatal("unable to start config manager", zap.Error(err))
	}

	// Register for SIGHUP to reload configuration
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGHUP)
		for {
			select {
			case <-sigCh:
				logger.Info("SIGHUP received, reloading configuration")
				if err := configManager.Reload(); err != nil {
					logger.Error("failed to reload config on SIGHUP", zap.Error(err))
				}
			case <-configCtx.Done():
				return
			}
		}
	}()

	// Load BPF programs and maps
	logger.Info("loading BPF programs and maps")
	spec, err := tap.LoadTap()
	if err != nil {
		logger.Fatal("failed to load BPF programs and maps", zap.Error(err))
	}
	// write the current pid to the bpf program
	err = spec.RewriteConstants(map[string]interface{}{
		"qpid": uint32(os.Getpid()),
	})
	if err != nil {
		logger.Fatal("failed to rewrite constants", zap.Error(err))
	}
	tapObjs := tap.TapObjects{}
	err = spec.LoadAndAssign(&tapObjs, nil)
	if err != nil {
		logger.Fatal("failed to load BPF programs and maps", zap.Error(err))
	}
	defer tapObjs.Close()

	// Initialize process manager
	procEbpfMan, err := newEbpfProcManager(logger, &tapObjs)
	if err != nil {
		logger.Fatal("failed to get ebpf proc objs", zap.Error(err))
	}

	pm := process.NewProcessManager(logger, procEbpfMan)
	configManager.Subscribe(func(cfg *config.Config) {
		pm.SetConfig(cfg)
	})

	// Audit Log Processor
	auditLogger := auditlog.New(logger)
	configManager.Subscribe(func(cfg *config.Config) {
		auditLogger.SetConfig(cfg)
	})

	// Initialize container detection
	containerManager := container.NewManager(logger, dockerSocketEndpoint, containerdSocketEndpoint, criRuntimeSocketEndpoint)
	if err := containerManager.Start(ctx); err != nil {
		logger.Fatal("failed to start container manager", zap.Error(err))
	}
	pm.Observe(containerManager)

	// Initialize BPF trace manager
	tm, err := trace.NewTraceManager(logger, tapObjs.TraceToggleMap, tapObjs.TraceEvents, pm, bpfTraceQuery)
	if err != nil {
		panic(fmt.Errorf("failed to create bpf trace manager: %w", err))
	}

	// start the bpf trace manager
	if err := tm.Start(); err != nil {
		panic(fmt.Errorf("failed to start bpf trace manager: %w", err))
	}

	// add the bpf trace manager as a process observer
	pm.Observe(tm)

	// cleanup the bpf trace manager
	defer func() {
		if err := tm.Stop(); err != nil {
			logger.Error("unable to cleanup bpf trace manager")
		}
	}()

	// Initialize DNS resolver
	resolv := dns.NewDNSManager(logger, pm)
	if err := resolv.Start(); err != nil {
		panic(fmt.Errorf("failed to start dns manager: %w", err))
	}
	defer func() {
		if err := resolv.Stop(); err != nil {
			logger.Error("unable to cleanup dns manager")
		}
	}()

	// Parse HTTP buffer size
	httpBufsize, err := parseSizeString(httpBufferSize)
	if err != nil {
		panic(fmt.Errorf("failed to parse http buffer size: %w", err))
	}

	// Initialize service and plugin systems
	svcRegistry := services.NewServiceRegistry()
	svcManager := services.NewServiceManager(ctx, logger, svcRegistry)
	svcManager.RegisterFactory(serviceFactories...)
	configManager.Subscribe(func(cfg *config.Config) {
		svcManager.SetConfig(cfg)
	})

	pluginRegistry := plugins.NewRegistry(pluginFactories...)
	pluginManager := plugins.NewPluginManager(
		logger,
		plugins.SetBufferSize(int(httpBufsize)),
		plugins.SetServiceRegistry(svcRegistry),
		plugins.SetPluginRegistry(pluginRegistry),
	)
	configManager.Subscribe(func(cfg *config.Config) {
		pluginManager.SetConfig(cfg)
	})
	if err := pluginManager.Start(); err != nil {
		panic(fmt.Errorf("failed to start plugin manager: %w", err))
	}
	defer func() {
		pluginManager.Stop()
	}()

	// Initialize stream factory
	ds := stream.NewStreamFactory(
		logger,
		stream.SetDnsManager(resolv),
		stream.SetPluginManager(pluginManager),
	)

	//  Initialize connection manager
	connectionManager := connection.NewManager(
		logger,
		connection.SetProcessManager(pm),
		connection.SetDnsManager(resolv),
		connection.SetStreamFactory(ds),
		connection.SetAuditLogger(auditLogger),
		connection.SetConfig(configManager.GetConfig()),
		connection.SetDeploymentTags(dTags),
	)

	// Subscribe connection manager to config changes
	configManager.Subscribe(func(cfg *config.Config) {
		connectionManager.SetConfig(cfg)
	})

	// init a socket settings manager to push config changes
	// down into ebpf land
	socketSettingManager := socket.NewSocketSettingsManager(logger, tapObjs.TapMaps.SocketSettingsMap)

	// Subscribe socket settings manager to config changes
	configManager.Subscribe(func(cfg *config.Config) {
		socketSettingManager.SetConfig(cfg)
	})

	// Initialize socket manager
	socketManager, err := newEbpfSockManager(logger, connectionManager, &tapObjs)
	if err != nil {
		panic(fmt.Errorf("failed to create socket event manager: %w", err))
	}

	// Initialize TLS probes
	logger.Info("Starting TLS Probes", zap.String("probes", tlsProbes))
	tlsManager, err := initTLSProbes(logger, tlsProbes, &tapObjs)
	if err != nil {
		panic(fmt.Errorf("failed to initialize TLS probes: %w", err))
	}
	if tlsManager != nil {
		// add tls probes as process observers
		pm.Observe(tlsManager)

		defer func() {
			if err := tlsManager.Stop(); err != nil {
				logger.Error("unable to cleanup tls probes manager", zap.Error(err))
			}
		}()
	}

	// Start managers
	// Start the proc manager
	if err := pm.Start(); err != nil {
		panic(fmt.Errorf("failed to start process manager: %w", err))
	}

	// cleanup the process manager
	defer func() {
		if err := pm.Stop(); err != nil {
			logger.Error("unable to cleanup process manager")
		}
	}()

	// start the socket manager
	if err := socketManager.Start(); err != nil {
		panic(fmt.Errorf("failed to start socket listener: %w", err))
	}
	defer func() {
		if err := socketManager.Stop(); err != nil {
			logger.Error("unable to cleanup socket listener")
		}
	}()

	// Initialize status server
	s := status.NewBaseStatusServer(statusListen, logger, telemetry.Handler(), func() bool {
		return true
	})
	if err := s.Start(); err != nil {
		logger.Fatal("failed to start status server", zap.Error(err))
	}
	defer func() {
		if err := s.Stop(); err != nil {
			logger.Error("unable to cleanup status server")
		}
	}()

	logger.Info("eBPF program loaded and listening")

	// trap int/term signals
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	logger.Info("shutting down")
}

// parseDeploymentTags parses the deployment tags string into a tags.List
func parseDeploymentTags() (tags.List, error) {
	t := tags.New()
	for _, tag := range strings.Split(deploymentTags, ",") {
		if err := t.AddString(tag); err != nil {
			return nil, err
		}
	}
	return t, nil
}

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

func setupTelemetry(ctx context.Context, service string) (func(context.Context) error, error) {
	var tracingNotConfigured bool
	otel.SetTextMapPropagator(autoprop.NewTextMapPropagator())
	traceExporter, err := autoexport.NewSpanExporter(ctx, autoexport.WithFallbackSpanExporter(func(ctx context.Context) (oteltracesdk.SpanExporter, error) {
		tracingNotConfigured = true
		return telemetry.NoopSpanExporter{}, nil
	}))
	if err != nil {
		return nil, fmt.Errorf("creating trace exporter: %w", err)
	}

	var (
		tracerProvider oteltrace.TracerProvider
		shutdown       func(context.Context) error
	)
	if tracingNotConfigured {
		tracerProvider = noop.NewTracerProvider()
		shutdown = func(context.Context) error { return nil }
	} else {
		otelResource, err := telemetry.OtelResource(ctx, service)
		if err != nil {
			return nil, fmt.Errorf("creating otel resource: %w", err)
		}
		tp := oteltracesdk.NewTracerProvider(
			oteltracesdk.WithBatcher(traceExporter),
			oteltracesdk.WithResource(otelResource),
		)
		shutdown = tp.Shutdown
		tracerProvider = tp
	}
	otel.SetTracerProvider(tracerProvider)
	return shutdown, nil
}
