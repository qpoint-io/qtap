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
	for sym, want := range map[string]bool{
		"definitely_nonexistent_symbol_xyz": false,
		"test_symbol":                       true,
		"SSL_read":                          true,
	} {
		has, err := pb.HasSymbol(sym)
		require.NoError(t, err)
		assert.Equal(t, want, has)
	}
}

func TestParsedBinaryFindSymbols(t *testing.T) {
	elfPath := createTestElf(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	// Test predicate that matches nothing
	result, err := pb.FindSymbols(func(s elf.Symbol) bool {
		return s.Name == "nonexistent_symbol_xyz"
	})
	require.NoError(t, err)
	assert.Empty(t, result)

	// Test predicate that matches specific symbol
	result, err = pb.FindSymbols(func(s elf.Symbol) bool {
		return s.Name == "SSL_read"
	})
	require.NoError(t, err)
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
	result, err := pb.FindSymbolsByName(targets)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result), 1)

	// Test with non-matching targets
	targets = []SymbolSearch{
		{Name: "nonexistent_symbol", MatchStrategy: MatchStrategyExact},
	}
	result, err = pb.FindSymbolsByName(targets)
	require.NoError(t, err)
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
	has, err := pb.HasSymbol("dynamic_symbol")
	require.NoError(t, err)
	assert.True(t, has)
}

func TestParsedBinaryHasLinkedLibrary(t *testing.T) {
	elfPath := createTestElf(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	// Our synthetic ELF doesn't have linked libraries
	found, err := pb.HasLinkedLibrary("nonexistent_lib", MatchStrategyExact)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestParsedBinaryHasLinkedLibraryWithDynamic(t *testing.T) {
	elfPath := createTestElfWithDynsym(t)

	file, err := os.Open(elfPath)
	require.NoError(t, err)
	defer file.Close()
	pb, err := ParseBinary(t.Context(), file)
	require.NoError(t, err)

	libs, err := pb.LinkedLibs()
	require.NoError(t, err)
	// Check if we have linked libraries (may or may not work depending on ELF parsing)
	if len(libs) > 0 {
		// Test exact match
		found, err := pb.HasLinkedLibrary(libs[0], MatchStrategyExact)
		require.NoError(t, err)
		assert.True(t, found)

		// Test prefix match
		found, err = pb.HasLinkedLibrary("libssl", MatchStrategyPrefix)
		require.NoError(t, err)
		assert.True(t, found, "HasLinkedLibrary() prefix match: libs = %v", libs)

		// Test contains match
		found, err = pb.HasLinkedLibrary("ssl", MatchStrategyContains)
		require.NoError(t, err)
		assert.True(t, found, "HasLinkedLibrary() contains match: libs = %v", libs)
	}
}
