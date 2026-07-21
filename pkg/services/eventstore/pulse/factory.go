package pulse

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
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

	defaultRPCAttemptTimeout = 30 * time.Second
	defaultCloseTimeout      = 30 * time.Second
)

var errFactoryShuttingDown = errors.New("eventstore is shutting down")

type flushCommand struct {
	ctx    context.Context
	result chan error
}

type ingestCommand struct {
	event    *eventstorev1.Event
	flush    *flushCommand
	shutdown bool
}

type Factory struct {
	eventstore.BaseEventStore

	// DefaultURL is the fallback endpoint used when the config URL is empty.
	DefaultURL string

	ctx          context.Context
	cancel       context.CancelFunc
	logger       *zap.Logger
	client       eventstorev1connect.EventStoreServiceClient
	token        string
	ingestEvents chan ingestCommand
	ingestDone   chan struct{}

	acceptMu  sync.Mutex
	accepting bool
	next      *Factory

	// shutdownGate serializes shutdown attempts while still allowing callers to
	// stop waiting when their context expires.
	shutdownGate chan struct{}

	batchSize         int
	flushInterval     time.Duration
	maxRetryTime      time.Duration
	rpcAttemptTimeout time.Duration
	retryInitial      time.Duration
	retryMax          time.Duration
	retryMultiplier   float64
	retryJitter       float64
}

var _ services.NextFactory = (*Factory)(nil)

func (f *Factory) Init(ctx context.Context, cfg any) error {
	c, ok := cfg.(config.ServiceEventStore)
	if !ok {
		return fmt.Errorf("invalid config type: %T wanted config.ServiceEventStore", cfg)
	}

	// Resolve credentials before allocating lifecycle resources or constructing
	// a transport so invalid secret sources have zero startup activity.
	token := c.Token.String()
	if strings.TrimSpace(token) == "" {
		return errors.New("eventstore token is empty")
	}

	logger := zap.L().With(zap.String("service_factory", f.FactoryType().String()))
	if c.URL == "" {
		c.URL = f.DefaultURL
	}
	if u, err := url.Parse(c.URL); err != nil {
		return fmt.Errorf("invalid eventstore url: %w", err)
	} else if u.Scheme != "https" {
		if c.AllowInsecure {
			logger.Warn("using insecure eventstore connection", zap.String("url", c.URL))
		} else {
			return errors.New("provide an https:// eventstore url or set allow_insecure to true")
		}
	}

	f.batchSize = DefaultBatchSize
	f.flushInterval = DefaultFlushInterval
	f.maxRetryTime = 5 * time.Minute
	f.rpcAttemptTimeout = defaultRPCAttemptTimeout
	f.retryInitial = 100 * time.Millisecond
	f.retryMax = 30 * time.Second
	f.retryMultiplier = 2
	f.retryJitter = 0.1

	// The transport lifetime is owned by Shutdown. In particular, cancellation
	// of Init's context must not abort the final flush before Shutdown can wait.
	f.ctx, f.cancel = context.WithCancel(context.WithoutCancel(ctx))
	f.logger = logger
	f.token = token
	f.ingestEvents = make(chan ingestCommand, int(math.Max(1024, float64(f.batchSize))))
	f.ingestDone = make(chan struct{})
	f.shutdownGate = make(chan struct{}, 1)
	f.shutdownGate <- struct{}{}
	f.accepting = true

	f.client = eventstorev1connect.NewEventStoreServiceClient(
		newHTTP2Client(http2ClientOptions{h2c: c.AllowInsecure}),
		c.URL,
		// Cloudflare is strict about gRPC support and so ConnectRPC's connect protocol is not supported.
		// Configure the client to use the regular gRPC protocol.
		connect.WithGRPC(),
	)

	go f.ingest()
	f.setupMetrics()
	return nil
}

func (f *Factory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	return &Store{saveFn: f.enqueue}, nil
}

func (f *Factory) enqueue(event *eventstorev1.Event) {
	current := f
	var visited map[*Factory]struct{}
	for {
		next, accepted := current.tryEnqueue(event)
		if accepted {
			return
		}
		if next == nil {
			current.logEnqueueFailure("event store is closed and has no replacement; discarding request", event)
			return
		}
		if visited == nil {
			visited = map[*Factory]struct{}{current: {}}
		}
		if _, seen := visited[next]; seen {
			current.logEnqueueFailure("event store replacement chain contains a cycle; discarding request", event)
			return
		}
		visited[next] = struct{}{}
		current = next
	}
}

func (f *Factory) tryEnqueue(event *eventstorev1.Event) (*Factory, bool) {
	f.acceptMu.Lock()
	if !f.accepting {
		next := f.next
		f.acceptMu.Unlock()
		return next, false
	}
	select {
	case f.ingestEvents <- ingestCommand{event: event}:
		f.acceptMu.Unlock()
	default:
		f.acceptMu.Unlock()
		f.logEnqueueFailure("event store queue is full; discarding request", event)
	}
	return nil, true
}

func (f *Factory) logEnqueueFailure(message string, event *eventstorev1.Event) {
	logger := f.logger
	if logger == nil {
		logger = zap.L()
	}
	logger.Error(message, zap.Any("request", event))
}

// Next forwards saves from services created by this factory after it stops
// accepting to the replacement Pulse factory.
func (f *Factory) Next(next services.Factory) {
	replacement, ok := next.(*Factory)
	f.acceptMu.Lock()
	if ok {
		f.next = replacement
	}
	f.acceptMu.Unlock()
	if !ok {
		f.logEnqueueFailure("event store replacement is incompatible; saves cannot be forwarded", nil)
	}
}

// FactoryType returns the service type.
func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", eventstore.TypeEventStore, Type))
}

// Flush sends every event accepted before its FIFO barrier. A failed send leaves
// the current batch intact so a later Flush can retry it.
func (f *Factory) Flush(ctx context.Context) error {
	if f.ingestEvents == nil {
		return nil
	}

	command := &flushCommand{ctx: ctx, result: make(chan error, 1)}
	f.acceptMu.Lock()
	if !f.accepting {
		f.acceptMu.Unlock()
		return errFactoryShuttingDown
	}
	select {
	case f.ingestEvents <- ingestCommand{flush: command}:
		f.acceptMu.Unlock()
	case <-ctx.Done():
		f.acceptMu.Unlock()
		return ctx.Err()
	case <-f.ingestDone:
		f.acceptMu.Unlock()
		return errFactoryShuttingDown
	}

	return f.waitForCommand(ctx, command)
}

// Shutdown stops acceptance, drains commands accepted before its FIFO barrier,
// flushes their events, and waits for the ingest goroutine. If flushing fails,
// the worker remains available for a later idempotent Shutdown retry.
func (f *Factory) Shutdown(ctx context.Context) error {
	if f.ingestDone == nil {
		return nil
	}
	if f.ingestStopped() {
		f.cancel()
		return nil
	}

	select {
	case <-f.shutdownGate:
	case <-f.ingestDone:
		f.cancel()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { f.shutdownGate <- struct{}{} }()

	if f.ingestStopped() {
		f.cancel()
		return nil
	}

	command := &flushCommand{ctx: ctx, result: make(chan error, 1)}
	f.acceptMu.Lock()
	f.accepting = false
	select {
	case f.ingestEvents <- ingestCommand{flush: command, shutdown: true}:
		f.acceptMu.Unlock()
	case <-ctx.Done():
		f.acceptMu.Unlock()
		return ctx.Err()
	case <-f.ingestDone:
		f.acceptMu.Unlock()
		f.cancel()
		return nil
	}

	if err := f.waitForCommand(ctx, command); err != nil {
		// Another concurrent shutdown may have completed successfully while this
		// command was queued behind it.
		if f.ingestStopped() {
			f.cancel()
			return nil
		}
		return err
	}
	select {
	case <-f.ingestDone:
		f.cancel()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *Factory) waitForCommand(ctx context.Context, command *flushCommand) error {
	select {
	case err := <-command.result:
		return err
	case <-f.ingestDone:
		select {
		case err := <-command.result:
			return err
		default:
			return errFactoryShuttingDown
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *Factory) ingestStopped() bool {
	select {
	case <-f.ingestDone:
		return true
	default:
		return false
	}
}

// Close cleans up resources with a finite upper bound.
func (f *Factory) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCloseTimeout)
	defer cancel()
	return f.Shutdown(ctx)
}

// isRetryableError determines if an error is transient and should be retried.
func isRetryableError(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	switch connect.CodeOf(err) {
	case connect.CodeUnavailable,
		connect.CodeDeadlineExceeded,
		connect.CodeResourceExhausted,
		connect.CodeAborted:
		return true
	case connect.CodeUnauthenticated,
		connect.CodePermissionDenied,
		connect.CodeInvalidArgument:
		return false
	default:
		return false
	}
}

// flushBatchWithRetry sends one immutable batch with exponential backoff.
func (f *Factory) flushBatchWithRetry(ctx context.Context, batch []*eventstorev1.Event, batchSize int) error {
	if len(batch) == 0 {
		return nil
	}

	b := backoff.NewExponentialBackOff()
	b.InitialInterval = durationOr(f.retryInitial, 100*time.Millisecond)
	b.MaxInterval = durationOr(f.retryMax, 30*time.Second)
	b.MaxElapsedTime = durationOr(f.maxRetryTime, 5*time.Minute)
	b.Multiplier = floatOr(f.retryMultiplier, 2)
	b.RandomizationFactor = f.retryJitter
	b.Reset()

	operation := func() error {
		attemptCtx, cancel := context.WithTimeout(ctx, durationOr(f.rpcAttemptTimeout, defaultRPCAttemptTimeout))
		req := connect.NewRequest(eventstorev1.IngestBatchRequest_builder{Events: batch}.Build())
		req.Header().Set("Authorization", "Bearer "+f.token)
		_, err := f.client.IngestBatch(attemptCtx, req)
		cancel()
		if err == nil {
			f.logger.Debug("successfully sent batch", zap.Int("batch_size", batchSize))
			return nil
		}
		if ctx.Err() != nil {
			return backoff.Permanent(ctx.Err())
		}
		if isRetryableError(err) {
			f.logger.Warn("retryable error sending batch",
				zap.Error(err),
				zap.Int("batch_size", batchSize),
				zap.String("error_code", connect.CodeOf(err).String()),
			)
			return err
		}

		f.logger.Error("permanent error sending batch; retaining batch",
			zap.Error(err),
			zap.Int("batch_size", batchSize),
			zap.String("error_code", connect.CodeOf(err).String()),
		)
		return backoff.Permanent(err)
	}

	return backoff.Retry(operation, backoff.WithContext(b, ctx))
}

func durationOr(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func floatOr(value, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}

func (f *Factory) ingest() {
	ctx, span := tracer.Start(f.ctx, "eventstorev1.ingest", trace.WithSpanKind(trace.SpanKindConsumer))
	defer span.End()
	defer close(f.ingestDone)

	f.logger.Debug("starting ingest loop", zap.Int("batch_size", f.batchSize), zap.Duration("flush_interval", f.flushInterval))
	tick := time.NewTicker(f.flushInterval)
	defer tick.Stop()

	current := make([]*eventstorev1.Event, 0, f.batchSize)
	var pending []*eventstorev1.Event
	retained := false

	flush := func(flushCtx context.Context) error {
		for len(current) > 0 {
			batchSize := len(current)
			f.logger.Debug("flushing batch", zap.Int("batch_size", batchSize))
			if err := f.flushBatchWithRetry(flushCtx, current, batchSize); err != nil {
				retained = true
				f.logger.Error("failed to send batch; retaining for retry",
					zap.Error(err),
					zap.Int("batch_size", batchSize),
				)
				return err
			}

			current = nil
			retained = false
			if len(pending) > 0 {
				current = pending
				pending = nil
			}
		}
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return
		case command := <-f.ingestEvents:
			if command.event != nil {
				if retained {
					pending = append(pending, command.event)
				} else {
					current = append(current, command.event)
				}
				if len(current)+len(pending) >= f.batchSize {
					_ = flush(ctx)
				}
				continue
			}
			if command.flush != nil {
				err := flush(command.flush.ctx)
				command.flush.result <- err
				if command.shutdown && err == nil {
					return
				}
			}
		case <-tick.C:
			_ = flush(ctx)
		}
	}
}
