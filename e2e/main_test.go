//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/ca"
	"github.com/qpoint-io/qtap/pkg/cap"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/devtools"
	"github.com/qpoint-io/qtap/pkg/dns"
	"github.com/qpoint-io/qtap/pkg/e2e"
	"github.com/qpoint-io/qtap/pkg/ebpf/socket"
	"github.com/qpoint-io/qtap/pkg/ebpf/trace"
	"github.com/qpoint-io/qtap/pkg/egress"
	egressEbpf "github.com/qpoint-io/qtap/pkg/egress/ebpf"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/accesslogs"
	"github.com/qpoint-io/qtap/pkg/plugins/errordetection"
	"github.com/qpoint-io/qtap/pkg/plugins/httpcapture"
	"github.com/qpoint-io/qtap/pkg/plugins/llm"
	loggerPlugin "github.com/qpoint-io/qtap/pkg/plugins/logger"
	"github.com/qpoint-io/qtap/pkg/plugins/report"
	"github.com/qpoint-io/qtap/pkg/plugins/wrapper"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/connmeta"
	"github.com/qpoint-io/qtap/pkg/services/reporter"
	"github.com/qpoint-io/qtap/pkg/services/rulekitsvc"
	"github.com/qpoint-io/qtap/pkg/stream"
	"github.com/qpoint-io/qtap/pkg/tags"
	"go.uber.org/zap"
)

var (
	start            time.Time
	logger           *zap.Logger
	e2ectx           *e2e.Context
	serviceFactories []services.FactoryFn
	pluginFactories  []plugins.Plugin
	devtoolsManager  *devtools.Manager
)

func mainSetup() error {
	start = time.Now()
	logger = e2e.NewLogger(start)
	zap.ReplaceGlobals(logger)

	e2ectx = e2e.NewContext(context.Background())
	e2ectx.Start = start
	e2ectx.EventStore = e2e.NewEventStore(logger)
	e2ectx.ObjectStore = e2e.NewObjectStore(logger)
	e2ectx.TestConfig = func(mut func(*config.Config)) *config.Config {
		c := e2e.DefaultTestConfig(func(c *config.Config) {
			// ignore connections related to GitHub Actions tooling
			c.Tap.Filters.Custom = append(c.Tap.Filters.Custom, []config.TapFilter{
				// {
				// 	Exe:      "/usr/bin/python",
				// 	Strategy: config.MatchStrategy_PREFIX,
				// },
				{
					Exe:      "/usr/bin/docker-proxy",
					Strategy: config.MatchStrategy_PREFIX,
				},
				{
					Exe:      "/home/runner",
					Strategy: config.MatchStrategy_PREFIX,
				},
			}...)
		})
		if mut != nil {
			mut(c)
		}
		return c
	}
	e2ectx.ConfProvider = e2e.NewConfigProvider(e2ectx.TestConfig(nil))
	e2ectx.L = logger

	{
		// hit the google DNS to get the machines local IP
		conn, err := net.Dial("udp", "8.8.8.8:80")
		if err != nil {
			logger.Warn("⚠️ failed to get machine IP", zap.Error(err))
		}
		conn.Close()

		// set on context for use in tests that require hairpinning
		e2ectx.SetMachineIP(conn.LocalAddr().(*net.UDPAddr).IP)
	}

	if syscall.Getuid() != 0 {
		return fmt.Errorf("please run e2e tests as root to load BPF programs and maps")
	}

	// set up config
	if err := e2ectx.ConfProvider.Start(); err != nil {
		return fmt.Errorf("starting config provider: %w", err)
	}
	confManager := config.NewConfigManager(logger, e2ectx.ConfProvider)
	if err := confManager.Run(e2ectx); err != nil {
		return fmt.Errorf("running config manager: %w", err)
	}

	// Load BPF programs and maps
	logger.Info("loading BPF programs and maps")
	spec, err := tap.LoadTap()
	if err != nil {
		return fmt.Errorf("loading BPF programs and maps: %w", err)
	}
	// write the current pid to the bpf program
	qpid, ok := spec.Variables["qpid"]
	if !ok {
		return errors.New("finding qpid constant")
	}
	if err = qpid.Set(uint32(os.Getpid())); err != nil {
		return fmt.Errorf("rewriting constants: %w", err)
	}
	tapObjs := tap.TapObjects{}
	err = spec.LoadAndAssign(&tapObjs, nil)
	if err != nil {
		return fmt.Errorf("loading BPF programs and maps: %w", err)
	}
	e2ectx.RegisterErrCloser(tapObjs.Close)

	// Initialize process manager
	procEbpfMan, err := NewEbpfProcManager(logger, &tapObjs)
	if err != nil {
		return fmt.Errorf("getting ebpf proc objs: %w", err)
	}

	pm := process.NewProcessManager(logger, procEbpfMan)
	confManager.SubscribeSetter(pm)

	// Initialize devtools manager
	devtoolsManager = devtools.NewManager(
		devtools.WithLogger(logger),
		devtools.WithProcessSnapshotter(pm),
	)
	pm.Observe(devtoolsManager)
	devtoolsEventStoreFactory := devtoolsManager.EventStoreFactory()
	devtoolsObjectStoreFactory := devtoolsManager.ObjectStoreFactory()

	serviceFactories = []services.FactoryFn{
		func() services.Factory { return &rulekitsvc.Factory{} },
		func() services.Factory { return &connmeta.Factory{} },
		func() services.Factory { return &reporter.Factory{} },

		// Eventstore services
		func() services.Factory { return devtoolsEventStoreFactory },
		func() services.Factory { return e2ectx.EventStore },

		// Objectstore services
		func() services.Factory { return e2ectx.ObjectStore },
		func() services.Factory { return devtoolsObjectStoreFactory },
	}

	pluginFactories = []plugins.Plugin{
		wrapper.Catch(&loggerPlugin.Factory{}),
		wrapper.Catch(&report.Factory{}),
		wrapper.Catch(accesslogs.NewConsoleJSONFilter()),
		wrapper.Catch(accesslogs.NewConsoleHttpFilter()),
		wrapper.Catch(&httpcapture.Factory{}),
		wrapper.Catch(devtoolsManager.PluginFactory()),
		wrapper.Catch(&errordetection.Factory{}),
		wrapper.Catch(&llm.Factory{}),
	}

	// TODO(e2e)
	// Initialize container detection
	// containerManager := container.NewManager(logger, dockerSocketEndpoint, containerdSocketEndpoint, criRuntimeSocketEndpoint)
	// if err := containerManager.Start(e2ectx); err != nil {
	// 	return fmt.Errorf("starting container manager: %w", err)
	// }
	// pm.Observe(containerManager)

	// Initialize BPF trace manager
	bpfTraceQuery := "" // TODO(e2e)
	tm, err := trace.NewTraceManager(logger, tapObjs.TraceToggleMap, tapObjs.TraceEvents, pm, bpfTraceQuery)
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
	resolv := dns.NewDNSManager(logger, pm)
	if err := resolv.Start(); err != nil {
		return fmt.Errorf("starting dns manager: %w", err)
	}
	e2ectx.RegisterErrCloser(resolv.Stop)

	// Initialize service and plugin systems
	svcFactoryRegistry := services.NewFactoryRegistry()
	svcManager := services.NewFactoryManager(e2ectx, logger, svcFactoryRegistry)
	svcManager.RegisterFactory(serviceFactories...)
	// register core services that must always be included
	svcManager.AddExtraServices(
		// rulekit
		func(cfg *config.Config) *config.ServiceConfig {
			return &config.ServiceConfig{
				Type:   rulekitsvc.TypeRulekit.String(),
				Config: cfg.Rulekit,
			}
		},
		// connmeta
		func(cfg *config.Config) *config.ServiceConfig {
			return &config.ServiceConfig{
				Type: connmeta.Type.String(),
			}
		},
		// report connections to the e2e event store
		func(cfg *config.Config) *config.ServiceConfig {
			return &config.ServiceConfig{
				Type: reporter.Type.String(),
				Config: &reporter.Config{
					// only report connections once they are closed
					FirstReportDeadline: 0,
					ReportInterval:      0,
				},
			}
		},
		// devtools event store
		func(cfg *config.Config) *config.ServiceConfig {
			return &config.ServiceConfig{
				ID:   "devtools",
				Type: devtoolsEventStoreFactory.FactoryType().String(),
			}
		},
		// devtools object store
		func(cfg *config.Config) *config.ServiceConfig {
			return &config.ServiceConfig{
				ID:   "devtools",
				Type: devtoolsObjectStoreFactory.FactoryType().String(),
			}
		},
		// report connections to the devtools event store
		func(cfg *config.Config) *config.ServiceConfig {
			return &config.ServiceConfig{
				Type: reporter.Type.String(),
				Config: &reporter.Config{
					EventStoreID: "devtools",
					// only report connections once they are closed
					FirstReportDeadline: 0,
					ReportInterval:      0,
				},
			}
		},
	)
	confManager.SubscribeSetter(svcManager)

	pluginRegistry := plugins.NewRegistry(pluginFactories...)
	pluginManager := plugins.NewPluginManager(
		logger,
		plugins.SetBufferSize(2*1<<20), // 2MB
		plugins.SetPluginRegistry(pluginRegistry),
		plugins.AddPersistentPlugins(config.Plugin{
			Type: string(devtools.PluginTypeDevTools),
		}),
	)
	confManager.SubscribeSetter(pluginManager)
	if err := pluginManager.Start(); err != nil {
		return fmt.Errorf("starting plugin manager: %w", err)
	}
	e2ectx.RegisterNoErrCloser(pluginManager.Stop)

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
		connection.SetConfig(confManager.GetConfig()),
		connection.SetDeploymentTags(tags.FromValues(map[string]string{"e2e": "true"})),
	)
	confManager.SubscribeSetter(connectionManager)

	// init a socket settings manager to push config changes down into ebpf land
	socketSettingManager := socket.NewSocketSettingsManager(logger, tapObjs.TapMaps.SocketSettingsMap)
	confManager.SubscribeSetter(socketSettingManager)

	// Initialize socket manager
	socketManager, err := NewEbpfSockManager(logger, connectionManager, &tapObjs)
	if err != nil {
		return fmt.Errorf("creating socket event manager: %w", err)
	}

	// initialize a cert store
	certStore := egress.NewCertStore(100, logger)
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
	strategy := ca.StrategyFromString("inline")

	if strategy == ca.InjectStrategyEbpf {
		if err := cap.CanBpfProbeWriteUser(); err != nil {
			if errors.Is(err, cap.ErrBpfProbeWriteUser) {
				logger.Fatal("bpf_probe_write_user is not allowed, cannot use ebpf strategy for cert injection", zap.Error(err))
			}
			logger.Fatal("failed to check if bpf_probe_write_user is allowed", zap.Error(err))
		}

		certsObjs = tap.CertsObjects{}
		if err := tap.LoadCertsObjects(&certsObjs, nil); err != nil {
			panic(fmt.Errorf("failed to load BPF programs and maps: %w", err))
		}
		e2ectx.RegisterErrCloser(certsObjs.Close)
	}

	// init a ca manager
	caManager := ca.NewCaManager(rootCert, strategy, logger, &certsObjs, &tapObjs, pm)
	if err := caManager.Start(e2ectx); err != nil {
		panic(fmt.Errorf("failed to start ca manager: %w", err))
	}
	e2ectx.RegisterErrCloser(caManager.Stop)

	// add the ca manager as a process observer
	pm.Observe(caManager)

	// add JAVA_HOME to process manager env mask
	pm.MaskEnvVars([]string{"JAVA_HOME"})

	// set the ssl cert env vars to process manager env mask
	pm.MaskEnvVars(ca.SslCertEnvVars)
	pm.MaskEnvVars(ca.KeystoreEnvVars)

	// init a router
	router, err := egressEbpf.NewRouter(logger, &tapObjs)
	if err != nil {
		return fmt.Errorf("creating egress router: %w", err)
	}

	if router != nil {
		stopEgressManager, err := SetupEgressManager(
			logger,
			connectionManager,
			&tapObjs,
			certStore,
			router,
			caManager,
		)
		if err != nil {
			return fmt.Errorf("setting up egress manager: %w", err)
		}
		e2ectx.RegisterErrCloser(stopEgressManager)
	}

	// Initialize TLS probes
	tlsProbes := "openssl,gotls,nodetls,javassl"
	logger.Info("starting TLS Probes", zap.String("probes", tlsProbes))
	tlsManager, err := InitTLSProbes(e2ectx, logger, tlsProbes, &tapObjs, connectionManager, confManager)
	if err != nil {
		return fmt.Errorf("initializing TLS probes: %w", err)
	}
	if tlsManager != nil {
		tlsManager.SetObserver(e2ectx) // set the observer so when we know when scans are done.

		// add tls probes as process observers
		pm.Observe(tlsManager)

		e2ectx.RegisterErrCloser(tlsManager.Close)
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
		errs, ok := e2ectx.EventStore.Errors()
		if !ok {
			logger.Error("event store exited with errors", zap.Any("errors", errs))
		}
	})

	logger.Info("🥟 completed e2e setup")
	return nil
}

func TestMain(m *testing.M) {
	if err := mainSetup(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	code := m.Run()
	if err := e2ectx.Close(); err != nil {
		logger.Error("closing resources", zap.Error(err))
	}
	os.Exit(code)
}
