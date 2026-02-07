package mysql

// MySQL Protocol Behavior:
//
// Traditional MySQL is strictly synchronous - each command must wait for its
// response before the next command is sent. This is a half-duplex protocol.
//
// MySQL 5.7.12+ added optional command pipelining where multiple commands can
// be sent before responses arrive, but responses ALWAYS return in the same
// order as the commands were sent (like Redis or HTTP/1.1 pipelining).
//
// There is NO multiplexing within a single TCP connection - each connection
// handles exactly one session. Multiple concurrent sessions require multiple
// TCP connections. This is different from HTTP/2 or protocols with stream IDs.
//
// Our correlation approach uses a simple FIFO queue of pending commands,
// matching each response to the oldest pending command. This handles both:
// - Traditional synchronous MySQL (queue depth 0-1)
// - Pipelined MySQL 5.7.12+ (queue depth 0-N)

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

	// Handshake state
	handshakeComplete bool

	// Result set state machine
	resultSetState  resultSetPhase
	activeResultSet *ResultSet
	activeCommand   *PendingCommand
	resultSetBytes  int64 // track total response bytes for result sets

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

// resultSetPhase tracks where we are in multi-packet result set parsing
type resultSetPhase int

const (
	phaseNone       resultSetPhase = iota
	phaseColumnDefs                // reading column definition packets
	phaseColumnsEOF                // expecting optional EOF after column defs (deprecate-EOF compat)
	phaseRows                      // reading row data packets
)

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

		// Skip all request packets during the auth/handshake phase.
		// MySQL 8.0 caching_sha2_password can involve multiple
		// client→server exchanges (auth response, public key request,
		// encrypted password) before the server sends the final OK.
		// If we parse these as commands, their responses (AuthMoreData
		// packets starting with 0x01) get misidentified as result sets.
		if !s.handshakeComplete {
			s.logger.Debug("mysql skipping auth-phase packet",
				zap.Int("length", int(pkt.Length)))
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
				s.logger.Debug("creating plugin connection", zap.Error(err))
			} else if pluginConn != nil {
				pending.PluginConn = pluginConn
				pluginCmd := &plugins.MySQLCommand{
					Type:      cmd,
					Query:     pending.Query,
					Timestamp: timestamp,
				}
				if err := pluginConn.OnMySQLCommand(pluginCmd); err != nil {
					s.logger.Debug("plugin mysql command", zap.Error(err))
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

		// If we're in the middle of a multi-packet result set, handle it
		if s.resultSetState != phaseNone {
			s.processResultSetPacket(pkt)
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

		// If this is a result set header (column count), start collecting packets
		if rs, ok := resp.(*ResultSet); ok {
			s.resultSetState = phaseColumnDefs
			s.activeResultSet = rs
			s.activeResultSet.Columns = make([]ColumnDefinition, 0, rs.ColumnCount)
			s.activeResultSet.Rows = nil
			s.activeCommand = pending
			s.resultSetBytes = int64(pkt.Length + 4)
			continue
		}

		// Simple single-packet response (OK, ERR, EOF)
		s.completeResponse(pending, pkt, resp)
	}
}

// processResultSetPacket handles a packet that's part of an in-progress result set
func (s *Stream) processResultSetPacket(pkt *Packet) {
	s.resultSetBytes += int64(pkt.Length + 4)

	switch s.resultSetState {
	case phaseColumnDefs:
		// Check for EOF (end of column definitions in non-deprecate-EOF mode)
		if IsEOF(pkt) {
			s.resultSetState = phaseRows
			s.activeResultSet.Rows = make([][]Value, 0)
			return
		}
		// Check for ERR
		if IsError(pkt) {
			s.finishResultSet()
			return
		}
		// Parse column definition
		col, err := parseColumnDefinition(pkt.Payload)
		if err != nil {
			s.logger.Debug("mysql parse column definition error", zap.Error(err))
			s.finishResultSet()
			return
		}
		s.activeResultSet.Columns = append(s.activeResultSet.Columns, *col)

		// If we've received all expected column definitions, transition to
		// phaseColumnsEOF which handles both EOF-terminated and deprecate-EOF
		// protocols. The next packet is either an EOF (consumed and discarded)
		// or the first row data packet.
		if uint64(len(s.activeResultSet.Columns)) >= s.activeResultSet.ColumnCount {
			s.resultSetState = phaseColumnsEOF
			s.activeResultSet.Rows = make([][]Value, 0)
			return
		}

	case phaseColumnsEOF:
		// After all column definitions, we may receive an EOF packet (standard
		// protocol) or jump straight to row data (CLIENT_DEPRECATE_EOF).
		if IsEOF(pkt) {
			// Standard protocol: consume the EOF and move to rows
			s.resultSetState = phaseRows
			return
		}
		// No EOF — this is deprecate-EOF mode, the packet is already a row
		// (or ERR/OK terminator). Fall through to phaseRows handling.
		s.resultSetState = phaseRows
		s.processResultSetPacket(pkt)
		return

	case phaseRows:
		// Check for EOF (end of rows in standard protocol) or OK terminator
		// (CLIENT_DEPRECATE_EOF mode). In deprecate-EOF mode, the server
		// sends an OK packet with 0xFE header (length >= 9) instead of EOF.
		if IsEOF(pkt) || isOKTerminator(pkt) {
			s.finishResultSet()
			return
		}
		// Check for ERR
		if IsError(pkt) {
			s.finishResultSet()
			return
		}
		// Count every row, even those we don't store
		s.activeResultSet.RowCount++
		// Parse row (only if under the cap)
		if len(s.activeResultSet.Rows) < MaxResultSetRows {
			row, err := parseResultSetRow(pkt.Payload, int(s.activeResultSet.ColumnCount))
			if err != nil {
				s.logger.Debug("mysql parse row error", zap.Error(err))
				// Skip this row but continue
				return
			}
			s.activeResultSet.Rows = append(s.activeResultSet.Rows, row)
		}
		// If we've hit the cap, we still consume packets but don't store them
		// The Truncated flag will be set in buildPluginResult based on RowCount vs len(Rows)
	}
}

// finishResultSet completes result set collection and correlates with the pending command
func (s *Stream) finishResultSet() {
	rs := s.activeResultSet
	pending := s.activeCommand
	totalBytes := s.resultSetBytes

	// Reset state
	s.resultSetState = phaseNone
	s.activeResultSet = nil
	s.activeCommand = nil
	s.resultSetBytes = 0

	// Create a fake packet for logging (with aggregate size)
	fakePkt := &Packet{Length: uint32(totalBytes)}
	s.logCorrelatedPair(pending, fakePkt, rs)

	if pending.PluginConn != nil {
		pluginResult := s.buildPluginResult(rs)
		if err := pending.PluginConn.OnMySQLResult(pluginResult); err != nil {
			s.logger.Debug("plugin mysql result", zap.Error(err))
		}
		pending.PluginConn.Meta().SetWriteBytes(totalBytes)
		pending.PluginConn.Teardown()
	}
}

// completeResponse handles a single-packet response correlation
func (s *Stream) completeResponse(pending *PendingCommand, pkt *Packet, resp interface{}) {
	s.logCorrelatedPair(pending, pkt, resp)

	if pending.PluginConn != nil {
		pluginResult := s.buildPluginResult(resp)
		if err := pending.PluginConn.OnMySQLResult(pluginResult); err != nil {
			s.logger.Debug("plugin mysql result", zap.Error(err))
		}
		pending.PluginConn.Meta().SetWriteBytes(int64(pkt.Length + 4))
		pending.PluginConn.Teardown()
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
			zap.Uint64("column_count", r.ColumnCount),
			zap.Int("row_count", r.RowCount))
	case string:
		fields = append(fields, zap.String("response", r))
	default:
		fields = append(fields, zap.String("response", "unknown"))
	}

	// Log all request/response pairs at debug level — ERR responses from the
	// MySQL server are normal application behavior (syntax errors, permission
	// denied, etc.), not qtap issues. Don't alarm customers with warn/error.
	s.logger.Debug("mysql request/response", fields...)
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

	// Check if it's OK/ERR response completing the handshake
	if !s.handshakeComplete {
		if pkt.Payload[0] == 0x00 || pkt.Payload[0] == 0xfe || pkt.Payload[0] == 0xff {
			s.handshakeComplete = true
			s.logger.Debug("mysql handshake complete")
			return
		}
	}

	// Uncorrelated responses (handshake, auth result packets) are normal
	// during connection setup - don't log them
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
		result.RowCount = r.RowCount
		result.Columns = make([]string, len(r.Columns))
		for i, col := range r.Columns {
			result.Columns[i] = col.Name
		}
		result.Rows = make([][]string, len(r.Rows))
		for i, row := range r.Rows {
			result.Rows[i] = make([]string, len(row))
			for j, val := range row {
				result.Rows[i][j] = val.String()
			}
		}
		// If total rows exceeded the stored rows, it was truncated
		if r.RowCount > len(r.Rows) {
			result.Truncated = true
		}
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
		// Filter out COM_QUIT - it legitimately has no response
		realPending := make([]*PendingCommand, 0)
		for _, pending := range s.pendingCommands {
			if pending.CommandType == ComQuit {
				// COM_QUIT never gets a response, just teardown plugin
				if pending.PluginConn != nil {
					pending.PluginConn.Teardown()
				}
			} else {
				realPending = append(realPending, pending)
			}
		}

		if len(realPending) > 0 {
			s.logger.Debug("mysql stream closed with pending commands",
				zap.Int("pending_count", len(realPending)))

			for _, pending := range realPending {
				s.logger.Debug("mysql command without response",
					zap.String("command", CommandName(pending.CommandType)),
					zap.Duration("age", time.Since(pending.Timestamp)))
				if pending.PluginConn != nil {
					pending.PluginConn.Teardown()
				}
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
