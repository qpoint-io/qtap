package binutils

import (
	"bytes"
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.opentelemetry.io/otel/trace"
)

var tracer = telemetry.Tracer()

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

	// Thread-safe lazy initialization of elf.File
	elfOnce sync.Once
	ef      *elf.File
	efErr   error

	isClosed bool
	closeMu  sync.Mutex
}

// NewElf creates a new Elf instance
// Returns ErrNotELF if the file is not an ELF
// Remember to call Close() when done
func NewElf(ctx context.Context, exe string, root string, isContainer bool) (*Elf, error) {
	ctx, span := tracer.WithoutCancel(ctx, "NewElf") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()

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
	e.closeMu.Lock()
	defer e.closeMu.Unlock()

	if e.isClosed {
		return nil
	}

	e.isClosed = true

	if e.file != nil {
		return e.file.Close()
	}

	return nil
}

func (e *Elf) getFilePath() string {
	if e.isContainer {
		return filepath.Join(e.root, e.exe)
	}
	return e.exe
}

func (e *Elf) isELF() (bool, error) {
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

func (p *Elf) Elf(ctx context.Context) (*elf.File, error) {
	p.closeMu.Lock()
	closed := p.isClosed
	p.closeMu.Unlock()

	if closed {
		return nil, ErrFileClosed
	}
	if p.file == nil {
		return nil, ErrNoFileLoaded
	}

	// Thread-safe lazy initialization
	p.elfOnce.Do(func() {
		_, span := tracer.Start(context.TODO(), "Elf.NewFile", trace.WithLinks(trace.LinkFromContext(ctx)))
		defer span.End()

		p.ef, p.efErr = elf.NewFile(p.file)
		if p.efErr != nil {
			p.efErr = fmt.Errorf("opening ELF: %w", p.efErr)
		}
	})

	return p.ef, p.efErr
}

func (p *Elf) SearchSymbols(ctx context.Context, targets []SymbolSearch, sectionTypes ...elf.SectionType) ([]elf.Symbol, error) {
	ctx, span := tracer.WithoutCancel(ctx, "Elf.SearchSymbols")
	defer span.End()
	if p.file == nil {
		return nil, ErrNoFileLoaded
	}

	f, err := p.Elf(ctx)
	if err != nil {
		return nil, err
	}

	var allMatches []elf.Symbol

	for _, sectionType := range sectionTypes {
		var matches []elf.Symbol
		var err error

		switch f.Class {
		case elf.ELFCLASS64:
			matches, err = p.getSymbols64(ctx, f, targets, sectionType)
		case elf.ELFCLASS32:
			matches, err = p.getSymbols32(ctx, f, targets, sectionType)
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

func (p *Elf) getSymbols32(ctx context.Context, f *elf.File, targets []SymbolSearch, typ elf.SectionType) ([]elf.Symbol, error) {
	ctx, span := tracer.WithoutCancel(ctx, "Elf.getSymbols32") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()
	matches := []elf.Symbol{}

	symtabSection := f.SectionByType(typ)
	if symtabSection == nil {
		return nil, ErrNoSymbols
	}

	link := symtabSection.Link
	if link <= 0 || link >= uint32(len(f.Sections)) {
		return nil, errors.New("section has invalid string table link")
	}

	// Get the string table section as ReaderAt (thread-safe)
	strSection := f.Sections[link]

	// Read symbol table data
	symData, err := symtabSection.Data()
	if err != nil {
		return nil, fmt.Errorf("failed to read symbol table: %w", err)
	}

	// Skip the first entry (16 bytes) in the symbol table
	symData = symData[elf.Sym32Size:]

	var sym elf.Sym32
	for len(symData) >= elf.Sym32Size {
		if len(matches) == len(targets) {
			break
		}

		// Parse symbol from data
		sym.Name = f.ByteOrder.Uint32(symData[0:4])
		sym.Value = f.ByteOrder.Uint32(symData[4:8])
		sym.Size = f.ByteOrder.Uint32(symData[8:12])
		sym.Info = symData[12]
		sym.Other = symData[13]
		sym.Shndx = f.ByteOrder.Uint16(symData[14:16])
		symData = symData[elf.Sym32Size:]

		// Read the symbol name from the string table (thread-safe)
		name, err := readStringAt(strSection, int64(sym.Name))
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

func (p *Elf) getSymbols64(ctx context.Context, f *elf.File, targets []SymbolSearch, typ elf.SectionType) ([]elf.Symbol, error) {
	ctx, span := tracer.WithoutCancel(ctx, "Elf.getSymbols64") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()
	matches := []elf.Symbol{}

	symtabSection := f.SectionByType(typ)
	if symtabSection == nil {
		return nil, ErrNoSymbols
	}

	link := symtabSection.Link
	if link <= 0 || link >= uint32(len(f.Sections)) {
		return nil, errors.New("section has invalid string table link")
	}

	// Get the string table section as ReaderAt (thread-safe)
	strSection := f.Sections[link]

	// Read symbol table data
	symData, err := symtabSection.Data()
	if err != nil {
		return nil, fmt.Errorf("failed to read symbol table: %w", err)
	}

	// Skip the first entry (24 bytes) in the symbol table
	symData = symData[elf.Sym64Size:]

	var sym elf.Sym64
	for len(symData) >= elf.Sym64Size {
		if len(matches) == len(targets) {
			break
		}

		// Parse symbol from data (Sym64 layout: name, info, other, shndx, value, size)
		sym.Name = f.ByteOrder.Uint32(symData[0:4])
		sym.Info = symData[4]
		sym.Other = symData[5]
		sym.Shndx = f.ByteOrder.Uint16(symData[6:8])
		sym.Value = f.ByteOrder.Uint64(symData[8:16])
		sym.Size = f.ByteOrder.Uint64(symData[16:24])
		symData = symData[elf.Sym64Size:]

		// Read the symbol name from the string table (thread-safe)
		name, err := readStringAt(strSection, int64(sym.Name))
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

// readStringAt reads a null-terminated string from the given ReaderAt starting at the given offset.
// This function is thread-safe as it uses ReadAt which doesn't modify reader state.
func readStringAt(r io.ReaderAt, offset int64) (string, error) {
	// Use a per-call buffer for thread safety
	var buf [bufferSize]byte

	n, err := r.ReadAt(buf[:], offset)
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

func (p *Elf) ContainsAnySymbols(ctx context.Context, targetSymbols []SymbolSearch, typ ...elf.SectionType) (bool, error) {
	ctx, span := tracer.WithoutCancel(ctx, "Elf.ContainsAnySymbols")
	defer span.End()

	f, err := p.Elf(ctx)
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
			m, err := p.containsAnySymbols(ctx, f, t, targetSymbols)
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
			m, err := p.containsAnySymbols(ctx, f, t, targetSymbols)
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

func (p *Elf) containsAnySymbols(ctx context.Context, f *elf.File, typ elf.SectionType, targetSymbols []SymbolSearch) (bool, error) {
	ctx, span := tracer.WithoutCancel(ctx, "Elf.containsAnySymbols") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()

	var recordSize int
	switch f.Class {
	case elf.ELFCLASS64:
		recordSize = elf.Sym64Size
	case elf.ELFCLASS32:
		recordSize = elf.Sym32Size
	default:
		return false, fmt.Errorf("unsupported ELF class: %d", f.Class)
	}

	symtabSection := f.SectionByType(typ)
	if symtabSection == nil {
		return false, ErrNoSymbols
	}

	// Read symbol table data (thread-safe: allocates fresh buffer)
	symData, err := symtabSection.Data()
	if err != nil {
		return false, fmt.Errorf("failed to read symbol table: %w", err)
	}

	// Skip the first entry in the symbol table
	symData = symData[recordSize:]

	// Get string table section as ReaderAt (thread-safe)
	strSection := f.Sections[symtabSection.Link]
	if strSection == nil {
		return false, errors.New("string table section not found")
	}

	// Iterate through the symbol table
	for len(symData) >= recordSize {
		// Extract the name offset from the symbol record (always at offset 0, 4 bytes)
		nameOffsetValue := int64(f.ByteOrder.Uint32(symData[0:4]))
		symData = symData[recordSize:]

		// Check if the symbol name matches any of the target symbols
		for _, target := range targetSymbols {
			if matchSymbolAt(strSection, nameOffsetValue, target.Bytes(), target.MatchStrategy) {
				return true, nil
			}
		}
	}

	return false, nil
}

// matchSymbolAt checks if a symbol at the given offset in the string table matches the target.
// This function is thread-safe as it uses ReadAt which doesn't modify reader state.
//
// Parameters:
// - strSection: The string table section (implements io.ReaderAt)
// - nameOffset: The offset in the string table where the symbol name starts
// - target: The byte slice containing the symbol name to search for
// - strategy: The matching strategy to use
//
// Returns:
// - bool: true if the symbol matches, false otherwise
func matchSymbolAt(strSection io.ReaderAt, nameOffset int64, target []byte, strategy MatchStrategy) bool {
	// Read enough bytes to check the target (plus null terminator for exact match)
	bufSize := len(target) + 1
	if bufSize < 64 {
		bufSize = 64 // Minimum buffer for prefix/suffix matching
	}
	buf := make([]byte, bufSize)

	n, err := strSection.ReadAt(buf, nameOffset)
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	if n == 0 {
		return false
	}

	// Find the null terminator to get the actual symbol name
	nullIdx := bytes.IndexByte(buf[:n], 0)
	if nullIdx == -1 {
		nullIdx = n // Symbol might be truncated, use what we have
	}
	symName := string(buf[:nullIdx])

	return match(symName, string(target), strategy)
}

// CalculateUprobeAddresses calculates the loaded address of a symbol (needed for uprobes)
func (p *Elf) CalculateUprobeAddresses(ctx context.Context, symbols []elf.Symbol) []elf.Symbol {
	ctx, span := tracer.WithoutCancel(ctx, "Elf.CalculateUprobeAddresses")
	defer span.End()

	// create a copy of the input symbols to modify .Value
	results := make([]elf.Symbol, len(symbols))
	copy(results, symbols)

	file, err := p.Elf(ctx)
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

func (p *Elf) GetSections(ctx context.Context) []*elf.Section {
	file, err := p.Elf(ctx)
	if err != nil {
		return nil
	}
	return file.Sections
}

func (p *Elf) Ldd(ctx context.Context) ([]string, error) {
	file, err := p.Elf(ctx)
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
