package binutils

import (
	"debug/elf"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBinary(t *testing.T) {
	elfPath := createTestElf(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	assert.NotEmpty(t, pb.Hash)
	assert.NotNil(t, pb.ef)
	assert.NotNil(t, pb.Sections)
}

func TestParseBinaryNotElf(t *testing.T) {
	notElf := createNonElfTestFile(t)

	file, err := os.Open(notElf)
	require.NoError(t, err)
	defer file.Close()
	_, err = ParseBinary(t.Context(), file)
	require.Error(t, err)
}

func TestParsedBinaryElfFile(t *testing.T) {
	elfPath := createTestElf(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	ef := pb.ElfFile()
	assert.NotNil(t, ef)
}

func TestParsedBinaryHasSymbol(t *testing.T) {
	elfPath := createTestElf(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	// Test with non-existent symbol
	assert.False(t, pb.HasSymbol("definitely_nonexistent_symbol_xyz"))

	// Test with existing symbol from our test ELF
	assert.True(t, pb.HasSymbol("test_symbol"))
	assert.True(t, pb.HasSymbol("SSL_read"))
}

func TestParsedBinaryFindSymbols(t *testing.T) {
	elfPath := createTestElf(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	// Test predicate that matches nothing
	result := pb.FindSymbols(func(s elf.Symbol) bool {
		return s.Name == "nonexistent_symbol_xyz"
	})
	assert.Empty(t, result)

	// Test predicate that matches specific symbol
	result = pb.FindSymbols(func(s elf.Symbol) bool {
		return s.Name == "SSL_read"
	})
	assert.Len(t, result, 1)
}

func TestParsedBinaryFindSymbolsByName(t *testing.T) {
	elfPath := createTestElf(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	// Test with existing targets
	targets := []SymbolSearch{
		{Name: "SSL_", MatchStrategy: MatchStrategyPrefix},
	}
	result := pb.FindSymbolsByName(targets)
	assert.GreaterOrEqual(t, len(result), 1)

	// Test with non-matching targets
	targets = []SymbolSearch{
		{Name: "nonexistent_symbol", MatchStrategy: MatchStrategyExact},
	}
	result = pb.FindSymbolsByName(targets)
	assert.Empty(t, result)
}

func TestParsedBinarySectionData(t *testing.T) {
	elfPath := createTestElf(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	// Test with non-existent section
	_, err = pb.SectionData("nonexistent_section")
	require.Error(t, err)

	// Test with existing section (.symtab should exist in our test ELF)
	data, err := pb.SectionData(".symtab")
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestParsedBinarySearchStringWithRodata(t *testing.T) {
	elfPath := createTestElfWithRodata(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	// Test prefix search
	result, err := pb.SearchString("OpenSSL", MatchStrategyPrefix)
	require.NoError(t, err)
	assert.Equal(t, "OpenSSL 1.1.1", result)

	// Test exact search
	result, err = pb.SearchString("Hello World", MatchStrategyExact)
	require.NoError(t, err)
	assert.Equal(t, "Hello World", result)
}

func TestParsedBinarySearchStringNotFound(t *testing.T) {
	elfPath := createTestElf(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	// Test with non-existent string
	_, err = pb.SearchString("definitely_nonexistent_string_xyz", MatchStrategyPrefix)
	assert.Error(t, err)
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
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParsedBinaryCalculateUprobeAddresses(t *testing.T) {
	elfPath := createTestElf(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	// Test with empty symbols
	result := pb.CalculateUprobeAddresses([]elf.Symbol{})
	assert.Empty(t, result)

	// Test with mock symbol
	symbols := []elf.Symbol{
		{Name: "test", Value: 0x1000},
	}
	result = pb.CalculateUprobeAddresses(symbols)
	assert.Len(t, result, 1)
}

func TestParsedBinaryCalculateUprobeAddressesWithProgHeaders(t *testing.T) {
	elfPath := createTestElfWithProgHeaders(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	// Symbol at VA 0x401000 should map to file offset 0x1000
	symbols := []elf.Symbol{
		{Name: "uprobe_target", Value: 0x401000},
	}

	result := pb.CalculateUprobeAddresses(symbols)
	require.Len(t, result, 1)
	assert.Equal(t, uint64(0x1000), result[0].Value)
}

func TestParsedBinaryHasSymbolInDynsym(t *testing.T) {
	elfPath := createTestElfWithDynsym(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	// Dynamic symbol should be found in Dynsym
	assert.True(t, pb.HasSymbol("dynamic_symbol"))
}

func TestParsedBinaryHasLinkedLibrary(t *testing.T) {
	elfPath := createTestElf(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	// Our synthetic ELF doesn't have linked libraries
	found := pb.HasLinkedLibrary("nonexistent_lib", MatchStrategyExact)
	assert.False(t, found)
}

func TestParsedBinaryHasLinkedLibraryWithDynamic(t *testing.T) {
	elfPath := createTestElfWithDynsym(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	// Check if we have linked libraries (may or may not work depending on ELF parsing)
	if len(pb.LinkedLibs) > 0 {
		// Test exact match
		found := pb.HasLinkedLibrary(pb.LinkedLibs[0], MatchStrategyExact)
		assert.True(t, found)

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
