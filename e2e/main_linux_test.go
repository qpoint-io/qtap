//go:build e2e && linux

package e2e

import (
	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/cmd"
	"github.com/qpoint-io/qtap/pkg/connection"
	ebpfProcess "github.com/qpoint-io/qtap/pkg/ebpf/process"
	"github.com/qpoint-io/qtap/pkg/ebpf/socket"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls"
	"go.uber.org/zap"
)

func NewEbpfProcManager(logger *zap.Logger, objs *tap.TapObjects) (*ebpfProcess.Manager, error) {
	return cmd.NewEbpfProcManager(logger, objs)
}

func NewEbpfSockManager(logger *zap.Logger, connMan *connection.Manager, objs *tap.TapObjects) (*socket.SocketEventManager, error) {
	return cmd.NewEbpfSockManager(logger, connMan, objs)
}

func InitTLSProbes(logger *zap.Logger, tlsProbesStr string, objs *tap.TapObjects) (*tls.TlsManager, error) {
	return cmd.InitTLSProbes(logger, tlsProbesStr, objs)
}
