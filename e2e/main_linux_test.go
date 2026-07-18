//go:build e2e && linux

package e2e

import (
	"context"
	"fmt"

	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/ca"
	"github.com/qpoint-io/qtap/pkg/cmd"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/connection"
	ebpfProcess "github.com/qpoint-io/qtap/pkg/ebpf/process"
	"github.com/qpoint-io/qtap/pkg/ebpf/socket"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls"
	"github.com/qpoint-io/qtap/pkg/egress"
	egressEbpf "github.com/qpoint-io/qtap/pkg/egress/ebpf"
	"go.uber.org/zap"
)

func NewEbpfProcManager(logger *zap.Logger, objs *tap.TapObjects) (*ebpfProcess.Manager, error) {
	return cmd.NewEbpfProcManager(logger, objs)
}

func NewEbpfSockManager(logger *zap.Logger, connMan *connection.Manager, objs *tap.TapObjects) (*socket.SocketEventManager, error) {
	return cmd.NewEbpfSockManager(logger, connMan, objs)
}

func InitTLSProbes(ctx context.Context, logger *zap.Logger, tlsProbesStr string, objs *tap.TapObjects, connEvents *connection.Manager, configManager *config.ConfigManager) (*tls.TlsManager, error) {
	return cmd.InitTLSProbes(ctx, logger, tlsProbesStr, objs, connEvents, configManager)
}

func SetupEgressManager(
	logger *zap.Logger,
	connMan *connection.Manager,
	objs *tap.TapObjects,
	certStore *egress.CertStore,
	router *egressEbpf.Router,
	caManager *ca.CaManager,
) (func() error, error) {
	logger.Info("starting egress controller")
	tlsOkStrategy := egress.TLSOkStrategyOnCertInject
	m := egress.NewEgressManager(certStore, logger, router, tlsOkStrategy, egress.WithConnEventer(connMan))
	if err := m.Start(); err != nil {
		return nil, fmt.Errorf("starting egress manager: %w", err)
	}

	// add egress manager as a ca observer
	caManager.Observe(m)
	return m.Stop, nil
}
