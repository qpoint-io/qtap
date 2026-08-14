package gotls

import (
	"debug/elf"
	"errors"
	"fmt"
	"strings"

	version "github.com/hashicorp/go-version"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls/gotls/gobin"
)

/*
Package handles TLS connection probing by resolving critical memory addresses and offsets in Go binaries.
Key challenges:

1. Memory Layout Variability:
- Function addresses change across binaries due to compilation differences
- Memory locations shift when binaries are mapped into different processes

2. Struct Offset Stability:
- Offsets depend on struct field layouts (e.g. sysfd in poll.FD)
- Go version updates may alter struct layouts, requiring per-version scanning
- Offsets remain consistent across languages when struct definitions match

Probing Objectives:
1. Locate SSL-related functions (e.g. Read/Write) in memory
2. Correlate SSL operations with their underlying file descriptors
   to monitor encrypted connections
*/

// GoTLSSymAddr represents the symbol offsets for go tls
type GoTLSSymAddr struct {
	// ---- itable symbols ----

	// net.conn interface types.
	InternalSyscallConn int64 `json:"internal_syscall_conn,omitempty"` // go.itab.*google.golang.org/grpc/credentials/internal.syscallconn,net.conn
	TLSConn             int64 `json:"tls_conn,omitempty"`              // go.itab.*cry	pto/tls.conn,net.conn
	NetTCPConn          int64 `json:"net_tcp_conn,omitempty"`          // go.itab.*net.tcpconn,net.conn

	// ---- struct member offsets ----

	FDSysfdOffset     int32 `json:"fd_sysfd_offset,omitempty"`     // struct internal/poll.FD - property sysfd
	TLSConnOffset     int32 `json:"tls_conn_offset,omitempty"`     // struct crypto/tls.Conn - property conn
	SyscallConnOffset int32 `json:"syscall_conn_offset,omitempty"` // struct google.golang.org/grpc/credentials/internal.syscallconn - property sysfd
	GoidOffset        int32 `json:"goid_offset,omitempty"`         // struct runtime.g - property goid
}

func (symaddrs *GoTLSSymAddr) populateCommonTypeAddrs(matches []elf.Symbol) {
	// iterate through the matches
	for _, match := range matches {
		// set the InternalSyscallConn
		if strings.HasSuffix(match.Name, SymbolInternalSyscallConn) {
			symaddrs.InternalSyscallConn = int64(match.Value)
			continue
		}

		// set the TlsConn
		if strings.HasSuffix(match.Name, SymbolTLSConn) {
			symaddrs.TLSConn = int64(match.Value)
			continue
		}

		// set the NetTCPConn
		if strings.HasSuffix(match.Name, SymbolNetTCPConn) {
			symaddrs.NetTCPConn = int64(match.Value)
			continue
		}
	}
}

func (symaddrs *GoTLSSymAddr) validateSymaddrs() error {
	if symaddrs.NetTCPConn == -1 {
		return errors.New("missing NetTCPConn")
	}

	if symaddrs.FDSysfdOffset == -1 {
		return errors.New("missing FDSysfdOffset")
	}

	return nil
}

// InjectPrecomputedSymAddrsFromVersion returns the GoTLSSymAddr for the given Go version
func InjectPrecomputedSymAddrsFromVersion(ver *version.Version, addrs *GoTLSSymAddr) error {
	pollsysfd, err := gobin.GetOffset("internal/poll.FD", "Sysfd", ver.String())
	if err != nil {
		return fmt.Errorf("getting precomputed sysfd offset %s: %w", ver.String(), err)
	}
	addrs.FDSysfdOffset = int32(pollsysfd)

	connOffset, err := gobin.GetOffset("crypto/tls.Conn", "conn", ver.String())
	if err != nil {
		return fmt.Errorf("getting precomputed conn offset %s: %w", ver.String(), err)
	}
	addrs.TLSConnOffset = int32(connOffset)

	goidOffset, err := gobin.GetOffset("runtime.g", "goid", ver.String())
	if err != nil {
		return fmt.Errorf("getting precomputed goid offset %s: %w", ver.String(), err)
	}
	addrs.GoidOffset = int32(goidOffset)

	return nil
}
