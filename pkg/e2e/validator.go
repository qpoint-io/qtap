package e2e

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

// RequestExpectations defines what we expect from the client
type RequestExpectations struct {
	Method      string
	Path        string
	HTTPVersion string
	Headers     map[string]string
	Body        string
}

// CapturedRequest stores what actually arrived at the server
type CapturedRequest struct {
	Method     string
	Path       string
	Proto      string
	Headers    http.Header
	Body       []byte
	RemoteAddr string
	Timestamp  time.Time
}

// RequestValidator validates incoming requests
type RequestValidator struct {
	t            *testing.T
	expectations RequestExpectations
	mu           sync.Mutex
	captured     []CapturedRequest
}

// NewRequestValidator creates a new validator
func NewRequestValidator(t *testing.T, exp RequestExpectations) *RequestValidator {
	return &RequestValidator{
		t:            t,
		expectations: exp,
		captured:     make([]CapturedRequest, 0),
	}
}

// Middleware wraps an HTTP handler with validation
func (rv *RequestValidator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the request
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		captured := CapturedRequest{
			Method:     r.Method,
			Path:       r.URL.Path,
			Proto:      r.Proto,
			Headers:    r.Header.Clone(),
			Body:       bodyBytes,
			RemoteAddr: r.RemoteAddr,
			Timestamp:  time.Now(),
		}

		rv.mu.Lock()
		rv.captured = append(rv.captured, captured)
		rv.mu.Unlock()

		// Validate the request
		if err := rv.validateRequest(captured); err != nil {
			rv.t.Errorf("Request validation failed: %v", err)
			http.Error(w, fmt.Sprintf("Validation failed: %v", err), http.StatusBadRequest)
			return
		}

		// Forward to the actual handler
		next.ServeHTTP(w, r)
	})
}

func (rv *RequestValidator) validateRequest(captured CapturedRequest) error {
	exp := rv.expectations

	// Validate method
	if exp.Method != "" && captured.Method != exp.Method {
		return fmt.Errorf("method mismatch: expected %s, got %s", exp.Method, captured.Method)
	}

	// Validate path
	if exp.Path != "" && captured.Path != exp.Path {
		return fmt.Errorf("path mismatch: expected %s, got %s", exp.Path, captured.Path)
	}

	// Validate HTTP version - strict matching, no fallbacks
	if exp.HTTPVersion != "" {
		expectedProto := fmt.Sprintf("HTTP/%s", exp.HTTPVersion)
		if captured.Proto != expectedProto {
			return fmt.Errorf("protocol mismatch: expected %s, got %s",
				expectedProto, captured.Proto)
		}
	}

	// Validate headers
	for key, expectedValue := range exp.Headers {
		actualValue := captured.Headers.Get(key)
		if actualValue != expectedValue {
			return fmt.Errorf("header %s mismatch: expected %q, got %q",
				key, expectedValue, actualValue)
		}
	}

	// Validate body
	if exp.Body != "" && string(captured.Body) != exp.Body {
		return fmt.Errorf("body mismatch: expected %q, got %q",
			exp.Body, string(captured.Body))
	}

	return nil
}

// GetCaptured returns all captured requests
func (rv *RequestValidator) GetCaptured() []CapturedRequest {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	return append([]CapturedRequest{}, rv.captured...)
}
