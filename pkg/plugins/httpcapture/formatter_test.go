package httpcapture

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHttpTransactionFormatting(t *testing.T) {
	// Create test transactions
	transactions := []struct {
		name                     string
		transaction              HttpTransaction
		expectedJSONFields       []string
		expectedTextSubstrings   []string
		unexpectedJSONFields     []string
		unexpectedTextSubstrings []string
	}{
		{
			name: "Complete transaction with all fields",
			transaction: HttpTransaction{
				TransactionTime: time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC),
				DurationMs:      150,
				Direction:       "egress-external",
				Metadata: Metadata{
					ProcessID:       "1234",
					ProcessExe:      "/usr/bin/curl",
					BytesSent:       100,
					BytesReceived:   200,
					ConnectionID:    "conn-123",
					EndpointID:      "ep-123",
					ContainerName:   "test-container",
					ContainerImage:  "nginx:latest",
					QpointRequestID: "req-123",
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
			expectedJSONFields: []string{
				"transaction_time", "duration_ms", "direction",
				"metadata", "process_id", "process_exe", "container_name", "container_image",
				"request", "method", "path", "url", "headers", "body",
				"response", "status", "headers", "body",
			},
			expectedTextSubstrings: []string{
				"HTTP Transaction",
				"Transaction Time: 2025-05-20T10:00:00Z",
				"Duration: 150ms",
				"Process ID: 1234",
				"Method: POST",
				"URL: https://example.com/api/data",
				"Authorization: Bearer token123",
				"{\"data\":\"test\"}",
				"Status: 201",
				"{\"id\":\"123\",\"status\":\"created\"}",
			},
			unexpectedJSONFields:     []string{}, // All fields should be present
			unexpectedTextSubstrings: []string{}, // All substrings should be present
		},
		{
			name: "Summary level transaction (no headers or body)",
			transaction: HttpTransaction{
				TransactionTime: time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC),
				DurationMs:      150,
				Direction:       "egress-external",
				Metadata: Metadata{
					ProcessID:     "1234",
					BytesSent:     100,
					BytesReceived: 200,
				},
				Request: Request{
					Method: "GET",
					URL:    "https://example.com/api",
					Path:   "/api",
					// No headers or body
				},
				Response: Response{
					Status: 200,
					// No headers or body
				},
			},
			expectedJSONFields: []string{
				"transaction_time", "duration_ms", "direction",
				"metadata", "process_id",
				"request", "method", "url",
				"response", "status",
			},
			expectedTextSubstrings: []string{
				"HTTP Transaction",
				"Transaction Time: 2025-05-20T10:00:00Z",
				"Duration: 150ms",
				"Process ID: 1234",
				"Method: GET",
				"URL: https://example.com/api",
				"Status: 200",
			},
			unexpectedJSONFields: []string{
				"request.headers", "request.body",
				"response.headers", "response.body",
			},
			unexpectedTextSubstrings: []string{
				"Authorization",
				"Content-Type",
				"{\"data\":\"test\"}",
				"{\"id\":\"123\",\"status\":\"created\"}",
			},
		},
	}

	for _, tc := range transactions {
		t.Run(tc.name, func(t *testing.T) {
			// Test JSON format
			jsonData, err := tc.transaction.ToJSON()
			require.NoError(t, err)

			// Convert to string for simple substring checking
			jsonStr := string(jsonData)

			// Check expected JSON fields
			for _, field := range tc.expectedJSONFields {
				assert.Contains(t, jsonStr, field, "JSON should contain field %s", field)
			}

			// Check unexpected JSON fields
			for _, field := range tc.unexpectedJSONFields {
				// Ensure accurate checking by using proper JSON path format
				parts := strings.Split(field, ".")
				lastPart := parts[len(parts)-1]

				// Look for "field":" or "field": [ patterns (handle both string and array/object)
				pattern1 := "\"" + lastPart + "\":\""
				pattern2 := "\"" + lastPart + "\":[{]"

				assert.NotContains(t, jsonStr, pattern1, "JSON should not contain field %s", field)
				assert.NotContains(t, jsonStr, pattern2, "JSON should not contain field %s", field)
			}

			// Test text format
			textOutput := tc.transaction.ToString()

			// Check expected text substrings
			for _, substring := range tc.expectedTextSubstrings {
				assert.Contains(t, textOutput, substring, "Text should contain substring %s", substring)
			}

			// Check unexpected text substrings
			for _, substring := range tc.unexpectedTextSubstrings {
				assert.NotContains(t, textOutput, substring, "Text should not contain substring %s", substring)
			}
		})
	}
}

func TestFormatSpecificFields(t *testing.T) {
	// Create minimal test transaction
	tx := HttpTransaction{
		TransactionTime: time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC),
		Request: Request{
			Method: "GET",
			URL:    "https://example.com",
		},
		Response: Response{
			Status: 200,
		},
	}

	// Test JSON format specific features
	jsonData, err := tx.ToJSON()

	// Ensure JSON can be unmarshaled
	require.NoError(t, err)
	var parsedJSON map[string]any
	err = json.Unmarshal(jsonData, &parsedJSON)
	require.NoError(t, err)

	// Verify structure matches expected fields
	assert.Contains(t, parsedJSON, "transaction_time")
	assert.Contains(t, parsedJSON, "request")
	assert.Contains(t, parsedJSON, "response")

	// Verify value types are correct
	assert.IsType(t, map[string]any{}, parsedJSON["request"])
	assert.IsType(t, map[string]any{}, parsedJSON["response"])

	// Test text format specific features
	textOutput := tx.ToString()

	// Basic text format validation
	assert.True(t, strings.HasPrefix(textOutput, "HTTP Transaction"))
	assert.Contains(t, textOutput, "Request:")
	assert.Contains(t, textOutput, "Response:")

	// Verify formatting is table-like
	lines := strings.Split(textOutput, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) != "" &&
			!strings.HasPrefix(strings.TrimSpace(line), "HTTP Transaction") &&
			!strings.HasPrefix(strings.TrimSpace(line), "==") {
			// Each content line should either have a colon for key-value or be a section header
			issectionHeader := strings.HasSuffix(line, ":")
			hasColon := strings.Contains(line, ":")

			assert.True(t, issectionHeader || hasColon,
				"Text format line should be section header or contain colon: %s", line)
		}
	}
}
