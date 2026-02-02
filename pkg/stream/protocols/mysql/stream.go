package mysql

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/rs/xid"
	"go.uber.org/zap"
)

// Stream implements connection.StreamProcessor for MySQL protocol
type Stream struct {
	ctx    context.Context
	logger *zap.Logger
	conn   *connection.Connection

	// Two parsers - named by semantic role
	requestParser  *Parser // Parses request (command) data
	responseParser *Parser // Parses response data

	// Correlation state
	pendingCommands []*PendingCommand

	// Plugin integration
	pluginManager *plugins.Manager
	domain        string

	closed bool
	mu     sync.Mutex
}

// PendingCommand represents a command awaiting its response
type PendingCommand struct {
	CommandType byte
	Query       string
	Timestamp   time.Time
	PluginConn  *plugins.Connection
}

type StreamOpt func(*Stream)

func SetPluginManager(manager *plugins.Manager) StreamOpt {
	return func(s *Stream) {
		s.pluginManager = manager
	}
}

func SetDomain(domain string) StreamOpt {
	return func(s *Stream) {
		s.domain = domain
	}
}

// NewStream creates a new MySQL stream processor
func NewStream(ctx context.Context, logger *zap.Logger, conn *connection.Connection, opts ...StreamOpt) *Stream {
	s := &Stream{
		ctx:             ctx,
		logger:          logger.With(zap.String("protocol", "mysql")),
		conn:            conn,
		requestParser:   NewParser(),
		responseParser:  NewParser(),
		pendingCommands: make([]*PendingCommand, 0),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Process handles incoming data events
func (s *Stream) Process(event *connection.DataEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	source := s.conn.OpenEvent.Source
	dir := event.Direction

	if isRequest(source, dir) {
		s.requestParser.Append(event.Data)
		s.processRequests()
	} else if isResponse(source, dir) {
		s.responseParser.Append(event.Data)
		s.processResponses()
	}

	return nil
}

// processRequests handles request (command) data
func (s *Stream) processRequests() {
	for {
		pkt, err := s.requestParser.ParsePacket()
		if err != nil {
			return // Incomplete or error, wait for more data
		}

		// Skip empty packets
		if len(pkt.Payload) == 0 {
			continue
		}

		cmd, data, err := ParseCommand(pkt)
		if err != nil {
			s.logger.Debug("mysql parse command error", zap.Error(err))
			continue
		}

		// Create pending command
		timestamp := time.Now()
		pending := &PendingCommand{
			CommandType: cmd,
			Timestamp:   timestamp,
		}

		// Extract query for COM_QUERY
		if cmd == ComQuery {
			if qc, ok := data.(*QueryCommand); ok {
				pending.Query = qc.Query
			}
		} else if cmd == ComStmtPrepare {
			if qc, ok := data.(*QueryCommand); ok {
				pending.Query = qc.Query
			}
		}

		// Create plugin connection for this command
		if s.pluginManager != nil && (cmd == ComQuery || cmd == ComStmtPrepare || cmd == ComStmtExecute) {
			requestID := xid.New().String()
			pluginConn, err := s.pluginManager.NewConnection(s.ctx, plugins.ConnectionType_MYSQL, s.conn, requestID)
			if err != nil {
				s.logger.Error("creating plugin connection", zap.Error(err))
			} else {
				pending.PluginConn = pluginConn
				pluginCmd := &plugins.MySQLCommand{
					Type:      cmd,
					Query:     pending.Query,
					Timestamp: timestamp,
				}
				if err := pluginConn.OnMySQLCommand(pluginCmd); err != nil {
					s.logger.Error("plugin mysql command", zap.Error(err))
				}
				pluginConn.Meta().SetReadBytes(int64(pkt.Length + 4))
			}
		}

		s.pendingCommands = append(s.pendingCommands, pending)

		s.logger.Debug("mysql command queued",
			zap.String("command", CommandName(cmd)),
			zap.String("query", s.truncateQuery(pending.Query)),
			zap.Int("pending_count", len(s.pendingCommands)))
	}
}

// processResponses handles response data and correlates with pending commands
func (s *Stream) processResponses() {
	for {
		pkt, err := s.responseParser.ParsePacket()
		if err != nil {
			return // Incomplete or error, wait for more data
		}

		// Skip empty packets
		if len(pkt.Payload) == 0 {
			continue
		}

		// Dequeue the oldest pending command for correlation
		if len(s.pendingCommands) == 0 {
			// Response without a pending command (could be handshake, auth, etc.)
			s.logUncorrelatedResponse(pkt)
			continue
		}

		pending := s.pendingCommands[0]
		s.pendingCommands = s.pendingCommands[1:]

		// Parse the response
		resp, err := ParseResponse(pkt)
		if err != nil {
			s.logger.Debug("mysql parse response error", zap.Error(err))
			if pending.PluginConn != nil {
				pending.PluginConn.Teardown()
			}
			continue
		}

		// Log the correlated pair
		s.logCorrelatedPair(pending, pkt, resp)

		// Call plugin and teardown
		if pending.PluginConn != nil {
			pluginResult := s.buildPluginResult(resp)
			if err := pending.PluginConn.OnMySQLResult(pluginResult); err != nil {
				s.logger.Error("plugin mysql result", zap.Error(err))
			}
			pending.PluginConn.Meta().SetWriteBytes(int64(pkt.Length + 4))
			pending.PluginConn.Teardown()
		}
	}
}

// logCorrelatedPair logs a correlated command and response pair
func (s *Stream) logCorrelatedPair(cmd *PendingCommand, pkt *Packet, resp interface{}) {
	latency := time.Since(cmd.Timestamp)

	fields := []zap.Field{
		zap.String("command", CommandName(cmd.CommandType)),
		zap.String("query", s.truncateQuery(cmd.Query)),
		zap.Duration("latency", latency),
	}

	switch r := resp.(type) {
	case *OKResponse:
		fields = append(fields,
			zap.String("response", "OK"),
			zap.Uint64("affected_rows", r.AffectedRows),
			zap.Uint64("last_insert_id", r.LastInsertID),
			zap.Uint16("warnings", r.Warnings))
	case *ERRResponse:
		fields = append(fields,
			zap.String("response", "ERR"),
			zap.Uint16("error_code", r.ErrorCode),
			zap.String("sql_state", r.SQLState),
			zap.String("error_message", r.ErrorMessage))
	case *ResultSet:
		fields = append(fields,
			zap.String("response", "ResultSet"),
			zap.Uint64("column_count", r.ColumnCount))
	case string:
		fields = append(fields, zap.String("response", r))
	default:
		fields = append(fields, zap.String("response", "unknown"))
	}

	if _, ok := resp.(*ERRResponse); ok {
		s.logger.Warn("mysql request/response", fields...)
	} else {
		s.logger.Debug("mysql request/response", fields...)
	}
}

// logUncorrelatedResponse logs responses without matching commands
func (s *Stream) logUncorrelatedResponse(pkt *Packet) {
	if len(pkt.Payload) == 0 {
		return
	}

	// Check if it's a handshake
	if pkt.SequenceID == 0 && pkt.Payload[0] == 0x0a {
		hs, err := ParseServerHandshake(pkt)
		if err == nil {
			s.logger.Debug("mysql server handshake",
				zap.String("version", hs.ServerVersion),
				zap.Uint32("connection_id", hs.ConnectionID))
			return
		}
	}

	s.logger.Debug("mysql response (uncorrelated)",
		zap.Uint8("seq_id", pkt.SequenceID),
		zap.Int("length", int(pkt.Length)))
}

// buildPluginResult converts parsed response to plugin result
func (s *Stream) buildPluginResult(resp interface{}) *plugins.MySQLResult {
	result := &plugins.MySQLResult{}

	switch r := resp.(type) {
	case *OKResponse:
		result.Type = "OK"
		result.AffectedRows = r.AffectedRows
		result.LastInsertID = r.LastInsertID
	case *ERRResponse:
		result.Type = "Error"
		result.ErrorCode = r.ErrorCode
		result.ErrorMessage = r.ErrorMessage
	case *ResultSet:
		result.Type = "ResultSet"
		// Note: We only get column count from first packet
		// Full result set parsing would require more packets
	case string:
		result.Type = r
	default:
		result.Type = "Unknown"
	}

	return result
}

// truncateQuery truncates long queries for logging
func (s *Stream) truncateQuery(query string) string {
	query = strings.TrimSpace(query)
	if len(query) > 100 {
		return query[:97] + "..."
	}
	return query
}

// Close marks the stream as closed
func (s *Stream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.pendingCommands) > 0 {
		s.logger.Warn("mysql stream closed with pending commands",
			zap.Int("pending_count", len(s.pendingCommands)))

		for _, pending := range s.pendingCommands {
			s.logger.Debug("mysql command without response",
				zap.String("command", CommandName(pending.CommandType)),
				zap.Duration("age", time.Since(pending.Timestamp)))
			if pending.PluginConn != nil {
				pending.PluginConn.Teardown()
			}
		}
	}

	s.logger.Debug("closing mysql stream")
	s.closed = true
}

// Closed returns whether the stream is closed
func (s *Stream) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// isRequest returns true when data is flowing Client → Server
func isRequest(source connection.Source, dir connection.Direction) bool {
	return (source == connection.Client && dir == connection.Egress) ||
		(source == connection.Server && dir == connection.Ingress)
}

// isResponse returns true when data is flowing Server → Client
func isResponse(source connection.Source, dir connection.Direction) bool {
	return (source == connection.Client && dir == connection.Ingress) ||
		(source == connection.Server && dir == connection.Egress)
}
