//go:build linux

package egress

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"crypto/tls"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/l7detect"
	"github.com/qpoint-io/qtap/pkg/tlsutils"
	"github.com/sourcegraph/conc"
	"go.uber.org/zap"
)

const (
	SO_ORIGINAL_DST = 80 // Socket option to get the original destination address
	SO_MARK         = 36 // Socket option to get the "tls ok" mark
	SO_COOKIE       = 57 // Socket option to get the cookie
)

// SockAddrIn is a struct to hold the sockaddr_in structure for IPv4 "retrieved" by the SO_ORIGINAL_DST.
type SockAddrIn struct {
	SinFamily uint16
	SinPort   uint16
	SinAddr   [4]byte
	SinZero   [8]byte
}

// SockAddrIn6 is a struct to hold the sockaddr_in6 structure for IPv6 "retrieved" by the SO_ORIGINAL_DST.
type SockAddrIn6 struct {
	SinFamily   uint16
	SinPort     uint16
	SinFlowInfo uint32
	SinAddr     [16]byte
	SinScopeId  uint32
}

type Forwarder struct {
	listen4    string
	listen6    string
	logger     *zap.Logger
	listener4  net.Listener
	listener6  net.Listener
	ctx        context.Context
	cancel     context.CancelFunc
	wg         conc.WaitGroup
	certStore  *CertStore
	connEvents ConnectionEvents
}

func familyToString(family int) string {
	switch family {
	case syscall.AF_INET:
		return "ipv4"
	case syscall.AF_INET6:
		return "ipv6"
	default:
		return "unknown"
	}
}

func (p *Forwarder) getRawFd(conn net.Conn) (int, error) {
	// Get the syscall.Conn interface
	syscallConn, ok := conn.(syscall.Conn)
	if !ok {
		return -1, errors.New("connection does not implement syscall.Conn")
	}

	// Get the raw connection
	rawConn, err := syscallConn.SyscallConn()
	if err != nil {
		return -1, fmt.Errorf("failed to get raw connection: %w", err)
	}

	var fd int
	err = rawConn.Control(func(f uintptr) {
		fd = int(f)
	})
	if err != nil {
		return -1, fmt.Errorf("failed to get fd from raw connection: %w", err)
	}

	return fd, nil
}

func (p *Forwarder) getTlsTerminationSafe(fd int) (bool, error) {
	tlsTerminationSafe, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, SO_MARK)
	if err != nil {
		return false, err
	}
	return tlsTerminationSafe == 1, nil
}

func (p *Forwarder) getRawConnectionCookie(fd int) (uint64, error) {
	var usCookie uint64
	optlen := uint32(unsafe.Sizeof(usCookie))
	err := getsockopt(int(fd), syscall.SOL_SOCKET, SO_COOKIE, unsafe.Pointer(&usCookie), &optlen)
	if err != nil {
		return 0, err
	}

	return usCookie, nil
}

func (p *Forwarder) getConnectionMeta(fd int, logger *zap.Logger) (*connectionMeta, error) {
	meta := &connectionMeta{
		Logger: logger,
		FD:     fd,
	}

	var err error
	meta.Family, err = syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_DOMAIN)
	if err != nil {
		logger.Error("failed to get socket domain family", zap.Error(err))
		return nil, err
	}
	meta.Logger = logger.With(zap.String("address_family", familyToString(meta.Family)))

	meta.TlsTerminationSafe, err = p.getTlsTerminationSafe(fd)
	if err != nil {
		logger.Error("failed to determine if TLS termination is safe", zap.Error(err))
		return nil, err
	}

	meta.Cookie, err = p.getRawConnectionCookie(fd)
	if err != nil {
		logger.Error("failed to get socket cookie", zap.Error(err))
		return nil, err
	}

	switch meta.Family {
	case syscall.AF_INET:
		addr := &SockAddrIn{}
		optlen := uint32(unsafe.Sizeof(*addr))
		err = getsockopt(int(fd), syscall.SOL_IP, SO_ORIGINAL_DST, unsafe.Pointer(addr), &optlen)
		if err != nil {
			logger.Error("failed to get original destination", zap.Error(err), zap.Int("family", meta.Family))
			return nil, err
		}
		meta.OriginalDstAddr = &net.TCPAddr{
			IP:   net.IP(addr.SinAddr[:]),
			Port: int(binary.BigEndian.Uint16((*[2]byte)(unsafe.Pointer(&addr.SinPort))[:])),
		}
		meta.Logger = meta.Logger.With(zap.String("original_destination", meta.OriginalDstAddr.String()))
		meta.Logger.Debug("IPv4 original destination")

	case syscall.AF_INET6:
		addr := &SockAddrIn6{}
		optlen := uint32(unsafe.Sizeof(*addr))
		err = getsockopt(int(fd), syscall.SOL_IPV6, SO_ORIGINAL_DST, unsafe.Pointer(addr), &optlen)
		if err != nil {
			logger.Error("failed to get original destination", zap.Error(err), zap.Int("family", meta.Family))
			return nil, err
		}
		meta.OriginalDstAddr = &net.TCPAddr{
			IP:   net.IP(addr.SinAddr[:]),
			Port: int(binary.BigEndian.Uint16((*[2]byte)(unsafe.Pointer(&addr.SinPort))[:])),
		}
		meta.Logger = meta.Logger.With(
			zap.String("original_destination", meta.OriginalDstAddr.String()),
			zap.Bool("is_ipv4_mapped", meta.OriginalDstAddr.IP.To4() != nil),
		)
		meta.Logger.Debug("IPv6 original destination")

	default:
		return nil, fmt.Errorf("unsupported address family: %d", meta.Family)
	}

	return meta, nil
}

func NewForwarder(ctx context.Context, logger *zap.Logger, listen4, listen6 string, certStore *CertStore, socketEvents ConnectionEvents) (*Forwarder, error) {
	f := &Forwarder{
		listen4:    listen4,
		listen6:    listen6,
		logger:     logger,
		certStore:  certStore,
		connEvents: socketEvents,
	}

	// ensure we have a cancelable context
	if ctx == nil {
		ctx = context.Background()
	}

	f.ctx, f.cancel = context.WithCancel(ctx)

	return f, nil
}

func (p *Forwarder) Start() error {
	if p == nil {
		return errors.New("forwarder is nil")
	}
	var err error
	p.listener4, err = net.Listen("tcp4", p.listen4)
	if err != nil {
		return err
	}

	p.listener6, err = net.Listen("tcp6", p.listen6)
	if err != nil {
		return err
	}

	p.logger.Info("forwarder started", zap.String("listen4", p.listen4), zap.String("listen6", p.listen6))

	p.wg.Go(func() { p.acceptConnections(p.listener4) })
	p.wg.Go(func() { p.acceptConnections(p.listener6) })

	return nil
}

func (p *Forwarder) Stop() error {
	if p == nil {
		return errors.New("forwarder is nil")
	}

	p.cancel()
	if p.listener4 != nil {
		if err := p.listener4.Close(); err != nil {
			return err
		}
	}
	if p.listener6 != nil {
		if err := p.listener6.Close(); err != nil {
			return err
		}
	}
	p.wg.Wait()
	p.logger.Info("forwarder stopped")
	return nil
}

func (p *Forwarder) acceptConnections(listener net.Listener) {
	for {
		select {
		case <-p.ctx.Done():
			return
		default:
			conn, err := listener.Accept()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					p.logger.Error("failed to accept connection", zap.Error(err))
					continue
				}
				return
			}

			logger := p.logger.With(
				zap.String("remote_downstream_addr", conn.RemoteAddr().String()),
				zap.String("local_downstream_addr", conn.LocalAddr().String()))

			logger.Debug("accepted connection, checking for metadata")

			fd, err := p.getRawFd(conn)
			if err != nil {
				logger.Error("failed to get raw connection file descriptor", zap.Error(err))
				conn.Close()
				continue
			}

			meta, err := p.getConnectionMeta(fd, logger)
			if err != nil {
				logger.Error("failed to get connection metadata", zap.Error(err))
				conn.Close()
				continue
			}

			fc, err := newConn(conn, meta)
			if err != nil {
				logger.Error("failed to create forwarder connection", zap.Error(err))
				conn.Close()
				continue
			}

			go p.handleConnection(logger, fc)
		}
	}
}

func (p *Forwarder) handleConnection(logger *zap.Logger, fc *conn) {
	defer fc.Close()

	if p.connEvents != nil {
		defer p.connEvents.WriteDoneEvent(fc.getMeta().Cookie)
	}

	if fc.getMeta().OriginalDstAddr == nil {
		logger.Error("original destination not set")
		return
	}

	// check for self-referential forwarding
	if p.isDestinationSelf(fc.getMeta().OriginalDstAddr) {
		logger.Warn("forwarding loop detected - original destination points to this forwarder")
		return
	}

	l7Protocol := connection.Protocol_UNKNOWN
	var clientHello *tlsutils.ClientHello
	var hostname string
	var protocols []string

	// check if the connection is a TLS connection
	isTLS, err := l7detect.DetectTLS(fc)
	if err != nil {
		logger.Error("failed to detect TLS", zap.Error(err))
		return
	}
	logger = logger.With(zap.Bool("is_tls", isTLS))
	logger = logger.With(zap.Bool("tls_termination_safe", fc.getMeta().TlsTerminationSafe))

	// if the connection is TLS and its safe to terminate TLS, then the connection is TLS terminateable
	isTLSTerminateable := isTLS && fc.getMeta().TlsTerminationSafe

	if isTLS {
		logger.Debug("detected TLS")

		// Capture the client's SNI and supported protocols before initiating upstream connection
		clientHello, err = l7detect.DetectClientHello(fc)
		if err != nil {
			logger.Error("failed to peek client hello", zap.Error(err))
			return
		}
		if clientHello.SNI != "" {
			hostname = clientHello.SNI
		}
		if len(clientHello.ALPNs) > 0 {
			protocols = clientHello.ALPNs
		}

		logger = logger.With(
			zap.String("hostname", hostname),
			zap.Strings("client_supported_protocols", protocols))

		logger.Debug("peeked client hello")
	}
	var upstreamConn net.Conn
	var usCookie uint64
	var downstreamConn bufferedConn
	targetIP := fc.getMeta().OriginalDstAddr.IP
	targetAddr := targetIP.String()
	targetPort := fc.getMeta().OriginalDstAddr.Port
	remoteUpstreamAddr := net.JoinHostPort(targetAddr, strconv.FormatUint(uint64(targetPort), 10))

	if isTLSTerminateable {
		// Establish the TLS connection upstream
		upstreamTLSConfig := &tls.Config{
			InsecureSkipVerify: false,
			ServerName:         hostname,
			NextProtos:         protocols,
		}

		usConn, err := tls.Dial("tcp", remoteUpstreamAddr, upstreamTLSConfig)
		if err != nil {
			logger.Error("upstream TLS handshake failed", zap.Error(err))
			return
		}
		upstreamConn = usConn

		fd, err := p.getRawFd(usConn.NetConn())
		if err != nil {
			logger.Error("failed to get raw connection", zap.Error(err))
			return
		}

		usCookie, err = p.getRawConnectionCookie(fd)
		if err != nil {
			logger.Error("failed to get upstream socket cookie", zap.Error(err))
			return
		}

		// Retrieve the ALPN values
		upstreamConnState := usConn.ConnectionState()
		negotiatedProtocol := upstreamConnState.NegotiatedProtocol

		logger = logger.With(
			zap.String("negotiated_protocol", negotiatedProtocol),
			zap.String("version", tls.VersionName(upstreamConnState.Version)),
			zap.String("cipher_suite", tls.CipherSuiteName(upstreamConnState.CipherSuite)))

		logger.Debug("upstream TLS handshake completed")

		// Prepare downstream TLS config
		downstreamProtos := []string{negotiatedProtocol}
		if negotiatedProtocol != "" {
			// If a protocol was negotiated, offer it first, then fall back to client's supported protocols
			downstreamProtos = append(downstreamProtos, protocols...)
		} else {
			// If no protocol was negotiated, just use client's supported protocols
			downstreamProtos = protocols
		}

		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS10,
			MaxVersion: tls.VersionTLS13,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			},
			GetCertificate: func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
				return p.certStore.GetCert(clientHello.ServerName)
			},
			NextProtos: downstreamProtos,
		}

		tlsClientConn := tls.Server(fc, tlsConfig)
		logger.Debug("initiating downstream TLS handshake")

		// Set a timeout for the handshake
		handshakeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Use a goroutine to perform the handshake
		handshakeErr := make(chan error, 1)
		go func() {
			err := tlsClientConn.HandshakeContext(handshakeCtx)
			if err != nil {
				logger.Debug("handshake error details",
					zap.Error(err), zap.String("error_type", fmt.Sprintf("%T", err)))
			}
			handshakeErr <- err
		}()

		// Wait for the handshake to complete or timeout
		select {
		case err = <-handshakeErr:
			if err != nil {
				logger.Error("downstream TLS handshake failed", zap.Error(err))

				if p.connEvents != nil {
					p.connEvents.WriteErrorEvent(
						fc.getMeta().Cookie,
						connection.ErrType_ClientTLSHandshake,
						"client handshake failed: "+err.Error())
				}

				return
			}
		case <-handshakeCtx.Done():
			logger.Error("downstream TLS handshake timed out")

			if p.connEvents != nil {
				p.connEvents.WriteErrorEvent(
					fc.getMeta().Cookie,
					connection.ErrType_ClientTLSHandshakeTimeout,
					"TLS handshake timed out")
			}
			return
		}

		// Log TLS connection details
		logger.Debug("downstream TLS handshake completed")

		// set the downstream connection to the buffered tls connection
		downstreamConn = newBufferedConn(tlsClientConn)

		// detect the protocol
		l7Protocol, err = l7detect.DetectProtocol(logger, downstreamConn)
		if err != nil {
			logger.Error("failed to detect protocol", zap.Error(err))
		}

		logger = logger.With(
			zap.String("L7_protocol", l7Protocol.String()))

		logger.Debug("detected protocol")
	} else {
		if !isTLS {
			// detect the protocol
			l7Protocol, err = l7detect.DetectProtocol(logger, fc)
			if err != nil {
				logger.Error("failed to detect protocol", zap.Error(err))
			}

			logger.Debug("detected protocol")
		}

		downstreamConn = fc.bufferedConn
		usConn, err := net.DialTimeout("tcp", remoteUpstreamAddr, 5*time.Second)
		if err != nil {
			logger.Error("failed to connect to original destination", zap.Error(err))
			return
		}
		upstreamConn = usConn

		fd, err := p.getRawFd(usConn)
		if err != nil {
			logger.Error("failed to get raw connection", zap.Error(err))
			return
		}

		usCookie, err = p.getRawConnectionCookie(fd)
		if err != nil {
			logger.Error("failed to get upstream socket cookie", zap.Error(err))
			return
		}
	}
	defer upstreamConn.Close()

	logger.Debug("connections established")

	if p.connEvents != nil {
		// Set type of client connection handler type
		p.connEvents.WriteHandlerTypeEvent(fc.getMeta().Cookie, connection.HandlerType_REDIRECTED)

		// write the original destination event
		err = p.connEvents.WriteOriginalDestinationEvent(fc.getMeta().Cookie, fc.getMeta().OriginalDstAddr)
		if err != nil {
			logger.Error("failed to write original destination event", zap.Error(err))
		}

		if clientHello != nil {
			p.connEvents.WriteTLSClientHelloEvent(fc.getMeta().Cookie, clientHello)
		}

		// set the handler type of egress connection to FORWARDING
		p.connEvents.WriteHandlerTypeEvent(usCookie, connection.HandlerType_FORWARDING)

		// write the protocol event, if it's known
		if l7Protocol != connection.Protocol_UNKNOWN {
			err = p.connEvents.WriteProtocolEvent(fc.getMeta().Cookie, l7Protocol, isTLS)
			if err != nil {
				logger.Error("failed to write protocol event", zap.Error(err))
			}
		}
	}

	// Determine whether to skip teeing (copying) the data for plugins
	// We only want to tee the data if:
	// 1. The connection is TLS terminatable (we can decrypt it) OR it's not TLS at all, AND
	// 2. The protocol is either HTTP/1 or HTTP/2
	canTee := (isTLSTerminateable || !isTLS) && (l7Protocol == connection.Protocol_HTTP1 || l7Protocol == connection.Protocol_HTTP2)
	skipTee := !canTee

	errChan := make(chan error, 2)
	go p.proxy(logger, downstreamConn, upstreamConn, errChan, fc.getMeta().Cookie, connection.Ingress, skipTee)
	go p.proxy(logger, upstreamConn, downstreamConn, errChan, fc.getMeta().Cookie, connection.Egress, skipTee)

	<-errChan

	logger.Debug("connections closed")
}

// isDestinationSelf checks if the destination address is the same as the listener address
func (p *Forwarder) isDestinationSelf(dst *net.TCPAddr) bool {
	// Check IPv4 listener
	if p.listener4 != nil {
		if addr, ok := p.listener4.Addr().(*net.TCPAddr); ok {
			if addr.Port == dst.Port && (addr.IP.Equal(dst.IP) || addr.IP.IsUnspecified()) {
				return true
			}
		}
	}

	// Check IPv6 listener
	if p.listener6 != nil {
		if addr, ok := p.listener6.Addr().(*net.TCPAddr); ok {
			if addr.Port == dst.Port && (addr.IP.Equal(dst.IP) || addr.IP.IsUnspecified()) {
				return true
			}
		}
	}

	return false
}

// retrieve a socket option from the socket file descriptor
func getsockopt(s int, level int, optname int, optval unsafe.Pointer, optlen *uint32) (err error) {
	_, _, e := syscall.Syscall6(
		syscall.SYS_GETSOCKOPT,
		uintptr(s),
		uintptr(level),
		uintptr(optname),
		uintptr(optval),
		uintptr(unsafe.Pointer(optlen)),
		0,
	)
	if e != 0 {
		return e
	}
	return
}

// multiWriter implements io.Writer to efficiently handle both destination writing
// and data teeing in a single pass. This eliminates the need for separate write
// operations and additional buffering.
//
// Performance benefits:
// 1. Single allocation per write for teeing (only when needed)
// 2. No additional buffering beyond what io.Copy provides
// 3. Minimal memory overhead when skipTee is true
type multiWriter struct {
	dst        io.Writer
	cookie     uint64
	direction  connection.Direction
	connEvents ConnectionEvents
	logger     *zap.Logger
	skipTee    bool
}

// Write implements io.Writer and handles both destination writing and data teeing
// in a single operation. This method is called by io.Copy internally.
//
// Performance considerations:
// - Only allocates new memory for teeing when absolutely necessary
// - Uses a single write operation to the destination
// - Copies data for teeing only when required (skipTee == false)
// - Maintains original data integrity by copying before async event writing
func (w *multiWriter) Write(p []byte) (n int, err error) {
	// Write to destination first to maintain low latency
	n, err = w.dst.Write(p)
	if err != nil {
		return n, err
	}

	// Tee the data only if needed, avoiding unnecessary allocations and copies
	if !w.skipTee && w.connEvents != nil {
		// Allocate and copy only the data that was successfully written
		teeDst := make([]byte, n)
		copy(teeDst, p[:n])
		// Async event writing doesn't block the main data flow
		if err := w.connEvents.WriteDataEvent(w.cookie, w.direction, teeDst); err != nil {
			w.logger.Error("failed to write data event",
				zap.Error(err),
				zap.String("direction", w.direction.String()))
		}
	}

	return n, nil
}

// proxy efficiently copies data between src and dst while optionally teeing the data
// for monitoring purposes. It uses io.Copy for optimal performance and handles
// the complexity of data copying and teeing through the multiWriter.
//
// Performance benefits:
// 1. Uses io.Copy's internal buffering which is optimized for different types of readers/writers
// 2. Avoids double buffering that would occur with manual read/write loops
// 3. Reduces memory allocations by reusing buffers internally
// 4. Minimizes syscalls by using appropriately sized buffers in io.Copy
// 5. Zero-copy operations when teeing is disabled
func (p *Forwarder) proxy(logger *zap.Logger, dst io.Writer, src io.Reader, errChan chan<- error, cookie uint64, direction connection.Direction, skipTee bool) {
	writer := &multiWriter{
		dst:        dst,
		cookie:     cookie,
		direction:  direction,
		connEvents: p.connEvents,
		logger:     logger,
		skipTee:    skipTee,
	}

	// io.Copy handles the actual data transfer with internal optimizations:
	// - Uses optimal buffer sizes
	// - Implements fast-path for known types
	// - Minimizes memory allocations
	_, err := io.Copy(writer, src)
	if err != nil && !errors.Is(err, io.EOF) {
		errChan <- err
	} else {
		errChan <- nil
	}
}
