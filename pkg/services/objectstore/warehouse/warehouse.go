package warehouse

import (
	"context"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
)

var _ objectstore.ObjectStore = &ObjectStore{}

var tracer = telemetry.Tracer()

type ObjectStore struct {
	services.LogHelper
	objectstore.BaseObjectStore
	put        func(logger *zap.Logger, artifact *eventstore.Artifact, eventStore eventstore.EventStore)
	eventStore eventstore.EventStore
}

func (o *ObjectStore) Put(ctx context.Context, artifact *eventstore.Artifact) {
	logger := o.Log().With(o.LogFields(artifact)...)
	go o.put(logger, artifact, o.eventStore)
}
