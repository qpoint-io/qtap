//go:build linux

package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/buildinfo"
	"github.com/qpoint-io/qtap/pkg/ca"
	"github.com/qpoint-io/qtap/pkg/cap"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/config/remote"
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/container"
	"github.com/qpoint-io/qtap/pkg/devtools"
	"github.com/qpoint-io/qtap/pkg/dns"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	ebpfProcess "github.com/qpoint-io/qtap/pkg/ebpf/process"
	"github.com/qpoint-io/qtap/pkg/ebpf/socket"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls/gotls"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls/javassl"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls/nodetls"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls/openssl"
	"github.com/qpoint-io/qtap/pkg/ebpf/trace"
	"github.com/qpoint-io/qtap/pkg/egress"
	egresEbpf "github.com/qpoint-io/qtap/pkg/egress/ebpf"
	"github.com/qpoint-io/qtap/pkg/heartbeat"
	"github.com/qpoint-io/qtap/pkg/httpclient"
	"github.com/qpoint-io/qtap/pkg/kernel"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/accesslogs"
	"github.com/qpoint-io/qtap/pkg/plugins/dlp"
	"github.com/qpoint-io/qtap/pkg/plugins/errordetection"
	httpmetrics "github.com/qpoint-io/qtap/pkg/plugins/http"
	"github.com/qpoint-io/qtap/pkg/plugins/httpcapture"
	"github.com/qpoint-io/qtap/pkg/plugins/llm"
	"github.com/qpoint-io/qtap/pkg/plugins/logger"
	"github.com/qpoint-io/qtap/pkg/plugins/qscan"
	"github.com/qpoint-io/qtap/pkg/plugins/report"
	"github.com/qpoint-io/qtap/pkg/plugins/wrapper"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/register"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/reporter"
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
	"golang.org/x/term"
)

var (
	tlsProbes                string
	sanCertMaxSize           int
	dockerSocketEndpoint     string
	containerdSocketEndpoint string
	criRuntimeSocketEndpoint string
	enableDevTools           bool
	javasslLoaderBasePath    string
	javasslAgentBasePath     string
	enableEgressController   bool
	registrationToken        string
	registrationEndpoint     string
)

var (
	pluginFactories = []plugins.Plugin{
		wrapper.Catch(&logger.Factory{}),
		wrapper.Catch(&report.Factory{}),
		wrapper.Catch(accesslogs.NewConsoleJSONFilter()),
		wrapper.Catch(accesslogs.NewConsoleHttpFilter()),
		wrapper.Catch(&httpcapture.Factory{}),
		wrapper.Catch(&httpmetrics.Factory{}),

		// Pro plugins
		wrapper.Catch(&dlp.Factory{}),
		wrapper.Catch(&errordetection.Factory{}),
		wrapper.Catch(&qscan.Factory{}),
		wrapper.Catch(&llm.Factory{}),
	}

	persistentPlugins []config.Plugin
)

func init() {
	// Common options
	rootCmd.Flags().StringVar(&qpointConfig, "config",
		getEnvOr("QPOINT_CONFIG", ""),
		"Configuration file path or URL (starting with http:// or https://)")
	_ = rootCmd.Flags().Int("audit-log-buffer-size", 0, "[deprecated]")
	_ = rootCmd.Flags().MarkDeprecated("audit-log-buffer-size", "this flag is no longer applicable to the new audit log implementation")
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
	rootCmd.Flags().BoolVar(&enableEgressController, "enable-egress-controller",
		egressControllerEnabledFromEnv(),
		"Enable the egress controller (MITM forwarding with CA injection)")

	// Control-plane registration options
	rootCmd.Flags().StringVar(&registrationEndpoint, "registration-endpoint",
		getEnvOr("REGISTRATION_ENDPOINT", "https://api.qpoint.io"),
		"Control-plane registration endpoint")
	rootCmd.Flags().StringVar(&registrationToken, "registration-token",
		getEnvOr("REGISTRATION_TOKEN", ""),
		"Control-plane registration token (enables managed config, heartbeats, and live updates)")

	// Initialize flags with environment variable fallbacks
	rootCmd.Flags().StringVar(&tlsProbes, "tls-probes",
		getEnvOr("TLS_PROBES", "nodetls,openssl,gotls,javassl"),
		"Comma-separated list of TLS probes to use")
	rootCmd.Flags().StringVar(&javasslLoaderBasePath, "javassl-loader-base-path",
		getEnvOr("JAVASSL_LOADER_BASE_PATH", javassl.DefaultExecutionBasePath),
		"Writable executable directory in Qtap's namespace for the Java attachment runtime")
	rootCmd.Flags().StringVar(&javasslAgentBasePath, "javassl-agent-base-path",
		getEnvOr("JAVASSL_AGENT_BASE_PATH", javassl.DefaultExecutionBasePath),
		"Writable executable directory in target Java process namespaces for probe assets")

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

	// http server options
	rootCmd.Flags().StringVar(&httpdListen, "status-listen",
		getEnvOr("STATUS_LISTEN", "0.0.0.0:10001"),
		"IP:PORT of status server to listen on")
	_ = rootCmd.Flags().MarkDeprecated("status-listen", "use --httpd-listen instead")

	rootCmd.Flags().StringVar(&httpdListen, "httpd-listen",
		getEnvOr("HTTPD_LISTEN", "0.0.0.0:10001"),
		"IP:PORT of qtap http server to listen on")

	// Dev Tools options
	rootCmd.Flags().BoolVar(&enableDevTools, "enable-dev-tools",
		getEnvBoolOr("ENABLE_DEV_TOOLS", false),
		"Enable local Dev Tools server")
}

func egressControllerEnabledFromEnv() bool {
	return getEnvBoolOr("ENABLE_EGRESS_CONTROLLER", false)
}

// This skeleton version of runrootCmd provides the basic structure
// but will need to be fleshed out with actual implementation
func runTapCmd(logger *zap.Logger) (retErr error) {
	ctx, cancelRoot := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancelRoot()
	shutdown := newShutdownBudget(shutdownTimeout)
	defer shutdown.Close()

	shutdownTelemetry, err := setupTelemetry(ctx, "tap")
	if err != nil {
		return fmt.Errorf("setup telemetry: %w", err)
	}
	defer func() {
		if err := runCleanup(shutdown.Context(), func() error { return shutdownTelemetry(shutdown.Context()) }); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("shutdown telemetry: %w", err))
		}
	}()

	// Log startup information
	logger.Info("Starting Qtap",
		zap.String("version", buildinfo.Version()),
		zap.Strings("tags", strings.Split(deploymentTags, ",")),
		telemetry.GetSysInfoAsFields(),
	)

	meetsMinimumKernel, err := kernel.CheckVersion(5, 10, 0)
	if err != nil {
		return fmt.Errorf("check kernel version: %w", err)
	}
	if !meetsMinimumKernel {
		return errors.New("Qtap requires kernel version 5.10 or greater")
	}

	// Check if running as root (required for eBPF)
	if syscall.Getuid() != 0 {
		return errors.New("this program requires root privileges to load BPF programs and maps; run as root or with sudo")
	}

	var ready atomic.Bool
	statusServer := status.NewBaseStatusServer(httpdListen, logger, ready.Load)
	if err := statusServer.Start(); err != nil {
		return fmt.Errorf("start status server: %w", err)
	}
	defer func() {
		ready.Store(false)
		if err := runCleanup(shutdown.Context(), statusServer.Stop); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("stop status server: %w", err))
		}
	}()

	// Parse deployment tags if provided
	var dTags tags.List
	if deploymentTags != "" {
		var err error
		dTags, err = parseDeploymentTags()
		if err != nil {
			logger.Error("failed to parse deployment tags", zap.Error(err))
		}
	}

	// Ensure a registration token passed by flag is also visible via the
	// REGISTRATION_TOKEN env var, which ValueSource-based config (pulse,
	// warehouse) reads to resolve the Qpoint token consistently.
	if registrationToken != "" {
		if t := os.Getenv("REGISTRATION_TOKEN"); t != registrationToken {
			_ = os.Setenv("REGISTRATION_TOKEN", registrationToken)
		}
	}

	// Create config provider based on command line flags
	var provider config.ConfigProvider

	// Setup configuration context
	configCtx, configCancel := context.WithCancel(ctx)
	defer configCancel()

	// Initialize a config provider: explicit config file, control-plane
	// registration, or the built-in default (in that precedence order).
	switch {
	case qpointConfig != "":
		provider = config.NewLocalConfigProvider(logger, qpointConfig)
	case registrationToken != "":
		var registrationCleanup cleanupFunc
		provider, registrationCleanup, err = newRegistrationProvider(ctx, logger)
		if err != nil {
			return err
		}
		defer func() {
			if err := runCleanup(shutdown.Context(), func() error {
				return registrationCleanup(shutdown.Context())
			}); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("stop registration services: %w", err))
			}
		}()
	default:
		logger.Warn("no config file provided, using default config")
		provider = config.NewDefaultConfigProvider(logger, enableDevTools)
	}

	// Create and start config manager
	configManager := config.NewConfigManager(logger, provider)
	if err := configManager.Run(configCtx); err != nil {
		return fmt.Errorf("start config manager: %w", err)
	}

	// Capture SIGHUP now, but do not consume it until all subscribers are wired.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	// Load BPF programs and maps
	logger.Info("loading BPF programs and maps")
	spec, err := tap.LoadTap()
	if err != nil {
		return fmt.Errorf("load BPF programs and maps: %w", err)
	}
	// write the current pid to the bpf program
	err = spec.RewriteConstants(map[string]any{
		"qpid": uint32(os.Getpid()),
	})
	if err != nil {
		return fmt.Errorf("rewrite BPF constants: %w", err)
	}
	tapObjs := tap.TapObjects{}
	err = spec.LoadAndAssign(&tapObjs, nil)
	if err != nil {
		return fmt.Errorf("assign BPF programs and maps: %w", err)
	}
	defer func() {
		if err := runCleanup(shutdown.Context(), tapObjs.Close); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close BPF objects: %w", err))
		}
	}()

	// Initialize process manager
	procEbpfMan, err := NewEbpfProcManager(logger, &tapObjs)
	if err != nil {
		return fmt.Errorf("create eBPF process manager: %w", err)
	}

	pm := process.NewProcessManager(logger, procEbpfMan)
	if err := configManager.SubscribeSetter(pm); err != nil {
		return fmt.Errorf("apply initial process configuration: %w", err)
	}

	// Initialize container detection
	containerManager := container.NewManager(logger, dockerSocketEndpoint, containerdSocketEndpoint, criRuntimeSocketEndpoint)
	if err := containerManager.Start(ctx); err != nil {
		return fmt.Errorf("start container manager: %w", err)
	}
	pm.Observe(process.NewContainerEnricher(containerManager))

	// Initialize BPF trace manager
	tm, err := trace.NewTraceManager(logger, tapObjs.TraceToggleMap, tapObjs.TraceEvents, pm, bpfTraceQuery)
	if err != nil {
		return fmt.Errorf("create BPF trace manager: %w", err)
	}

	// start the bpf trace manager
	if err := tm.Start(); err != nil {
		return fmt.Errorf("start BPF trace manager: %w", err)
	}

	// add the bpf trace manager as a process observer
	pm.Observe(tm)

	// cleanup the bpf trace manager
	defer func() {
		if err := runCleanup(shutdown.Context(), tm.Stop); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("stop BPF trace manager: %w", err))
		}
	}()

	// Initialize DNS resolver
	resolv := dns.NewDNSManager(logger, pm)
	if err := resolv.Start(); err != nil {
		return fmt.Errorf("start DNS manager: %w", err)
	}
	defer func() {
		if err := runCleanup(shutdown.Context(), resolv.Stop); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("stop DNS manager: %w", err))
		}
	}()

	// Parse HTTP buffer size
	httpBufsize, err := parseSizeString(httpBufferSize)
	if err != nil {
		return fmt.Errorf("parse HTTP buffer size: %w", err)
	}

	var devtoolsManager *devtools.Manager
	if enableDevTools {
		devtoolsManager = devtools.NewManager(
			devtools.WithLogger(logger),
			devtools.WithProcessSnapshotter(pm),
		)

		// set up process observer
		pm.Observe(devtoolsManager)

		// register the dev tools stores
		devtoolsEventStoreFactory := devtoolsManager.EventStoreFactory()
		devtoolsObjectStoreFactory := devtoolsManager.ObjectStoreFactory()
		serviceFactories = append(serviceFactories,
			func() services.Factory { return devtoolsEventStoreFactory },
			func() services.Factory { return devtoolsObjectStoreFactory },
		)

		// register dev tools service configs
		extraServiceConfigs = append(extraServiceConfigs,
			// event store
			func(cfg *config.Config) *config.ServiceConfig {
				return &config.ServiceConfig{
					ID:   "devtools",
					Type: devtoolsEventStoreFactory.FactoryType().String(),
				}
			},
			// object store
			func(cfg *config.Config) *config.ServiceConfig {
				return &config.ServiceConfig{
					ID:   "devtools",
					Type: devtoolsObjectStoreFactory.FactoryType().String(),
				}
			},
			// connection reporter
			func(cfg *config.Config) *config.ServiceConfig {
				return &config.ServiceConfig{
					Type: reporter.Type.String(),
					Config: &reporter.Config{
						EventStoreID:        "devtools",
						FirstReportDeadline: 100 * time.Millisecond,
						ReportInterval:      1 * time.Second,
					},
				}
			},
		)

		// register plugin
		persistentPlugins = append(persistentPlugins, config.Plugin{
			Type: string(devtools.PluginTypeDevTools),
		})
		pluginFactories = append(pluginFactories, wrapper.Catch(devtoolsManager.PluginFactory()))
	}

	// Initialize service and plugin systems
	svcFactoryRegistry := services.NewFactoryRegistry()
	svcManager := services.NewFactoryManager(context.WithoutCancel(ctx), logger, svcFactoryRegistry)
	svcManager.AddExtraServices(extraServiceConfigs...)
	svcManager.RegisterFactory(serviceFactories...)
	if err := configManager.SubscribeSetter(svcManager); err != nil {
		return fmt.Errorf("apply initial service configuration: %w", err)
	}
	pulseHeartbeatStop, err := startPulseHeartbeat(logger, configManager)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, stopPulseHeartbeat(shutdown.Context(), pulseHeartbeatStop))
	}()
	defer func() {
		if err := runCleanup(shutdown.Context(), func() error {
			return shutdownServices(shutdown.Context(), svcManager)
		}); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()

	pluginRegistry := plugins.NewRegistry(pluginFactories...)
	pluginManager := plugins.NewPluginManager(
		logger,
		plugins.SetBufferSize(int(httpBufsize)),
		plugins.SetPluginRegistry(pluginRegistry),
		plugins.AddPersistentPlugins(persistentPlugins...),
	)
	if err := configManager.SubscribeSetter(pluginManager); err != nil {
		return fmt.Errorf("apply initial plugin configuration: %w", err)
	}
	if err := pluginManager.Start(); err != nil {
		return fmt.Errorf("start plugin manager: %w", err)
	}
	defer func() {
		if err := runCleanup(shutdown.Context(), func() error {
			pluginManager.Stop()
			return nil
		}); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("stop plugin manager: %w", err))
		}
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
		connection.SetServiceFactoryRegistry(svcFactoryRegistry),
		connection.SetConfig(configManager.GetConfig()),
		connection.SetDeploymentTags(dTags),
	)

	// Subscribe connection manager to config changes
	if err := configManager.SubscribeSetter(connectionManager); err != nil {
		return fmt.Errorf("apply initial connection configuration: %w", err)
	}
	defer func() {
		retErr = errors.Join(retErr, runCleanup(shutdown.Context(), func() error {
			return shutdownConnections(shutdown.Context(), connectionManager)
		}))
	}()

	// init a socket settings manager to push config changes
	// down into ebpf land
	socketSettingManager := socket.NewSocketSettingsManager(logger, tapObjs.TapMaps.SocketSettingsMap)

	// Subscribe socket settings manager to config changes
	if err := configManager.SubscribeSetter(socketSettingManager); err != nil {
		return fmt.Errorf("apply initial socket configuration: %w", err)
	}

	// Initialize socket manager
	socketManager, err := NewEbpfSockManager(logger, connectionManager, &tapObjs)
	if err != nil {
		return fmt.Errorf("create socket event manager: %w", err)
	}

	// initialize egress controller
	egressCleanup, err := initOptionalEgressController(ctx, logger, pm, connectionManager, &tapObjs)
	if err != nil {
		return fmt.Errorf("initialize egress controller: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, stopEgressController(shutdown.Context(), egressCleanup)) }()

	// Initialize TLS probes
	logger.Info("starting TLS Probes", zap.String("probes", tlsProbes))
	tlsManager, err := InitTLSProbes(ctx, logger, tlsProbes, &tapObjs, connectionManager, configManager)
	if err != nil {
		return fmt.Errorf("initialize TLS probes: %w", err)
	}
	if tlsManager != nil {
		// add tls probes as process observers
		pm.Observe(tlsManager)

		defer func() {
			if err := runCleanup(shutdown.Context(), tlsManager.Close); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("stop TLS probes: %w", err))
			}
		}()
	}

	// All config subscribers are now wired and have synchronously applied the
	// current generation, so reloads can be consumed without stale replay.
	go watchConfigReloads(configCtx, sigCh, logger, configManager)

	// Start managers
	// Start the proc manager
	if err := pm.Start(); err != nil {
		return fmt.Errorf("start process manager: %w", err)
	}

	// cleanup the process manager
	defer func() {
		if err := runCleanup(shutdown.Context(), pm.Stop); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("stop process manager: %w", err))
		}
	}()

	// start the socket manager
	if err := socketManager.Start(); err != nil {
		return fmt.Errorf("start socket listener: %w", err)
	}
	defer func() {
		if err := runCleanup(shutdown.Context(), socketManager.Stop); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("stop socket listener: %w", err))
		}
	}()

	registerDevtoolsRoutes(logger, statusServer, devtoolsManager)

	ready.Store(true)
	logger.Info("eBPF program loaded and listening")

	if err := waitForTapShutdown(ctx, statusServer); err != nil {
		retErr = fmt.Errorf("status server failed: %w", err)
		cancelRoot()
	}
	ready.Store(false)
	logger.Info("shutting down")
	return retErr
}

func watchConfigReloads(ctx context.Context, sigCh <-chan os.Signal, logger *zap.Logger, manager *config.ConfigManager) {
	for {
		select {
		case <-sigCh:
			logger.Info("SIGHUP received, reloading configuration")
			if err := manager.Reload(); err != nil {
				logger.Error("failed to reload config on SIGHUP", zap.Error(err))
			}
		case <-ctx.Done():
			return
		}
	}
}

func registerDevtoolsRoutes(logger *zap.Logger, server *status.BaseStatusServer, manager *devtools.Manager) {
	if manager == nil {
		return
	}
	if err := manager.RegisterRoutes(server.Mux(), "/devtools"); err != nil {
		logger.Error("failed to register devtools routes", zap.Error(err))
		return
	}
	server.Mux().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/devtools/", http.StatusTemporaryRedirect)
	})

	devtoolsURL := "http://" + httpdListen + "/devtools"
	if term.IsTerminal(int(os.Stdout.Fd())) {
		PrintDevToolsBox(devtoolsURL)
	}
	logger.Info("devtools running", zap.String("url", devtoolsURL))
}

func waitForTapShutdown(ctx context.Context, server status.StatusServer) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-server.Errors():
		return err
	}
}

func stopPulseHeartbeat(ctx context.Context, stop func(context.Context) error) error {
	if err := stop(ctx); err != nil {
		return fmt.Errorf("stop pulse heartbeat: %w", err)
	}
	return nil
}

func shutdownServices(ctx context.Context, manager *services.FactoryManager) error {
	if err := manager.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown services: %w", err)
	}
	return nil
}

func shutdownConnections(ctx context.Context, manager *connection.Manager) error {
	if err := manager.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown connections: %w", err)
	}
	return nil
}

// parseDeploymentTags parses the deployment tags string into a tags.List
func parseDeploymentTags() (tags.List, error) {
	t := tags.New()
	for tag := range strings.SplitSeq(deploymentTags, ",") {
		if err := t.AddString(tag); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// PrintDevToolsBox prints a nicely formatted box with the devtools URL
func PrintDevToolsBox(url string) {
	purple := lipgloss.Color("#A855F7")

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#9CA3AF")).
		Bold(true)

	urlStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C026D3")).
		Bold(true).
		Underline(true)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(purple).
		Padding(1, 2)

	content := titleStyle.Render("QTap DevTools is running at:") + "\n\n" +
		urlStyle.Render(url)

	fmt.Println()
	fmt.Println(boxStyle.Render(content))
	fmt.Println()
}

func NewEbpfProcManager(logger *zap.Logger, objs *tap.TapObjects) (*ebpfProcess.Manager, error) {
	procManTps := []*common.Tracepoint{
		common.NewTracepoint("syscalls", "sys_enter_execve", objs.TapPrograms.SyscallProbeEntryExecve),
		common.NewTracepoint("syscalls", "sys_exit_execve", objs.TapPrograms.SyscallProbeRetExecve),
		common.NewTracepoint("syscalls", "sys_enter_execveat", objs.TapPrograms.SyscallProbeEntryExecveat),
		common.NewTracepoint("syscalls", "sys_exit_execveat", objs.TapPrograms.SyscallProbeRetExecveat),
		common.NewTracepoint("syscalls", "sys_enter_exit_group", objs.TapPrograms.SyscallProbeEntryExitGroup),
		common.NewTracepoint("sched", "sched_process_exit", objs.TapPrograms.TracepointSchedProcessExit),
	}

	procManRB, err := ringbuf.NewReader(objs.TapMaps.ProcEvents)
	if err != nil {
		return nil, fmt.Errorf("failed to create proc event reader: %w", err)
	}

	procMan := ebpfProcess.New(logger, objs.TapMaps.ProcessMetaMap, procManRB, procManTps)

	return procMan, nil
}

// newRegistrationProvider connects to the managed control plane using the
// registration token: it starts the deploy heartbeat, fetches registration,
// and returns a remote config provider fed by live Ably updates, plus a
// cleanup func to stop the heartbeat and close the Ably connection.
func newRegistrationProvider(ctx context.Context, logger *zap.Logger) (config.ConfigProvider, cleanupFunc, error) {
	var cleanups []cleanupFunc
	cleanup := func(ctx context.Context) error { return runCleanups(ctx, cleanups) }

	// start the deploy heartbeat (every 24h)
	hb, err := heartbeat.New(logger, registrationEndpoint, registrationToken, time.Hour*24, heartbeat.Deploy)
	if err != nil {
		return nil, cleanup, fmt.Errorf("create deploy heartbeat: %w", err)
	}
	if err := hb.Start(); err != nil {
		return nil, cleanup, fmt.Errorf("start deploy heartbeat: %w", err)
	}
	cleanups = append(cleanups, func(ctx context.Context) error {
		if err := hb.StopContext(ctx); err != nil {
			return fmt.Errorf("stop deploy heartbeat: %w", err)
		}
		return nil
	})

	// use the control-plane client to fetch registration
	qpointClient := httpclient.New(registrationToken)
	registration, err := register.FetchRegistration(qpointClient, registrationEndpoint)
	if err != nil {
		return nil, func(context.Context) error { return nil }, errors.Join(
			fmt.Errorf("fetch registration: %w", err),
			cleanup(ctx),
		)
	}

	remoteProvider := remote.NewRemoteConfigProvider(logger, registrationEndpoint, qpointClient)

	// setup the Ably updater for live config pushes
	ablyUpdater := remote.NewAblyUpdater(logger, registration.AblyToken)
	if err := ablyUpdater.Init(); err != nil {
		return nil, func(context.Context) error { return nil }, errors.Join(
			fmt.Errorf("initialize Ably updater: %w", err),
			cleanup(ctx),
		)
	}
	cleanups = append(cleanups, func(context.Context) error {
		if err := ablyUpdater.Close(); err != nil {
			return fmt.Errorf("close Ably updater: %w", err)
		}
		return nil
	})

	if err := remoteProvider.RegisterWatcher(
		ablyUpdater,
		registration.OrgID+"-deploy",
		"config.changed",
		time.Second*5,
		100,
	); err != nil {
		return nil, func(context.Context) error { return nil }, errors.Join(
			fmt.Errorf("register Ably watcher: %w", err),
			cleanup(ctx),
		)
	}

	return remoteProvider, cleanup, nil
}

// startPulseHeartbeat starts a 5-minute pulse heartbeat if the active config
// has a pulse eventstore, using its credentials. Returns a no-op cleanup when
// no pulse eventstore is configured.
func startPulseHeartbeat(logger *zap.Logger, configManager *config.ConfigManager) (func(context.Context) error, error) {
	cfg := configManager.GetConfig()
	if !cfg.Services.HasAnyEventStores() {
		return func(context.Context) error { return nil }, nil
	}
	for _, es := range cfg.Services.EventStores {
		if es.Type != config.EventStoreType_PULSE {
			continue
		}
		endpoint := es.URL
		if endpoint == "" {
			endpoint = DefaultPulseURL
		}
		hb, err := heartbeat.New(logger, endpoint, es.Token.String(), time.Minute*5, heartbeat.Pulse)
		if err != nil {
			return nil, fmt.Errorf("create pulse heartbeat: %w", err)
		}
		if err := hb.Start(); err != nil {
			return nil, fmt.Errorf("start pulse heartbeat: %w", err)
		}
		return hb.StopContext, nil
	}
	return func(context.Context) error { return nil }, nil
}

func initOptionalEgressController(ctx context.Context, logger *zap.Logger, pm *process.Manager, connectionManager *connection.Manager, tapObjs *tap.TapObjects) (cleanupFunc, error) {
	if !enableEgressController {
		return func(context.Context) error { return nil }, nil
	}
	return initEgressController(ctx, logger, pm, connectionManager, tapObjs)
}

func stopEgressController(ctx context.Context, cleanup cleanupFunc) error {
	err := runCleanup(ctx, func() error {
		return cleanup(ctx)
	})
	if err != nil {
		return fmt.Errorf("stop egress controller: %w", err)
	}
	return nil
}

// initEgressController wires up the MITM egress controller (cert store, CA
// injection, and traffic router) and returns a cleanup func to stop it.
func initEgressController(ctx context.Context, logger *zap.Logger, pm *process.Manager, connectionManager *connection.Manager, tapObjs *tap.TapObjects) (cleanupFunc, error) {
	var cleanups []cleanupFunc
	cleanup := func(ctx context.Context) error { return runCleanups(ctx, cleanups) }

	// initialize a cert store
	certStore := egress.NewCertStore(sanCertMaxSize, logger)
	if err := certStore.Init(); err != nil {
		return nil, fmt.Errorf("initialize certificate store: %w", err)
	}

	// get the root cert bytes
	rootCert, err := certStore.GetRootCertBytes()
	if err != nil {
		return nil, fmt.Errorf("get root certificate: %w", err)
	}

	logger.Info("starting certificate injector")
	certsObjs := tap.CertsObjects{}
	strategy := ca.StrategyFromString(certInjectionStrategy)

	if strategy == ca.InjectStrategyEbpf {
		if err := cap.CanBpfProbeWriteUser(); err != nil {
			if errors.Is(err, cap.ErrBpfProbeWriteUser) {
				return nil, fmt.Errorf("bpf_probe_write_user is not allowed for ebpf certificate injection: %w", err)
			}
			return nil, fmt.Errorf("check bpf_probe_write_user capability: %w", err)
		}

		if err := tap.LoadCertsObjects(&certsObjs, nil); err != nil {
			return nil, fmt.Errorf("load certificate BPF programs and maps: %w", err)
		}
		cleanups = append(cleanups, func(context.Context) error {
			if err := certsObjs.Close(); err != nil {
				return fmt.Errorf("close certificate BPF objects: %w", err)
			}
			return nil
		})
	}

	// init a ca manager
	caManager := ca.NewCaManager(rootCert, strategy, logger, &certsObjs, tapObjs, pm)
	if err := caManager.Start(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("start ca manager: %w", err), cleanup(ctx))
	}
	cleanups = append(cleanups, func(context.Context) error {
		if err := caManager.Stop(); err != nil {
			return fmt.Errorf("stop ca manager: %w", err)
		}
		return nil
	})

	// add the ca manager as a process observer
	pm.Observe(caManager)

	// add JAVA_HOME to process manager env mask
	pm.MaskEnvVars([]string{"JAVA_HOME"})

	// set the ssl cert env vars to process manager env mask
	pm.MaskEnvVars(ca.SslCertEnvVars)
	pm.MaskEnvVars(ca.KeystoreEnvVars)

	// init a router
	router, err := egresEbpf.NewRouter(logger, tapObjs)
	if err != nil && !errors.Is(err, cap.ErrCgroupsV2NotEnabled) {
		return nil, errors.Join(fmt.Errorf("create egress router: %w", err), cleanup(ctx))
	}

	if router != nil {
		logger.Info("starting egress controller")
		tlsOk := egress.TLSOkStrategyFromString(tlsOkStrategy)
		m := egress.NewEgressManager(certStore, logger, router, tlsOk, egress.WithConnEventer(connectionManager))
		if err := m.Start(); err != nil {
			return nil, errors.Join(fmt.Errorf("start egress manager: %w", err), cleanup(ctx))
		}
		cleanups = append(cleanups, func(context.Context) error {
			if err := m.Stop(); err != nil {
				return fmt.Errorf("stop egress manager: %w", err)
			}
			return nil
		})

		// add egress manager as a ca observer
		caManager.Observe(m)
	}

	return cleanup, nil
}

func InitTLSProbes(ctx context.Context, logger *zap.Logger, tlsProbesStr string, objs *tap.TapObjects, connEvents *connection.Manager, configManager *config.ConfigManager) (*tls.TlsManager, error) {
	// Split the string and trim whitespace
	tlsProbesList := strings.Split(tlsProbesStr, ",")
	for i, probe := range tlsProbesList {
		tlsProbesList[i] = strings.TrimSpace(probe)
	}

	enableTLS := true
	var probes []tls.Probe
	for _, mode := range tlsProbesList {
		mode = strings.ToLower(mode)
		switch mode {
		case "javassl":
			// create the java ssl engine bridge
			sslEngineBridge, err := newEbpfJavaSslEngineBridge(objs)
			if err != nil {
				return nil, fmt.Errorf("creating javassl engine bridge: %w", err)
			}
			sslEngineManager := javassl.NewSslEngineManager(logger, sslEngineBridge, connEvents)
			if err := sslEngineManager.Start(); err != nil {
				return nil, fmt.Errorf("starting javassl engine bridge: %w", err)
			}

			probes = append(probes, javassl.NewProbeWithExecutionPaths(
				ctx,
				logger,
				sslEngineManager,
				newEbpfJavaSslProbesCreator(objs),
				javasslLoaderBasePath,
				javasslAgentBasePath,
			))

			// add the ssl engine manager as a config subscriber
			if err := configManager.SubscribeSetter(sslEngineManager); err != nil {
				_ = sslEngineManager.Stop()
				return nil, fmt.Errorf("applying initial Java SSL configuration: %w", err)
			}
		case "nodetls":
			probes = append(probes, nodetls.NewProbe(logger, objs.TapMaps.NodeTlsSymaddrsMap, newEbpfNodeTlsProbesCreator(objs)))
		case "gotls":
			probes = append(probes, gotls.NewProbe(logger, objs.TapMaps.GoTlsSymaddrsMap, newEbpfGoTlsProbesCreator(objs)))
		case "openssl":
			probe := openssl.NewProbe(logger, NewEbpfOpenSSLprobesCreator(objs))
			probes = append(probes, probe)
		case "none", "":
			enableTLS = false
			logger.Info("No TLS probes enabled")
		default:
			logger.Warn("Unknown TLS probe specified", zap.String("probe", mode))
		}
	}

	if enableTLS || len(probes) > 0 {
		// init tls probes manager
		scanner := tls.NewTargetScanner(logger, probes)
		manager := tls.NewTlsManager(logger, scanner)
		return manager, nil
	}

	return nil, nil
}

func NewEbpfSockManager(logger *zap.Logger, connMan *connection.Manager, objs *tap.TapObjects) (*socket.SocketEventManager, error) {
	// set the tracepoints (⚠️ order is important!)
	tps := []common.Probe{
		// sni tracepoints
		common.NewTracepoint("syscalls", "sys_exit_sendto", objs.TapPrograms.SyscallProbeRetSendtoInit),
		common.NewTracepoint("syscalls", "sys_exit_sendmsg", objs.TapPrograms.SyscallProbeRetSendmsgInit),
		common.NewTracepoint("syscalls", "sys_exit_write", objs.TapPrograms.SyscallProbeRetWriteInit),
		common.NewTracepoint("syscalls", "sys_exit_writev", objs.TapPrograms.SyscallProbeRetWritevInit),
		common.NewTracepoint("syscalls", "sys_exit_recvfrom", objs.TapPrograms.SyscallProbeRetRecvfromInit),
		common.NewTracepoint("syscalls", "sys_exit_recvmsg", objs.TapPrograms.SyscallProbeRetRecvmsgInit),
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
		common.NewTracepoint("syscalls", "sys_enter_sendmsg", objs.TapPrograms.SyscallProbeEntrySendmsg),
		common.NewTracepoint("syscalls", "sys_exit_sendmsg", objs.TapPrograms.SyscallProbeRetSendmsg),
		common.NewTracepoint("syscalls", "sys_enter_read", objs.TapPrograms.SyscallProbeEntryRead),
		common.NewTracepoint("syscalls", "sys_enter_readv", objs.TapPrograms.SyscallProbeEntryReadv),
		common.NewTracepoint("syscalls", "sys_exit_read", objs.TapPrograms.SyscallProbeRetRead),
		common.NewTracepoint("syscalls", "sys_exit_readv", objs.TapPrograms.SyscallProbeRetReadv),
		common.NewTracepoint("syscalls", "sys_enter_recvfrom", objs.TapPrograms.SyscallProbeEntryRecvfrom),
		common.NewTracepoint("syscalls", "sys_exit_recvfrom", objs.TapPrograms.SyscallProbeRetRecvfrom),
		common.NewTracepoint("syscalls", "sys_enter_recvmsg", objs.TapPrograms.SyscallProbeEntryRecvmsg),
		common.NewTracepoint("syscalls", "sys_exit_recvmsg", objs.TapPrograms.SyscallProbeRetRecvmsg),
		common.NewTracepoint("syscalls", "sys_enter_socket", objs.TapPrograms.SyscallProbeEntrySocket),
		common.NewTracepoint("syscalls", "sys_exit_socket", objs.TapPrograms.SyscallProbeRetSocket),

		// pid/fd mapping kprobes
		common.NewKprobe(objs.TapPrograms.TrackSockAllocFileEntry, "sock_alloc_file"),
		common.NewKretprobe(objs.TapPrograms.TrackSockAllocFileRet, "sock_alloc_file"),
		common.NewKprobe(objs.TapPrograms.TrackFdInstallEntry, "fd_install"),
		common.NewKprobe(objs.TapPrograms.CleanupPidFdFileEntries, "__fput", "fput", "__pfx_fput", "__pfx___fput"),
		common.NewKprobe(objs.TapPrograms.TraceTcpClose, "tcp_close"),

		// ftraces
		common.NewFexit("tcp_v4_connect", objs.TapPrograms.TraceTcpV4ConnectFexit),
		common.NewFexit("tcp_v6_connect", objs.TapPrograms.TraceTcpV6ConnectFexit),
		common.NewFexit("tcp_recvmsg", objs.TapPrograms.TraceTcpRecvmsgFexit),
	}

	// open a ring buffer reader
	rb, err := ringbuf.NewReader(objs.TapMaps.SocketEvents)
	if err != nil {
		return nil, fmt.Errorf("creating socket event reader: %w", err)
	}

	return socket.NewSocketEventManager(logger, connMan, rb, tps), nil
}

// NewEbpfOpenSSLprobesCreator creates a function that returns a list of uprobes for the OpenSSL library
// this is used to create new probes for each many instances.
func NewEbpfOpenSSLprobesCreator(objs *tap.TapObjects) func() []*common.Uprobe {
	return func() []*common.Uprobe {
		return []*common.Uprobe{
			// ssl entry uprobes
			common.NewUprobe("SSL_read", objs.TapPrograms.OpensslProbeEntrySSL_read),
			common.NewUprobe("SSL_read_ex", objs.TapPrograms.OpensslProbeEntrySSL_readEx),
			common.NewUprobe("SSL_write", objs.TapPrograms.OpensslProbeEntrySSL_write),
			common.NewUprobe("SSL_write_ex", objs.TapPrograms.OpensslProbeEntrySSL_writeEx),
			common.NewUprobe("SSL_free", objs.TapPrograms.OpensslProbeEntrySSL_free),
			common.NewUprobe("SSL_set_fd", objs.TapPrograms.OpensslProbeEntrySSL_setFd),

			// ssl return uprobes
			common.NewUretprobe("SSL_read", objs.TapPrograms.OpensslProbeRetSSL_read),
			common.NewUretprobe("SSL_read_ex", objs.TapPrograms.OpensslProbeRetSSL_readEx),
			common.NewUretprobe("SSL_write", objs.TapPrograms.OpensslProbeRetSSL_write),
			common.NewUretprobe("SSL_write_ex", objs.TapPrograms.OpensslProbeRetSSL_writeEx),
			common.NewUretprobe("SSL_new", objs.TapPrograms.OpensslProbeRetSSL_new),

			// node required openssl uprobes
			common.NewUprobe("SSL_set_cert_cb", objs.TapPrograms.NodetlsProbeEntrySSL_setCertCb),
			common.NewUprobe("SSL_free", objs.TapPrograms.NodetlsProbeEntrySSL_free),
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
