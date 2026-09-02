//go:build linux

package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
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
		getEnvOr("ENABLE_EGRESS_CONTROLLER", "false") == "true",
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

// This skeleton version of runrootCmd provides the basic structure
// but will need to be fleshed out with actual implementation
func runTapCmd(logger *zap.Logger) {
	ctx, cancelRoot := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancelRoot()

	shutdownTelemetry, err := setupTelemetry(ctx, "tap")
	if err != nil {
		logger.Fatal("unable to setup telemetry", zap.Error(err))
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
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

	meetsMinimumKernel, err := kernel.CheckVersion(5, 10, 0)
	if err != nil {
		logger.Fatal("unable to check kernel version", zap.Error(err))
	}
	if !meetsMinimumKernel {
		logger.Fatal("Qtap requires kernel version 5.10 or greater.")
	}

	// Check if running as root (required for eBPF)
	if syscall.Getuid() != 0 {
		logger.Error("This program requires root privileges to load BPF programs and maps. Please run as root or with sudo.")
		defer os.Exit(1)
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

	// Ensure a registration token passed by flag is also visible via the
	// REGISTRATION_TOKEN env var, which ValueSource-based config (pulse,
	// warehouse) reads to resolve the Qpoint token consistently.
	if registrationToken != "" {
		if t := os.Getenv("REGISTRATION_TOKEN"); !strings.EqualFold(t, registrationToken) {
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
		var registrationCleanup func()
		provider, registrationCleanup = newRegistrationProvider(logger)
		defer registrationCleanup()
	default:
		logger.Warn("no config file provided, using default config")
		provider = config.NewDefaultConfigProvider(logger, enableDevTools)
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
	qpid, ok := spec.Variables["qpid"]
	if !ok {
		logger.Fatal("failed to find qpid constant")
	}
	if err = qpid.Set(uint32(os.Getpid())); err != nil {
		logger.Fatal("failed to rewrite constants", zap.Error(err))
	}
	tapObjs := tap.TapObjects{}
	err = spec.LoadAndAssign(&tapObjs, nil)
	if err != nil {
		logger.Fatal("failed to load BPF programs and maps", zap.Error(err))
	}
	defer tapObjs.Close()

	// Initialize process manager
	procEbpfMan, err := NewEbpfProcManager(logger, &tapObjs)
	if err != nil {
		logger.Fatal("failed to get ebpf proc objs", zap.Error(err))
	}

	pm := process.NewProcessManager(logger, procEbpfMan)
	configManager.SubscribeSetter(pm)

	// if the active config has a pulse eventstore, start a heartbeat with its creds
	defer startPulseHeartbeat(logger, configManager)()

	// Initialize container detection
	containerManager := container.NewManager(logger, dockerSocketEndpoint, containerdSocketEndpoint, criRuntimeSocketEndpoint)
	if err := containerManager.Start(ctx); err != nil {
		logger.Fatal("failed to start container manager", zap.Error(err))
	}
	pm.Observe(process.NewContainerEnricher(containerManager))

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
	svcManager := services.NewFactoryManager(ctx, logger, svcFactoryRegistry)
	svcManager.AddExtraServices(extraServiceConfigs...)
	svcManager.RegisterFactory(serviceFactories...)
	configManager.SubscribeSetter(svcManager)

	pluginRegistry := plugins.NewRegistry(pluginFactories...)
	pluginManager := plugins.NewPluginManager(
		logger,
		plugins.SetBufferSize(int(httpBufsize)),
		plugins.SetPluginRegistry(pluginRegistry),
		plugins.AddPersistentPlugins(persistentPlugins...),
	)
	configManager.SubscribeSetter(pluginManager)
	if err := pluginManager.Start(); err != nil {
		panic(fmt.Errorf("failed to start plugin manager: %w", err))
	}
	defer pluginManager.Stop()

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
	configManager.SubscribeSetter(connectionManager)

	// init a socket settings manager to push config changes
	// down into ebpf land
	socketSettingManager := socket.NewSocketSettingsManager(logger, tapObjs.SocketSettingsMap)

	// Subscribe socket settings manager to config changes
	configManager.SubscribeSetter(socketSettingManager)

	// Initialize socket manager
	socketManager, err := NewEbpfSockManager(logger, connectionManager, &tapObjs)
	if err != nil {
		panic(fmt.Errorf("failed to create socket event manager: %w", err))
	}

	// initialize egress controller
	if enableEgressController {
		egressCleanup := initEgressController(ctx, logger, pm, connectionManager, &tapObjs)
		defer egressCleanup()
	}

	// Initialize TLS probes
	logger.Info("starting TLS Probes", zap.String("probes", tlsProbes))
	tlsManager, err := InitTLSProbes(ctx, logger, tlsProbes, &tapObjs, connectionManager, configManager)
	if err != nil {
		panic(fmt.Errorf("failed to initialize TLS probes: %w", err))
	}
	if tlsManager != nil {
		// add tls probes as process observers
		pm.Observe(tlsManager)

		defer func() {
			if err := tlsManager.Close(); err != nil {
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

	// Initialize status server with product metrics endpoint
	s := status.NewBaseStatusServer(httpdListen, logger, func() bool {
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
	if devtoolsManager != nil {
		// register http routes
		if err := devtoolsManager.RegisterRoutes(s.Mux(), "/devtools"); err != nil {
			logger.Error("failed to register devtools routes", zap.Error(err))
		} else {
			// set up `/` -> `/devtools` redirect
			s.Mux().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/devtools/", http.StatusTemporaryRedirect)
			})

			devtoolsURL := "http://" + httpdListen + "/devtools"

			// Print pretty box if running in a terminal
			if term.IsTerminal(int(os.Stdout.Fd())) {
				PrintDevToolsBox(devtoolsURL)
			}

			logger.Info("devtools running", zap.String("url", devtoolsURL))
		}
	}

	logger.Info("eBPF program loaded and listening")

	// trap int/term signals
	<-ctx.Done()
	logger.Info("shutting down")
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
		common.NewTracepoint("syscalls", "sys_enter_execve", objs.SyscallProbeEntryExecve),
		common.NewTracepoint("syscalls", "sys_exit_execve", objs.SyscallProbeRetExecve),
		common.NewTracepoint("syscalls", "sys_enter_execveat", objs.SyscallProbeEntryExecveat),
		common.NewTracepoint("syscalls", "sys_exit_execveat", objs.SyscallProbeRetExecveat),
		common.NewTracepoint("syscalls", "sys_enter_exit_group", objs.SyscallProbeEntryExitGroup),
		common.NewTracepoint("sched", "sched_process_exit", objs.TracepointSchedProcessExit),
	}

	procManRB, err := ringbuf.NewReader(objs.ProcEvents)
	if err != nil {
		return nil, fmt.Errorf("failed to create proc event reader: %w", err)
	}

	procMan := ebpfProcess.New(logger, objs.ProcessMetaMap, procManRB, procManTps)

	return procMan, nil
}

// newRegistrationProvider connects to the managed control plane using the
// registration token: it starts the deploy heartbeat, fetches registration,
// and returns a remote config provider fed by live Ably updates, plus a
// cleanup func to stop the heartbeat and close the Ably connection.
func newRegistrationProvider(logger *zap.Logger) (config.ConfigProvider, func()) {
	var cleanups []func()
	cleanup := func() {
		for _, cleanup := range slices.Backward(cleanups) {
			cleanup()
		}
	}

	// start the deploy heartbeat (every 24h)
	hb, err := heartbeat.New(logger, registrationEndpoint, registrationToken, time.Hour*24, heartbeat.Deploy)
	if err != nil {
		logger.Fatal("creating heartbeat", zap.Error(err))
	}
	hb.Start()
	cleanups = append(cleanups, hb.Stop)

	// use the control-plane client to fetch registration
	qpointClient := httpclient.New(registrationToken)
	registration, err := register.FetchRegistration(qpointClient, registrationEndpoint)
	if err != nil {
		logger.Fatal("failed to fetch registration", zap.Error(err))
	}

	remoteProvider := remote.NewRemoteConfigProvider(logger, registrationEndpoint, qpointClient)

	// setup the Ably updater for live config pushes
	ablyUpdater := remote.NewAblyUpdater(logger, registration.AblyToken)
	if err := ablyUpdater.Init(); err != nil {
		logger.Fatal("failed to initialize Ably updater", zap.Error(err))
	}
	cleanups = append(cleanups, func() { _ = ablyUpdater.Close() })

	if err := remoteProvider.RegisterWatcher(
		ablyUpdater,
		registration.OrgID+"-deploy",
		"config.changed",
		time.Second*5,
		100,
	); err != nil {
		logger.Fatal("failed to register Ably watcher", zap.Error(err))
	}

	return remoteProvider, cleanup
}

// startPulseHeartbeat starts a 5-minute pulse heartbeat if the active config
// has a pulse eventstore, using its credentials. Returns a no-op cleanup when
// no pulse eventstore is configured.
func startPulseHeartbeat(logger *zap.Logger, configManager *config.ConfigManager) func() {
	cfg := configManager.GetConfig()
	if !cfg.Services.HasAnyEventStores() {
		return func() {}
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
			logger.Fatal("creating pulse heartbeat", zap.Error(err))
		}
		hb.Start()
		return hb.Stop
	}
	return func() {}
}

// initEgressController wires up the MITM egress controller (cert store, CA
// injection, and traffic router) and returns a cleanup func to stop it.
func initEgressController(ctx context.Context, logger *zap.Logger, pm *process.Manager, connectionManager *connection.Manager, tapObjs *tap.TapObjects) func() {
	var cleanups []func()
	cleanup := func() {
		for _, cleanup := range slices.Backward(cleanups) {
			cleanup()
		}
	}

	// initialize a cert store
	certStore := egress.NewCertStore(sanCertMaxSize, logger)
	if err := certStore.Init(); err != nil {
		panic(fmt.Errorf("failed to initialize certificate store: %w", err))
	}

	// get the root cert bytes
	rootCert, err := certStore.GetRootCertBytes()
	if err != nil {
		panic(fmt.Errorf("failed to get root certificate: %w", err))
	}

	logger.Info("starting certificate injector")
	certsObjs := tap.CertsObjects{}
	strategy := ca.StrategyFromString(certInjectionStrategy)

	if strategy == ca.InjectStrategyEbpf {
		if err := cap.CanBpfProbeWriteUser(); err != nil {
			if errors.Is(err, cap.ErrBpfProbeWriteUser) {
				logger.Fatal("bpf_probe_write_user is not allowed, cannot use ebpf strategy for cert injection", zap.Error(err))
			}
			logger.Fatal("failed to check if bpf_probe_write_user is allowed", zap.Error(err))
		}

		if err := tap.LoadCertsObjects(&certsObjs, nil); err != nil {
			panic(fmt.Errorf("failed to load BPF programs and maps: %w", err))
		}
		cleanups = append(cleanups, func() { certsObjs.Close() })
	}

	// init a ca manager
	caManager := ca.NewCaManager(rootCert, strategy, logger, &certsObjs, tapObjs, pm)
	if err := caManager.Start(ctx); err != nil {
		panic(fmt.Errorf("failed to start ca manager: %w", err))
	}
	cleanups = append(cleanups, func() {
		if err := caManager.Stop(); err != nil {
			logger.Error("failed to stop ca manager", zap.Error(err))
		}
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
		logger.Error("failed to create egress router", zap.Error(err))
	}

	if router != nil {
		logger.Info("starting egress controller")
		tlsOk := egress.TLSOkStrategyFromString(tlsOkStrategy)
		m := egress.NewEgressManager(certStore, logger, router, tlsOk, egress.WithConnEventer(connectionManager))
		if err := m.Start(); err != nil {
			logger.Fatal("failed to start egress manager", zap.Error(err))
		}
		cleanups = append(cleanups, func() {
			if err := m.Stop(); err != nil {
				logger.Error("failed to stop egress manager", zap.Error(err))
			}
		})

		// add egress manager as a ca observer
		caManager.Observe(m)
	}

	return cleanup
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

			probes = append(probes, javassl.NewProbe(
				ctx,
				logger,
				sslEngineManager,
				newEbpfJavaSslProbesCreator(objs),
			))

			// add the ssl engine manager as a config subscriber
			configManager.SubscribeSetter(sslEngineManager)
		case "nodetls":
			probes = append(probes, nodetls.NewProbe(logger, objs.NodeTlsSymaddrsMap, newEbpfNodeTlsProbesCreator(objs)))
		case "gotls":
			probes = append(probes, gotls.NewProbe(logger, objs.GoTlsSymaddrsMap, newEbpfGoTlsProbesCreator(objs)))
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
		common.NewTracepoint("syscalls", "sys_exit_sendto", objs.SyscallProbeRetSendtoInit),
		common.NewTracepoint("syscalls", "sys_exit_sendmsg", objs.SyscallProbeRetSendmsgInit),
		common.NewTracepoint("syscalls", "sys_exit_write", objs.SyscallProbeRetWriteInit),
		common.NewTracepoint("syscalls", "sys_exit_writev", objs.SyscallProbeRetWritevInit),
		common.NewTracepoint("syscalls", "sys_exit_recvfrom", objs.SyscallProbeRetRecvfromInit),
		common.NewTracepoint("syscalls", "sys_exit_recvmsg", objs.SyscallProbeRetRecvmsgInit),
		common.NewTracepoint("syscalls", "sys_exit_read", objs.SyscallProbeRetReadInit),
		common.NewTracepoint("syscalls", "sys_exit_readv", objs.SyscallProbeRetReadvInit),

		// syscall socket events
		common.NewTracepoint("syscalls", "sys_enter_accept", objs.SyscallProbeEntryAccept),
		common.NewTracepoint("syscalls", "sys_exit_accept", objs.SyscallProbeRetAccept),
		common.NewTracepoint("syscalls", "sys_enter_accept4", objs.SyscallProbeEntryAccept4),
		common.NewTracepoint("syscalls", "sys_exit_accept4", objs.SyscallProbeRetAccept4),
		common.NewTracepoint("syscalls", "sys_enter_connect", objs.SyscallProbeEntryConnect),
		common.NewTracepoint("syscalls", "sys_exit_connect", objs.SyscallProbeRetConnect),
		common.NewTracepoint("syscalls", "sys_enter_close", objs.SyscallProbeEntryClose),
		common.NewTracepoint("syscalls", "sys_exit_close", objs.SyscallProbeRetClose),
		common.NewTracepoint("syscalls", "sys_enter_write", objs.SyscallProbeEntryWrite),
		common.NewTracepoint("syscalls", "sys_enter_writev", objs.SyscallProbeEntryWritev),
		common.NewTracepoint("syscalls", "sys_exit_write", objs.SyscallProbeRetWrite),
		common.NewTracepoint("syscalls", "sys_exit_writev", objs.SyscallProbeRetWritev),
		common.NewTracepoint("syscalls", "sys_enter_sendto", objs.SyscallProbeEntrySendto),
		common.NewTracepoint("syscalls", "sys_exit_sendto", objs.SyscallProbeRetSendto),
		common.NewTracepoint("syscalls", "sys_enter_sendmsg", objs.SyscallProbeEntrySendmsg),
		common.NewTracepoint("syscalls", "sys_exit_sendmsg", objs.SyscallProbeRetSendmsg),
		common.NewTracepoint("syscalls", "sys_enter_read", objs.SyscallProbeEntryRead),
		common.NewTracepoint("syscalls", "sys_enter_readv", objs.SyscallProbeEntryReadv),
		common.NewTracepoint("syscalls", "sys_exit_read", objs.SyscallProbeRetRead),
		common.NewTracepoint("syscalls", "sys_exit_readv", objs.SyscallProbeRetReadv),
		common.NewTracepoint("syscalls", "sys_enter_recvfrom", objs.SyscallProbeEntryRecvfrom),
		common.NewTracepoint("syscalls", "sys_exit_recvfrom", objs.SyscallProbeRetRecvfrom),
		common.NewTracepoint("syscalls", "sys_enter_recvmsg", objs.SyscallProbeEntryRecvmsg),
		common.NewTracepoint("syscalls", "sys_exit_recvmsg", objs.SyscallProbeRetRecvmsg),
		common.NewTracepoint("syscalls", "sys_enter_socket", objs.SyscallProbeEntrySocket),
		common.NewTracepoint("syscalls", "sys_exit_socket", objs.SyscallProbeRetSocket),

		// pid/fd mapping kprobes
		common.NewKprobe(objs.TrackSockAllocFileEntry, "sock_alloc_file"),
		common.NewKretprobe(objs.TrackSockAllocFileRet, "sock_alloc_file"),
		common.NewKprobe(objs.TrackFdInstallEntry, "fd_install"),
		common.NewKprobe(objs.CleanupPidFdFileEntries, "__fput", "fput", "__pfx_fput", "__pfx___fput"),
		common.NewKprobe(objs.TraceTcpClose, "tcp_close"),

		// ftraces
		common.NewFexit("tcp_v4_connect", objs.TraceTcpV4ConnectFexit),
		common.NewFexit("tcp_v6_connect", objs.TraceTcpV6ConnectFexit),
		common.NewFexit("tcp_recvmsg", objs.TraceTcpRecvmsgFexit),
	}

	// open a ring buffer reader
	rb, err := ringbuf.NewReader(objs.SocketEvents)
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
			common.NewUprobe("SSL_read", objs.OpensslProbeEntrySSL_read),
			common.NewUprobe("SSL_read_ex", objs.OpensslProbeEntrySSL_readEx),
			common.NewUprobe("SSL_write", objs.OpensslProbeEntrySSL_write),
			common.NewUprobe("SSL_write_ex", objs.OpensslProbeEntrySSL_writeEx),
			common.NewUprobe("SSL_free", objs.OpensslProbeEntrySSL_free),
			common.NewUprobe("SSL_set_fd", objs.OpensslProbeEntrySSL_setFd),

			// ssl return uprobes
			common.NewUretprobe("SSL_read", objs.OpensslProbeRetSSL_read),
			common.NewUretprobe("SSL_read_ex", objs.OpensslProbeRetSSL_readEx),
			common.NewUretprobe("SSL_write", objs.OpensslProbeRetSSL_write),
			common.NewUretprobe("SSL_write_ex", objs.OpensslProbeRetSSL_writeEx),
			common.NewUretprobe("SSL_new", objs.OpensslProbeRetSSL_new),

			// node required openssl uprobes
			common.NewUprobe("SSL_set_cert_cb", objs.NodetlsProbeEntrySSL_setCertCb),
			common.NewUprobe("SSL_free", objs.NodetlsProbeEntrySSL_free),
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
