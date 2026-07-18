// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package gobin

// Func represents a function target.
type Func struct {
	Name          string
	Offset        uint64
	ReturnOffsets []uint64
}
