// pkg/services/eventstore/otel/otel.go
package otel

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"go.opentelemetry.io/otel/log"
	"go.uber.org/zap"
)

type EventStore struct {
	services.LogHelper
	eventstore.BaseEventStore

	conn        *connection.Connection
	logger      log.Logger
	objectStore objectstore.ObjectStore
	serviceName string
	environment string
}

type connidable interface {
	SetConnectionID(id string)
}

type taggable interface {
	AddTags(tag ...string)
}

// Save submits an event to OpenTelemetry
func (s *EventStore) Save(ctx context.Context, item any) {
	// Add connection metadata if available
	if s.conn != nil {
		if c, ok := item.(connidable); ok {
			c.SetConnectionID(s.conn.ID())
		}
		if t, ok := item.(taggable); ok {
			t.AddTags(s.conn.Tags().List()...)
		}
	}

	switch i := item.(type) {
	case *eventstore.Request:
		s.logEvent(ctx, i, log.SeverityInfo, fmt.Sprintf("HTTP %s (%d) %s", i.Method, i.Status, i.Url))
	case *eventstore.Issue:
		s.logEvent(ctx, i, log.SeverityError, fmt.Sprintf("HTTP Error: %s (%d) %s :: %s", i.Method, i.Status, i.URL, i.Error))
	case *eventstore.PIIEntity:
		s.logEvent(ctx, i, log.SeverityWarn, fmt.Sprintf("PII detected: %s (score: %.2f)", i.EntityType, i.Score))
	case *eventstore.ArtifactRecord:
		s.logEvent(ctx, i, log.SeverityInfo, fmt.Sprintf("Artifact stored: %s", i.Type))
	case *eventstore.Connection:
		host := "unknown"
		if i.System != nil {
			host = i.System.Hostname
		}
		s.logEvent(ctx, i, log.SeverityInfo, fmt.Sprintf("Connection: [%s via %s] %s → %s", i.Direction, i.SocketProtocol, host, i.EndpointId))
	case *eventstore.Artifact:
		go func() {
			ar, err := s.objectStore.Put(*i)
			if err != nil {
				// Use zap.L() as fallback if LogHelper not initialized
				if s.Log() != nil {
					s.Log().Error("failed to put artifact", zap.Error(err))
				} else {
					zap.L().Error("failed to put artifact", zap.Error(err))
				}
				return
			}
			// Only save artifact record if it's not nil
			if ar != nil {
				s.Save(ctx, ar)
			}
		}()
	default:
		// Safely log unsupported event type
		if s.Log() != nil {
			s.Log().Warn("unsupported event type",
				zap.String("type", fmt.Sprintf("%T", i)),
				zap.Any("item", i))
		} else {
			zap.L().Warn("unsupported event type",
				zap.String("type", fmt.Sprintf("%T", i)),
				zap.Any("item", i))
		}
	}
}

// logEvent is a generic method that converts any event struct to OTEL attributes using JSON marshaling
func (s *EventStore) logEvent(ctx context.Context, item any, severity log.Severity, bodyMessage string) {
	attrs, err := s.structToAttributes(item)
	if err != nil {
		if s.Log() != nil {
			s.Log().Error("failed to convert item to attributes", zap.Error(err))
		} else {
			zap.L().Error("failed to convert item to attributes", zap.Error(err))
		}
		return
	}

	// Add event type
	attrs = append([]log.KeyValue{log.String("event.type", s.getEventType(item))}, attrs...)

	// Extract timestamp from the item
	timestamp := s.extractTimestamp(item)

	record := log.Record{}
	record.SetTimestamp(timestamp)
	record.SetSeverity(severity)
	record.SetBody(log.StringValue(bodyMessage))
	record.AddAttributes(attrs...)

	s.logger.Emit(ctx, record)
	incrementSubmittedRecords()
}

// structToAttributes converts any struct to OTEL log attributes using JSON marshaling
func (s *EventStore) structToAttributes(item any) ([]log.KeyValue, error) {
	// Convert struct to map using JSON marshaling
	data, err := json.Marshal(item)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal item: %w", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal to map: %w", err)
	}

	// Convert map to OTEL attributes
	return s.mapToAttributes(m, ""), nil
}

// mapToAttributes recursively converts a map to OTEL log attributes
//
// Note: I'm not sure how I feel about this method of creating attributes.
func (s *EventStore) mapToAttributes(m map[string]any, prefix string) []log.KeyValue {
	var attrs []log.KeyValue

	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}

		// Skip empty values to reduce noise
		if v == nil || (reflect.ValueOf(v).Kind() == reflect.String && v.(string) == "") {
			continue
		}

		switch val := v.(type) {
		case string:
			attrs = append(attrs, log.String(key, val))
		case bool:
			attrs = append(attrs, log.Bool(key, val))
		case float64:
			// JSON unmarshaling converts all numbers to float64
			if val == float64(int64(val)) {
				// It's an integer
				attrs = append(attrs, log.Int64(key, int64(val)))
			} else {
				// It's a float
				attrs = append(attrs, log.Float64(key, val))
			}
		case []any:
			// Convert slice to OTEL slice
			values := make([]log.Value, len(val))
			for i, item := range val {
				values[i] = s.anyToLogValue(item)
			}
			attrs = append(attrs, log.Slice(key, values...))
		case map[string]any:
			// Recursively handle nested maps
			attrs = append(attrs, s.mapToAttributes(val, key)...)
		default:
			// Fallback to string representation
			attrs = append(attrs, log.String(key, fmt.Sprintf("%v", val)))
		}
	}

	return attrs
}

// anyToLogValue converts any value to OTEL log.Value
func (s *EventStore) anyToLogValue(v any) log.Value {
	switch val := v.(type) {
	case string:
		return log.StringValue(val)
	case bool:
		return log.BoolValue(val)
	case float64:
		if val == float64(int64(val)) {
			return log.Int64Value(int64(val))
		}
		return log.Float64Value(val)
	default:
		return log.StringValue(fmt.Sprintf("%v", val))
	}
}

// getEventType extracts the event type from the item
func (s *EventStore) getEventType(item any) string {
	switch item.(type) {
	case *eventstore.Request:
		return "request"
	case *eventstore.Issue:
		return "issue"
	case *eventstore.PIIEntity:
		return "pii_entity"
	case *eventstore.ArtifactRecord:
		return "artifact_record"
	case *eventstore.Connection:
		return "connection"
	default:
		return fmt.Sprintf("%T", item)
	}
}

// extractTimestamp extracts the timestamp from the item
func (s *EventStore) extractTimestamp(item any) time.Time {
	switch i := item.(type) {
	case *eventstore.Request:
		return i.Timestamp
	case *eventstore.Issue:
		return i.Timestamp
	case *eventstore.PIIEntity:
		return i.Timestamp
	case *eventstore.ArtifactRecord:
		return i.Timestamp
	case *eventstore.Connection:
		return i.Timestamp
	default:
		return time.Now()
	}
}

// SetConnection sets the connection context for events
func (s *EventStore) SetConnection(conn *connection.Connection) {
	s.conn = conn
}
