package e2e

import (
	"testing"
)

// ValidationFunc validates test execution results
type ValidationFunc func(t *testing.T, ctx ValidationContext) error

// ValidationContext provides all test artifacts for validation
//
// TODO(Jon): Rename this to TestContext, but TextContext already exists
// and this could be merged with it. The helper functions would go from:
// "WithValidation(...)" to "WithTest(...)" and take funcs that have the
// *testing.T and *TestContext as arguments.
type ValidationContext struct {
	TestContext *TestContext
	TestCase    *TestCase
	Container   *ContainerResult
}

// TODO(Jon): Add more validation functions
