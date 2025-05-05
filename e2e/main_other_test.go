//go:build e2e && !linux

package e2e

import "fmt"

func mainSetup() error {
	return fmt.Errorf("e2e tests are not supported on non-Linux platforms")
}
