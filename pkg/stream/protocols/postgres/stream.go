package postgres

// PostgreSQL Protocol Behavior:
//
// PostgreSQL is client-first: the client sends either SSLRequest or StartupMessage
// as the first bytes. The server never speaks first (unlike MySQL).
//
// The startup phase is:
//   1. Client may send SSLRequest → server replies 'S'/'N' → TLS upgrade
//   2. Client sends StartupMessage (version + params)
//   3. Server sends auth challenges → client responds
//   4. Server sends ParameterStatus, BackendKeyData, ReadyForQuery
//
// After startup, there are two query protocols:
//   - Simple Query: Client sends 'Q' with full SQL → server sends results → ReadyForQuery
//   - Extended Query: Client sends Parse/Bind/Execute/Sync → server sends results → ReadyForQuery
//
// Our correlation approach:
//   - Simple Query: FIFO — each 'Q' message matches the next CommandComplete/ErrorResponse
//   - Extended Query: Track Parse messages (which contain SQL) → match to CommandComplete/ErrorResponse
//   - Both terminate with ReadyForQuery
//
// Direction detection uses the same isRequest/isResponse pattern as MySQL/Redis.

import (
	"context"
	"encoding/binary"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/rs/xid"
	"go.uber.org/zap"
)

// Stream state machine
type streamState int

const (
	stateInit  streamState = iota // Waiting for first message
	stateSSL                      // SSL negotiation in progress
	stateAuth                     // Authentication in progress
	stateReady                    // Ready for queries
)

// PendingCommand represents a command awaiting its response
type PendingCommand struct {
	Query      string
	Timestamp  time.Time
	PluginConn *plugins.Connection

	// Result row accumulation
	Columns   []ColumnInfo
	Rows      [][]string
	NullFlags [][]bool
	Truncated bool
}

// Stream implements connection.StreamProcessor for PostgreSQL protocol
type Stream struct {
	ctx    context.Context
	logger *zap.Logger
	conn   *connection.Connection

	// Two parsers — named by semantic role
	requestParser  *Parser // Parses request (client→server) data
	responseParser *Parser // Parses response (server→client) data

	// State machine
	state streamState

	// Correlation state
	pendingCommands []*PendingCommand

	// Extended query tracking
	// Maps statement name → SQL from the most recent Parse message
	preparedStatements map[string]string
	// The SQL text from the most recent Parse for unnamed statements
	lastParseSQL string
	// SQL resolved from the most recent Bind, waiting for Execute
	pendingBindSQL []string

	// Plugin integration
	pluginManager *plugins.Manager
	domain        string

	closed bool
	mu     sync.Mutex
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

// NewStream creates a new PostgreSQL stream processor
func NewStream(ctx context.Context, logger *zap.Logger, conn *connection.Connection, opts ...StreamOpt) *Stream {
	s := &Stream{
		ctx:                ctx,
		logger:             logger.With(zap.String("protocol", "postgres")),
		conn:               conn,
		requestParser:      NewParser(),
		responseParser:     NewParser(),
		state:              stateInit,
		pendingCommands:    make([]*PendingCommand, 0),
		preparedStatements: make(map[string]string),
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

// processRequests handles request (client→server) data
func (s *Stream) processRequests() {
	for {
		switch s.state {
		case stateInit:
			// First message from client is untyped (StartupMessage, SSLRequest, etc.)
			msg, err := s.requestParser.ParseStartupMessage()
			if err != nil {
				return // need more data
			}

			if IsSSLRequest(msg.Payload) {
				s.logger.Debug("postgres SSLRequest detected")
				s.state = stateSSL
				continue
			}

			if IsCancelRequest(msg.Payload) {
				s.logger.Debug("postgres CancelRequest (ignoring)")
				continue
			}

			// Must be a StartupMessage
			version, params := ParseStartupPayload(msg.Payload)
			if version == ProtocolVersion30 || version == ProtocolVersion32 {
				s.logger.Debug("postgres StartupMessage",
					zap.Uint32("version", version),
					zap.String("user", params["user"]),
					zap.String("database", params["database"]))
				s.state = stateAuth
			} else {
				s.logger.Debug("postgres unknown startup version",
					zap.Uint32("version", version))
				s.state = stateAuth
			}

		case stateSSL:
			// After SSL negotiation, client sends StartupMessage
			// The SSL handshake itself is transparent to us (BPF handles it)
			// We might see the StartupMessage directly
			msg, err := s.requestParser.ParseStartupMessage()
			if err != nil {
				return
			}

			version, params := ParseStartupPayload(msg.Payload)
			s.logger.Debug("postgres StartupMessage after SSL",
				zap.Uint32("version", version),
				zap.String("user", params["user"]),
				zap.String("database", params["database"]))
			s.state = stateAuth

		case stateAuth:
			// During auth, client sends password/SASL responses (typed messages)
			msg, err := s.requestParser.ParseMessage()
			if err != nil {
				return
			}
			s.logger.Debug("postgres auth message",
				zap.String("type", string(msg.Type)))
			// Just consume auth messages; we transition to Ready when we see ReadyForQuery

		case stateReady:
			msg, err := s.requestParser.ParseMessage()
			if err != nil {
				return
			}

			switch msg.Type {
			case MsgQuery:
				// Simple Query Protocol
				// Keep the full multi-statement SQL together. Each CommandComplete
				// from the server will correlate with this pending command.
				sql := ParseQueryMessage(msg.Payload)
				s.enqueueCommand(sql)

			case MsgParse:
				// Extended Query Protocol: Parse contains the SQL
				stmtName, sql := ParseParseMessage(msg.Payload)
				if stmtName == "" {
					// Unnamed (most common)
					s.lastParseSQL = sql
				} else {
					// Named prepared statement
					s.preparedStatements[stmtName] = sql
				}

			case MsgBind:
				// Bind message: resolve statement name → SQL and queue for Execute
				_, stmtName := ParseBindMessage(msg.Payload)
				var sql string
				if stmtName == "" {
					sql = s.lastParseSQL
				} else {
					sql = s.preparedStatements[stmtName]
				}
				s.pendingBindSQL = append(s.pendingBindSQL, sql)

			case MsgExecute:
				// Execute triggers the query — dequeue from pendingBindSQL
				var sql string
				if len(s.pendingBindSQL) > 0 {
					sql = s.pendingBindSQL[0]
					s.pendingBindSQL = s.pendingBindSQL[1:]
				}
				if sql != "" {
					s.enqueueCommand(sql)
				}

			case MsgSync:
				// Sync marks end of an extended query batch — nothing to do

			case MsgClose:
				// Close message: clean up named prepared statements
				closeType, closeName := ParseCloseMessage(msg.Payload)
				if closeType == 'S' && closeName != "" {
					delete(s.preparedStatements, closeName)
				}

			case MsgDescribe, MsgFlush:
				// Informational messages, skip

			case MsgTerminate:
				s.logger.Debug("postgres client terminate")

			case MsgCopyData, MsgCopyDone, MsgCopyFail:
				// COPY operations, skip

			default:
				s.logger.Debug("postgres unknown request message",
					zap.String("type", string(msg.Type)))
			}
		}
	}
}

// processResponses handles response (server→client) data
func (s *Stream) processResponses() {
	for {
		switch s.state {
		case stateSSL:
			// Server responds with single byte: 'S' (ok) or 'N' (no)
			resp, err := s.responseParser.ParseSSLResponse()
			if err != nil {
				return
			}
			if resp == 'S' {
				s.logger.Debug("postgres SSL accepted")
			} else {
				s.logger.Debug("postgres SSL rejected", zap.String("response", string(resp)))
			}
			// After SSL response, we stay in SSL state until we see the StartupMessage
			// from the request side (which transitions us to Auth)
			return

		case stateInit, stateAuth:
			// Parse typed messages during startup/auth
			msg, err := s.responseParser.ParseMessage()
			if err != nil {
				return
			}

			switch msg.Type {
			case MsgAuthentication:
				if len(msg.Payload) >= 4 {
					authType := binary.BigEndian.Uint32(msg.Payload[:4])
					if authType == AuthOk {
						s.logger.Debug("postgres authentication OK")
					}
				}

			case MsgParameterStatus:
				// Server sends runtime parameter (server_version, etc.)
				// Skip for now

			case MsgBackendKeyData:
				// Process ID + secret key for cancellation
				// Skip

			case MsgReadyForQuery:
				// Transition to ready state
				if len(msg.Payload) >= 1 {
					txStatus := msg.Payload[0]
					s.logger.Debug("postgres ready for query",
						zap.String("tx_status", string(txStatus)))
				}
				s.state = stateReady

			case MsgErrorResponse:
				severity, code, message := ParseErrorResponse(msg.Payload)
				// Server-side errors during startup (bad credentials, etc.) are
				// not qtap issues — log at debug to avoid alarming customers.
				s.logger.Debug("postgres error during startup",
					zap.String("severity", severity),
					zap.String("code", code),
					zap.String("message", message))

			case MsgNoticeResponse:
				// Notices during startup, skip

			default:
				s.logger.Debug("postgres startup response",
					zap.String("type", string(msg.Type)))
			}

		case stateReady:
			msg, err := s.responseParser.ParseMessage()
			if err != nil {
				return
			}

			switch msg.Type {
			case MsgCommandComplete:
				tag := ParseCommandComplete(msg.Payload)
				s.completeCommand(tag, false)

			case MsgErrorResponse:
				severity, code, message := ParseErrorResponse(msg.Payload)
				s.completeCommandWithError(severity, code, message)

			case MsgEmptyQueryResponse:
				s.completeCommand("", true)

			case MsgReadyForQuery:
				// End of a query cycle. Drain any leftover pending commands
				// from aborted multi-statement batches.
				s.drainAbortedCommands()

			case MsgRowDescription:
				s.handleRowDescription(msg.Payload)

			case MsgDataRow:
				s.handleDataRow(msg.Payload)

			case MsgParseComplete, MsgBindComplete, MsgCloseComplete:
				// Extended query confirmations, skip

			case MsgParameterDescription, MsgNoData:
				// Extended query describe responses, skip

			case MsgPortalSuspended:
				// Portal suspended (partial results), skip

			case MsgNotificationResponse:
				// Async LISTEN/NOTIFY, skip

			case MsgParameterStatus:
				// Runtime parameter change, skip

			case MsgCopyInResponse, MsgCopyOutResponse, MsgCopyBothResponse:
				// COPY responses, skip

			case MsgNoticeResponse:
				// Notice, skip

			default:
				s.logger.Debug("postgres unknown response message",
					zap.String("type", string(msg.Type)))
			}
		}
	}
}

// handleRowDescription parses and stores column metadata on the current pending command
func (s *Stream) handleRowDescription(payload []byte) {
	if len(s.pendingCommands) == 0 {
		return
	}
	cols, err := ParseRowDescription(payload)
	if err != nil {
		s.logger.Debug("postgres failed to parse RowDescription", zap.Error(err))
		return
	}
	pending := s.pendingCommands[0]
	pending.Columns = cols
	pending.Rows = nil
	pending.NullFlags = nil
}

// handleDataRow parses and accumulates a data row on the current pending command
func (s *Stream) handleDataRow(payload []byte) {
	if len(s.pendingCommands) == 0 {
		return
	}
	pending := s.pendingCommands[0]

	if len(pending.Rows) >= MaxRows {
		pending.Truncated = true
		return
	}

	values, nulls, valueTruncated, err := ParseDataRow(payload)
	if err != nil {
		s.logger.Debug("postgres failed to parse DataRow", zap.Error(err))
		return
	}
	if valueTruncated {
		pending.Truncated = true
	}
	pending.Rows = append(pending.Rows, values)
	pending.NullFlags = append(pending.NullFlags, nulls)
}

// enqueueCommand creates a pending command for correlation
func (s *Stream) enqueueCommand(sql string) {
	timestamp := time.Now()
	pending := &PendingCommand{
		Query:     sql,
		Timestamp: timestamp,
	}

	// Create plugin connection
	if s.pluginManager != nil {
		requestID := xid.New().String()
		pluginConn, err := s.pluginManager.NewConnection(s.ctx, plugins.ConnectionType_POSTGRES, s.conn, requestID)
		if err != nil {
			s.logger.Debug("creating plugin connection", zap.Error(err))
		} else if pluginConn != nil {
			pending.PluginConn = pluginConn
			pluginCmd := &plugins.PostgresCommand{
				Query:     sql,
				Timestamp: timestamp,
			}
			if err := pluginConn.OnPostgresCommand(pluginCmd); err != nil {
				s.logger.Debug("plugin postgres command", zap.Error(err))
			}
		}
	}

	const maxPendingCommands = 1000
	if len(s.pendingCommands) >= maxPendingCommands {
		dropped := s.pendingCommands[0]
		s.logger.Warn("postgres pending commands queue full, dropping oldest",
			zap.String("dropped_query", truncateQuery(dropped.Query)))
		if dropped.PluginConn != nil {
			dropped.PluginConn.Teardown()
		}
		copy(s.pendingCommands, s.pendingCommands[1:])
		s.pendingCommands = s.pendingCommands[:len(s.pendingCommands)-1]
	}

	s.pendingCommands = append(s.pendingCommands, pending)

	s.logger.Debug("postgres command queued",
		zap.String("query", truncateQuery(sql)),
		zap.Int("pending_count", len(s.pendingCommands)))
}

// completeCommand matches a CommandComplete to the oldest pending command
func (s *Stream) completeCommand(tag string, isEmpty bool) {
	if len(s.pendingCommands) == 0 {
		// Response without a pending command (could be auto-commit or internal)
		if tag != "" {
			s.logger.Debug("postgres uncorrelated command complete",
				zap.String("tag", tag))
		}
		return
	}

	pending := s.pendingCommands[0]
	s.pendingCommands = s.pendingCommands[1:]

	latency := time.Since(pending.Timestamp)

	resultType := "CommandComplete"
	if isEmpty {
		resultType = "EmptyQuery"
	}

	s.logger.Debug("postgres request/response",
		zap.String("query", truncateQuery(pending.Query)),
		zap.String("tag", tag),
		zap.String("result", resultType),
		zap.Duration("latency", latency))

	// Call plugin
	if pending.PluginConn != nil {
		pluginResult := &plugins.PostgresResult{
			Type:       resultType,
			CommandTag: tag,
			RowCount:   parseRowCount(tag),
			Rows:       pending.Rows,
			NullFlags:  pending.NullFlags,
			Truncated:  pending.Truncated,
		}
		// Extract column names
		if len(pending.Columns) > 0 {
			pluginResult.Columns = make([]string, len(pending.Columns))
			for i, col := range pending.Columns {
				pluginResult.Columns[i] = col.Name
			}
		}
		if err := pending.PluginConn.OnPostgresResult(pluginResult); err != nil {
			s.logger.Debug("plugin postgres result", zap.Error(err))
		}
		pending.PluginConn.Teardown()
	}
}

// completeCommandWithError matches an ErrorResponse to the oldest pending command
func (s *Stream) completeCommandWithError(severity, code, message string) {
	if len(s.pendingCommands) == 0 {
		// Uncorrelated errors can happen during normal operation (e.g.,
		// async notices, cancelled queries). Not a qtap issue.
		s.logger.Debug("postgres uncorrelated error",
			zap.String("severity", severity),
			zap.String("code", code),
			zap.String("message", message))
		return
	}

	pending := s.pendingCommands[0]
	s.pendingCommands = s.pendingCommands[1:]

	latency := time.Since(pending.Timestamp)

	// SQL errors are normal application behavior (syntax errors, missing
	// tables, permission denied, etc.) — not qtap issues. Log at debug.
	s.logger.Debug("postgres request/response",
		zap.String("query", truncateQuery(pending.Query)),
		zap.String("result", "Error"),
		zap.String("severity", severity),
		zap.String("code", code),
		zap.String("message", message),
		zap.Duration("latency", latency))

	// Call plugin
	if pending.PluginConn != nil {
		pluginResult := &plugins.PostgresResult{
			Type:         "Error",
			ErrorCode:    code,
			ErrorMessage: message,
		}
		if err := pending.PluginConn.OnPostgresResult(pluginResult); err != nil {
			s.logger.Debug("plugin postgres result", zap.Error(err))
		}
		pending.PluginConn.Teardown()
	}
}

// Close marks the stream as closed
func (s *Stream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.pendingCommands) > 0 {
		s.logger.Debug("postgres stream closed with pending commands",
			zap.Int("pending_count", len(s.pendingCommands)))

		for _, pending := range s.pendingCommands {
			s.logger.Debug("postgres command without response",
				zap.String("query", truncateQuery(pending.Query)),
				zap.Duration("age", time.Since(pending.Timestamp)))
			if pending.PluginConn != nil {
				pending.PluginConn.Teardown()
			}
		}
	}

	s.logger.Debug("closing postgres stream")
	s.closed = true
}

// Closed returns whether the stream is closed
func (s *Stream) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Helper functions

// truncateQuery truncates long queries for logging
// drainAbortedCommands cleans up pending commands that will never receive
// a response (e.g., remaining statements in a multi-statement simple query
// after the server hit an error and skipped them).
func (s *Stream) drainAbortedCommands() {
	for _, pending := range s.pendingCommands {
		s.logger.Debug("postgres draining aborted command",
			zap.String("query", truncateQuery(pending.Query)))
		if pending.PluginConn != nil {
			pluginResult := &plugins.PostgresResult{
				Type:         "Aborted",
				ErrorMessage: "statement skipped (earlier statement in batch failed)",
			}
			if err := pending.PluginConn.OnPostgresResult(pluginResult); err != nil {
				s.logger.Debug("plugin postgres result", zap.Error(err))
			}
			pending.PluginConn.Teardown()
		}
	}
	s.pendingCommands = s.pendingCommands[:0]
}

func truncateQuery(query string) string {
	query = strings.TrimSpace(query)
	if len(query) > 100 {
		return query[:97] + "..."
	}
	return query
}

// parseRowCount extracts the row count from a CommandComplete tag.
// Examples: "SELECT 5" → 5, "INSERT 0 3" → 3, "UPDATE 2" → 2
func parseRowCount(tag string) int64 {
	if tag == "" {
		return 0
	}
	parts := strings.Fields(tag)
	if len(parts) == 0 {
		return 0
	}
	// The row count is always the last numeric field
	last := parts[len(parts)-1]
	n, err := strconv.ParseInt(last, 10, 64)
	if err != nil {
		return 0
	}
	return n
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
