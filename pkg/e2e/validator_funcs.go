package e2e

import (
	"testing"
)

// ValidationFunc validates test execution results
type ValidationFunc func(t *testing.T, ctx ValidationContext) error

// ValidationContext provides all test artifacts for validation
type ValidationContext struct {
	// TestCase    *TestCase
	Container  *ContainerResult
	ServerReqs []CapturedRequest
	// TODO(Jon): Add event store ref here
	// AgentEvents []AgentEvent
}

// ExpectStatus validates HTTP status code
func ExpectStatus(code int) ValidationFunc {
	return func(t *testing.T, ctx ValidationContext) error {
		// Validate against your test server's response
		// This would check the status code returned to the client
		return nil
	}
}
