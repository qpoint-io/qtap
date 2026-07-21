package warehouse

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/client"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const (
	TypeWarehouse        services.ServiceType = "warehouse"
	maxConcurrentUploads                      = 100
	closeTimeout                              = 5 * time.Second
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
	allowHTTP          bool
	mu                 sync.Mutex
	accepting          bool
	tasks              int
	drained            chan struct{}
	taskErr            error
	slots              chan struct{}
	next               *Factory
	eventStoreSelector *config.EventStoreSelector
}

type Option func(*Factory)

// NewFactory creates a warehouse factory with optional transport overrides.
func NewFactory(options ...Option) *Factory {
	factory := &Factory{}
	for _, option := range options {
		option(factory)
	}
	return factory
}

// WithHTTPClient injects a client and permits an HTTP endpoint. Production
// factories use the default HTTPS-only transport path.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(factory *Factory) {
		if httpClient != nil {
			factory.client = httpClient
			factory.allowHTTP = true
		}
	}
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

	endpoint := c.URL
	if endpoint == "" {
		endpoint = f.DefaultURL
	}
	token := c.Token.String()
	if strings.TrimSpace(token) == "" {
		return errors.New("warehouse token is required")
	}
	parsedURL, err := url.Parse(endpoint)
	validScheme := parsedURL != nil && parsedURL.Scheme == "https"
	if parsedURL != nil && parsedURL.Scheme == "http" && f.allowHTTP && f.client != nil {
		validScheme = true
	}
	if err != nil || parsedURL.Host == "" || !validScheme {
		return errors.New("warehouse endpoint must be a valid https URL")
	}

	httpClient := f.client
	if httpClient == nil {
		httpClient = client.NewHttpClient()
	}
	drained := make(chan struct{})
	close(drained)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.ctx = ctx
	f.url = endpoint
	f.token = token
	f.client = httpClient
	f.accepting = true
	f.tasks = 0
	f.drained = drained
	f.taskErr = nil
	f.slots = make(chan struct{}, maxConcurrentUploads)
	f.eventStoreSelector = &c.EventStore

	return nil
}

func (f *Factory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	eventStore, err := services.GetService[eventstore.EventStore](ctx, svcRegistry, eventstore.TypeEventStore, f.eventStoreSelector.ID)
	if err != nil {
		return nil, fmt.Errorf("getting event store: %w", err)
	}

	store := &ObjectStore{
		put:        f.put,
		eventStore: eventStore,
	}
	store.SetLogger(f.logger)
	return store, nil
}

func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", objectstore.TypeObjectStore, TypeWarehouse))
}

func (f *Factory) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	return f.Shutdown(ctx)
}

// Shutdown stops accepting uploads and waits for every accepted task to finish.
func (f *Factory) Shutdown(ctx context.Context) error {
	f.mu.Lock()
	f.accepting = false
	drained := f.drained
	f.mu.Unlock()

	if drained == nil {
		return nil
	}

	// Prefer a completed drain over a concurrently canceled shutdown context.
	select {
	case <-drained:
		return f.uploadError()
	default:
	}

	select {
	case <-drained:
		return f.uploadError()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *Factory) Next(next services.Factory) {
	if next, ok := next.(*Factory); ok {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.next = next
	}
}

func (f *Factory) put(ctx context.Context, logger *zap.Logger, artifact *eventstore.Artifact, eventStore eventstore.EventStore) {
	_, span := tracer.Start(ctx, "Warehouse.Put", trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	putSpanCtx := span.SpanContext()
	next, accepted := f.register(func(taskCtx context.Context) error {
		taskCtx, uploadSpan := tracer.Start(
			taskCtx, "Warehouse.uploadArtifact",
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithLinks(trace.Link{SpanContext: putSpanCtx}),
		)
		defer uploadSpan.End()

		if err := f.uploadArtifact(taskCtx, artifact); err != nil {
			logger.Error("uploading artifact", zap.Error(err))
			return fmt.Errorf("uploading artifact: %w", err)
		}

		if eventStore != nil {
			assetURL, err := url.JoinPath(f.url, "assets", artifact.Digest())
			if err != nil {
				return fmt.Errorf("assembling qpoint warehouse asset URL: %w", err)
			}
			eventStore.Save(taskCtx, artifact.Record(assetURL))
		}

		return nil
	})
	if accepted {
		return
	}
	if next != nil {
		next.put(ctx, logger, artifact, eventStore)
		return
	}
	logger.Error("warehouse is closed")
}

func (f *Factory) register(task func(context.Context) error) (*Factory, bool) {
	f.mu.Lock()
	if !f.accepting {
		next := f.next
		f.mu.Unlock()
		return next, false
	}
	if f.tasks == 0 {
		f.drained = make(chan struct{})
	}
	f.tasks++
	taskCtx := f.ctx
	slots := f.slots
	f.mu.Unlock()

	go func() {
		var err error
		select {
		case slots <- struct{}{}:
			err = task(taskCtx)
			<-slots
		case <-taskCtx.Done():
			err = taskCtx.Err()
		}
		f.finishTask(err)
	}()
	return nil, true
}

func (f *Factory) finishTask(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil {
		f.taskErr = errors.Join(f.taskErr, err)
	}
	f.tasks--
	if f.tasks == 0 {
		close(f.drained)
	}
}

func (f *Factory) uploadError() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.taskErr
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
	resp.Body.Close()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	if resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("warehouse HEAD returned %s", resp.Status)
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
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("warehouse PUT returned %s", resp.Status)
	}

	// debugging
	// body, err := httputil.DumpResponse(resp, true)
	// if err != nil {
	// 	w.logger.Error("dumping response", zap.Error(err))
	// 	return err
	// }

	// w.logger.Debug("artifact upload response", zap.String("payload", string(body)))

	return nil
}
