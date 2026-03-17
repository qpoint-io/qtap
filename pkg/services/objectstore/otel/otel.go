package otel

import (
	"context"
	"fmt"
	"time"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
	"go.opentelemetry.io/otel/log"
	"go.uber.org/zap"
)

var submittedArtifacts = metrics.NewCounter("objectstore_otel_artifacts_total", "The number of artifacts submitted to OpenTelemetry")

type ObjectStore struct {
	services.LogHelper
	objectstore.BaseObjectStore

	logger      log.Logger
	endpoint    string
	eventStore  eventstore.EventStore
	serviceName string
	environment string
}

func (s *ObjectStore) Put(ctx context.Context, artifact *eventstore.Artifact) {
	zapLogger := s.Log().With(s.LogFields(artifact)...)

	// Use WithoutCancel so the upload isn't cancelled when the connection closes
	ctx = context.WithoutCancel(ctx)

	go func() {
		record := log.Record{}
		record.SetTimestamp(time.Now())
		record.SetSeverity(log.SeverityInfo)
		record.SetBody(log.StringValue(fmt.Sprintf("Artifact: %s (%s, %d bytes)",
			artifact.Type, artifact.ContentType, len(artifact.Data))))

		attrs := []log.KeyValue{
			log.String("artifact.type", artifact.Type.String()),
			log.String("artifact.content_type", artifact.ContentType),
			log.String("artifact.digest", artifact.Digest()),
			log.Int64("artifact.size_bytes", int64(len(artifact.Data))),
			log.String("artifact.data", string(artifact.Data)),
		}

		// Add connection metadata if available
		if id := artifact.ConnectionID; id != "" {
			attrs = append(attrs, log.String("connection.id", id))
		}
		if id := artifact.EndpointId; id != "" {
			attrs = append(attrs, log.String("connection.endpoint_id", id))
		}
		if id := artifact.RequestId; id != "" {
			attrs = append(attrs, log.String("connection.request_id", id))
		}

		// Add summary as nested attributes
		for k, v := range artifact.Summary {
			attrs = append(attrs, log.String("artifact.summary."+k, fmt.Sprintf("%v", v)))
		}

		record.AddAttributes(attrs...)
		s.logger.Emit(ctx, record)
		submittedArtifacts.Inc()

		// Optionally save ArtifactRecord to EventStore
		if s.eventStore != nil {
			url := fmt.Sprintf("otel://%s/%s", s.endpoint, artifact.Digest())
			artifactRecord := artifact.Record(url)
			s.eventStore.Save(ctx, artifactRecord)
			zapLogger.Debug("artifact emitted to otel", zap.String("url", url))
		} else {
			zapLogger.Debug("artifact emitted to otel")
		}
	}()
}
