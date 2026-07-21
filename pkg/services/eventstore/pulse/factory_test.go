package pulse

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	eventstorev1 "github.com/qpoint-io/proto/gen/go/eventstore/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockEventStoreClient is a mock implementation of EventStoreServiceClient
type mockEventStoreClient struct {
	ingestBatchFunc func(context.Context, *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error)
}

func (m *mockEventStoreClient) Ping(ctx context.Context, req *connect.Request[eventstorev1.PingRequest]) (*connect.Response[eventstorev1.PingResponse], error) {
	return connect.NewResponse(&eventstorev1.PingResponse{}), nil
}

func (m *mockEventStoreClient) Ingest(ctx context.Context) *connect.BidiStreamForClient[eventstorev1.IngestRequest, eventstorev1.IngestResponse] {
	return nil
}

func (m *mockEventStoreClient) IngestBatch(ctx context.Context, req *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error) {
	if m.ingestBatchFunc != nil {
		return m.ingestBatchFunc(ctx, req)
	}
	return connect.NewResponse(&eventstorev1.IngestBatchResponse{}), nil
}

func createTestLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "context canceled",
			err:       context.Canceled,
			retryable: false,
		},
		{
			name:      "unavailable code",
			err:       connect.NewError(connect.CodeUnavailable, errors.New("service unavailable")),
			retryable: true,
		},
		{
			name:      "deadline exceeded code",
			err:       connect.NewError(connect.CodeDeadlineExceeded, errors.New("deadline exceeded")),
			retryable: true,
		},
		{
			name:      "resource exhausted code",
			err:       connect.NewError(connect.CodeResourceExhausted, errors.New("resource exhausted")),
			retryable: true,
		},
		{
			name:      "aborted code",
			err:       connect.NewError(connect.CodeAborted, errors.New("aborted")),
			retryable: true,
		},
		{
			name:      "invalid argument code",
			err:       connect.NewError(connect.CodeInvalidArgument, errors.New("invalid argument")),
			retryable: false,
		},
		{
			name:      "permission denied code",
			err:       connect.NewError(connect.CodePermissionDenied, errors.New("permission denied")),
			retryable: false,
		},
		{
			name:      "unauthenticated code",
			err:       connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated")),
			retryable: false,
		},
		{
			name:      "not found code",
			err:       connect.NewError(connect.CodeNotFound, errors.New("not found")),
			retryable: false,
		},
		{
			name:      "already exists code",
			err:       connect.NewError(connect.CodeAlreadyExists, errors.New("already exists")),
			retryable: false,
		},
		{
			name:      "failed precondition code",
			err:       connect.NewError(connect.CodeFailedPrecondition, errors.New("failed precondition")),
			retryable: false,
		},
		{
			name:      "out of range code",
			err:       connect.NewError(connect.CodeOutOfRange, errors.New("out of range")),
			retryable: false,
		},
		{
			name:      "unimplemented code",
			err:       connect.NewError(connect.CodeUnimplemented, errors.New("unimplemented")),
			retryable: false,
		},
		{
			name:      "internal code",
			err:       connect.NewError(connect.CodeInternal, errors.New("internal error")),
			retryable: false,
		},
		{
			name:      "data loss code",
			err:       connect.NewError(connect.CodeDataLoss, errors.New("data loss")),
			retryable: false,
		},
		{
			name:      "unknown error",
			err:       errors.New("unknown error"),
			retryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			assert.Equal(t, tt.retryable, result, "error %v should have retryable=%v", tt.err, tt.retryable)
		})
	}
}

func TestFlushWithRetries_Success(t *testing.T) {
	// Create a factory with a mock client that fails twice then succeeds
	f := &Factory{
		logger:       createTestLogger(),
		token:        "test-token",
		maxRetryTime: 1 * time.Minute,
	}

	attemptCount := atomic.Int32{}
	f.client = &mockEventStoreClient{
		ingestBatchFunc: func(ctx context.Context, req *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error) {
			count := attemptCount.Add(1)
			if count <= 2 {
				return nil, connect.NewError(connect.CodeUnavailable, errors.New("temporary failure"))
			}
			return connect.NewResponse(&eventstorev1.IngestBatchResponse{}), nil
		},
	}

	ctx := t.Context()
	batch := []*eventstorev1.Event{
		eventstorev1.Event_builder{}.Build(),
		eventstorev1.Event_builder{}.Build(),
	}

	// Test the flush operation by calling it directly
	err := f.flushBatchWithRetry(ctx, batch, len(batch))
	require.NoError(t, err)
	assert.Equal(t, int32(3), attemptCount.Load(), "should have made 3 attempts (2 failures + 1 success)")
}

func TestFlushWithRetries_PermanentError(t *testing.T) {
	// Create a factory with a mock client that returns a non-retryable error
	f := &Factory{
		logger:       createTestLogger(),
		token:        "test-token",
		maxRetryTime: 1 * time.Minute,
	}

	attemptCount := atomic.Int32{}
	f.client = &mockEventStoreClient{
		ingestBatchFunc: func(ctx context.Context, req *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error) {
			attemptCount.Add(1)
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid request"))
		},
	}

	ctx := t.Context()
	batch := []*eventstorev1.Event{
		eventstorev1.Event_builder{}.Build(),
	}

	// Test the flush operation - should fail immediately without retries
	err := f.flushBatchWithRetry(ctx, batch, len(batch))
	require.Error(t, err)
	assert.Equal(t, int32(1), attemptCount.Load(), "should have made only 1 attempt for non-retryable error")
}

func TestFlushWithRetries_TransientError(t *testing.T) {
	// Create a factory with a mock client that always returns a retryable error
	f := &Factory{
		logger:       createTestLogger(),
		token:        "test-token",
		maxRetryTime: 500 * time.Millisecond, // Short timeout for testing
	}

	attemptCount := atomic.Int32{}
	f.client = &mockEventStoreClient{
		ingestBatchFunc: func(ctx context.Context, req *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error) {
			attemptCount.Add(1)
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("service unavailable"))
		},
	}

	ctx := t.Context()
	batch := []*eventstorev1.Event{
		eventstorev1.Event_builder{}.Build(),
	}

	// Test the flush operation - should retry multiple times until timeout
	err := f.flushBatchWithRetry(ctx, batch, len(batch))

	require.Error(t, err)
	assert.Greater(t, attemptCount.Load(), int32(1), "should have made multiple retry attempts")
	// Backoff may stop before maxRetryTime if it determines no more attempts can be made
}

func TestFlushWithRetries_ContextCancellation(t *testing.T) {
	// Create a factory with a mock client that returns retryable errors
	f := &Factory{
		logger:       createTestLogger(),
		token:        "test-token",
		maxRetryTime: 1 * time.Minute,
	}

	attemptCount := atomic.Int32{}
	f.client = &mockEventStoreClient{
		ingestBatchFunc: func(ctx context.Context, req *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error) {
			attemptCount.Add(1)
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("service unavailable"))
		},
	}

	ctx, cancel := context.WithCancel(t.Context())
	batch := []*eventstorev1.Event{
		eventstorev1.Event_builder{}.Build(),
	}

	// Cancel context after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	// Test the flush operation - should stop retrying when context is cancelled
	start := time.Now()
	err := f.flushBatchWithRetry(ctx, batch, len(batch))
	duration := time.Since(start)

	require.Error(t, err)
	assert.Less(t, duration, 1*time.Second, "should have stopped quickly after context cancellation")
	assert.Positive(t, attemptCount.Load(), "should have made at least one attempt")
}

func TestBackoffTiming(t *testing.T) {
	// Create a factory with a mock client that tracks request times
	f := &Factory{
		logger:       createTestLogger(),
		token:        "test-token",
		maxRetryTime: 5 * time.Second,
	}

	requestTimes := make([]time.Time, 0)
	f.client = &mockEventStoreClient{
		ingestBatchFunc: func(ctx context.Context, req *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error) {
			requestTimes = append(requestTimes, time.Now())
			if len(requestTimes) < 4 {
				return nil, connect.NewError(connect.CodeUnavailable, errors.New("service unavailable"))
			}
			return connect.NewResponse(&eventstorev1.IngestBatchResponse{}), nil
		},
	}

	ctx := t.Context()
	batch := []*eventstorev1.Event{
		eventstorev1.Event_builder{}.Build(),
	}

	// Test the flush operation
	err := f.flushBatchWithRetry(ctx, batch, len(batch))
	require.NoError(t, err)
	require.Len(t, requestTimes, 4, "should have made 4 attempts")

	// Verify exponential backoff timing
	// Initial interval is 100ms, multiplier is 2.0
	// Expected delays: ~100ms, ~200ms, ~400ms (with 10% jitter)
	initialInterval := 100.0 // milliseconds
	multiplier := 2.0
	for i := 1; i < len(requestTimes); i++ {
		delay := requestTimes[i].Sub(requestTimes[i-1])
		// Exponential backoff: InitialInterval * Multiplier^(i-1)
		expectedDelay := time.Duration(initialInterval*math.Pow(multiplier, float64(i-1))) * time.Millisecond
		minDelay := time.Duration(float64(expectedDelay) * 0.9) // Account for jitter
		maxDelay := time.Duration(float64(expectedDelay) * 2.2) // Account for jitter and multiplier

		assert.GreaterOrEqual(t, delay, minDelay, "delay %v should be at least %v", delay, minDelay)
		assert.LessOrEqual(t, delay, maxDelay, "delay %v should be at most %v", delay, maxDelay)
	}
}

func TestFlushBatchWithRetry_EmptyBatch(t *testing.T) {
	f := &Factory{
		logger:       createTestLogger(),
		token:        "test-token",
		maxRetryTime: 1 * time.Minute,
	}

	callCount := atomic.Int32{}
	f.client = &mockEventStoreClient{
		ingestBatchFunc: func(ctx context.Context, req *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error) {
			callCount.Add(1)
			return connect.NewResponse(&eventstorev1.IngestBatchResponse{}), nil
		},
	}

	ctx := t.Context()
	err := f.flushBatchWithRetry(ctx, nil, 0)
	require.NoError(t, err)
	assert.Equal(t, int32(0), callCount.Load(), "should not call client for empty batch")

	err = f.flushBatchWithRetry(ctx, []*eventstorev1.Event{}, 0)
	require.NoError(t, err)
	assert.Equal(t, int32(0), callCount.Load(), "should not call client for empty batch slice")
}

func TestFlushBatchWithRetry_ImmediateSuccess(t *testing.T) {
	f := &Factory{
		logger:       createTestLogger(),
		token:        "test-token",
		maxRetryTime: 1 * time.Minute,
	}

	callCount := atomic.Int32{}
	f.client = &mockEventStoreClient{
		ingestBatchFunc: func(ctx context.Context, req *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error) {
			callCount.Add(1)
			return connect.NewResponse(&eventstorev1.IngestBatchResponse{}), nil
		},
	}

	ctx := t.Context()
	batch := []*eventstorev1.Event{
		eventstorev1.Event_builder{}.Build(),
	}

	err := f.flushBatchWithRetry(ctx, batch, len(batch))
	require.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load(), "should make exactly one call for immediate success")
}

func TestFlushBatchWithRetry_AuthorizationHeader(t *testing.T) {
	f := &Factory{
		logger:       createTestLogger(),
		token:        "test-auth-token-123",
		maxRetryTime: 1 * time.Minute,
	}

	var capturedReq *connect.Request[eventstorev1.IngestBatchRequest]
	f.client = &mockEventStoreClient{
		ingestBatchFunc: func(ctx context.Context, req *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error) {
			capturedReq = req
			return connect.NewResponse(&eventstorev1.IngestBatchResponse{}), nil
		},
	}

	ctx := t.Context()
	batch := []*eventstorev1.Event{
		eventstorev1.Event_builder{}.Build(),
	}

	err := f.flushBatchWithRetry(ctx, batch, len(batch))
	require.NoError(t, err)
	require.NotNil(t, capturedReq, "should have captured request")
	assert.Equal(t, "Bearer test-auth-token-123", capturedReq.Header().Get("Authorization"), "should set correct authorization header")
}

func TestFlushBatchWithRetry_RequestMade(t *testing.T) {
	f := &Factory{
		logger:       createTestLogger(),
		token:        "test-token",
		maxRetryTime: 1 * time.Minute,
	}

	callCount := atomic.Int32{}
	f.client = &mockEventStoreClient{
		ingestBatchFunc: func(ctx context.Context, req *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error) {
			callCount.Add(1)
			// Verify request is not nil
			require.NotNil(t, req, "request should not be nil")
			require.NotNil(t, req.Msg, "request message should not be nil")
			return connect.NewResponse(&eventstorev1.IngestBatchResponse{}), nil
		},
	}

	ctx := t.Context()
	expectedEvents := []*eventstorev1.Event{
		eventstorev1.Event_builder{}.Build(),
		eventstorev1.Event_builder{}.Build(),
		eventstorev1.Event_builder{}.Build(),
	}

	err := f.flushBatchWithRetry(ctx, expectedEvents, len(expectedEvents))
	require.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load(), "should have made request")
}

func TestFlushBatchWithRetry_AllRetryableErrorCodes(t *testing.T) {
	retryableCodes := []connect.Code{
		connect.CodeUnavailable,
		connect.CodeDeadlineExceeded,
		connect.CodeResourceExhausted,
		connect.CodeAborted,
	}

	for _, code := range retryableCodes {
		t.Run(code.String(), func(t *testing.T) {
			f := &Factory{
				logger:       createTestLogger(),
				token:        "test-token",
				maxRetryTime: 2 * time.Second, // Enough time for retries
			}

			attemptCount := atomic.Int32{}
			f.client = &mockEventStoreClient{
				ingestBatchFunc: func(ctx context.Context, req *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error) {
					count := attemptCount.Add(1)
					if count <= 2 {
						return nil, connect.NewError(code, errors.New("retryable error"))
					}
					return connect.NewResponse(&eventstorev1.IngestBatchResponse{}), nil
				},
			}

			ctx := t.Context()
			batch := []*eventstorev1.Event{
				eventstorev1.Event_builder{}.Build(),
			}

			err := f.flushBatchWithRetry(ctx, batch, len(batch))
			require.NoError(t, err, "should eventually succeed after retries for %s", code)
			assert.Equal(t, int32(3), attemptCount.Load(), "should have retried for %s", code)
		})
	}
}

func TestFlushBatchWithRetry_MaxRetryTimeRespected(t *testing.T) {
	f := &Factory{
		logger:       createTestLogger(),
		token:        "test-token",
		maxRetryTime: 200 * time.Millisecond,
	}

	attemptCount := atomic.Int32{}
	f.client = &mockEventStoreClient{
		ingestBatchFunc: func(ctx context.Context, req *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error) {
			attemptCount.Add(1)
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("service unavailable"))
		},
	}

	ctx := t.Context()
	batch := []*eventstorev1.Event{
		eventstorev1.Event_builder{}.Build(),
	}

	start := time.Now()
	err := f.flushBatchWithRetry(ctx, batch, len(batch))
	duration := time.Since(start)

	require.Error(t, err, "should fail after max retry time")
	// Backoff may stop before maxRetryTime if it determines no more attempts can be made,
	// but it should not significantly exceed it
	assert.LessOrEqual(t, duration, 500*time.Millisecond, "should not exceed max retry time significantly")
	assert.Greater(t, attemptCount.Load(), int32(1), "should have made multiple attempts")
}

func TestFlushBatchWithRetry_BatchSizeParameter(t *testing.T) {
	f := &Factory{
		logger:       createTestLogger(),
		token:        "test-token",
		maxRetryTime: 1 * time.Minute,
	}

	// This test verifies that the batchSize parameter is used correctly
	// by checking that it's passed to logging (we can't easily test logging,
	// but we can verify the function works with different batch sizes)
	callCount := atomic.Int32{}
	f.client = &mockEventStoreClient{
		ingestBatchFunc: func(ctx context.Context, req *connect.Request[eventstorev1.IngestBatchRequest]) (*connect.Response[eventstorev1.IngestBatchResponse], error) {
			callCount.Add(1)
			return connect.NewResponse(&eventstorev1.IngestBatchResponse{}), nil
		},
	}

	ctx := t.Context()
	batch := []*eventstorev1.Event{
		eventstorev1.Event_builder{}.Build(),
		eventstorev1.Event_builder{}.Build(),
	}

	// Test with batchSize matching actual batch length
	err := f.flushBatchWithRetry(ctx, batch, len(batch))
	require.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())

	// Test with different batchSize (should still work, just used for logging)
	err = f.flushBatchWithRetry(ctx, batch, 999)
	require.NoError(t, err)
	assert.Equal(t, int32(2), callCount.Load())
}
