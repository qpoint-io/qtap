package e2e

import (
	"testing"
)

// ValidationFunc validates test execution results
type ValidationFunc func(t *testing.T, ctx ValidationContext) error

// ValidationContext provides all test artifacts for validation
type ValidationContext struct {
	TestContext *TestContext
	TestCase    *TestCase
	Container   *ContainerResult
}

// TODO(Jon): Add more validation functions
