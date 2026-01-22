package redis

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

// Stream implements connection.StreamProcessor for Redis RESP protocol
type Stream struct {
	ctx    context.Context
	logger *zap.Logger
	conn   *connection.Connection

	// Two parsers - named by semantic role
	requestParser  *Parser // Parses request (command) data
	responseParser *Parser // Parses response data

	// Correlation state
	pendingCommands *CommandQueue

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

// NewStream creates a new Redis stream processor
func NewStream(ctx context.Context, logger *zap.Logger, conn *connection.Connection, opts ...StreamOpt) *Stream {
	logger.Warn("🆕 new redis stream", zap.String("connection", conn.ID()), zap.String("direction", conn.Direction()))
	s := &Stream{
		ctx:             ctx,
		logger:          logger.With(zap.String("protocol", "redis")),
		conn:            conn,
		requestParser:   NewParser(),
		responseParser:  NewParser(),
		pendingCommands: NewCommandQueue(),
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
		value, err := s.requestParser.Parse()
		if err != nil {
			return // Incomplete or error, wait for more data
		}

		cmd, ok := value.ToCommand()
		if !ok {
			// Not a command array, log as raw value
			s.logger.Debug("redis request value",
				zap.String("type", value.Type.String()))
			continue
		}

		// Create pending command and enqueue
		timestamp := time.Now()
		pending := &PendingCommand{
			Command:   cmd,
			Timestamp: timestamp,
		}

		// Create a new plugin connection for this command
		if s.pluginManager != nil {
			requestID := xid.New().String()
			pluginConn, err := s.pluginManager.NewConnection(s.ctx, plugins.ConnectionType_REDIS, s.conn, requestID)
			if err != nil {
				s.logger.Error("creating plugin connection", zap.Error(err))
			} else {
				pending.PluginConn = pluginConn
				pluginCmd := &plugins.RedisCommand{
					Name:      cmd.Name,
					Args:      cmd.Args,
					Timestamp: timestamp,
				}
				if err := pluginConn.OnRedisCommand(pluginCmd); err != nil {
					s.logger.Error("plugin redis command", zap.Error(err))
				}
			}
		}

		s.pendingCommands.Enqueue(pending)

		// Debug log for command queuing
		s.logger.Debug("redis command queued",
			zap.String("command", strings.ToUpper(cmd.Name)),
			zap.Int("argc", len(cmd.Args)),
			zap.Int("pending_count", s.pendingCommands.Len()))
	}
}

// processResponses handles response data and correlates with pending commands
func (s *Stream) processResponses() {
	for {
		value, err := s.responseParser.Parse()
		if err != nil {
			return // Incomplete or error, wait for more data
		}

		// Dequeue the oldest pending command for correlation
		pending := s.pendingCommands.Dequeue()
		if pending == nil {
			// Response without a pending command
			s.logger.Warn("redis response without pending command",
				zap.String("response_type", value.Type.String()))
			s.logUncorrelatedResponse(value)
			continue
		}

		// Log the correlated request/response pair
		s.logCorrelatedPair(pending, value)

		// Call plugin and teardown the connection for this command
		if pending.PluginConn != nil {
			pluginResult := &plugins.RedisResult{
				Type:    value.Type.String(),
				Value:   s.extractValue(value),
				IsError: value.Type == TypeError || value.Type == TypeBulkError,
			}
			if err := pending.PluginConn.OnRedisResult(pluginResult); err != nil {
				s.logger.Error("plugin redis result", zap.Error(err))
			}
			pending.PluginConn.Teardown()
		}
	}
}

// logCorrelatedPair logs a correlated command and response pair
func (s *Stream) logCorrelatedPair(cmd *PendingCommand, response *Value) {
	latency := time.Since(cmd.Timestamp)

	// Determine if response is an error
	isError := response.Type == TypeError || response.Type == TypeBulkError

	// Build base fields
	fields := []zap.Field{
		zap.String("command", strings.ToUpper(cmd.Command.Name)),
		zap.Int("argc", len(cmd.Command.Args)),
		zap.Strings("args", s.sanitizeArgs(cmd.Command.Name, cmd.Command.Args)),
		zap.String("response_type", response.Type.String()),
		zap.Duration("latency", latency),
		zap.Bool("error", isError),
	}

	// Add response-specific fields
	fields = append(fields, s.responseFields(response)...)

	// Log at appropriate level
	if isError {
		s.logger.Warn("redis request/response", fields...)
	} else {
		s.logger.Debug("redis request/response", fields...)
	}
}

// responseFields returns zap fields specific to the response type
func (s *Stream) responseFields(value *Value) []zap.Field {
	switch value.Type {
	case TypeSimpleString:
		return []zap.Field{zap.String("response_value", value.Str)}

	case TypeError:
		return []zap.Field{zap.String("error_message", value.Str)}

	case TypeBulkError:
		return []zap.Field{zap.String("error_message", value.Str)}

	case TypeInteger:
		return []zap.Field{zap.Int64("response_value", value.Int)}

	case TypeBulkString:
		if value.IsNull {
			return []zap.Field{zap.Bool("null", true)}
		}
		return []zap.Field{zap.Int("response_length", len(value.Str))}

	case TypeArray:
		if value.IsNull {
			return []zap.Field{zap.Bool("null", true)}
		}
		return []zap.Field{zap.Int("response_count", len(value.Array))}

	case TypeNull:
		return []zap.Field{zap.Bool("null", true)}

	case TypeBoolean:
		return []zap.Field{zap.Bool("response_value", value.Bool)}

	case TypeDouble:
		return []zap.Field{zap.Float64("response_value", value.Float)}

	case TypeBigNumber:
		return []zap.Field{zap.String("response_value", value.Str)}

	case TypeMap:
		return []zap.Field{zap.Int("response_count", len(value.Map))}

	case TypeSet:
		return []zap.Field{zap.Int("response_count", len(value.Array))}

	case TypeVerbatim:
		return []zap.Field{zap.Int("response_length", len(value.Str))}

	case TypePush:
		return []zap.Field{zap.Int("response_count", len(value.Array))}

	default:
		return nil
	}
}

// logUncorrelatedResponse logs a response that has no matching command
func (s *Stream) logUncorrelatedResponse(value *Value) {
	fields := []zap.Field{
		zap.String("type", value.Type.String()),
	}
	fields = append(fields, s.responseFields(value)...)
	s.logger.Info("redis response (uncorrelated)", fields...)
}

// extractValue converts a Redis Value to a Go any type for plugin consumption
func (s *Stream) extractValue(value *Value) any {
	switch value.Type {
	case TypeSimpleString, TypeError, TypeBulkError, TypeBulkString, TypeVerbatim, TypeBigNumber:
		if value.IsNull {
			return nil
		}
		return value.Str
	case TypeInteger:
		return value.Int
	case TypeBoolean:
		return value.Bool
	case TypeDouble:
		return value.Float
	case TypeNull:
		return nil
	case TypeArray, TypeSet, TypePush:
		if value.IsNull {
			return nil
		}
		result := make([]any, len(value.Array))
		for i := range value.Array {
			result[i] = s.extractValue(&value.Array[i])
		}
		return result
	case TypeMap:
		result := make(map[string]any, len(value.Map))
		for k := range value.Map {
			v := value.Map[k]
			result[k] = s.extractValue(&v)
		}
		return result
	default:
		return nil
	}
}

// sanitizeArgs returns args suitable for logging, redacting sensitive values
func (s *Stream) sanitizeArgs(cmd string, args []string) []string {
	cmd = strings.ToUpper(cmd)

	// Commands where we should redact argument values
	sensitiveCommands := map[string]bool{
		"AUTH":   true,
		"CONFIG": true, // CONFIG SET requirepass
	}

	if sensitiveCommands[cmd] {
		sanitized := make([]string, len(args))
		for i := range args {
			sanitized[i] = "[REDACTED]"
		}
		return sanitized
	}

	return args
}

// Close marks the stream as closed
func (s *Stream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Log warning if there are pending commands without responses
	remaining := s.pendingCommands.Clear()
	if len(remaining) > 0 {
		s.logger.Warn("redis stream closed with pending commands",
			zap.Int("pending_count", len(remaining)))

		// Log each pending command and teardown its plugin connection
		for _, pending := range remaining {
			s.logger.Debug("redis command without response",
				zap.String("command", pending.Command.Name),
				zap.Duration("age", time.Since(pending.Timestamp)))
			if pending.PluginConn != nil {
				pending.PluginConn.Teardown()
			}
		}
	}

	s.logger.Debug("closing redis stream")
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
