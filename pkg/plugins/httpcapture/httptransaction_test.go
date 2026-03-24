package httpcapture

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHttpTransactionToJSONFormat(t *testing.T) {
	tests := []struct {
		name              string
		transaction       HttpTransaction
		expectedJSON      map[string]any
		expectedJSONError bool
	}{
		{
			name: "Basic transaction with all fields",
			transaction: HttpTransaction{
				TransactionTime: time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC),
				DurationMs:      150,
				Direction:       "egress-external",
				Metadata: Metadata{
					ProcessID:     "1234",
					ProcessExe:    "/usr/bin/curl",
					ConnectionID:  "conn-123",
					EndpointID:    "ep-123",
					RequestID:     "req-123",
					BytesSent:     100,
					BytesReceived: 200,
				},
				Request: Request{
					Method:      "GET",
					URL:         "https://example.com/api",
					Path:        "/api",
					Authority:   "example.com",
					Scheme:      "https",
					UserAgent:   "curl/7.68.0",
					ContentType: "application/json",
					Headers: map[string]string{
						":method":      "GET",
						":path":        "/api",
						":authority":   "example.com",
						":scheme":      "https",
						"User-Agent":   "curl/7.68.0",
						"Accept":       "*/*",
						"Content-Type": "application/json",
					},
					Body: []byte(`{"key":"value"}`),
				},
				Response: Response{
					Status:      200,
					ContentType: "application/json",
					Headers: map[string]string{
						":status":      "200",
						"Content-Type": "application/json",
						"Server":       "nginx",
					},
					Body: []byte(`{"result":"success"}`),
				},
			},
			expectedJSON: map[string]any{
				"transaction_time": "2025-05-20T10:00:00Z",
				"duration_ms":      float64(150),
				"direction":        "egress-external",
				"metadata": map[string]any{
					"process_id":     "1234",
					"process_exe":    "/usr/bin/curl",
					"connection_id":  "conn-123",
					"endpoint_id":    "ep-123",
					"request_id":     "req-123",
					"bytes_sent":     float64(100),
					"bytes_received": float64(200),
				},
				"request": map[string]any{
					"method":       "GET",
					"url":          "https://example.com/api",
					"path":         "/api",
					"authority":    "example.com",
					"scheme":       "https",
					"user_agent":   "curl/7.68.0",
					"content_type": "application/json",
					"headers": map[string]any{
						":method":      "GET",
						":path":        "/api",
						":authority":   "example.com",
						":scheme":      "https",
						"User-Agent":   "curl/7.68.0",
						"Accept":       "*/*",
						"Content-Type": "application/json",
					},
					"body": "eyJrZXkiOiJ2YWx1ZSJ9",
				},
				"response": map[string]any{
					"status":       float64(200),
					"content_type": "application/json",
					"headers": map[string]any{
						":status":      "200",
						"Content-Type": "application/json",
						"Server":       "nginx",
					},
					"body": "eyJyZXN1bHQiOiJzdWNjZXNzIn0=",
				},
			},
			expectedJSONError: false,
		},
		{
			name: "Minimal transaction",
			transaction: HttpTransaction{
				TransactionTime: time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC),
				Request: Request{
					Method: "GET",
					URL:    "https://example.com",
				},
				Response: Response{
					Status: 200,
				},
			},
			expectedJSON: map[string]any{
				"transaction_time": "2025-05-20T10:00:00Z",
				"metadata":         map[string]any{},
				"request": map[string]any{
					"method": "GET",
					"url":    "https://example.com",
				},
				"response": map[string]any{
					"status": float64(200),
				},
			},
			expectedJSONError: false,
		},
		{
			name: "Transaction with tags",
			transaction: HttpTransaction{
				TransactionTime: time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC),
				Metadata: Metadata{
					Tags: []string{"env:production", "team:backend"},
				},
				Request: Request{
					Method: "GET",
					URL:    "https://example.com",
				},
				Response: Response{
					Status: 200,
				},
			},
			expectedJSON: map[string]any{
				"transaction_time": "2025-05-20T10:00:00Z",
				"metadata": map[string]any{
					"tags": []any{"env:production", "team:backend"},
				},
				"request": map[string]any{
					"method": "GET",
					"url":    "https://example.com",
				},
				"response": map[string]any{
					"status": float64(200),
				},
			},
			expectedJSONError: false,
		},
		{
			name: "Nil tags are omitted from JSON",
			transaction: HttpTransaction{
				TransactionTime: time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC),
				Metadata: Metadata{
					ProcessExe: "/usr/bin/curl",
					Tags:       nil,
				},
				Request: Request{
					Method: "GET",
					URL:    "https://example.com",
				},
				Response: Response{
					Status: 200,
				},
			},
			expectedJSON: map[string]any{
				"transaction_time": "2025-05-20T10:00:00Z",
				"metadata": map[string]any{
					"process_exe": "/usr/bin/curl",
				},
				"request": map[string]any{
					"method": "GET",
					"url":    "https://example.com",
				},
				"response": map[string]any{
					"status": float64(200),
				},
			},
			expectedJSONError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Convert to JSON
			jsonData, err := tc.transaction.ToJSON()

			// Check if error matches expectation
			if tc.expectedJSONError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Parse the JSON back to a map for comparison
			var actualJSON map[string]any
			err = json.Unmarshal(jsonData, &actualJSON)
			require.NoError(t, err)

			// Compare the JSON contents
			assert.Equal(t, tc.expectedJSON, actualJSON)
		})
	}
}

func TestHttpTransactionToText(t *testing.T) {
	tests := []struct {
		name               string
		transaction        HttpTransaction
		expectedSubstrings []string
	}{
		{
			name: "Full transaction with all fields",
			transaction: HttpTransaction{
				TransactionTime: time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC),
				DurationMs:      150,
				Direction:       "egress-external",
				Metadata: Metadata{
					ProcessID:      "1234",
					ProcessExe:     "/usr/bin/curl",
					BytesSent:      100,
					BytesReceived:  200,
					ConnectionID:   "conn-123",
					EndpointID:     "ep-123",
					ContainerName:  "test-container",
					ContainerImage: "nginx:latest",
				},
				Request: Request{
					Method:      "POST",
					URL:         "https://example.com/api/data",
					Path:        "/api/data",
					Authority:   "example.com",
					Scheme:      "https",
					ContentType: "application/json",
					Headers: map[string]string{
						"Content-Type":  "application/json",
						"Authorization": "Bearer token123",
					},
					Body: []byte(`{"data":"test"}`),
				},
				Response: Response{
					Status:      201,
					ContentType: "application/json",
					Headers: map[string]string{
						"Content-Type":   "application/json",
						"Content-Length": "30",
					},
					Body: []byte(`{"id":"123","status":"created"}`),
				},
			},
			expectedSubstrings: []string{
				"HTTP Transaction",
				"Transaction Time: 2025-05-20T10:00:00Z",
				"Duration: 150ms",
				"Direction: egress-external",
				"Process ID: 1234",
				"Process: /usr/bin/curl",
				"Bytes Sent: 100",
				"Bytes Received: 200",
				"Method: POST",
				"URL: https://example.com/api/data",
				"Content-Type: application/json",
				"Headers:",
				"Authorization: Bearer token123",
				"Body:",
				"{\"data\":\"test\"}",
				"Status: 201",
				"Content-Length: 30",
				"{\"id\":\"123\",\"status\":\"created\"}",
			},
		},
		{
			name: "Minimal transaction",
			transaction: HttpTransaction{
				TransactionTime: time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC),
				Request: Request{
					Method: "GET",
					URL:    "https://example.com",
				},
				Response: Response{
					Status: 200,
				},
			},
			expectedSubstrings: []string{
				"HTTP Transaction",
				"Transaction Time: 2025-05-20T10:00:00Z",
				"Bytes Sent: 0",
				"Bytes Received: 0",
				"Method: GET",
				"URL: https://example.com",
				"Status: 200",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Convert to text
			textOutput := tc.transaction.ToString()

			// Verify each expected substring is present
			for _, expectedStr := range tc.expectedSubstrings {
				assert.Contains(t, textOutput, expectedStr)
			}
		})
	}
}
