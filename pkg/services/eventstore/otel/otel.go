// pkg/services/eventstore/otel/otel.go
package otel

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
	"go.opentelemetry.io/otel/log"
	"go.uber.org/zap"
)

var submittedRecords = metrics.NewCounter("records_submitted_total", "The number of records submitted to OpenTelemetry")

type EventStore struct {
	services.LogHelper
	eventstore.BaseEventStore

	logger      log.Logger
	serviceName string
	environment string
}

// Save submits an event to OpenTelemetry
func (s *EventStore) Save(ctx context.Context, item any) {
	ll := s.Log()
	switch i := item.(type) {
	case *eventstore.Request:
		s.logEvent(ctx, i, log.SeverityInfo, fmt.Sprintf("HTTP %s (%d) %s", i.Method, i.Status, i.Url))
	case *eventstore.DatabaseRequest:
		severity := log.SeverityInfo
		if i.IsError {
			severity = log.SeverityError
		}
		s.logEvent(ctx, i, severity, fmt.Sprintf("%s %s", i.DatabaseType, i.Statement))
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
		ll.DPanic("event stores do not support artifacts", zap.Any("artifact", i))
		return
	default:
		ll.Warn("unsupported event type",
			zap.String("type", fmt.Sprintf("%T", i)),
			zap.Any("item", i))
	}
}

// logEvent is a generic method that converts any event struct to OTEL attributes using JSON marshaling
func (s *EventStore) logEvent(ctx context.Context, item any, severity log.Severity, bodyMessage string) {
	attrs, err := s.structToAttributes(item)
	if err != nil {
		s.Log().Error("failed to convert item to attributes", zap.Error(err))
		return
	}

	// Add event type
	logAttrs := []log.KeyValue{
		log.String("event.type", s.getEventType(item)),
		log.Map("event", attrs...),
	}

	// Extract timestamp from the item
	timestamp := s.extractTimestamp(item)

	record := log.Record{}
	record.SetTimestamp(timestamp)
	record.SetSeverity(severity)
	record.SetBody(log.StringValue(bodyMessage))
	record.AddAttributes(logAttrs...)

	s.logger.Emit(ctx, record)
	submittedRecords.Inc()
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
	case *eventstore.DatabaseRequest:
		return "database_request"
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
	case *eventstore.DatabaseRequest:
		return i.Timestamp
	case *eventstore.Issue:
		return i.Timestamp
	case *eventstore.PIIEntity:
		return i.Timestamp
	case *eventstore.ArtifactRecord:
		return i.Timestamp
	case *eventstore.Connection:
		return i.CreatedAt
	default:
		return time.Now()
	}
}
