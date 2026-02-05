// Package rustls implements TLS interception for rustls-based applications.
package rustls

import (
	"debug/elf"
	"fmt"

	"github.com/go-delve/delve/pkg/dwarf/frame"
)

// EHFrameParser parses .eh_frame sections to extract function boundaries.
// This is the key innovation - .eh_frame survives stripping and contains
// function start/end addresses needed for probe attachment.
//
// Uses the battle-tested parser from the delve debugger.
type EHFrameParser struct {
	file *elf.File
}

// NewEHFrameParser creates a parser for the given ELF file.
func NewEHFrameParser(f *elf.File) *EHFrameParser {
	return &EHFrameParser{file: f}
}

// FunctionBound represents a function's address range from .eh_frame.
type FunctionBound struct {
	// Start address (PC begin)
	Start uint64
	// End address (PC begin + PC range)
	End uint64
}

// Size returns the function size in bytes.
func (f FunctionBound) Size() uint64 {
	return f.End - f.Start
}

// Parse extracts function boundaries from .eh_frame using delve's parser.
func (p *EHFrameParser) Parse() ([]FunctionBound, error) {
	// Get .eh_frame section
	ehFrame := p.file.Section(".eh_frame")
	if ehFrame == nil {
		return nil, fmt.Errorf(".eh_frame section not found")
	}

	data, err := ehFrame.Data()
	if err != nil {
		return nil, fmt.Errorf("failed to read .eh_frame: %w", err)
	}

	// Determine pointer size
	ptrSize := 4
	if p.file.Class == elf.ELFCLASS64 {
		ptrSize = 8
	}

	// Use delve's parser
	// Parameters: data, byte order, static base, pointer size, eh_frame address
	fdes, err := frame.Parse(data, p.file.ByteOrder, 0, ptrSize, ehFrame.Addr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse .eh_frame: %w", err)
	}

	// Convert to our FunctionBound type
	var functions []FunctionBound
	for _, fde := range fdes {
		if fde.Begin() > 0 && fde.End() > fde.Begin() {
			functions = append(functions, FunctionBound{
				Start: fde.Begin(),
				End:   fde.End(),
			})
		}
	}

	return functions, nil
}
