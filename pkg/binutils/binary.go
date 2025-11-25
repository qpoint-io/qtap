package binutils

import (
	"bytes"
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/kamaln7/resolvable"
)

// Binary represents a pre-parsed ELF binary with all data needed for TLS probe scanning.
// All fields are immutable after construction, making it safe for concurrent access.
type Binary struct {
	// ef is the parsed ELF file
	ef *elf.File
	// mu protects the ELF data
	mu sync.Mutex

	// Sections from the ELF file
	Sections []*elf.Section

	// Symbol tables
	Symtab resolvable.V[[]elf.Symbol]
	Dynsym resolvable.V[[]elf.Symbol]

	// LinkedLibs contains the names of dynamically linked libraries
	LinkedLibs resolvable.V[[]string]
}

// ParseBinary opens and parses an ELF binary, pre-loading all data needed for TLS scanning.
// The returned ParsedBinary keeps the file descriptor open for uprobe attachment.
// Caller must call Close() when done.
func ParseBinary(ctx context.Context, fd *os.File) (*Binary, error) {
	// Verify it's an ELF file
	var ident [4]byte
	if _, err := fd.ReadAt(ident[:], 0); err != nil {
		fd.Close()
		return nil, fmt.Errorf("reading ELF header: %w", err)
	}
	if ident[0] != '\x7f' || ident[1] != 'E' || ident[2] != 'L' || ident[3] != 'F' {
		fd.Close()
		return nil, ErrNotELF
	}

	ef, err := elf.NewFile(fd)
	if err != nil {
		fd.Close()
		return nil, fmt.Errorf("parsing ELF: %w", err)
	}

	pb := &Binary{
		ef:       ef,
		Sections: ef.Sections,
	}

	pb.Symtab = resolvable.New(
		func(ctx context.Context) ([]elf.Symbol, error) {
			// Symbols() is thread-safe
			symbols, err := ef.Symbols()
			if err != nil {
				if errors.Is(err, elf.ErrNoSymbols) {
					return nil, nil
				}
				return nil, fmt.Errorf("getting static symbols: %w", err)
			}
			return symbols, nil
		},
		resolvable.WithRetry(),
	).WithContext(context.TODO())

	pb.Dynsym = resolvable.New(
		func(ctx context.Context) ([]elf.Symbol, error) {
			// DynamicSymbols() is not thread-safe
			pb.mu.Lock()
			defer pb.mu.Unlock()

			symbols, err := ef.DynamicSymbols()
			if err != nil {
				if errors.Is(err, elf.ErrNoSymbols) {
					return nil, nil
				}
				return nil, fmt.Errorf("getting dynamic symbols: %w", err)
			}
			return symbols, nil
		},
		resolvable.WithRetry(),
	).WithContext(context.TODO())

	// Pre-load linked libraries
	pb.LinkedLibs = resolvable.New(
		func(ctx context.Context) ([]string, error) {
			pb.mu.Lock()
			defer pb.mu.Unlock()

			return ef.ImportedLibraries()
		},
		resolvable.WithRetry(),
	).WithContext(context.TODO())

	return pb, nil
}

// ElfFile returns the underlying elf.File for advanced operations.
// Note: Some elf.File methods are not thread-safe (e.g., DynamicSymbols).
// Use the pre-parsed fields (Symtab, Dynsym) for thread-safe access.
func (pb *Binary) ElfFile() *elf.File {
	return pb.ef
}

// HasSymbol checks if a symbol with the given name exists in either symbol table.
func (pb *Binary) HasSymbol(name string) (bool, error) {
	symtab, err := pb.Symtab()
	if err != nil {
		return false, fmt.Errorf("getting static symbols: %w", err)
	}
	for _, s := range symtab {
		if s.Name == name {
			return true, nil
		}
	}

	dynsym, err := pb.Dynsym()
	if err != nil {
		return false, fmt.Errorf("getting dynamic symbols: %w", err)
	}
	for _, s := range dynsym {
		if s.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// FindSymbols returns all symbols matching the given predicate from both symbol tables.
func (pb *Binary) FindSymbols(predicate func(elf.Symbol) bool) ([]elf.Symbol, error) {
	var result []elf.Symbol

	symtab, err := pb.Symtab()
	if err != nil {
		return nil, fmt.Errorf("getting static symbols: %w", err)
	}
	for _, s := range symtab {
		if predicate(s) {
			result = append(result, s)
		}
	}

	dynsym, err := pb.Dynsym()
	if err != nil {
		return nil, fmt.Errorf("getting dynamic symbols: %w", err)
	}
	for _, s := range dynsym {
		if predicate(s) {
			result = append(result, s)
		}
	}

	return result, nil
}

// FindSymbolsByName returns all symbols matching any of the given search criteria.
func (pb *Binary) FindSymbolsByName(targets []SymbolSearch) ([]elf.Symbol, error) {
	return pb.FindSymbols(func(s elf.Symbol) bool {
		return MatchSymbol(s.Name, targets)
	})
}

// SectionData returns the data for a section by name.
// This method is thread-safe as Section.Data() allocates a fresh buffer.
func (pb *Binary) SectionData(name string) ([]byte, error) {
	sec := pb.ef.Section(name)
	if sec == nil {
		return nil, fmt.Errorf("section %s not found", name)
	}
	return sec.Data()
}

// SearchString searches for a string with the given prefix in common string sections.
// Returns the full string if found, or an error if not found.
func (pb *Binary) SearchString(prefix string, strategy MatchStrategy) (string, error) {
	// Search in common string sections
	sections := []string{".rodata", ".data", ".dynstr", ".strtab"}

	for _, secName := range sections {
		data, err := pb.SectionData(secName)
		if err != nil {
			continue
		}

		result := searchStringInData(data, prefix, strategy)
		if result != "" {
			return result, nil
		}
	}

	return "", errors.New("string not found")
}

// searchStringInData searches for a string in the given data.
func searchStringInData(data []byte, target string, strategy MatchStrategy) string {
	targetBytes := []byte(target)

	switch strategy {
	case MatchStrategyPrefix:
		idx := bytes.Index(data, targetBytes)
		if idx == -1 {
			return ""
		}
		// Find the null terminator
		end := bytes.IndexByte(data[idx:], 0)
		if end == -1 {
			return ""
		}
		return string(data[idx : idx+end])

	case MatchStrategyExact:
		idx := bytes.Index(data, targetBytes)
		if idx == -1 {
			return ""
		}
		// Verify null terminator follows
		endIdx := idx + len(targetBytes)
		if endIdx < len(data) && data[endIdx] == 0 {
			return target
		}
		return ""

	default:
		return ""
	}
}

// CalculateUprobeAddresses calculates the file offsets for uprobe attachment.
// This converts virtual addresses to file offsets using program headers.
func (pb *Binary) CalculateUprobeAddresses(symbols []elf.Symbol) []elf.Symbol {
	results := make([]elf.Symbol, len(symbols))
	copy(results, symbols)

	processed := make([]bool, len(symbols))

	for _, prog := range pb.ef.Progs {
		if prog.Type != elf.PT_LOAD || (prog.Flags&elf.PF_X) == 0 {
			continue
		}

		for i, sym := range symbols {
			if processed[i] {
				continue
			}

			if prog.Vaddr <= sym.Value && sym.Value < (prog.Vaddr+prog.Memsz) {
				results[i].Value = sym.Value - prog.Vaddr + prog.Off
				processed[i] = true
			}
		}
	}

	return results
}

// HasLinkedLibrary checks if the binary links against a library matching the pattern.
func (pb *Binary) HasLinkedLibrary(pattern string, strategy MatchStrategy) (bool, error) {
	libs, err := pb.LinkedLibs()
	if err != nil {
		return false, fmt.Errorf("getting linked libraries: %w", err)
	}
	for _, lib := range libs {
		if match(lib, pattern, strategy) {
			return true, nil
		}
	}
	return false, nil
}
