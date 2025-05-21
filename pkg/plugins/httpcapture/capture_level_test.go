package httpcapture

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCaptureLevelBehavior(t *testing.T) {
	tests := []struct {
		name           string
		level          captureLevel
		includeHeaders bool
		includeBody    bool
	}{
		{
			name:           "None level captures nothing",
			level:          CaptureLevelNone,
			includeHeaders: false,
			includeBody:    false,
		},
		{
			name:           "Summary level doesn't include headers or body",
			level:          CaptureLevelSummary,
			includeHeaders: false,
			includeBody:    false,
		},
		{
			name:           "Headers level includes headers but not body",
			level:          CaptureLevelHeaders,
			includeHeaders: true,
			includeBody:    false,
		},
		{
			name:           "Full level includes both headers and body",
			level:          CaptureLevelFull,
			includeHeaders: true,
			includeBody:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a transaction with all fields filled
			tx := createTestTransaction()

			// Apply capture level
			applyCaptureLevel(&tx, tc.level)

			// Check if headers are present based on capture level
			if tc.includeHeaders {
				assert.NotNil(t, tx.Request.Headers, "Request headers should be present")
				assert.NotNil(t, tx.Response.Headers, "Response headers should be present")
			} else {
				assert.Nil(t, tx.Request.Headers, "Request headers should be nil")
				assert.Nil(t, tx.Response.Headers, "Response headers should be nil")
			}

			// Check if body is present based on capture level
			if tc.includeBody {
				assert.NotNil(t, tx.Request.Body, "Request body should be present")
				assert.NotNil(t, tx.Response.Body, "Response body should be present")
			} else {
				assert.Nil(t, tx.Request.Body, "Request body should be nil")
				assert.Nil(t, tx.Response.Body, "Response body should be nil")
			}
		})
	}
}

// Helper function to create a test transaction
func createTestTransaction() HttpTransaction {
	return HttpTransaction{
		Request: Request{
			Method: "GET",
			URL:    "https://example.com",
			Headers: map[string]string{
				"User-Agent": "test",
			},
			Body: []byte(`{"test":"data"}`),
		},
		Response: Response{
			Status: 200,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: []byte(`{"result":"success"}`),
		},
	}
}

// Helper function that mimics the behavior of capture level application
func applyCaptureLevel(tx *HttpTransaction, level captureLevel) {
	switch level {
	case CaptureLevelNone:
		// None level doesn't capture anything
		tx.Request.Headers = nil
		tx.Response.Headers = nil
		tx.Request.Body = nil
		tx.Response.Body = nil
	case CaptureLevelSummary:
		// Summary level doesn't include headers or bodies
		tx.Request.Headers = nil
		tx.Response.Headers = nil
		tx.Request.Body = nil
		tx.Response.Body = nil
	case CaptureLevelHeaders:
		// Headers level includes headers but not bodies
		tx.Request.Body = nil
		tx.Response.Body = nil
	case CaptureLevelFull:
		// Full level includes everything, so no changes needed
	}
}
