package connection

import (
	"errors"
	"net"
	"time"

	"github.com/qpoint-io/qtap/pkg/qnet"
	"github.com/qpoint-io/qtap/pkg/tlsutils"
	"go.uber.org/zap"
)

var (
	ErrConnectionNotFound = errors.New("connection not found")
)

func (m *Manager) WriteProtocolEvent(cookie uint64, protocol Protocol, isTLS bool) error {
	m.logger.Debug("writing protocol event",
		zap.Uint64("cookie", cookie),
		zap.String("protocol", protocol.String()),
		zap.Bool("is_tls", isTLS),
	)

	m.HandleEvent(ProtocolEvent{
		Cookie:      Cookie(cookie),
		TimestampNS: uint64(time.Now().UnixNano()),
		Protocol:    protocol,
		IsTLS:       isTLS,
	})

	return nil
}

func (m *Manager) WriteHostnameEvent(cookie uint64, hostname string) error {
	m.logger.Debug("writing hostname event",
		zap.Uint64("cookie", cookie),
		zap.String("hostname", hostname),
	)

	m.HandleEvent(HostnameEvent{
		Cookie: Cookie(cookie),
		Name:   hostname,
	})

	return nil
}

func (m *Manager) WriteDataEvent(cookie uint64, direction Direction, data []byte) error {
	// Note: this is very noisy
	// m.logger.Debug("writing data event",
	// 	zap.Stringer("src", src),
	// 	zap.Stringer("dst", dst),
	// 	zap.String("direction", direction.String()),
	// 	zap.Int("size", len(data)))

	m.HandleEvent(DataEvent{
		Cookie:    Cookie(cookie),
		Direction: direction,
		Size:      int(len(data)),
		Data:      data,
	})

	return nil
}

func (m *Manager) WriteOriginalDestinationEvent(cookie uint64, originalDst *net.TCPAddr) error {
	m.logger.Debug("writing original destination event",
		zap.Uint64("cookie", cookie),
		zap.Stringer("originalDst", originalDst),
	)

	m.HandleEvent(OriginalDestinationEvent{
		Cookie:      Cookie(cookie),
		Destination: qnet.NetAddrFromTCPAddr(originalDst),
	})

	return nil
}

func (m *Manager) WriteErrorEvent(cookie uint64, eventType ErrorEventType, message string) {
	m.logger.Debug("writing error event",
		zap.Uint64("cookie", cookie),
		zap.String("event_type", string(eventType)),
		zap.String("message", message),
	)

	m.HandleEvent(ErrorEvent{
		Cookie:  Cookie(cookie),
		Type:    eventType,
		Message: message,
	})
}

func (m *Manager) WriteHandlerTypeEvent(cookie uint64, handlerType HandlerType) {
	m.logger.Debug("writing connection handler type event",
		zap.Uint64("cookie", cookie),
		zap.Stringer("handler_type", handlerType),
	)

	m.HandleEvent(HandlerTypeEvent{
		Cookie: Cookie(cookie),
		Type:   handlerType,
	})
}

func (m *Manager) WriteDoneEvent(cookie uint64) {
	m.logger.Debug("writing done event",
		zap.Uint64("cookie", cookie),
	)

	m.HandleEvent(DoneEvent{
		Cookie: Cookie(cookie),
	})
}

func (m *Manager) WriteTLSClientHelloEvent(c uint64, h *tlsutils.ClientHello) {
	m.logger.Debug("writing tls client hello event",
		zap.Uint64("cookie", c),
		zap.Any("client_hello", h),
	)

	m.HandleEvent(TLSClientHelloEvent{
		Cookie: Cookie(c),
		Msg:    h,
	})
}

func (m *Manager) WriteTLSServerHelloEvent(c uint64, h *tlsutils.ServerHello) {
	m.logger.Debug("writing tls server hello event",
		zap.Uint64("cookie", c),
		zap.Any("server_hello", h),
	)

	m.HandleEvent(TLSServerHelloEvent{
		Cookie: Cookie(c),
		Msg:    h,
	})
}
