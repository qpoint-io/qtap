//go:build linux

package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/qpoint-io/qtap/pkg/ca"
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/tlsutils"
	"go.uber.org/zap"
)

// ConnectionEvents is an interface that allows for connection pairings to be registered
type ConnectionEvents interface {
	WriteProtocolEvent(cookie uint64, protocol connection.Protocol, isTLS bool) error
	WriteHostnameEvent(cookie uint64, hostname string) error
	WriteDataEvent(cookie uint64, direction connection.Direction, data []byte) error
	WriteOriginalDestinationEvent(cookie uint64, originalDst *net.TCPAddr) error
	WriteErrorEvent(cookie uint64, eventType connection.ErrorEventType, message string)
	WriteHandlerTypeEvent(cookie uint64, handlerType connection.HandlerType)
	WriteDoneEvent(cookie uint64)
	WriteTLSClientHelloEvent(c uint64, h *tlsutils.ClientHello)
}

type Router interface {
	SetMgmtAddrs(ipv4 net.IP, ipv6 net.IP, port int) error
	Start() error
	Stop() error
}

// Root cgroup path
const CGROUP_PATH = "/sys/fs/cgroup"

type EgressManager struct {
	// logger
	logger *zap.Logger

	// tls ok strategy
	tlsOkStrategy TLSOkStrategy

	// forwarder
	forwarder *Forwarder

	// router
	router Router

	// certificate store
	certStore *CertStore

	// connection pair registrar
	connEvents ConnectionEvents

	// inject a default ca observer
	ca.DefaultObserver
}

type Option func(m *EgressManager)

func WithConnEventer(s ConnectionEvents) Option {
	return func(m *EgressManager) {
		m.connEvents = s
	}
}

func NewEgressManager(certStore *CertStore, logger *zap.Logger, router Router, tlsOkStrategy TLSOkStrategy, opts ...Option) *EgressManager {
	m := &EgressManager{
		logger:        logger,
		router:        router,
		certStore:     certStore,
		tlsOkStrategy: tlsOkStrategy,
	}

	// apply options
	for _, opt := range opts {
		opt(m)
	}

	return m
}

func (m *EgressManager) Start() error {
	if m.router == nil {
		m.logger.Warn("router is nil, skipping egress manager start")
		return nil
	}

	// scan for the best interfaces
	ipv4, ipv6, err := findBestInterface()
	if err != nil {
		return fmt.Errorf("failed to find best interface: %w", err)
	}

	// find an available port
	port, err := findAvailablePort(12345, 100)
	if err != nil {
		return fmt.Errorf("failed to find available port: %w", err)
	}

	// log the listener addrs
	fields := []zap.Field{
		zap.Int("port", port),
	}
	if ipv4 != nil {
		fields = append(fields, zap.String("ipv4", ipv4.String()))
	}
	if ipv6 != nil {
		fields = append(fields, zap.String("ipv6", ipv6.String()))
	}

	// log the listener addrs
	m.logger.Debug("forwarder found best interface", fields...)

	// Configure eBPF map with listen addresses
	if err := m.router.SetMgmtAddrs(ipv4, ipv6, port); err != nil {
		return fmt.Errorf("setting up listen addresses: %w", err)
	}

	// initialize the forwarder
	f, err := NewForwarder(context.Background(), m.logger, fmt.Sprintf("0.0.0.0:%d", port), fmt.Sprintf("[::]:%d", port), m.certStore, m.connEvents)
	if err != nil {
		return fmt.Errorf("failed to create forwarder: %w", err)
	}
	if f == nil {
		return errors.New("forwarder is nil")
	}
	// start the forwarder
	if err := f.Start(); err != nil {
		return fmt.Errorf("starting forwarder: %w", err)
	}

	// set the forwarder
	m.forwarder = f

	// Start the eBPF router
	if err := m.router.Start(); err != nil {
		return fmt.Errorf("starting router: %w", err)
	}

	return nil
}

func (m *EgressManager) Stop() error {
	// stop the forwarder
	if err := m.forwarder.Stop(); err != nil {
		return fmt.Errorf("stopping forwarder: %w", err)
	}

	// stop the router
	if err := m.router.Stop(); err != nil {
		return fmt.Errorf("stopping router: %w", err)
	}

	return nil
}

func (m *EgressManager) CertRead(p *process.Process, path string) error {
	// this is called when a process reads the injected ca so we
	// know that it's safe to set tls ok
	if err := p.SetTlsOk(true); err != nil {
		return fmt.Errorf("setting tls ok: %w", err)
	}

	return nil
}

func (m *EgressManager) CertInjected(p *process.Process, path string, rootID uint64) error {
	// if our tls ok strategy is on cert read, we can't be optimistic
	// and need to wait until we see the cert read event
	if m.tlsOkStrategy == TLSOkStrategyOnCertRead {
		return nil
	}

	// IMPORTANT:
	//
	// processes that have been running have likely already read the ca which did not
	// have the qpoint ca in it, so we can't take this liberty for processes that aren't new
	if p.PredatesQpoint {
		return nil
	}

	// set tls ok
	if err := p.SetTlsOk(true); err != nil {
		return fmt.Errorf("setting tls ok: %w", err)
	}

	return nil
}

// findBestInterface finds the best interface to use for forwarder listening.
// It prefers interfaces that are not virtual or loopback, and are not docker or bridge interfaces.
// It returns the best IPv4 and IPv6 interfaces found, or an error if no suitable interface is found.
func findBestInterface() (net.IP, net.IP, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get network interfaces: %w", err)
	}

	var bestIPv4, bestIPv6 net.IP

	// First pass: Look for private IP addresses
	bestIPv4, bestIPv6 = findIPAddresses(interfaces, true)

	// Second pass: If no private IPv4 found, look for public IP addresses
	if bestIPv4 == nil || bestIPv6 == nil {
		publicIPv4, publicIPv6 := findIPAddresses(interfaces, false)
		if bestIPv4 == nil {
			bestIPv4 = publicIPv4
		}
		if bestIPv6 == nil {
			bestIPv6 = publicIPv6
		}
	}

	return bestIPv4, bestIPv6, nil
}

func findIPAddresses(interfaces []net.Interface, privateOnly bool) (net.IP, net.IP) {
	var bestIPv4, bestIPv6 net.IP

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || isUnwantedInterface(iface.Name) {
			continue // Skip interfaces that are down, loopback, or unwanted
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			if ipv4 := ipNet.IP.To4(); ipv4 != nil && ipv4.IsGlobalUnicast() {
				isPrivate := isPrivateIPv4(ipv4)
				if (privateOnly && isPrivate) || (!privateOnly && !isPrivate) {
					if bestIPv4 == nil {
						bestIPv4 = ipv4
					}
				}
			} else if ipv6 := ipNet.IP.To16(); ipv6 != nil && ipv6.IsGlobalUnicast() {
				if bestIPv6 == nil {
					bestIPv6 = ipv6
				}
			}

			if bestIPv4 != nil && bestIPv6 != nil {
				return bestIPv4, bestIPv6
			}
		}
	}

	return bestIPv4, bestIPv6
}

func isPrivateIPv4(ip net.IP) bool {
	return ip[0] == 10 || // 10.x.x.x
		(ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31) || // 172.16.x.x - 172.31.x.x
		(ip[0] == 192 && ip[1] == 168) // 192.168.x.x
}

func isUnwantedInterface(name string) bool {
	return strings.HasPrefix(name, "vnet") ||
		strings.HasPrefix(name, "bridge") ||
		strings.HasPrefix(name, "docker") ||
		strings.Contains(name, "tun")
}

func findAvailablePort(startPort int, maxAttempts int) (int, error) {
	for port := startPort; port < startPort+maxAttempts; port++ {
		address := fmt.Sprintf(":%d", port)
		listener, err := net.Listen("tcp", address)
		if err != nil {
			continue // Port is not available, try the next one
		}
		listener.Close()
		return port, nil // Found an available port
	}
	return 0, fmt.Errorf("no available ports found after %d attempts", maxAttempts)
}
