package pulse

import (
	"context"
	"reflect"

	eventstorev1 "github.com/qpoint-io/proto/gen/go/eventstore/v1"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type Store struct {
	services.LogHelper
	eventstore.BaseEventStore
	saveFn func(*eventstorev1.Event)
}

func (s *Store) Save(ctx context.Context, item any) {
	_, span := tracer.Start(ctx, "eventstorev1.Save", trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	ll := s.Log()
	ll.Debug("submitting event", zap.Any("item", item))

	var event eventstorev1.Event
	switch i := item.(type) {
	case *eventstore.Request:
		event.SetRequest(pbRequest(i))
	case *eventstore.Issue:
		event.SetIssue(pbIssue(i))
	case *eventstore.ArtifactRecord:
		event.SetArtifact(pbArtifactRecord(i))
	case *eventstore.PIIEntity:
		event.SetPiiEntity(pbPIIEntity(i))
	case *eventstore.Connection:
		event.SetConnection(pbConnection(i))
	case *eventstore.DatabaseRequest:
		event.SetDatabaseRequest(pbDatabaseRequest(i))
	case *eventstore.GrpcRequest:
		event.SetGrpcRequest(pbGrpcRequest(i))
	case *eventstore.Artifact:
		ll.DPanic("event stores do not support artifacts", zap.Any("artifact", i))
		return
	default:
		ll.Debug("unknown type", zap.String("type", reflect.TypeOf(i).String()))
		return
	}

	s.saveFn(&event)
}
