//go:build !linux

package gobin

// Stubs for non-linux systems

//nolint:typecheck
func findRetInstructions(_ []byte) ([]uint64, error) {
	return nil, nil
}
