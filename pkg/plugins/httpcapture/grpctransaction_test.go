package httpcapture

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrpcTransaction_ToJSON(t *testing.T) {
	tx := &GrpcTransaction{
		Service:         "grpc.health.v1.Health",
		Method:          "Check",
		GrpcStatus:      "0",
		GrpcStatusName:  "OK",
		TransactionTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		DurationMs:      42,
		Direction:       "egress",
		Request: GrpcMessage{
			ContentType: "application/grpc",
			Headers:     map[string]string{":path": "/grpc.health.v1.Health/Check"},
		},
		Response: GrpcResponse{
			ContentType:    "application/grpc",
			GrpcStatus:     "0",
			GrpcStatusName: "OK",
			Headers:        map[string]string{"grpc-status": "0"},
		},
	}

	data, err := tx.ToJSON()
	require.NoError(t, err)

	var decoded GrpcTransaction
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "grpc.health.v1.Health", decoded.Service)
	assert.Equal(t, "Check", decoded.Method)
	assert.Equal(t, "0", decoded.GrpcStatus)
	assert.Equal(t, "OK", decoded.GrpcStatusName)
	assert.Equal(t, int64(42), decoded.DurationMs)
	assert.Equal(t, "egress", decoded.Direction)
	assert.Equal(t, "application/grpc", decoded.Request.ContentType)
	assert.Equal(t, "application/grpc", decoded.Response.ContentType)
}

func TestGrpcTransaction_ToJSON_WithBody(t *testing.T) {
	tx := &GrpcTransaction{
		Service:         "myservice.v1.MyService",
		Method:          "DoStuff",
		GrpcStatus:      "2",
		GrpcStatusName:  "UNKNOWN",
		GrpcMessage:     "something went wrong",
		TransactionTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		DurationMs:      100,
		Request: GrpcMessage{
			ContentType: "application/grpc",
			Headers:     map[string]string{":path": "/myservice.v1.MyService/DoStuff"},
			Body:        []byte("request-body"),
		},
		Response: GrpcResponse{
			ContentType:    "application/grpc",
			GrpcStatus:     "2",
			GrpcStatusName: "UNKNOWN",
			GrpcMessage:    "something went wrong",
			Headers:        map[string]string{"grpc-status": "2"},
			Body:           []byte("response-body"),
		},
	}

	data, err := tx.ToJSON()
	require.NoError(t, err)

	var decoded GrpcTransaction
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "something went wrong", decoded.GrpcMessage)
	assert.Equal(t, []byte("request-body"), decoded.Request.Body)
	assert.Equal(t, []byte("response-body"), decoded.Response.Body)
}

func TestGrpcTransaction_ToString(t *testing.T) {
	tx := &GrpcTransaction{
		Service:         "grpc.health.v1.Health",
		Method:          "Check",
		GrpcStatus:      "0",
		GrpcStatusName:  "OK",
		TransactionTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		DurationMs:      5,
		Direction:       "egress",
	}

	text := tx.ToString()
	assert.Contains(t, text, "gRPC Transaction")
	assert.Contains(t, text, "grpc.health.v1.Health")
	assert.Contains(t, text, "Check")
	assert.Contains(t, text, "OK (0)")
	assert.Contains(t, text, "5ms")
}

func TestGrpcTransaction_Summary(t *testing.T) {
	tx := &GrpcTransaction{
		Service:        "grpc.health.v1.Health",
		Method:         "Check",
		GrpcStatus:     "0",
		GrpcStatusName: "OK",
		DurationMs:     10,
		Direction:      "egress",
	}

	summary := tx.Summary()
	assert.Equal(t, "grpc.health.v1.Health", summary["grpc_service"])
	assert.Equal(t, "Check", summary["grpc_method"])
	assert.Equal(t, "0", summary["grpc_status"])
	assert.Equal(t, "OK", summary["grpc_status_name"])
	assert.Equal(t, int64(10), summary["duration_ms"])
	assert.Equal(t, "egress", summary["direction"])
}

func TestGrpcTransaction_SummaryLevel(t *testing.T) {
	// Verify summary level clears headers and body
	tx := &GrpcTransaction{
		Service:    "svc",
		Method:     "m",
		GrpcStatus: "0",
		Request: GrpcMessage{
			Headers: map[string]string{"k": "v"},
			Body:    []byte("body"),
		},
		Response: GrpcResponse{
			Headers: map[string]string{"k": "v"},
			Body:    []byte("body"),
		},
	}

	// Simulate summary level stripping
	tx.Request.Headers = nil
	tx.Response.Headers = nil
	tx.Request.Body = nil
	tx.Response.Body = nil

	data, err := tx.ToJSON()
	require.NoError(t, err)

	var decoded GrpcTransaction
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Nil(t, decoded.Request.Headers)
	assert.Nil(t, decoded.Response.Headers)
	assert.Nil(t, decoded.Request.Body)
	assert.Nil(t, decoded.Response.Body)
}
