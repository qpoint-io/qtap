package warehouse

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/client"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"github.com/sourcegraph/conc/pool"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const (
	TypeWarehouse services.ServiceType = "warehouse"
)

var _ services.Factory = &Factory{}

type Factory struct {
	objectstore.BaseObjectStore

	// DefaultURL is the fallback endpoint used when the config URL is empty.
	DefaultURL string

	ctx                context.Context
	logger             *zap.Logger
	url                string
	token              string
	client             *http.Client
	pool               *pool.ContextPool
	closed             atomic.Bool
	next               *Factory
	eventStoreSelector *config.EventStoreSelector
}

func (f *Factory) Init(ctx context.Context, cfg any) error {
	f.logger = zap.L().With(zap.String("factory_type", f.FactoryType().String()))

	c, ok := cfg.(config.ServiceObjectStore)
	if !ok {
		return fmt.Errorf("invalid config type: %T wanted config.ServiceObjectStore", cfg)
	}

	if c.Type != config.ObjectStoreType_QPOINT {
		return fmt.Errorf("invalid object store type: %s", c.Type)
	}

	f.ctx = ctx
	f.url = c.URL
	if f.url == "" {
		f.url = f.DefaultURL
	}
	f.token = c.Token.String()
	f.client = client.NewHttpClient()
	f.pool = pool.New().WithContext(ctx).WithMaxGoroutines(100)
	f.eventStoreSelector = &c.EventStore

	return nil
}

func (f *Factory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	eventStore, err := services.GetService[eventstore.EventStore](ctx, svcRegistry, eventstore.TypeEventStore, f.eventStoreSelector.ID)
	if err != nil {
		return nil, fmt.Errorf("getting event store: %w", err)
	}

	return &ObjectStore{
		put:        f.put,
		eventStore: eventStore,
	}, nil
}

func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", objectstore.TypeObjectStore, TypeWarehouse))
}

func (f *Factory) Close() error {
	if f.closed.Load() {
		return nil
	}

	f.closed.Store(true)

	if err := f.pool.Wait(); err != nil {
		return fmt.Errorf("waiting for warehouse pool to drain: %w", err)
	}

	return nil
}

func (f *Factory) Next(next services.Factory) {
	if next, ok := next.(*Factory); ok {
		f.next = next
	}
}

func (f *Factory) put(logger *zap.Logger, artifact *eventstore.Artifact, eventStore eventstore.EventStore) {
	_, span := tracer.Start(f.ctx, "Warehouse.Put", trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	if f.closed.Load() {
		// if the replacement factory is available, send this there
		if f.next != nil {
			f.next.put(logger, artifact, eventStore)
			return
		}
		logger.Error("warehouse is closed")
		return
	}

	// save the span context so we can link it to the uploadArtifact span
	putSpanCtx := span.SpanContext()
	go f.pool.Go(func(ctx context.Context) error {
		// this is run in a goroutine and not waited on. we need to link the spans as the context
		// this function receives is not linked to the parent Put context.
		ctx, span := tracer.Start(
			ctx, "Warehouse.uploadArtifact",
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithLinks(trace.Link{
				SpanContext: putSpanCtx,
			}),
		)
		defer span.End()

		err := f.uploadArtifact(ctx, artifact)
		if err != nil {
			return fmt.Errorf("uploading artifact: %w", err)
		}

		if eventStore != nil {
			url, err := url.JoinPath(f.url, "assets", artifact.Digest())
			if err != nil {
				return fmt.Errorf("assembling qpoint warehouse asset URL %w", err)
			}

			record := artifact.Record(url)
			eventStore.Save(ctx, record)
		}

		return nil
	})
}

func (w *Factory) uploadArtifact(ctx context.Context, artifact *eventstore.Artifact) error {
	assembledURL, err := url.JoinPath(w.url, "/assets/"+artifact.Digest())
	if err != nil {
		return fmt.Errorf("assembling target egress destination url error: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, assembledURL, nil)
	if err != nil {
		w.logger.Error("creating http Request", zap.Error(err))
		return err
	}

	if t := w.token; t != "" {
		req.Header.Add("Authorization", "Bearer "+t)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		w.logger.Error("sending egress request", zap.Error(err))
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		// Artifact already exists
		return nil
	}

	err = w.shipArtifact(ctx, assembledURL, artifact)
	if err != nil {
		w.logger.Error("shipping artifact", zap.Error(err))
		return err
	}

	return nil
}

func (w *Factory) shipArtifact(ctx context.Context, url string, artifact *eventstore.Artifact) error {
	ctx, span := tracer.Start(ctx, "Warehouse.shipArtifact")
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(artifact.Data))
	if err != nil {
		w.logger.Error("creating http Request", zap.Error(err))
		return err
	}
	req.Header.Add("Content-Type", artifact.ContentType)
	req.Header.Add("x-qpoint-artifact-type", string(artifact.Type))

	if t := w.token; t != "" {
		req.Header.Add("Authorization", "Bearer "+t)
	}

	w.logger.Debug("uploading artifact",
		zap.String("url", url),
		zap.Int("size", len(artifact.Data)))

	resp, err := w.client.Do(req)
	if err != nil {
		w.logger.Error("sending egress request", zap.Error(err))
		return err
	}
	defer resp.Body.Close()

	// debugging
	// body, err := httputil.DumpResponse(resp, true)
	// if err != nil {
	// 	w.logger.Error("dumping response", zap.Error(err))
	// 	return err
	// }

	// w.logger.Debug("artifact upload response", zap.String("payload", string(body)))

	return nil
}
