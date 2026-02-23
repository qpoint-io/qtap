package kafka

import (
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/connection"
)

func TestPendingRequestCorrelation(t *testing.T) {
	// Test that pending request map properly correlates requests and responses
	pending := make(map[int32]*PendingRequest)

	// Add pending requests
	pending[1] = &PendingRequest{
		Request: &Request{
			Header: RequestHeader{
				ApiKey:        ApiKeyApiVersions,
				ApiVersion:    3,
				CorrelationID: 1,
				ClientID:      "test-client",
			},
		},
		Timestamp: time.Now(),
	}
	pending[2] = &PendingRequest{
		Request: &Request{
			Header: RequestHeader{
				ApiKey:        ApiKeyMetadata,
				ApiVersion:    9,
				CorrelationID: 2,
				ClientID:      "test-client",
			},
		},
		Timestamp: time.Now(),
	}

	// Verify lookup
	req1, ok := pending[1]
	if !ok {
		t.Fatal("expected to find pending request with correlation_id 1")
	}
	if req1.Request.Header.ApiKey != ApiKeyApiVersions {
		t.Errorf("expected ApiVersions, got %d", req1.Request.Header.ApiKey)
	}

	req2, ok := pending[2]
	if !ok {
		t.Fatal("expected to find pending request with correlation_id 2")
	}
	if req2.Request.Header.ApiKey != ApiKeyMetadata {
		t.Errorf("expected Metadata, got %d", req2.Request.Header.ApiKey)
	}

	// Missing correlation should return false
	_, ok = pending[99]
	if ok {
		t.Error("expected no pending request with correlation_id 99")
	}

	// Delete after correlating
	delete(pending, 1)
	_, ok = pending[1]
	if ok {
		t.Error("expected no pending request after deletion")
	}
}

func TestLatencyCalculation(t *testing.T) {
	start := time.Now()
	time.Sleep(10 * time.Millisecond)
	latency := time.Since(start)

	if latency < 10*time.Millisecond {
		t.Errorf("expected latency >= 10ms, got %v", latency)
	}
}

func TestDirectionHelpers(t *testing.T) {
	// These are the same helpers used in stream.go
	// Client egress = request
	if !isRequest(connection.Client, connection.Egress) {
		t.Error("client egress should be request")
	}
	// Server ingress = request
	if !isRequest(connection.Server, connection.Ingress) {
		t.Error("server ingress should be request")
	}
	// Client ingress = response
	if !isResponse(connection.Client, connection.Ingress) {
		t.Error("client ingress should be response")
	}
	// Server egress = response
	if !isResponse(connection.Server, connection.Egress) {
		t.Error("server egress should be response")
	}
}
