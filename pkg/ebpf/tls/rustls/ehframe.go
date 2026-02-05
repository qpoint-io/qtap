// Package rustls implements TLS interception for rustls-based applications.
package rustls

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"io"

	// For production, use delve's parser:
	// "github.com/go-delve/delve/pkg/dwarf/frame"
	//
	// Their parser is battle-tested and handles all the edge cases.
	// See: https://github.com/go-delve/delve/tree/master/pkg/dwarf/frame
)

// EHFrameParser parses .eh_frame sections to extract function boundaries.
// This is the key innovation - .eh_frame survives stripping and contains
// function start/end addresses needed for probe attachment.
type EHFrameParser struct {
	// The ELF file being parsed
	file *elf.File

	// Pointer size (4 for 32-bit, 8 for 64-bit)
	ptrSize int

	// Whether this is a 64-bit DWARF format
	is64Bit bool
}

// NewEHFrameParser creates a parser for the given ELF file.
func NewEHFrameParser(f *elf.File) *EHFrameParser {
	ptrSize := 4
	if f.Class == elf.ELFCLASS64 {
		ptrSize = 8
	}
	return &EHFrameParser{
		file:    f,
		ptrSize: ptrSize,
	}
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

// Parse extracts function boundaries from .eh_frame.
//
// The .eh_frame section contains:
// - CIE (Common Information Entry) - shared configuration
// - FDE (Frame Description Entry) - per-function info with PC range
//
// Reference: https://refspecs.linuxfoundation.org/LSB_5.0.0/LSB-Core-generic/LSB-Core-generic/ehframechpt.html
//
// TODO: This implementation is a work in progress. The .eh_frame encoding is
// complex (supports multiple address encodings, augmentation data, etc.).
// For MVP, consider using .eh_frame_hdr search table or external tools.
func (p *EHFrameParser) Parse() ([]FunctionBound, error) {
	section := p.file.Section(".eh_frame")
	if section == nil {
		return nil, fmt.Errorf(".eh_frame section not found")
	}

	data, err := section.Data()
	if err != nil {
		return nil, fmt.Errorf("failed to read .eh_frame: %w", err)
	}

	// Also get .eh_frame_hdr for base address if available
	var textBase uint64
	for _, prog := range p.file.Progs {
		if prog.Type == elf.PT_LOAD && prog.Flags&elf.PF_X != 0 {
			textBase = prog.Vaddr
			break
		}
	}

	var functions []FunctionBound
	offset := 0

	for offset < len(data) {
		if offset+4 > len(data) {
			break
		}

		// Read length (4 bytes, or 12 if extended)
		length := binary.LittleEndian.Uint32(data[offset:])
		if length == 0 {
			// Null terminator
			break
		}

		headerSize := 4
		if length == 0xffffffff {
			// Extended length (64-bit DWARF format)
			if offset+12 > len(data) {
				break
			}
			length = uint32(binary.LittleEndian.Uint64(data[offset+4:]))
			headerSize = 12
			p.is64Bit = true
		}

		entryStart := offset + headerSize
		entryEnd := entryStart + int(length)
		if entryEnd > len(data) {
			break
		}

		// Read CIE pointer (offset from this field to CIE, or 0 for CIE itself)
		var ciePtr uint32
		if p.is64Bit {
			ciePtr = uint32(binary.LittleEndian.Uint64(data[entryStart:]))
		} else {
			ciePtr = binary.LittleEndian.Uint32(data[entryStart:])
		}

		if ciePtr == 0 {
			// This is a CIE, skip it
			offset = entryEnd
			continue
		}

		// This is an FDE - extract PC range
		fde, err := p.parseFDE(data[entryStart:entryEnd], section.Addr+uint64(entryStart), textBase)
		if err != nil {
			// Skip malformed FDE
			offset = entryEnd
			continue
		}

		if fde.Start > 0 && fde.End > fde.Start {
			functions = append(functions, fde)
		}

		offset = entryEnd
	}

	return functions, nil
}

// parseFDE parses a Frame Description Entry.
func (p *EHFrameParser) parseFDE(data []byte, fdeAddr, textBase uint64) (FunctionBound, error) {
	if len(data) < 8 {
		return FunctionBound{}, io.ErrUnexpectedEOF
	}

	ptrSize := p.ptrSize
	if p.is64Bit {
		ptrSize = 8
	}

	// Skip CIE pointer (already read)
	offset := ptrSize

	if offset+ptrSize*2 > len(data) {
		return FunctionBound{}, io.ErrUnexpectedEOF
	}

	// PC begin (encoded relative to FDE location)
	var pcBegin uint64
	if ptrSize == 8 {
		// Signed 64-bit offset
		pcBegin = uint64(int64(fdeAddr) + int64(int64(binary.LittleEndian.Uint64(data[offset:]))))
	} else {
		// Signed 32-bit offset
		pcBegin = uint64(int64(fdeAddr) + int64(int32(binary.LittleEndian.Uint32(data[offset:]))))
	}
	offset += ptrSize

	// PC range (size of function)
	var pcRange uint64
	if ptrSize == 8 {
		pcRange = binary.LittleEndian.Uint64(data[offset:])
	} else {
		pcRange = uint64(binary.LittleEndian.Uint32(data[offset:]))
	}

	return FunctionBound{
		Start: pcBegin,
		End:   pcBegin + pcRange,
	}, nil
}
