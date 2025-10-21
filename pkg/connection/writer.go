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

func (m *Manager) WriteProtocolEvent(connKey ConnKey, protocol Protocol, isTLS bool) error {
	m.logger.Debug("writing protocol event",
		zap.String("conn_key", connKey.String()),
		zap.String("protocol", protocol.String()),
		zap.Bool("is_tls", isTLS),
	)

	m.HandleEvent(ProtocolEvent{
		ConnKey:     connKey,
		TimestampNS: uint64(time.Now().UnixNano()),
		Protocol:    protocol,
		IsTLS:       isTLS,
	})

	return nil
}

func (m *Manager) WriteHostnameEvent(connKey ConnKey, hostname string) error {
	m.logger.Debug("writing hostname event",
		zap.String("conn_key", connKey.String()),
		zap.String("hostname", hostname),
	)

	m.HandleEvent(HostnameEvent{
		ConnKey: connKey,
		Name:    hostname,
	})

	return nil
}

func (m *Manager) WriteDataEvent(connKey ConnKey, direction Direction, data []byte) error {
	// Note: this is very noisy
	// m.logger.Debug("writing data event",
	// 	zap.Stringer("src", src),
	// 	zap.Stringer("dst", dst),
	// 	zap.String("direction", direction.String()),
	// 	zap.Int("size", len(data)))

	m.HandleEvent(DataEvent{
		ConnKey:   connKey,
		Direction: direction,
		Size:      int(len(data)),
		Data:      data,
	})

	return nil
}

func (m *Manager) WriteOriginalDestinationEvent(connKey ConnKey, originalDst *net.TCPAddr) error {
	m.logger.Debug("writing original destination event",
		zap.String("conn_key", connKey.String()),
		zap.Stringer("originalDst", originalDst),
	)

	m.HandleEvent(OriginalDestinationEvent{
		ConnKey:     connKey,
		Destination: qnet.NetAddrFromTCPAddr(originalDst),
	})

	return nil
}

func (m *Manager) WriteErrorEvent(connKey ConnKey, eventType ErrorEventType, message string) {
	m.logger.Debug("writing error event",
		zap.String("conn_key", connKey.String()),
		zap.String("event_type", string(eventType)),
		zap.String("message", message),
	)

	m.HandleEvent(ErrorEvent{
		ConnKey: connKey,
		Type:    eventType,
		Message: message,
	})
}

func (m *Manager) WriteHandlerTypeEvent(connKey ConnKey, handlerType HandlerType) {
	m.logger.Debug("writing connection handler type event",
		zap.String("conn_key", connKey.String()),
		zap.Stringer("handler_type", handlerType),
	)

	m.HandleEvent(HandlerTypeEvent{
		ConnKey: connKey,
		Type:    handlerType,
	})
}

func (m *Manager) WriteDoneEvent(connKey ConnKey) {
	m.logger.Debug("writing done event",
		zap.String("conn_key", connKey.String()),
	)

	m.HandleEvent(DoneEvent{
		ConnKey: connKey,
	})
}

func (m *Manager) WriteTLSClientHelloEvent(connKey ConnKey, h *tlsutils.ClientHello) {
	m.logger.Debug("writing handshake event",
		zap.String("conn_key", connKey.String()),
		zap.Any("client_hello", h),
	)

	m.HandleEvent(TLSClientHelloEvent{
		ConnKey: connKey,
		Msg:     h,
	})
}
