package axiom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/axiomhq/axiom-go/axiom"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

var (
	submittedRecords = metrics.NewCounter("records_submitted_total", "The number of records submitted to Axiom")
	ingestedRecords  = metrics.NewCounter("records_ingested_total", "The number of records successfully ingested by Axiom")
	failedRecords    = metrics.NewCounter("records_failed_total", "The number of records that failed to be ingested by Axiom")
)

type EventStore struct {
	services.LogHelper
	eventstore.BaseEventStore
	axiomClient *axiom.Client
	dataset     string

	objectStore objectstore.ObjectStore
}

// Save submits an event to Axiom
func (s *EventStore) Save(_ context.Context, item any) {
	ctx := context.Background()
	ctx, span := tracer.Start(ctx, "Axiom.Save", trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	switch i := item.(type) {
	case *eventstore.Request, *eventstore.PIIEntity, *eventstore.Issue, *eventstore.ArtifactRecord, *eventstore.Connection:
		submittedRecords.Inc()

		// Submit directly to Axiom without batching for simplicity
		if err := s.submitToAxiom(ctx, i); err != nil {
			s.Log().Error("failed to submit event to Axiom", zap.Error(err))
		}
	case *eventstore.Artifact:
		go func() {
			ar, err := s.objectStore.Put(*i)
			if err != nil {
				s.Log().Error("failed to put artifact", zap.Error(err))
				return
			}
			s.Save(ctx, ar)
		}()
	default:
		s.Log().Warn("unsupported event type",
			zap.String("type", fmt.Sprintf("%T", i)),
			zap.Any("item", i))
		return
	}
}

// submitToAxiom sends a single event to Axiom
func (s *EventStore) submitToAxiom(ctx context.Context, item any) error {
	if s.axiomClient == nil {
		return errors.New("Axiom client not initialized")
	}

	ctx, span := tracer.Start(ctx, "Axiom.Submit")
	defer span.End()

	// Convert the item to an Axiom Event (map[string]any)
	event, err := s.convertToAxiomEvent(item)
	if err != nil {
		return fmt.Errorf("failed to convert item to Axiom event: %w", err)
	}

	// Create events slice for ingestion
	events := []axiom.Event{event}

	// Ingest the event to Axiom
	res, err := s.axiomClient.Datasets.IngestEvents(ctx, s.dataset, events)
	if err != nil {
		return fmt.Errorf("failed to ingest event to Axiom: %w", err)
	}

	// Log the ingestion result
	s.Log().Debug("successfully submitted event to Axiom",
		zap.Uint64("ingested", res.Ingested),
		zap.Uint64("failed", res.Failed),
		zap.String("dataset", s.dataset))

	ingestedRecords.Add(float64(res.Ingested))
	failedRecords.Add(float64(res.Failed))

	return nil
}

// convertToAxiomEvent converts any supported event type to an Axiom Event
func (s *EventStore) convertToAxiomEvent(item any) (axiom.Event, error) {
	var event axiom.Event

	switch v := item.(type) {
	case *eventstore.Connection, *eventstore.Request, *eventstore.PIIEntity, *eventstore.Issue, *eventstore.ArtifactRecord:
		m, err := toMap(v)
		if err != nil {
			return nil, fmt.Errorf("failed to convert item to Axiom event: %w", err)
		}
		m["event_type"] = fmt.Sprintf("%T", v)
		event = m
	default:
		return nil, fmt.Errorf("unsupported event type: %T", item)
	}

	return event, nil
}

func toMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
