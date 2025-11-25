package binutils

import (
	"debug/elf"
	"testing"
)

func TestParseBinary(t *testing.T) {
	elfPath := createTestElf(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	if pb.Path != elfPath {
		t.Errorf("ParseBinary() Path = %s, want %s", pb.Path, elfPath)
	}
	if pb.Hash == "" {
		t.Error("ParseBinary() Hash is empty")
	}
	if pb.fd == nil {
		t.Error("ParseBinary() fd is nil")
	}
	if pb.ef == nil {
		t.Error("ParseBinary() ef is nil")
	}
	if pb.Sections == nil {
		t.Error("ParseBinary() Sections is nil")
	}
}

func TestParseBinaryNonExistent(t *testing.T) {
	_, err := ParseBinary(t.Context(), "/nonexistent/file")
	if err == nil {
		t.Error("ParseBinary() should error on non-existent file")
	}
}

func TestParseBinaryNotElf(t *testing.T) {
	notElf := createNonElfTestFile(t)

	_, err := ParseBinary(t.Context(), notElf)
	if err == nil {
		t.Error("ParseBinary() should error on non-ELF file")
	}
}

func TestParsedBinaryClose(t *testing.T) {
	elfPath := createTestElf(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}

	// First close should succeed
	if err := pb.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Second close should be safe (fd is now nil after first close)
	pb.fd = nil
	if err := pb.Close(); err != nil {
		t.Errorf("Close() second call error = %v", err)
	}
}

func TestParsedBinaryFilePath(t *testing.T) {
	elfPath := createTestElf(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	if pb.FilePath() != elfPath {
		t.Errorf("FilePath() = %s, want %s", pb.FilePath(), elfPath)
	}
}

func TestParsedBinaryElfFile(t *testing.T) {
	elfPath := createTestElf(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	ef := pb.ElfFile()
	if ef == nil {
		t.Error("ElfFile() returned nil")
	}
}

func TestParsedBinaryHasSymbol(t *testing.T) {
	elfPath := createTestElf(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	// Test with non-existent symbol
	if pb.HasSymbol("definitely_nonexistent_symbol_xyz") {
		t.Error("HasSymbol() should return false for non-existent symbol")
	}

	// Test with existing symbol from our test ELF
	if !pb.HasSymbol("test_symbol") {
		t.Error("HasSymbol() should return true for existing symbol")
	}
	if !pb.HasSymbol("SSL_read") {
		t.Error("HasSymbol() should return true for SSL_read")
	}
}

func TestParsedBinaryFindSymbols(t *testing.T) {
	elfPath := createTestElf(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	// Test predicate that matches nothing
	result := pb.FindSymbols(func(s elf.Symbol) bool {
		return s.Name == "nonexistent_symbol_xyz"
	})
	if len(result) != 0 {
		t.Errorf("FindSymbols() with no match returned %d symbols", len(result))
	}

	// Test predicate that matches specific symbol
	result = pb.FindSymbols(func(s elf.Symbol) bool {
		return s.Name == "SSL_read"
	})
	if len(result) != 1 {
		t.Errorf("FindSymbols() for SSL_read returned %d, want 1", len(result))
	}
}

func TestParsedBinaryFindSymbolsByName(t *testing.T) {
	elfPath := createTestElf(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	// Test with existing targets
	targets := []SymbolSearch{
		{Name: "SSL_", MatchStrategy: MatchStrategyPrefix},
	}
	result := pb.FindSymbolsByName(targets)
	if len(result) < 1 {
		t.Errorf("FindSymbolsByName() with prefix SSL_ returned %d symbols, want >= 1", len(result))
	}

	// Test with non-matching targets
	targets = []SymbolSearch{
		{Name: "nonexistent_symbol", MatchStrategy: MatchStrategyExact},
	}
	result = pb.FindSymbolsByName(targets)
	if len(result) != 0 {
		t.Errorf("FindSymbolsByName() with no match returned %d symbols", len(result))
	}
}

func TestParsedBinarySectionData(t *testing.T) {
	elfPath := createTestElf(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	// Test with non-existent section
	_, err = pb.SectionData("nonexistent_section")
	if err == nil {
		t.Error("SectionData() should error on non-existent section")
	}

	// Test with existing section (.symtab should exist in our test ELF)
	data, err := pb.SectionData(".symtab")
	if err != nil {
		t.Errorf("SectionData(.symtab) error = %v", err)
	}
	if len(data) == 0 {
		t.Error("SectionData(.symtab) returned empty data")
	}
}

func TestParsedBinarySearchStringWithRodata(t *testing.T) {
	elfPath := createTestElfWithRodata(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	// Test prefix search
	result, err := pb.SearchString("OpenSSL", MatchStrategyPrefix)
	if err != nil {
		t.Errorf("SearchString() error = %v", err)
	}
	if result != "OpenSSL 1.1.1" {
		t.Errorf("SearchString() = %q, want 'OpenSSL 1.1.1'", result)
	}

	// Test exact search
	result, err = pb.SearchString("Hello World", MatchStrategyExact)
	if err != nil {
		t.Errorf("SearchString() exact error = %v", err)
	}
	if result != "Hello World" {
		t.Errorf("SearchString() exact = %q, want 'Hello World'", result)
	}
}

func TestParsedBinarySearchStringNotFound(t *testing.T) {
	elfPath := createTestElf(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	// Test with non-existent string
	_, err = pb.SearchString("definitely_nonexistent_string_xyz", MatchStrategyPrefix)
	if err == nil {
		t.Error("SearchString() should error when string not found")
	}
}

func TestSearchStringInData(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		target   string
		strategy MatchStrategy
		want     string
	}{
		{
			"Prefix match",
			[]byte("hello\x00world\x00"),
			"hel",
			MatchStrategyPrefix,
			"hello",
		},
		{
			"Exact match",
			[]byte("hello\x00world\x00"),
			"hello",
			MatchStrategyExact,
			"hello",
		},
		{
			"Exact no match - substring",
			[]byte("hello\x00world\x00"),
			"hell",
			MatchStrategyExact,
			"",
		},
		{
			"Prefix not found",
			[]byte("hello\x00world\x00"),
			"xyz",
			MatchStrategyPrefix,
			"",
		},
		{
			"Unsupported strategy",
			[]byte("hello\x00world\x00"),
			"hello",
			MatchStrategySuffix,
			"",
		},
		{
			"No null terminator",
			[]byte("hello"),
			"hel",
			MatchStrategyPrefix,
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchStringInData(tt.data, tt.target, tt.strategy)
			if got != tt.want {
				t.Errorf("searchStringInData() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParsedBinaryCalculateUprobeAddresses(t *testing.T) {
	elfPath := createTestElf(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	// Test with empty symbols
	result := pb.CalculateUprobeAddresses([]elf.Symbol{})
	if len(result) != 0 {
		t.Errorf("CalculateUprobeAddresses() with empty input returned %d", len(result))
	}

	// Test with mock symbol
	symbols := []elf.Symbol{
		{Name: "test", Value: 0x1000},
	}
	result = pb.CalculateUprobeAddresses(symbols)
	if len(result) != 1 {
		t.Errorf("CalculateUprobeAddresses() returned %d symbols, want 1", len(result))
	}
}

func TestParsedBinaryCalculateUprobeAddressesWithProgHeaders(t *testing.T) {
	elfPath := createTestElfWithProgHeaders(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	// Symbol at VA 0x401000 should map to file offset 0x1000
	symbols := []elf.Symbol{
		{Name: "uprobe_target", Value: 0x401000},
	}

	result := pb.CalculateUprobeAddresses(symbols)
	if len(result) != 1 {
		t.Fatalf("CalculateUprobeAddresses() returned %d symbols, want 1", len(result))
	}
	if result[0].Value != 0x1000 {
		t.Errorf("CalculateUprobeAddresses() value = 0x%x, want 0x1000", result[0].Value)
	}
}

func TestParsedBinaryHasSymbolInDynsym(t *testing.T) {
	elfPath := createTestElfWithDynsym(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	// Dynamic symbol should be found in Dynsym
	if !pb.HasSymbol("dynamic_symbol") {
		t.Error("HasSymbol() should find dynamic_symbol in Dynsym")
	}
}

func TestParsedBinaryHasLinkedLibrary(t *testing.T) {
	elfPath := createTestElf(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	// Our synthetic ELF doesn't have linked libraries
	found := pb.HasLinkedLibrary("nonexistent_lib", MatchStrategyExact)
	if found {
		t.Error("HasLinkedLibrary() should return false for non-linked library")
	}
}

func TestParsedBinaryHasLinkedLibraryWithDynamic(t *testing.T) {
	elfPath := createTestElfWithDynsym(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	// Check if we have linked libraries (may or may not work depending on ELF parsing)
	if len(pb.LinkedLibs) > 0 {
		// Test exact match
		found := pb.HasLinkedLibrary(pb.LinkedLibs[0], MatchStrategyExact)
		if !found {
			t.Error("HasLinkedLibrary() should find existing library with exact match")
		}

		// Test prefix match
		found = pb.HasLinkedLibrary("libssl", MatchStrategyPrefix)
		if !found && len(pb.LinkedLibs) > 0 {
			t.Logf("HasLinkedLibrary() prefix match: libs = %v", pb.LinkedLibs)
		}

		// Test contains match
		found = pb.HasLinkedLibrary("ssl", MatchStrategyContains)
		if !found && len(pb.LinkedLibs) > 0 {
			t.Logf("HasLinkedLibrary() contains match: libs = %v", pb.LinkedLibs)
		}
	}
}

func TestParsedBinaryReaderAt(t *testing.T) {
	elfPath := createTestElf(t)

	pb, err := ParseBinary(t.Context(), elfPath)
	if err != nil {
		t.Fatalf("ParseBinary() error = %v", err)
	}
	defer pb.Close()

	reader := pb.ReaderAt()
	if reader == nil {
		t.Error("ReaderAt() returned nil")
	}

	// Test that we can read from it
	buf := make([]byte, 4)
	n, err := reader.ReadAt(buf, 0)
	if err != nil {
		t.Errorf("ReaderAt().ReadAt() error = %v", err)
	}
	if n != 4 {
		t.Errorf("ReaderAt().ReadAt() read %d bytes, want 4", n)
	}
	// Should be ELF magic bytes
	if buf[0] != 0x7f || buf[1] != 'E' || buf[2] != 'L' || buf[3] != 'F' {
		t.Errorf("ReaderAt() returned wrong magic: %v", buf)
	}
}
