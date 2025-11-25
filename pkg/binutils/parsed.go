package binutils

import (
	"bytes"
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"os"
)

// ParsedBinary represents a pre-parsed ELF binary with all data needed for TLS probe scanning.
// All fields are immutable after construction, making it safe for concurrent access.
// The underlying file descriptor is kept open for uprobe attachment.
type ParsedBinary struct {
	// Path is the original path to the binary
	Path string

	// Hash is a unique identifier for this binary (computed from content + metadata)
	Hash string

	// fd is kept open for uprobe attachment via link.OpenExecutable
	fd *os.File

	// ef is the parsed ELF file
	ef *elf.File

	// Pre-parsed symbol tables (immutable, safe for concurrent access)
	Symtab []elf.Symbol
	Dynsym []elf.Symbol

	// Sections from the ELF file
	Sections []*elf.Section

	// LinkedLibs contains the names of dynamically linked libraries
	LinkedLibs []string
}

// ParseBinary opens and parses an ELF binary, pre-loading all data needed for TLS scanning.
// The returned ParsedBinary keeps the file descriptor open for uprobe attachment.
// Caller must call Close() when done.
func ParseBinary(ctx context.Context, path string) (*ParsedBinary, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}

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

	pb := &ParsedBinary{
		Path:     path,
		fd:       fd,
		ef:       ef,
		Sections: ef.Sections,
	}

	// Compute hash
	pb.Hash, err = computeBinaryHash(fd)
	if err != nil {
		fd.Close()
		return nil, fmt.Errorf("computing hash: %w", err)
	}

	// Pre-parse symbol tables (thread-safety: these allocate fresh slices)
	// Symbols() is thread-safe, DynamicSymbols() has lazy init so we call it here
	pb.Symtab, _ = ef.Symbols()
	pb.Dynsym, _ = ef.DynamicSymbols()

	// Pre-load linked libraries
	pb.LinkedLibs, _ = ef.ImportedLibraries()

	return pb, nil
}

// Close releases all resources associated with the parsed binary.
func (pb *ParsedBinary) Close() error {
	if pb.fd != nil {
		return pb.fd.Close()
	}
	return nil
}

// FilePath returns the path to use for uprobe attachment.
// This is the path that was used to open the file.
func (pb *ParsedBinary) FilePath() string {
	return pb.Path
}

// ElfFile returns the underlying elf.File for advanced operations.
// Note: Some elf.File methods are not thread-safe (e.g., DynamicSymbols).
// Use the pre-parsed fields (Symtab, Dynsym) for thread-safe access.
func (pb *ParsedBinary) ElfFile() *elf.File {
	return pb.ef
}

// HasSymbol checks if a symbol with the given name exists in either symbol table.
func (pb *ParsedBinary) HasSymbol(name string) bool {
	for _, s := range pb.Symtab {
		if s.Name == name {
			return true
		}
	}
	for _, s := range pb.Dynsym {
		if s.Name == name {
			return true
		}
	}
	return false
}

// FindSymbols returns all symbols matching the given predicate from both symbol tables.
func (pb *ParsedBinary) FindSymbols(predicate func(elf.Symbol) bool) []elf.Symbol {
	var result []elf.Symbol
	for _, s := range pb.Symtab {
		if predicate(s) {
			result = append(result, s)
		}
	}
	for _, s := range pb.Dynsym {
		if predicate(s) {
			result = append(result, s)
		}
	}
	return result
}

// FindSymbolsByName returns all symbols matching any of the given search criteria.
func (pb *ParsedBinary) FindSymbolsByName(targets []SymbolSearch) []elf.Symbol {
	return pb.FindSymbols(func(s elf.Symbol) bool {
		return MatchSymbol(s.Name, targets)
	})
}

// SectionData returns the data for a section by name.
// This method is thread-safe as Section.Data() allocates a fresh buffer.
func (pb *ParsedBinary) SectionData(name string) ([]byte, error) {
	sec := pb.ef.Section(name)
	if sec == nil {
		return nil, fmt.Errorf("section %s not found", name)
	}
	return sec.Data()
}

// SearchString searches for a string with the given prefix in common string sections.
// Returns the full string if found, or an error if not found.
func (pb *ParsedBinary) SearchString(prefix string, strategy MatchStrategy) (string, error) {
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
func (pb *ParsedBinary) CalculateUprobeAddresses(symbols []elf.Symbol) []elf.Symbol {
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
func (pb *ParsedBinary) HasLinkedLibrary(pattern string, strategy MatchStrategy) bool {
	for _, lib := range pb.LinkedLibs {
		if match(lib, pattern, strategy) {
			return true
		}
	}
	return false
}

// ReaderAt returns an io.ReaderAt for the underlying file.
// This is useful for thread-safe random access reads.
func (pb *ParsedBinary) ReaderAt() io.ReaderAt {
	return pb.fd
}
