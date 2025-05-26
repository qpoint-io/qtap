package httpcapture

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHttpTransactionSerialization(t *testing.T) {
	// Create a simple transaction for testing
	tx := &HttpTransaction{
		TransactionTime: time.Now(),
		DurationMs:      100,
		Direction:       "egress-external",
		Metadata: Metadata{
			ProcessID:       "12345",
			ProcessExe:      "/usr/bin/app",
			ContainerName:   "test-container",
			EndpointID:      "test-endpoint",
			ConnectionID:    "test-connection",
			QpointRequestID: "test-request-id",
			BytesSent:       100,
			BytesReceived:   200,
		},
		Request: Request{
			Method:      "GET",
			URL:         "https://example.com/api/users",
			Path:        "/api/users",
			Authority:   "example.com",
			Scheme:      "https",
			UserAgent:   "test-agent",
			ContentType: "application/json",
			Headers: map[string]string{
				":method":           "GET",
				":path":             "/api/users",
				":authority":        "example.com",
				":scheme":           "https",
				"User-Agent":        "test-agent",
				"Content-Type":      "application/json",
				"qpoint-request-id": "test-request-id",
			},
			Body: []byte(`{"test":"request"}`),
		},
		Response: Response{
			Status:      200,
			ContentType: "application/json",
			Headers: map[string]string{
				":status":      "200",
				"Content-Type": "application/json",
			},
			Body: []byte(`{"result":"success"}`),
		},
	}

	// Test JSON serialization
	jsonData, err := tx.ToJSON()
	require.NoError(t, err)

	// Verify JSON can be parsed back
	var parsedTx HttpTransaction
	err = json.Unmarshal(jsonData, &parsedTx)
	require.NoError(t, err)

	// Verify key fields
	assert.Equal(t, tx.Request.Method, parsedTx.Request.Method)
	assert.Equal(t, tx.Response.Status, parsedTx.Response.Status)
	assert.Equal(t, tx.Metadata.ProcessID, parsedTx.Metadata.ProcessID)
	assert.Equal(t, tx.Direction, parsedTx.Direction)

	// Test text format
	textOutput := tx.ToString()

	// Verify text contains key information
	assert.Contains(t, textOutput, "HTTP Transaction")
	assert.Contains(t, textOutput, "Method: GET")
	assert.Contains(t, textOutput, "URL: https://example.com/api/users")
	assert.Contains(t, textOutput, "Status: 200")
	assert.Contains(t, textOutput, `{"test":"request"}`)
	assert.Contains(t, textOutput, `{"result":"success"}`)
}
