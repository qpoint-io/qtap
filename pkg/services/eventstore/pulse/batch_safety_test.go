package pulse

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	eventstorev1 "github.com/qpoint-io/proto/gen/go/eventstore/v1"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type batchCall struct {
	ctx     context.Context
	eventID []string
	result  chan error
}

func newBatchSafetyFactory(t *testing.T) (*Factory, *Store, <-chan batchCall) {
	t.Helper()

	f := &Factory{}
	require.NoError(t, f.Init(t.Context(), config.ServiceEventStore{
		EventStoreConfig: config.EventStoreConfig{
			Token: config.ValueSource{Type: config.ValueSourceType_TEXT, Value: "test-token"},
			EventStorePulseConfig: config.EventStorePulseConfig{
				URL:           "http://pulse.test",
				AllowInsecure: true,
			},
		},
	}))

	calls := make(chan batchCall, 16)
	f.client = &mockEventStoreClient{
		ingestBatchFunc: func(ctx context.Context, req *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error) {
			ids := make([]string, 0, len(req.Msg.GetEvents()))
			for _, event := range req.Msg.GetEvents() {
				ids = append(ids, event.GetRequest().GetId())
			}
			call := batchCall{ctx: ctx, eventID: ids, result: make(chan error, 1)}
			select {
			case calls <- call:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			select {
			case err := <-call.result:
				if err != nil {
					return nil, err
				}
				return connect.NewResponse(&eventstorev1.IngestBatchResponse{}), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	service, err := f.Create(t.Context(), nil)
	require.NoError(t, err)
	store, ok := service.(*Store)
	require.True(t, ok)

	t.Cleanup(func() {
		if f.cancel != nil {
			f.cancel()
		}
	})
	return f, store, calls
}

func saveRequest(t *testing.T, store *Store, id string) {
	t.Helper()
	req := &eventstore.Request{}
	req.SetRequestID(id)
	store.Save(t.Context(), req)
}

func receiveBatchCall(t *testing.T, calls <-chan batchCall) batchCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for batch RPC")
		return batchCall{}
	}
}

func TestInitRejectsEmptyResolvedTokenBeforeStartup(t *testing.T) {
	t.Setenv("QTAP_TEST_EMPTY_PULSE_TOKEN", "")

	f := &Factory{DefaultURL: "://invalid"}
	err := f.Init(t.Context(), config.ServiceEventStore{
		EventStoreConfig: config.EventStoreConfig{
			Token: config.ValueSource{
				Type:  config.ValueSourceType_ENV,
				Value: "QTAP_TEST_EMPTY_PULSE_TOKEN",
			},
		},
	})

	require.ErrorContains(t, err, "token")
	require.Nil(t, f.cancel, "lifecycle context must not start")
	require.Nil(t, f.client, "RPC client must not start")
	require.Nil(t, f.ingestEvents, "ingest queue must not start")
}

func TestFlushSendsAcceptedEventsAndClearsOnlyAfterSuccess(t *testing.T) {
	f, store, calls := newBatchSafetyFactory(t)
	saveRequest(t, store, "first")
	saveRequest(t, store, "second")

	flushResult := make(chan error, 1)
	go func() { flushResult <- f.Flush(t.Context()) }()

	call := receiveBatchCall(t, calls)
	assert.Equal(t, []string{"first", "second"}, call.eventID)
	_, hasDeadline := call.ctx.Deadline()
	assert.True(t, hasDeadline, "every RPC attempt must have a deadline")
	call.result <- nil
	require.NoError(t, <-flushResult)

	require.NoError(t, f.Flush(t.Context()))
	select {
	case unexpected := <-calls:
		t.Fatalf("successful batch was sent again: %v", unexpected.eventID)
	default:
	}
	require.NoError(t, f.Shutdown(t.Context()))
}

func TestPermanentFailureRetainsExactBatchForLaterFlush(t *testing.T) {
	f, store, calls := newBatchSafetyFactory(t)
	saveRequest(t, store, "retained-first")
	saveRequest(t, store, "retained-second")

	firstResult := make(chan error, 1)
	go func() { firstResult <- f.Flush(t.Context()) }()
	firstCall := receiveBatchCall(t, calls)
	firstCall.result <- connect.NewError(connect.CodeInvalidArgument, errors.New("invalid batch"))
	require.Error(t, <-firstResult)
	saveRequest(t, store, "accepted-after-failure")

	retryResult := make(chan error, 1)
	go func() { retryResult <- f.Flush(t.Context()) }()
	retryCall := receiveBatchCall(t, calls)
	assert.Equal(t, firstCall.eventID, retryCall.eventID)
	retryCall.result <- nil
	pendingCall := receiveBatchCall(t, calls)
	assert.Equal(t, []string{"accepted-after-failure"}, pendingCall.eventID)
	pendingCall.result <- nil
	require.NoError(t, <-retryResult)
	require.NoError(t, f.Shutdown(t.Context()))
}

func TestAttemptTimeoutRetriesTheExactRetainedBatch(t *testing.T) {
	f, store, calls := newBatchSafetyFactory(t)
	f.rpcAttemptTimeout = 20 * time.Millisecond
	f.retryInitial = time.Millisecond
	f.retryMax = time.Millisecond
	f.retryJitter = 0
	saveRequest(t, store, "times-out")

	flushResult := make(chan error, 1)
	go func() { flushResult <- f.Flush(t.Context()) }()
	firstCall := receiveBatchCall(t, calls)
	select {
	case <-firstCall.ctx.Done():
		require.ErrorIs(t, firstCall.ctx.Err(), context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("RPC attempt had no effective timeout")
	}

	retryCall := receiveBatchCall(t, calls)
	assert.Equal(t, firstCall.eventID, retryCall.eventID)
	retryCall.result <- nil
	require.NoError(t, <-flushResult)
	require.NoError(t, f.Shutdown(t.Context()))
}

func TestFlushContextTimeoutRetainsExactBatchForLaterRetry(t *testing.T) {
	f, store, calls := newBatchSafetyFactory(t)
	saveRequest(t, store, "flush-times-out")

	flushCtx, cancel := context.WithCancel(t.Context())
	flushResult := make(chan error, 1)
	go func() { flushResult <- f.Flush(flushCtx) }()
	firstCall := receiveBatchCall(t, calls)
	cancel()
	require.ErrorIs(t, <-flushResult, context.Canceled)

	retryResult := make(chan error, 1)
	go func() { retryResult <- f.Flush(t.Context()) }()
	retryCall := receiveBatchCall(t, calls)
	assert.Equal(t, firstCall.eventID, retryCall.eventID)
	retryCall.result <- nil
	require.NoError(t, <-retryResult)
	require.NoError(t, f.Shutdown(t.Context()))
}

func TestRetryExhaustionRetainsAndResetsBatchForNextFlush(t *testing.T) {
	f, store, calls := newBatchSafetyFactory(t)
	f.maxRetryTime = 5 * time.Millisecond
	f.retryInitial = time.Millisecond
	f.retryMax = time.Millisecond
	f.retryMultiplier = 1
	f.retryJitter = 0
	saveRequest(t, store, "retry-exhausted")

	flushResult := make(chan error, 1)
	go func() { flushResult <- f.Flush(t.Context()) }()
	attempts := 0
exhausted:
	for {
		select {
		case call := <-calls:
			assert.Equal(t, []string{"retry-exhausted"}, call.eventID)
			attempts++
			call.result <- connect.NewError(connect.CodeUnavailable, errors.New("try later"))
		case err := <-flushResult:
			require.Error(t, err)
			break exhausted
		case <-time.After(time.Second):
			t.Fatal("retry budget did not expire")
		}
	}
	assert.Greater(t, attempts, 1)

	retryResult := make(chan error, 1)
	go func() { retryResult <- f.Flush(t.Context()) }()
	retryCall := receiveBatchCall(t, calls)
	assert.Equal(t, []string{"retry-exhausted"}, retryCall.eventID)
	retryCall.result <- nil
	require.NoError(t, <-retryResult)
	require.NoError(t, f.Shutdown(t.Context()))
}

func TestFlushIsAFIFOBarrier(t *testing.T) {
	f, store, calls := newBatchSafetyFactory(t)
	saveRequest(t, store, "before-barrier")

	firstResult := make(chan error, 1)
	go func() { firstResult <- f.Flush(t.Context()) }()
	firstCall := receiveBatchCall(t, calls)
	assert.Equal(t, []string{"before-barrier"}, firstCall.eventID)

	saveRequest(t, store, "after-barrier")
	firstCall.result <- nil
	require.NoError(t, <-firstResult)
	select {
	case unexpected := <-calls:
		t.Fatalf("flush barrier included a later save: %v", unexpected.eventID)
	default:
	}

	secondResult := make(chan error, 1)
	go func() { secondResult <- f.Flush(t.Context()) }()
	secondCall := receiveBatchCall(t, calls)
	assert.Equal(t, []string{"after-barrier"}, secondCall.eventID)
	secondCall.result <- nil
	require.NoError(t, <-secondResult)
	require.NoError(t, f.Shutdown(t.Context()))
}

func TestShutdownStopsAcceptanceDrainsAndIsIdempotent(t *testing.T) {
	f, store, calls := newBatchSafetyFactory(t)
	saveRequest(t, store, "queued-first")
	saveRequest(t, store, "queued-second")

	firstResult := make(chan error, 1)
	go func() { firstResult <- f.Shutdown(t.Context()) }()
	call := receiveBatchCall(t, calls)
	assert.Equal(t, []string{"queued-first", "queued-second"}, call.eventID)

	// Acceptance is closed before the shutdown barrier is queued.
	saveRequest(t, store, "must-not-be-accepted")
	select {
	case <-call.ctx.Done():
		t.Fatal("shutdown canceled the in-flight transport before flush completed")
	case <-f.ctx.Done():
		t.Fatal("shutdown canceled its transport before flush completed")
	default:
	}

	secondResult := make(chan error, 1)
	go func() { secondResult <- f.Shutdown(t.Context()) }()
	call.result <- nil
	require.NoError(t, <-firstResult)
	require.NoError(t, <-secondResult)
	select {
	case <-f.ingestDone:
	default:
		t.Fatal("shutdown returned before the ingest goroutine stopped")
	}

	require.NoError(t, f.Shutdown(t.Context()))
	select {
	case unexpected := <-calls:
		t.Fatalf("shutdown sent an unaccepted event or flushed twice: %v", unexpected.eventID)
	default:
	}
}

func TestShutdownTimeoutRetainsBatchForIdempotentRetry(t *testing.T) {
	f, store, calls := newBatchSafetyFactory(t)
	saveRequest(t, store, "shutdown-retry")

	shutdownCtx, cancel := context.WithCancel(t.Context())
	firstResult := make(chan error, 1)
	go func() { firstResult <- f.Shutdown(shutdownCtx) }()
	firstCall := receiveBatchCall(t, calls)
	cancel()
	require.ErrorIs(t, <-firstResult, context.Canceled)

	retryResult := make(chan error, 1)
	go func() { retryResult <- f.Shutdown(t.Context()) }()
	retryCall := receiveBatchCall(t, calls)
	assert.Equal(t, firstCall.eventID, retryCall.eventID)
	retryCall.result <- nil
	require.NoError(t, <-retryResult)
	require.NoError(t, f.Shutdown(t.Context()))
}

func TestCachedStoreForwardsSavesAfterFactoryReplacement(t *testing.T) {
	oldFactory, oldStore, _ := newBatchSafetyFactory(t)
	newFactory, _, newCalls := newBatchSafetyFactory(t)
	oldFactory.Next(newFactory)
	require.NoError(t, oldFactory.Shutdown(t.Context()))

	saveRequest(t, oldStore, "saved-through-old-store")
	flushResult := make(chan error, 1)
	go func() { flushResult <- newFactory.Flush(t.Context()) }()
	call := receiveBatchCall(t, newCalls)
	assert.Equal(t, []string{"saved-through-old-store"}, call.eventID)
	call.result <- nil
	require.NoError(t, <-flushResult)
	require.NoError(t, newFactory.Shutdown(t.Context()))
}

func TestCachedStoreTraversesStoppedFactoryReplacementChain(t *testing.T) {
	firstFactory, firstStore, _ := newBatchSafetyFactory(t)
	secondFactory, _, _ := newBatchSafetyFactory(t)
	thirdFactory, _, thirdCalls := newBatchSafetyFactory(t)
	firstFactory.Next(secondFactory)
	secondFactory.Next(thirdFactory)
	require.NoError(t, firstFactory.Shutdown(t.Context()))
	require.NoError(t, secondFactory.Shutdown(t.Context()))

	saveRequest(t, firstStore, "saved-through-chain")
	flushResult := make(chan error, 1)
	go func() { flushResult <- thirdFactory.Flush(t.Context()) }()
	call := receiveBatchCall(t, thirdCalls)
	assert.Equal(t, []string{"saved-through-chain"}, call.eventID)
	call.result <- nil
	require.NoError(t, <-flushResult)
	require.NoError(t, thirdFactory.Shutdown(t.Context()))
}
