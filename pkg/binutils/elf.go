package binutils

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	ErrNotELF       = errors.New("file is not an ELF")
	ErrNoFileLoaded = errors.New("no file loaded")
	ErrNoSymbols    = errors.New("no symbol section")
	ErrFileClosed   = errors.New("file is closed")
)

const (
	chunkSize  = 1024
	bufferSize = 4096
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		return new([bufferSize]byte)
	},
}

// enum for symbol match strategy
type MatchStrategy int

const (
	MatchStrategyExact MatchStrategy = iota
	MatchStrategyPrefix
	MatchStrategySuffix
	MatchStrategyContains
)

type SymbolSearch struct {
	Name string
	MatchStrategy
}

func (s *SymbolSearch) Bytes() []byte {
	return []byte(s.Name)
}

type Elf struct {
	isContainer bool

	exe  string
	root string
	file *os.File
	ef   *elf.File

	isClosed bool
}

// NewElf creates a new Elf instance
// Returns ErrNotELF if the file is not an ELF
// Remember to call Close() when done
func NewElf(exe string, root string, isContainer bool) (*Elf, error) {
	e := &Elf{
		exe:         exe,
		root:        root,
		isContainer: isContainer,
	}

	filePath := e.getFilePath()

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening os file: %w", err)
	}

	e.file = file

	// Check if it's actually an ELF file
	isElf, err := e.isELF()
	if err != nil {
		file.Close()
		return nil, ErrNotELF
	}
	if !isElf {
		file.Close()
		return nil, fmt.Errorf("file is not an ELF: %s", filePath)
	}

	return e, nil
}

func (e *Elf) Close() error {
	if e.isClosed {
		return nil
	}

	if e.file != nil {
		return e.file.Close()
	}

	e.isClosed = true
	return nil
}

func (e *Elf) getFilePath() string {
	if e.isContainer {
		return filepath.Join(e.root, e.exe)
	}
	return e.exe
}

func (e Elf) isELF() (bool, error) {
	if e.file == nil {
		return false, ErrNoFileLoaded
	}

	var ident [4]uint8
	if _, err := e.file.ReadAt(ident[0:], 0); err != nil {
		return false, err
	}
	if ident[0] != '\x7f' || ident[1] != 'E' || ident[2] != 'L' || ident[3] != 'F' {
		return false, ErrNotELF
	}

	return true, nil
}

func (p *Elf) Elf() (*elf.File, error) {
	if p.isClosed {
		return nil, ErrFileClosed
	}
	if p.file == nil {
		return nil, ErrNoFileLoaded
	}
	if p.ef == nil {
		var err error
		p.ef, err = elf.NewFile(p.file)
		if err != nil {
			return nil, fmt.Errorf("opening ELF: %w", err)
		}
	}

	return p.ef, nil
}

func (p *Elf) SearchSymbols(targets []SymbolSearch, sectionTypes ...elf.SectionType) ([]elf.Symbol, error) {
	if p.file == nil {
		return nil, ErrNoFileLoaded
	}

	f, err := p.Elf()
	if err != nil {
		return nil, err
	}

	var allMatches []elf.Symbol

	for _, sectionType := range sectionTypes {
		var matches []elf.Symbol
		var err error

		switch f.Class {
		case elf.ELFCLASS64:
			matches, err = p.getSymbols64(f, targets, sectionType)
		case elf.ELFCLASS32:
			matches, err = p.getSymbols32(f, targets, sectionType)
		default:
			return nil, errors.New("unsupported ELF class")
		}

		if err != nil {
			if errors.Is(err, ErrNoSymbols) {
				continue // Skip to the next section type if no symbols found
			}
			return nil, fmt.Errorf("searching symbols in section type %v: %w", sectionType, err)
		}

		allMatches = append(allMatches, matches...)

		// If we've found all the targets, we can stop searching
		if len(allMatches) == len(targets) {
			break
		}
	}

	return allMatches, nil
}

func (p *Elf) getSymbols32(f *elf.File, targets []SymbolSearch, typ elf.SectionType) ([]elf.Symbol, error) {
	matches := []elf.Symbol{}

	symtabSection := f.SectionByType(typ)
	if symtabSection == nil {
		return nil, ErrNoSymbols
	}

	// Open the symbol table section
	tabReader := symtabSection.Open()

	link := symtabSection.Link
	if link <= 0 || link >= uint32(len(f.Sections)) {
		return nil, errors.New("section has invalid string table link")
	}

	// Open the string table section
	strReader := f.Sections[link].Open()

	// Skip the first entry (16 bytes) in the symbol table
	if _, err := tabReader.Seek(elf.Sym32Size, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek in symbol table: %w", err)
	}

	var sym elf.Sym32
	for {
		if len(matches) == len(targets) {
			break
		}

		err := binary.Read(tabReader, f.ByteOrder, &sym)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read symbol: %w", err)
		}

		// Read the symbol name from the string table
		name, err := readString(strReader, int64(sym.Name))
		if err != nil {
			return nil, fmt.Errorf("failed to read string: %w", err)
		}

		// Check if the symbol name matches any of the requested names
		if MatchSymbol(name, targets) {
			matches = append(matches, elf.Symbol{
				Name:    name,
				Info:    sym.Info,
				Other:   sym.Other,
				Section: elf.SectionIndex(sym.Shndx),
				Value:   uint64(sym.Value),
				Size:    uint64(sym.Size),
			})
		}
	}

	return matches, nil
}

func (p *Elf) getSymbols64(f *elf.File, targets []SymbolSearch, typ elf.SectionType) ([]elf.Symbol, error) {
	matches := []elf.Symbol{}

	symtabSection := f.SectionByType(typ)
	if symtabSection == nil {
		return nil, ErrNoSymbols
	}

	// Open the symbol table section
	tabReader := symtabSection.Open()

	link := symtabSection.Link
	if link <= 0 || link >= uint32(len(f.Sections)) {
		return nil, errors.New("section has invalid string table link")
	}

	// Open the string table section
	strReader := f.Sections[link].Open()

	// Skip the first entry (24 bytes) in the symbol table
	if _, err := tabReader.Seek(elf.Sym64Size, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek in symbol table: %w", err)
	}

	var sym elf.Sym64
	for {
		if len(matches) == len(targets) {
			break
		}

		err := binary.Read(tabReader, f.ByteOrder, &sym)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read symbol: %w", err)
		}

		// Read the symbol name from the string table
		name, err := readString(strReader, int64(sym.Name))
		if err != nil {
			return nil, fmt.Errorf("failed to read string: %w", err)
		}

		// Check if the symbol name matches any of the requested names
		if MatchSymbol(name, targets) {
			matches = append(matches, elf.Symbol{
				Name:    name,
				Info:    sym.Info,
				Other:   sym.Other,
				Section: elf.SectionIndex(sym.Shndx),
				Value:   sym.Value,
				Size:    sym.Size,
			})
		}
	}

	return matches, nil
}

// readString reads a null-terminated string from the given ReadSeeker starting at the given offset
func readString(r io.ReadSeeker, offset int64) (string, error) {
	_, err := r.Seek(offset, io.SeekStart)
	if err != nil {
		return "", fmt.Errorf("failed to seek to string offset: %w", err)
	}

	buf := bufferPool.Get().(*[bufferSize]byte)
	defer bufferPool.Put(buf)

	n, err := r.Read(buf[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("failed to read string data: %w", err)
	}

	// Find the null terminator
	end := bytes.IndexByte(buf[:n], 0)
	if end == -1 {
		return "", errors.New("string not null-terminated within buffer")
	}

	return string(buf[:end]), nil
}

func (p *Elf) ContainsAnySymbols(targetSymbols []SymbolSearch, typ ...elf.SectionType) (bool, error) {
	f, err := p.Elf()
	if err != nil {
		return false, err
	}

	matched := false

	for _, t := range typ {
		switch t {
		case elf.SHT_SYMTAB:
			if matched {
				return matched, nil
			}
			m, err := p.containsAnySymbols(f, t, targetSymbols)
			if err != nil {
				if errors.Is(err, ErrNoSymbols) {
					continue
				}
				return false, fmt.Errorf("failed to check for statically linked symbols: %w", err)
			}
			matched = matched || m
		case elf.SHT_DYNSYM:
			if matched {
				return matched, nil
			}
			m, err := p.containsAnySymbols(f, t, targetSymbols)
			if err != nil {
				if errors.Is(err, ErrNoSymbols) {
					continue
				}
				return false, fmt.Errorf("failed to check for dynamic symbols: %w", err)
			}
			matched = matched || m
		default:
			return false, fmt.Errorf("unsupported section type: %d", t)
		}
	}

	return matched, nil
}

func (p *Elf) containsAnySymbols(f *elf.File, typ elf.SectionType, targetSymbols []SymbolSearch) (bool, error) {
	var recordSize int64
	var nameOffset, nameSize int
	switch f.Class {
	case elf.ELFCLASS64:
		recordSize = elf.Sym64Size
		nameOffset = 0
		nameSize = 4
	case elf.ELFCLASS32:
		recordSize = elf.Sym32Size
		nameOffset = 0
		nameSize = 4
	default:
		return false, fmt.Errorf("unsupported ELF class: %d", f.Class)
	}

	symtabSection := f.SectionByType(typ)
	if symtabSection == nil {
		return false, ErrNoSymbols
	}

	// Open the symbol table section
	tabReader := symtabSection.Open()

	// Skip the first entry in the symbol table
	if _, err := tabReader.Seek(recordSize, io.SeekStart); err != nil {
		return false, fmt.Errorf("failed to seek in symbol table: %w", err)
	}

	// Buffer for reading symbol records
	// We use elf.Sym64Size to create a fixed-size array that's large enough to hold
	// both 32-bit and 64-bit symbol records. This ensures we have enough space
	// regardless of the ELF class. Later, we use 'recordSize' to determine how many
	// bytes to actually read into this buffer, which will be either Sym32Size or
	// Sym64Size depending on the ELF class.
	var recordBuffer [elf.Sym64Size]byte

	// Initialize string table reader and buffer
	strSection := f.Sections[symtabSection.Link]
	if strSection == nil {
		return false, errors.New("string table section not found")
	}
	strReader := strSection.Open()

	// Initialize buffer for reading the string table
	strBuffer := bufferPool.Get().(*[bufferSize]byte)
	defer bufferPool.Put(strBuffer)
	var strBufferOffset, strBufferLen int64

	// Iterate through the symbol table
	for {
		_, err := tabReader.Read(recordBuffer[:recordSize])
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return false, fmt.Errorf("failed to read symbol record: %w", err)
		}

		// Extract the name offset from the symbol record
		nameOffsetValue := int64(f.ByteOrder.Uint32(recordBuffer[nameOffset : nameOffset+nameSize]))

		// Check if the symbol name matches any of the target symbols
		for _, target := range targetSymbols {
			if searchSymbol(strReader, nameOffsetValue, target.Bytes(), strBuffer[:], &strBufferOffset, &strBufferLen, target.MatchStrategy == MatchStrategyPrefix) {
				return true, nil
			}
		}
	}

	return false, nil
}

// searchSymbol searches for a target symbol in the string table of an ELF file.
// It uses buffered reading to efficiently search through the string table.
//
// Parameters:
// - strReader: A ReadSeeker for the string table section
// - nameOffset: The offset in the string table where the symbol name starts
// - target: The byte slice containing the symbol name to search for
// - strBuffer: A pointer to a byte slice used as a buffer for reading the string table
// - strBufferOffset: A pointer to the current offset of the buffer in the string table
// - strBufferLen: A pointer to the current length of valid data in the buffer
// - prefixMatch: If true, the function will return true as soon as it finds a match for the entire target, without checking for a null terminator
//
// Returns:
// - bool: true if the symbol is found, false otherwise
func searchSymbol(strReader io.ReadSeeker, nameOffset int64, target []byte, strBuffer []byte, strBufferOffset, strBufferLen *int64, prefixMatch bool) bool {
	// Check if the name offset is within the current buffer
	// If not, we need to read a new chunk from the string table
	if nameOffset < *strBufferOffset || nameOffset >= *strBufferOffset+*strBufferLen {
		// Seek to the correct position in the string table
		_, err := strReader.Seek(nameOffset, io.SeekStart)
		if err != nil {
			return false
		}
		// Read a new chunk into the buffer
		n, err := strReader.Read(strBuffer)
		if err != nil && !errors.Is(err, io.EOF) {
			return false
		}
		// Update the buffer offset and length
		*strBufferOffset = nameOffset
		*strBufferLen = int64(n)
	}

	// Calculate the index within the buffer where the symbol name starts
	bufferIndex := int(nameOffset - *strBufferOffset)

	// Compare the symbol name with the target
	for i := range target {
		// If we've reached the end of the buffer, read more data
		if bufferIndex+i >= int(*strBufferLen) {
			n, err := strReader.Read(strBuffer)
			if err != nil && !errors.Is(err, io.EOF) {
				return false
			}
			// Update buffer offset and length, reset index
			*strBufferOffset += *strBufferLen
			*strBufferLen = int64(n)
			bufferIndex = 0
		}
		// Compare each byte of the symbol name
		if strBuffer[bufferIndex+i] != target[i] {
			return false
		}
	}

	// For prefix match, we don't need to check for null terminator
	if prefixMatch {
		return true
	}

	// For exact match, check for null terminator after the symbol name
	// This ensures we've found a complete symbol name, not just a prefix
	if bufferIndex+len(target) >= int(*strBufferLen) {
		// If we're at the end of the buffer, read more data
		n, err := strReader.Read(strBuffer)
		if err != nil && !errors.Is(err, io.EOF) {
			return false
		}
		// Update buffer offset and length, reset index
		*strBufferOffset += *strBufferLen
		*strBufferLen = int64(n)
		bufferIndex = 0
	}
	// The symbol is found if it's followed by a null terminator
	return strBuffer[bufferIndex+len(target)] == 0
}

// CalculateUprobeAddresses calculates the loaded address of a symbol (needed for uprobes)
func (p *Elf) CalculateUprobeAddresses(symbols []elf.Symbol) []elf.Symbol {
	// create a copy of the input symbols to modify .Value
	results := make([]elf.Symbol, len(symbols))
	copy(results, symbols)

	file, err := p.Elf()
	if err != nil {
		return results
	}

	// track if each symbol has been processed
	processed := make([]bool, len(symbols))

	// iterate through the program headers to find the symbol
	for _, prog := range file.Progs {
		if prog.Type != elf.PT_LOAD || (prog.Flags&elf.PF_X) == 0 {
			continue
		}

		for i, sym := range symbols {
			if processed[i] {
				continue
			}

			if prog.Vaddr <= sym.Value && sym.Value < (prog.Vaddr+prog.Memsz) {
				// calculate the file offset for the symbol
				// Formula: symbol file offset = symbol VA - segment VA + segment offset
				results[i].Value = sym.Value - prog.Vaddr + prog.Off
				processed[i] = true
			}
		}
	}

	return results
}

func (p *Elf) GetSections() []*elf.Section {
	file, err := p.Elf()
	if err != nil {
		return nil
	}
	return file.Sections
}

func (p *Elf) Ldd() ([]string, error) {
	file, err := p.Elf()
	if err != nil {
		return nil, err
	}

	// get the linked libraries
	libs, err := file.ImportedLibraries()
	if err != nil {
		return nil, err
	}

	return libs, nil
}

func MatchSymbol(symName string, targetSymbols []SymbolSearch) bool {
	for _, target := range targetSymbols {
		if match(symName, target.Name, target.MatchStrategy) {
			return true
		}
	}
	return false
}

func match(symName, targetName string, strategy MatchStrategy) bool {
	switch strategy {
	case MatchStrategyExact:
		return symName == targetName
	case MatchStrategyPrefix:
		return strings.HasPrefix(symName, targetName)
	case MatchStrategySuffix:
		return strings.HasSuffix(symName, targetName)
	case MatchStrategyContains:
		return strings.Contains(symName, targetName)
	default:
		return false
	}
}

// debugSearchSymbolsInELF is a debugging function that searches for symbols in an ELF file.
// This function should not be used in production as it may impact performance and generate excessive output.
// func debugSearchSymbolsInELF(f *elf.File, targetSymbols [][]byte) (bool, error) {
// 	for _, section := range f.Sections {
// 		data, err := section.Data()
// 		if err != nil {
// 			fmt.Printf("Error reading section %s: %v\n", section.Name, err)
// 			continue
// 		}

// 		fmt.Printf("Searching in section: %s\n", section.Name)

// 		for _, symbol := range targetSymbols {
// 			index := bytes.Index(data, symbol)
// 			if index != -1 {
// 				fmt.Printf("Found symbol in section %s at offset %d\n", section.Name, index)
// 				return true, nil
// 			}

// 			// Search for partial matches
// 			for i := 0; i < len(symbol); i++ {
// 				partialSymbol := symbol[:len(symbol)-i]
// 				index = bytes.Index(data, partialSymbol)
// 				if index != -1 {
// 					fmt.Printf("Partial match in section %s at offset %d: %s\n",
// 						section.Name, index, string(partialSymbol))
// 				}
// 			}
// 		}
// 	}

// 	return false, nil
// }
