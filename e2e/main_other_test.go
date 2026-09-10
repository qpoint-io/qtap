//go:build e2e && !linux

package e2e

import (
	"context"
	"fmt"

	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/ca"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/ebpf/socket"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls"
	"github.com/qpoint-io/qtap/pkg/egress"
	egressEbpf "github.com/qpoint-io/qtap/pkg/egress/ebpf"
	"github.com/qpoint-io/qtap/pkg/process"
	"go.uber.org/zap"
)

func NewEbpfProcManager(logger *zap.Logger, objs *tap.TapObjects) (process.Eventer, error) {
	return nil, fmt.Errorf("not supported on this platform")
}

func NewEbpfSockManager(logger *zap.Logger, connMan *connection.Manager, objs *tap.TapObjects) (*socket.SocketEventManager, error) {
	return nil, fmt.Errorf("not supported on this platform")
}

func InitTLSProbes(ctx context.Context, logger *zap.Logger, tlsProbesStr string, objs *tap.TapObjects, connEvents *connection.Manager, configManager *config.ConfigManager) (*tls.TlsManager, error) {
	return nil, fmt.Errorf("not supported on this platform")
}

func SetupEgressManager(
	logger *zap.Logger,
	connMan *connection.Manager,
	objs *tap.TapObjects,
	certStore *egress.CertStore,
	router *egressEbpf.Router,
	caManager *ca.CaManager,
) (func() error, error) {
	return nil, fmt.Errorf("not supported on this platform")
}
