//go:build e2e && !linux

package e2e

import (
	"fmt"

	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/connection"
	ebpfProcess "github.com/qpoint-io/qtap/pkg/ebpf/process"
	"github.com/qpoint-io/qtap/pkg/ebpf/socket"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls"
	"go.uber.org/zap"
)

func NewEbpfProcManager(logger *zap.Logger, objs *tap.TapObjects) (*ebpfProcess.Manager, error) {
	return nil, fmt.Errorf("not supported on this platform")
}

func NewEbpfSockManager(logger *zap.Logger, connMan *connection.Manager, objs *tap.TapObjects) (*socket.SocketEventManager, error) {
	return nil, fmt.Errorf("not supported on this platform")
}

func InitTLSProbes(logger *zap.Logger, tlsProbesStr string, objs *tap.TapObjects) (*tls.TlsManager, error) {
	return nil, fmt.Errorf("not supported on this platform")
}
