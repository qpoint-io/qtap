//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/dns"
	"github.com/qpoint-io/qtap/pkg/e2e"
	"github.com/qpoint-io/qtap/pkg/ebpf/socket"
	"github.com/qpoint-io/qtap/pkg/ebpf/trace"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/accesslogs"
	"github.com/qpoint-io/qtap/pkg/plugins/httpcapture"
	loggerPlugin "github.com/qpoint-io/qtap/pkg/plugins/logger"
	"github.com/qpoint-io/qtap/pkg/plugins/report"
	"github.com/qpoint-io/qtap/pkg/plugins/wrapper"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/connmeta"
	objectstorenoop "github.com/qpoint-io/qtap/pkg/services/objectstore/noop"
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
	serviceFactories []services.FactoryFactory
	pluginFactories  []plugins.HttpPlugin
)

func mainSetup() error {
	start = time.Now()
	logger = e2e.NewLogger(start)
	zap.ReplaceGlobals(logger)

	e2ectx = e2e.NewContext(context.Background())
	e2ectx.Start = start
	e2ectx.EventStore = e2e.NewEventStore(logger)
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
			logger.Info("⚠️ failed to get machine IP", zap.Error(err))
		}
		conn.Close()

		// set on context for use in tests that require hairpinning
		e2ectx.SetMachineIP(conn.LocalAddr().(*net.UDPAddr).IP)
	}

	serviceFactories = []services.FactoryFactory{
		// Eventstore services
		func() services.ServiceFactory { return e2ectx.EventStore },

		// Objectstore services
		func() services.ServiceFactory { return &objectstorenoop.Factory{} },

		// Add more services here...
		func() services.ServiceFactory { return &rulekitsvc.Factory{} },
		func() services.ServiceFactory { return &connmeta.Factory{} },
		func() services.ServiceFactory { return &reporter.Factory{} },
	}

	pluginFactories = []plugins.HttpPlugin{
		wrapper.Catch(&loggerPlugin.Factory{}),
		wrapper.Catch(&report.Factory{}),
		wrapper.Catch(accesslogs.NewConsoleJSONFilter()),
		wrapper.Catch(accesslogs.NewConsoleHttpFilter()),
		wrapper.Catch(&httpcapture.Factory{}),
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
	procEbpfMan, err := NewEbpfProcManager(logger, &tapObjs)
	if err != nil {
		return fmt.Errorf("getting ebpf proc objs: %w", err)
	}

	pm := process.NewProcessManager(logger, procEbpfMan)
	confManager.SubscribeSetter(pm)

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
	svcRegistry := services.NewFactoryRegistry()
	svcManager := services.NewServiceManager(e2ectx, logger, svcRegistry)
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
		// report connections to the default event store
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
	)
	confManager.SubscribeSetter(svcManager)

	pluginRegistry := plugins.NewRegistry(pluginFactories...)
	pluginManager := plugins.NewPluginManager(
		logger,
		plugins.SetBufferSize(2*1<<20), // 2MB
		plugins.SetServiceFactoryRegistry(svcRegistry),
		plugins.SetPluginRegistry(pluginRegistry),
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
		connection.SetServiceFactoryRegistry(svcRegistry),
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

	// Initialize TLS probes
	tlsProbes := "openssl"
	logger.Info("starting TLS Probes", zap.String("probes", tlsProbes))
	tlsManager, err := InitTLSProbes(logger, tlsProbes, &tapObjs)
	if err != nil {
		return fmt.Errorf("initializing TLS probes: %w", err)
	}
	if tlsManager != nil {
		tlsManager.SetFinalObserver(e2ectx) // set the final probe so when we know when scans are done.

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
