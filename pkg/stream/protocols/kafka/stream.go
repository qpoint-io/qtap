package kafka

import (
	"context"
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/rs/xid"
	"go.uber.org/zap"
)

// Stream implements connection.StreamProcessor for Kafka wire protocol
type Stream struct {
	ctx    context.Context
	logger *zap.Logger
	conn   *connection.Connection

	// Two parsers - named by semantic role
	requestParser  *Parser // Parses request (command) data
	responseParser *Parser // Parses response data

	// Correlation state: map of CorrelationID -> PendingRequest
	pendingRequests map[int32]*PendingRequest

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

// NewStream creates a new Kafka stream processor
func NewStream(ctx context.Context, logger *zap.Logger, conn *connection.Connection, opts ...StreamOpt) *Stream {
	s := &Stream{
		ctx:             ctx,
		logger:          logger.With(zap.String("protocol", "kafka")),
		conn:            conn,
		requestParser:   NewParser(),
		responseParser:  NewParser(),
		pendingRequests: make(map[int32]*PendingRequest),
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

// processRequests handles request data
func (s *Stream) processRequests() {
	for {
		req, err := s.requestParser.ParseRequest()
		if err != nil {
			return // Incomplete or error, wait for more data
		}

		timestamp := time.Now()
		operation := ApiKeyName(req.Header.ApiKey)

		pending := &PendingRequest{
			Request:   req,
			Timestamp: timestamp,
		}

		// Create a new plugin connection for this request
		if s.pluginManager != nil {
			requestID := xid.New().String()
			pluginConn, err := s.pluginManager.NewConnection(s.ctx, plugins.ConnectionType_KAFKA, s.conn, requestID)
			if err != nil {
				s.logger.Error("creating plugin connection", zap.Error(err))
			} else if pluginConn != nil {
				pending.PluginConn = pluginConn
				pluginCmd := &plugins.KafkaCommand{
					ApiKey:        req.Header.ApiKey,
					ApiVersion:    req.Header.ApiVersion,
					CorrelationID: req.Header.CorrelationID,
					ClientID:      req.Header.ClientID,
					Operation:     operation,
					Topics:        req.Topics,
					GroupID:       req.GroupID,
					Timestamp:     timestamp,
				}
				// Add Produce message samples
				for _, m := range req.Messages {
					pluginCmd.Messages = append(pluginCmd.Messages, plugins.KafkaMessage{
						Topic:     m.Topic,
						Partition: m.Partition,
						Key:       m.Key,
						Value:     m.Value,
						Truncated: m.Truncated,
					})
				}
				if err := pluginConn.OnKafkaCommand(pluginCmd); err != nil {
					s.logger.Error("plugin kafka command", zap.Error(err))
				}
				pluginConn.Meta().SetReadBytes(int64(req.TotalSize))
			}
		}

		// Store pending request keyed by CorrelationID
		s.pendingRequests[req.Header.CorrelationID] = pending

		// Build log fields
		fields := []zap.Field{
			zap.String("operation", operation),
			zap.Int16("api_key", req.Header.ApiKey),
			zap.Int16("api_version", req.Header.ApiVersion),
			zap.Int32("correlation_id", req.Header.CorrelationID),
			zap.String("client_id", req.Header.ClientID),
			zap.Int("pending_count", len(s.pendingRequests)),
		}
		if len(req.Topics) > 0 {
			fields = append(fields, zap.Strings("topics", req.Topics))
		}
		if req.GroupID != "" {
			fields = append(fields, zap.String("group_id", req.GroupID))
		}

		s.logger.Debug("kafka request", fields...)
	}
}

// processResponses handles response data and correlates with pending requests
func (s *Stream) processResponses() {
	for {
		resp, err := s.responseParser.ParseResponse()
		if err != nil {
			return // Incomplete or error, wait for more data
		}

		// Look up pending request by CorrelationID
		pending, ok := s.pendingRequests[resp.Header.CorrelationID]
		if !ok {
			s.logger.Debug("kafka response without pending request",
				zap.Int32("correlation_id", resp.Header.CorrelationID))
			continue
		}

		// Remove from pending
		delete(s.pendingRequests, resp.Header.CorrelationID)

		// Calculate latency
		latency := time.Since(pending.Timestamp)

		// Determine error status with API/version-aware extraction
		errorCode := resp.ErrorCode
		if code, ok := ExtractResponseErrorCode(resp.RawBody, pending.Request.Header.ApiKey, pending.Request.Header.ApiVersion); ok {
			errorCode = code
		}
		resp.ErrorCode = errorCode
		isError := errorCode != 0
		operation := ApiKeyName(pending.Request.Header.ApiKey)

		// Build log fields
		fields := []zap.Field{
			zap.String("operation", operation),
			zap.Int16("api_key", pending.Request.Header.ApiKey),
			zap.Int16("api_version", pending.Request.Header.ApiVersion),
			zap.Int32("correlation_id", resp.Header.CorrelationID),
			zap.String("client_id", pending.Request.Header.ClientID),
			zap.Duration("latency", latency),
			zap.Bool("error", isError),
		}
		if len(pending.Request.Topics) > 0 {
			fields = append(fields, zap.Strings("topics", pending.Request.Topics))
		}
		if pending.Request.GroupID != "" {
			fields = append(fields, zap.String("group_id", pending.Request.GroupID))
		}
		if isError {
			fields = append(fields, zap.Int16("error_code", resp.ErrorCode))
			fields = append(fields, zap.String("error_name", KafkaErrorName(resp.ErrorCode)))
		}

		// Log at appropriate level
		if isError {
			s.logger.Warn("kafka request/response", fields...)
		} else {
			s.logger.Debug("kafka request/response", fields...)
		}

		// Call plugin and teardown
		if pending.PluginConn != nil {
			// Extract Fetch response messages
			var fetchMessages []plugins.KafkaMessage
			if pending.Request.Header.ApiKey == ApiKeyFetch && resp.RawBody != nil {
				msgs := ExtractFetchResponseMessages(resp.RawBody, pending.Request.Header.ApiVersion)
				for _, m := range msgs {
					fetchMessages = append(fetchMessages, plugins.KafkaMessage{
						Topic:     m.Topic,
						Partition: m.Partition,
						Key:       m.Key,
						Value:     m.Value,
						Truncated: m.Truncated,
					})
				}
			}

			pluginResult := &plugins.KafkaResult{
				CorrelationID: resp.Header.CorrelationID,
				ErrorCode:     resp.ErrorCode,
				IsError:       isError,
				Messages:      fetchMessages,
			}
			if isError {
				pluginResult.ErrorMessage = KafkaErrorName(resp.ErrorCode)
			}
			if err := pending.PluginConn.OnKafkaResult(pluginResult); err != nil {
				s.logger.Error("plugin kafka result", zap.Error(err))
			}
			pending.PluginConn.Meta().SetWriteBytes(int64(resp.TotalSize))
			pending.PluginConn.Teardown()
		}
	}
}

// Close marks the stream as closed
func (s *Stream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Log warning if there are pending requests without responses
	if len(s.pendingRequests) > 0 {
		s.logger.Warn("kafka stream closed with pending requests",
			zap.Int("pending_count", len(s.pendingRequests)))

		for corrID, pending := range s.pendingRequests {
			s.logger.Debug("kafka request without response",
				zap.Int32("correlation_id", corrID),
				zap.String("operation", ApiKeyName(pending.Request.Header.ApiKey)),
				zap.Duration("age", time.Since(pending.Timestamp)))
			if pending.PluginConn != nil {
				pending.PluginConn.Teardown()
			}
		}
	}

	s.logger.Debug("closing kafka stream")
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
