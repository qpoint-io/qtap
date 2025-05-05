//go:build e2e && linux

package e2e

import (
	"fmt"
	"os"
	"syscall"

	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/cmd"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/dns"
	"github.com/qpoint-io/qtap/pkg/ebpf/socket"
	"github.com/qpoint-io/qtap/pkg/ebpf/trace"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/stream"
	"github.com/qpoint-io/qtap/pkg/tags"
	"go.uber.org/zap"
)

func mainSetup() error {
	if syscall.Getuid() != 0 {
		return fmt.Errorf("please run e2e tests as root to load BPF programs and maps")
	}

	// set up config
	if err := e2ectx.confProvider.Start(); err != nil {
		return fmt.Errorf("starting config provider: %w", err)
	}
	confManager := config.NewConfigManager(logger, e2ectx.confProvider)
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
	procEbpfMan, err := cmd.NewEbpfProcManager(logger, &tapObjs)
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
	svcRegistry := services.NewServiceRegistry()
	svcManager := services.NewServiceManager(e2ectx.ctx, logger, svcRegistry)
	svcManager.RegisterFactory(serviceFactories...)
	confManager.SubscribeSetter(svcManager)

	pluginRegistry := plugins.NewRegistry(pluginFactories...)
	pluginManager := plugins.NewPluginManager(
		logger,
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
		connection.SetServiceRegistry(svcRegistry),
		connection.SetConfig(confManager.GetConfig()),
		connection.SetDeploymentTags(tags.FromValues(map[string]string{"e2e": "true"})),
	)
	confManager.SubscribeSetter(connectionManager)

	// init a socket settings manager to push config changes down into ebpf land
	socketSettingManager := socket.NewSocketSettingsManager(logger, tapObjs.TapMaps.SocketSettingsMap)
	confManager.SubscribeSetter(socketSettingManager)

	// Initialize socket manager
	socketManager, err := cmd.NewEbpfSockManager(logger, connectionManager, &tapObjs)
	if err != nil {
		return fmt.Errorf("creating socket event manager: %w", err)
	}

	// Initialize TLS probes
	tlsProbes := "openssl"
	logger.Info("starting TLS Probes", zap.String("probes", tlsProbes))
	tlsManager, err := cmd.InitTLSProbes(logger, tlsProbes, &tapObjs)
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
			logger.Error("event store exited with errors", zap.Any("errors", errs))
		}
	})

	logger.Info("🥟 completed e2e setup")
	return nil
}
