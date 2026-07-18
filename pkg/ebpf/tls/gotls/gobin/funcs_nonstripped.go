// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package gobin

import (
	"debug/elf"
	"errors"
	"fmt"
)

func FindFunctionsUnStripped(elfF *elf.File, symbols []elf.Symbol) ([]*Func, error) {
	var result []*Func
	for _, f := range symbols {
		offset, err := getFuncOffsetUnstripped(elfF, f)
		if err != nil {
			return nil, err
		}

		returns, err := findFuncReturnsUnstripped(elfF, f, offset)
		if err != nil {
			return nil, err
		}

		function := &Func{
			Name:          f.Name,
			Offset:        offset,
			ReturnOffsets: returns,
		}

		result = append(result, function)
	}

	return result, nil
}

func getFuncOffsetUnstripped(f *elf.File, symbol elf.Symbol) (uint64, error) {
	var sections []*elf.Section

	for i := range f.Sections {
		if f.Sections[i].Flags == elf.SHF_ALLOC+elf.SHF_EXECINSTR {
			sections = append(sections, f.Sections[i])
		}
	}

	if len(sections) == 0 {
		return 0, fmt.Errorf("function %v not found in file", symbol)
	}

	var execSection *elf.Section
	for m := range sections {
		sectionStart := sections[m].Addr
		sectionEnd := sectionStart + sections[m].Size
		if symbol.Value >= sectionStart && symbol.Value < sectionEnd {
			execSection = sections[m]
			break
		}
	}

	if execSection == nil {
		return 0, errors.New("could not find symbol in executable sections of binary")
	}

	return uint64(symbol.Value - execSection.Addr + execSection.Offset), nil
}

func findFuncReturnsUnstripped(elfFile *elf.File, sym elf.Symbol, functionOffset uint64) ([]uint64, error) {
	textSection := elfFile.Section(".text")
	if textSection == nil {
		return nil, errors.New("could not find .text section in binary")
	}

	lowPC := sym.Value
	highPC := lowPC + sym.Size
	offset := lowPC - textSection.Addr
	buf := make([]byte, int(highPC-lowPC))

	readBytes, err := textSection.ReadAt(buf, int64(offset))
	if err != nil {
		return nil, fmt.Errorf("could not read text section: %w", err)
	}
	data := buf[:readBytes]
	instructionIndices, err := findRetInstructions(data)
	if err != nil {
		return nil, fmt.Errorf("error while scanning instructions: %w", err)
	}

	// Add the function lowPC to each index to obtain the actual locations
	newLocations := make([]uint64, len(instructionIndices))
	for i, instructionIndex := range instructionIndices {
		newLocations[i] = instructionIndex + functionOffset
	}

	return newLocations, nil
}
