package connection

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/qpoint-io/qtap/pkg/dns"
	"go.uber.org/zap"
)

func (m *Manager) processOpenEvent(event OpenEvent) {
	if _, exists := m.connections.Load(event.Key()); exists {
		return
	}

	// Because the process manager runs independently of the connection manager,
	// very rarely a race will occur where we get the connection open event
	// before the process manager has discovered the process.
	//
	// We give a 50ms buffer to avoid this race. If we time out we will proceed
	// with creating the connection without the process. On following events,
	// the connection manager will attempt to rediscover the process.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	proc, err := m.processManager.Await(ctx, int(event.PID))
	if err != nil {
		m.logger.Warn("connection process has not been discovered yet",
			zap.Error(err),
			zap.Uint32("pid", event.PID))
	}

	// get associated dns record
	var dnsRecord *dns.Record
	if proc != nil {
		dnsRecord = m.dnsManager.Get(event.Remote.ToBytes(), proc.ContainerID)
	}

	// create connection and store it
	conn := NewConnection(
		context.Background(),
		m.logger,
		&event,
		WithProcess(proc),
		WithDNSRecord(dnsRecord),
		WithServices(m),
		WithTags(m.deploymentTags),
		WithControlManager(m.controlManager),
		WithServiceRegistry(m.serviceRegistry),
	)

	// store connection
	m.connections.Store(event.Key(), conn)

	m.logger.Debug("socket open event",
		zap.String("conn_id", conn.ID()),
		zap.Stringer("conn_key", event.ConnKey),
		zap.Uint64("timestamp", event.TimestampNS),
		zap.Stringer("source", event.Source),
		zap.Uint32("pid", event.PID),
		zap.Stringer("address_family", event.Remote.Family),
		zap.Stringer("socket_type", event.SocketType),
		zap.Stringer("local", event.Local),
		zap.Stringer("remote", event.Remote),
		zap.Bool("is_redirected", event.IsRedirected),
	)

	// report event
	conn.reportEvent(event)

	// start watching the connection
	conn.Open()
}

func (c *Connection) processEvent(event any) {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("panic while processing connection event",
				zap.Any("panic", r),
				zap.String("event", fmt.Sprintf("%T", event)))
		}
	}()

	c.reportEvent(event)

	switch e := event.(type) {
	case ProtocolEvent:
		c.processProtocolEvent(e)
	case DataEvent:
		c.processDataEvent(e)
	case HostnameEvent:
		c.processHostnameEvent(e)
	case OriginalDestinationEvent:
		c.processOriginalDestinationEvent(e)
	case ErrorEvent:
		c.processErrorEvent(e)
	case HandlerTypeEvent:
		c.processHandlerTypeEvent(e)
	case CloseEvent:
		c.processCloseEvent(e)
	case DoneEvent:
		c.processDoneEvent(e)
	case TLSClientHelloEvent:
		c.processTLSClientHelloEvent(e)
	}
}

func (c *Connection) processProtocolEvent(event ProtocolEvent) {
	c.logger.Debug("processing protocol event", zap.Any("event", event))

	c.Protocol = event.Protocol
	c.IsTLS = event.IsTLS

	// add to connection logger
	c.logger = c.logger.With(zap.Stringer("protocol", c.Protocol), zap.Bool("is_tls", c.IsTLS))

	if c.Protocol != Protocol_UNKNOWN {
		c.tags.Add("protocol", c.Protocol.String())
	}

	// report metrics
	connProtocolTotal.WithLabelValues(c.OpenEvent.Remote.IP.String(), strconv.Itoa(int(c.OpenEvent.Remote.Port)), c.Direction(), c.Protocol.String()).Inc()

	// use the protocol event to create a stream processor
	c.streamProcessor = c.services.createStreamer(c)
}

func (c *Connection) processHostnameEvent(event HostnameEvent) {
	c.logger.Debug("processing hostname event", zap.String("key", event.Key()), zap.String("hostname", event.Name))

	c.SetDomain(event.String())
}

func (c *Connection) processDataEvent(event DataEvent) {
	// note: this is very noisey
	c.logger.Debug("⭕ processing data event", zap.String("key", event.Key()), zap.Int("event_byte_count", len(event.Data)))

	// process the data event
	if c.streamProcessor != nil && !c.streamProcessor.Closed() && !c.skipStreamProcessing {
		err := c.streamProcessor.Process(&event)
		if err != nil {
			var unrecoverableErr ErrStreamUnrecoverable
			if errors.As(err, &unrecoverableErr) {
				c.logger.Debug("stream processor unrecoverable error", zap.Error(err))
				c.skipStreamProcessing = true
				c.skipStreamProcessingReason = err.Error()
			} else {
				c.logger.Error("error processing data event", zap.Error(err))
			}
		}
	} else {
		c.logger.Debug("stream processor is nil or closed, skipping data event",
			zap.Any("event", event),
			zap.Bool("skip_stream_processing", c.skipStreamProcessing),
			zap.String("skip_stream_processing_reason", c.skipStreamProcessingReason))
	}
}

func (c *Connection) processOriginalDestinationEvent(event OriginalDestinationEvent) {
	c.logger.Debug("processing original destination event", zap.Any("event", event))

	c.OriginalDestination = &event.Destination

	// set original destination as domain
	c.SetDomain(event.Destination.IP.String())
}

func (c *Connection) processErrorEvent(event ErrorEvent) {
	c.logger.Debug("processing error event", zap.Any("event", event))

	switch event.Type {
	case ErrType_ClientTLSHandshake, ErrType_ClientTLSHandshakeTimeout:
		if p := c.Process(); p != nil {
			c.logger.Warn(event.Message,
				zap.String("domain", c.Domain()),
				zap.Int("pid", p.Pid),
				zap.String("exe", p.Exe))

			if err := p.SetTlsOk(false); err != nil {
				c.logger.Error("failed to set tls ok",
					zap.Int("pid", p.Pid),
					zap.String("exe", p.Exe),
					zap.Error(err))
			}
		}
	}
}

func (c *Connection) processHandlerTypeEvent(event HandlerTypeEvent) {
	c.logger.Debug("processing connection handler type event", zap.Any("event", event))

	c.HandlerType = event.Type
}

func (c *Connection) processCloseEvent(event CloseEvent) {
	// set close event
	c.CloseEvent = &event

	// debug
	c.logger.Debug("socket close event",
		zap.String("conn_id", c.ID()),
		zap.Uint64("timestamp", event.TimestampNS),
		zap.Int64("write_bytes", event.WrBytes),
		zap.Int64("read_bytes", event.RdBytes),
	)

	// finalize connection if it's not held
	if !c.held {
		c.Close()
	}
}

func (c *Connection) processDoneEvent(event DoneEvent) {
	// debug
	c.logger.Debug("processing connection done event", zap.Any("event", event))

	// release the connection
	c.held = false

	// if the CloseEvent is nil, the socket has not closed yet
	// so we only release the hold, otherwise we finalize the connection
	if c.CloseEvent == nil {
		return
	}

	c.Close()
}

func (c *Connection) processTLSClientHelloEvent(event TLSClientHelloEvent) {
	c.logger.Debug("processing tls handshake event",
		zap.String("key", event.Key()),
		zap.Any("msg", event.Msg))

	// set the domain
	if event.Msg.SNI != "" {
		c.SetDomain(event.Msg.SNI)
	}

	// report metrics
	connTLSHandshakeTotal.WithLabelValues(c.OpenEvent.Remote.IP.String(), strconv.Itoa(int(c.OpenEvent.Remote.Port)), c.Direction(), event.Msg.SNI, event.Msg.Version.String()).Inc()

	c.TLSClientHello = event.Msg
}
