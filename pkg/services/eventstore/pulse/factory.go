package pulse

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"time"

	"connectrpc.com/connect"
	"github.com/cenkalti/backoff/v4"
	eventstorev1 "github.com/qpoint-io/proto/gen/go/eventstore/v1"
	"github.com/qpoint-io/proto/gen/go/eventstore/v1/eventstorev1connect"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const (
	Type services.ServiceType = "eventstorev1"

	DefaultBatchSize     = 1000
	DefaultFlushInterval = 1 * time.Second
)

type Factory struct {
	eventstore.BaseEventStore

	// DefaultURL is the fallback endpoint used when the config URL is empty.
	DefaultURL string

	ctx          context.Context
	cancel       context.CancelFunc
	logger       *zap.Logger
	client       eventstorev1connect.EventStoreServiceClient
	token        string
	ingestEvents chan *eventstorev1.Event
	shutdown     chan struct{}

	batchSize     int
	flushInterval time.Duration
	maxRetryTime  time.Duration
}

func (f *Factory) Init(ctx context.Context, cfg any) error {
	c, ok := cfg.(config.ServiceEventStore)
	if !ok {
		return fmt.Errorf("invalid config type: %T wanted config.ServiceEventStore", cfg)
	}

	f.ctx, f.cancel = context.WithCancel(ctx)
	f.logger = zap.L().With(zap.String("service_factory", f.FactoryType().String()))
	f.ingestEvents = make(chan *eventstorev1.Event, int(math.Max(1024, float64(f.batchSize))))
	f.shutdown = make(chan struct{})

	// batch options
	f.batchSize = DefaultBatchSize
	f.flushInterval = DefaultFlushInterval
	f.maxRetryTime = 5 * time.Minute

	// endpoint options
	if c.URL == "" {
		c.URL = f.DefaultURL
	}
	f.token = c.Token.String()
	if u, err := url.Parse(c.URL); err != nil {
		return fmt.Errorf("invalid eventstore url: %w", err)
	} else if u.Scheme != "https" {
		if c.AllowInsecure {
			f.logger.Warn("using insecure eventstore connection", zap.String("url", c.URL))
		} else {
			return errors.New("provide an https:// eventstore url or set allow_insecure to true")
		}
	}

	// rpc client
	f.client = eventstorev1connect.NewEventStoreServiceClient(
		newHTTP2Client(http2ClientOptions{
			h2c: c.AllowInsecure,
		}),
		c.URL,
		// Cloudflare is strict about gRPC support and so ConnectRPC's connect protocol is not supported.
		// Configure the client to use the regular gRPC protocol.
		connect.WithGRPC(),
	)

	// run ingest loop
	go f.ingest()

	f.setupMetrics()

	return nil
}

func (f *Factory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	return &Store{
		saveFn: func(a *eventstorev1.Event) {
			select {
			case f.ingestEvents <- a:
			default:
				f.logger.Warn("event store queue is full; discarding request", zap.Any("request", a))
			}
		},
	}, nil
}

// ServiceType returns the service type
func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", eventstore.TypeEventStore, Type))
}

// Close cleans up resources
func (f *Factory) Close() error {
	defer f.cancel()

	f.logger.Info("closing eventstorev1 service")
	select {
	case f.shutdown <- struct{}{}:
	default:
	}
	return nil
}

// isRetryableError determines if an error is transient and should be retried
func isRetryableError(err error) bool {
	// Context cancellation and deadline exceeded are not retryable (shutdown signal or timeout)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Check Connect error codes for retryable conditions
	code := connect.CodeOf(err)
	switch code {
	case connect.CodeUnavailable,
		connect.CodeDeadlineExceeded,
		connect.CodeResourceExhausted,
		connect.CodePermissionDenied,
		connect.CodeAborted:
		return true
	}

	// Default to non-retryable for unknown errors
	return false
}

// flushBatchWithRetry sends a batch of events with exponential backoff retry logic
func (f *Factory) flushBatchWithRetry(ctx context.Context, batch []*eventstorev1.Event, batchSize int) error {
	if len(batch) == 0 {
		return nil
	}

	// Configure exponential backoff
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 100 * time.Millisecond
	b.MaxInterval = 30 * time.Second
	b.MaxElapsedTime = f.maxRetryTime
	b.Multiplier = 2.0
	b.RandomizationFactor = 0.1

	backoffWithContext := backoff.WithContext(b, ctx)

	// Retry operation
	operation := func() error {
		req := connect.NewRequest(eventstorev1.IngestBatchRequest_builder{
			Events: batch,
		}.Build())
		req.Header().Set("Authorization", "Bearer "+f.token)

		_, err := f.client.IngestBatch(ctx, req)
		if err != nil {
			if isRetryableError(err) {
				f.logger.Warn("retryable error sending batch",
					zap.Error(err),
					zap.Int("batch_size", batchSize),
					zap.String("error_code", connect.CodeOf(err).String()),
				)
				return err // Triggers retry
			}

			f.logger.Error("non-retryable error sending batch; discarding",
				zap.Error(err),
				zap.Int("batch_size", batchSize),
				zap.String("error_code", connect.CodeOf(err).String()),
			)
			return backoff.Permanent(err) // Stop retrying
		}

		f.logger.Debug("successfully sent batch", zap.Int("batch_size", batchSize))
		return nil
	}

	// Execute with retry
	return backoff.Retry(operation, backoffWithContext)
}

func (f *Factory) ingest() {
	ctx, span := tracer.Start(f.ctx, "eventstorev1.ingest", trace.WithSpanKind(trace.SpanKindConsumer))
	defer span.End()

	f.logger.Debug("starting ingest loop", zap.Int("batch_size", f.batchSize), zap.Duration("flush_interval", f.flushInterval))

	batch := make([]*eventstorev1.Event, 0, f.batchSize)
	// flush when the batch is full or the flush interval is reached
	tick := time.NewTicker(f.flushInterval)
	flush := func() {
		if len(batch) == 0 {
			return
		}

		batchSize := len(batch)
		f.logger.Debug("flushing batch", zap.Int("batch_size", batchSize))

		// Execute with retry
		err := f.flushBatchWithRetry(ctx, batch, batchSize)
		if err != nil {
			f.logger.Error("failed to send batch after retries; clearing batch to prevent accumulation",
				zap.Error(err),
				zap.Int("batch_size", batchSize),
				zap.Duration("max_retry_time", f.maxRetryTime),
			)
			// Clear batch to prevent accumulation of unprocessable events
			// TODO: Consider implementing dead-letter queue or graceful shutdown for persistent failures
			batch = batch[:0]
			tick.Reset(f.flushInterval)
			return
		}

		// Success - clear batch and reset ticker
		batch = batch[:0]
		tick.Reset(f.flushInterval)
	}

	// ingest loop
	for {
		select {
		case <-ctx.Done():
			f.logger.Debug("ingest loop shutting down", zap.Error(context.Cause(ctx)))
			return
		case <-f.shutdown:
			// flush any remaining events and gracefully shutdown
			flush()
			return
		case event := <-f.ingestEvents:
			batch = append(batch, event)
			if len(batch) >= f.batchSize {
				flush()
			}
		case <-tick.C:
			flush()
		}
	}
}
