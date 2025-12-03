package binutils

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatchFunction(t *testing.T) {
	tests := []struct {
		name        string
		symName     string
		targetName  string
		strategy    MatchStrategy
		shouldMatch bool
	}{
		{"Exact match", "testSymbol", "testSymbol", MatchStrategyExact, true},
		{"Exact mismatch", "testSymbol", "TestSymbol", MatchStrategyExact, false},
		{"Prefix match", "testSymbol", "test", MatchStrategyPrefix, true},
		{"Prefix mismatch", "testSymbol", "Test", MatchStrategyPrefix, false},
		{"Suffix match", "testSymbol", "Symbol", MatchStrategySuffix, true},
		{"Suffix mismatch", "testSymbol", "symbol", MatchStrategySuffix, false},
		{"Contains match", "testSymbol", "tSym", MatchStrategyContains, true},
		{"Contains mismatch", "testSymbol", "symTest", MatchStrategyContains, false},
		{"Empty target", "testSymbol", "", MatchStrategyContains, true},
		{"Empty source", "", "test", MatchStrategyContains, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := match(tt.symName, tt.targetName, tt.strategy)
			if result != tt.shouldMatch {
				t.Errorf("match(%q, %q, %v) = %v, want %v",
					tt.symName, tt.targetName, tt.strategy, result, tt.shouldMatch)
			}
		})
	}
}

func TestMatchSymbol(t *testing.T) {
	tests := []struct {
		name       string
		symName    string
		targets    []SymbolSearch
		wantResult bool
	}{
		{
			"Single exact match",
			"testSymbol",
			[]SymbolSearch{{Name: "testSymbol", MatchStrategy: MatchStrategyExact}},
			true,
		},
		{
			"Single exact mismatch",
			"testSymbol",
			[]SymbolSearch{{Name: "TestSymbol", MatchStrategy: MatchStrategyExact}},
			false,
		},
		{
			"Multiple targets with match",
			"testSymbol",
			[]SymbolSearch{
				{Name: "wrongSymbol", MatchStrategy: MatchStrategyExact},
				{Name: "test", MatchStrategy: MatchStrategyPrefix},
			},
			true,
		},
		{
			"Multiple targets with no match",
			"testSymbol",
			[]SymbolSearch{
				{Name: "wrongSymbol", MatchStrategy: MatchStrategyExact},
				{Name: "Symbol", MatchStrategy: MatchStrategyPrefix},
			},
			false,
		},
		{
			"Empty targets",
			"testSymbol",
			[]SymbolSearch{},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchSymbol(tt.symName, tt.targets)
			if result != tt.wantResult {
				t.Errorf("MatchSymbol(%q, %v) = %v, want %v",
					tt.symName, tt.targets, result, tt.wantResult)
			}
		})
	}
}

func TestSymbolSearchBytes(t *testing.T) {
	tests := []struct {
		name     string
		symName  string
		expected string
	}{
		{"Non-empty string", "testSymbol", "testSymbol"},
		{"Empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss := SymbolSearch{Name: tt.symName}
			result := string(ss.Bytes())
			if result != tt.expected {
				t.Errorf("SymbolSearch.Bytes() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestElfGetFilePath(t *testing.T) {
	tests := []struct {
		name        string
		exe         string
		root        string
		isContainer bool
		expected    string
	}{
		{
			"Container path",
			"usr/bin/test",
			"/root",
			true,
			filepath.Join("/root", "usr/bin/test"),
		},
		{
			"Non-container path",
			"/usr/bin/test",
			"/root",
			false,
			"/usr/bin/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Elf{
				exe:         tt.exe,
				root:        tt.root,
				isContainer: tt.isContainer,
			}
			result := e.getFilePath()
			if result != tt.expected {
				t.Errorf("Elf.getFilePath() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestElfCloseAlreadyClosed(t *testing.T) {
	// Create an Elf that's already closed
	e := &Elf{
		exe: "dummy",
	}

	require.NoError(t, e.Close())
	// Test closing an already closed Elf
	if err := e.Close(); err != nil {
		t.Errorf("Elf.Close() error = %v, want nil for already closed file", err)
	}
}

func TestElfCloseNilFile(t *testing.T) {
	// Create an Elf with nil file
	e := &Elf{
		exe:  "dummy",
		file: nil,
	}

	// Test closing the file
	if err := e.Close(); err != nil {
		t.Errorf("Elf.Close() error = %v, want nil for nil file", err)
	}
}

func TestSearchString(t *testing.T) {
	elfPath := createTestElf(t)

	// second file - make it bigger than the buffer size
	elfOffset := int64(math.Round(float64(bufferSize) * 3.3))
	f2path := t.TempDir() + "/large-file.bin"
	f2, err := os.OpenFile(f2path, os.O_CREATE|os.O_RDWR, 0644)
	require.NoError(t, err)
	padding := bytes.Repeat([]byte{'A'}, int(elfOffset))
	_, err = f2.WriteAt(padding, 0)
	require.NoError(t, err)
	// write the ELF data from the first file to the second file
	elfData, err := os.ReadFile(elfPath)
	require.NoError(t, err)
	_, err = f2.WriteAt(elfData, elfOffset)
	require.NoError(t, err)
	f2.Close()

	testFiles := []string{elfPath, f2path}

	tests := []struct {
		name      string
		searchStr string
		strategy  MatchStrategy
		wantMatch string
		wantErr   bool
	}{
		{
			name:      "Exact match",
			searchStr: "test_symbol",
			strategy:  MatchStrategyExact,
			wantMatch: "test_symbol",
		},
		{
			name:      "Prefix match SSL_",
			searchStr: "SSL_",
			strategy:  MatchStrategyPrefix,
			wantMatch: "SSL_read", // first SSL_ symbol encountered
		},
		{
			name:      "Suffix match _symbol",
			searchStr: "_symbol",
			strategy:  MatchStrategySuffix,
			wantMatch: "test_symbol",
		},
		{
			name:      "Contains match",
			searchStr: "ello_wor",
			strategy:  MatchStrategyContains,
			wantMatch: "hello_world",
		},
		{
			name:      "No match",
			searchStr: "nonexistent_string_xyz",
			strategy:  MatchStrategyExact,
			wantErr:   true,
		},
	}

	for _, file := range testFiles {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				f, err := os.Open(file)
				require.NoError(t, err)
				defer f.Close()
				result, err := searchString(f, tt.searchStr, tt.strategy)
				if tt.wantErr {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				require.Equal(t, tt.wantMatch, result)
			})
		}
	}
}

func TestSearchStringTrailingMatch(t *testing.T) {
	// File ending with printable string, no null terminator
	content := []byte("garbage\x00\x00\x00target_string") // no trailing \x00!

	path := t.TempDir() + "/trailing.bin"
	require.NoError(t, os.WriteFile(path, content, 0644))

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	// Should match at EOF via lines 66-72
	result, err := searchString(f, "target_string", MatchStrategyExact)
	require.NoError(t, err)
	require.Equal(t, "target_string", result)
}
