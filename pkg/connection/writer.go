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

func (m *Manager) WriteProtocolEvent(key ConnKey, protocol Protocol, isTLS bool) error {
	m.logger.Debug("writing protocol event",
		zap.String("key", key.Key()),
		zap.String("protocol", protocol.String()),
		zap.Bool("is_tls", isTLS),
	)

	m.HandleEvent(ProtocolEvent{
		ConnKey:     key,
		TimestampNS: uint64(time.Now().UnixNano()),
		Protocol:    protocol,
		IsTLS:       isTLS,
	})

	return nil
}

func (m *Manager) WriteHostnameEvent(key ConnKey, hostname string) error {
	m.logger.Debug("writing hostname event",
		zap.String("key", key.Key()),
		zap.String("hostname", hostname),
	)

	m.HandleEvent(HostnameEvent{
		ConnKey: key,
		Name:    hostname,
	})

	return nil
}

func (m *Manager) WriteDataEvent(key ConnKey, direction Direction, data []byte) error {
	// Note: this is very noisy
	// m.logger.Debug("writing data event",
	// 	zap.Stringer("src", src),
	// 	zap.Stringer("dst", dst),
	// 	zap.String("direction", direction.String()),
	// 	zap.Int("size", len(data)))

	m.HandleEvent(DataEvent{
		ConnKey:   key,
		Direction: direction,
		Size:      int(len(data)),
		Data:      data,
	})

	return nil
}

func (m *Manager) WriteOriginalDestinationEvent(key ConnKey, originalDst *net.TCPAddr) error {
	m.logger.Debug("writing original destination event",
		zap.String("key", key.Key()),
		zap.Stringer("originalDst", originalDst),
	)

	m.HandleEvent(OriginalDestinationEvent{
		ConnKey:     key,
		Destination: qnet.NetAddrFromTCPAddr(originalDst),
	})

	return nil
}

func (m *Manager) WriteErrorEvent(key ConnKey, eventType ErrorEventType, message string) {
	m.logger.Debug("writing error event",
		zap.String("key", key.Key()),
		zap.String("event_type", string(eventType)),
		zap.String("message", message),
	)

	m.HandleEvent(ErrorEvent{
		ConnKey: key,
		Type:    eventType,
		Message: message,
	})
}

func (m *Manager) WriteHandlerTypeEvent(key ConnKey, handlerType HandlerType) {
	m.logger.Debug("writing connection handler type event",
		zap.String("key", key.Key()),
		zap.Stringer("handler_type", handlerType),
	)

	m.HandleEvent(HandlerTypeEvent{
		ConnKey: key,
		Type:    handlerType,
	})
}

func (m *Manager) WriteDoneEvent(key ConnKey) {
	m.logger.Debug("writing done event",
		zap.String("key", key.Key()),
	)

	m.HandleEvent(DoneEvent{
		ConnKey: key,
	})
}

func (m *Manager) WriteTLSClientHelloEvent(key ConnKey, h *tlsutils.ClientHello) {
	m.logger.Debug("writing handshake event",
		zap.String("key", key.Key()),
		zap.Any("client_hello", h),
	)

	m.HandleEvent(TLSClientHelloEvent{
		ConnKey: key,
		Msg:     h,
	})
}
