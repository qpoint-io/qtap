//go:build arm64 && linux

package gobin

import (
	"golang.org/x/arch/arm64/arm64asm"
)

const (
	// In ARM64 each instruction is 4 bytes in length.
	armInstructionSize = 4
)

//nolint:typecheck
func findRetInstructions(data []byte) ([]uint64, error) {
	var returnOffsets []uint64
	index := 0
	for index < len(data) {
		instruction, err := arm64asm.Decode(data[index:])
		if err == nil && instruction.Op == arm64asm.RET {
			returnOffsets = append(returnOffsets, uint64(index))
		}

		index += armInstructionSize
	}

	return returnOffsets, nil
}
