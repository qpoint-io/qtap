package binutils

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
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
			assert.Equal(t, tt.shouldMatch, result)
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
			assert.Equal(t, tt.wantResult, result)
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
			assert.Equal(t, tt.expected, result)
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
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestElfCloseAlreadyClosed(t *testing.T) {
	// Create an Elf that's already closed
	e := &Elf{
		exe: "dummy",
	}
	e.isClosed.Store(true)

	// Test closing an already closed Elf
	require.NoError(t, e.Close())
}

func TestElfCloseNilFile(t *testing.T) {
	// Create an Elf with nil file
	e := &Elf{
		exe:  "dummy",
		file: nil,
	}

	// Test closing the file
	require.NoError(t, e.Close())

	// Test that the file is marked as closed
	assert.True(t, e.isClosed.Load())
}

func TestElfSearchString(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElfWithRodata(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	// Test search for non-existent string
	_, err = e.SearchString("definitely_nonexistent_string_xyz123", MatchStrategyExact)
	require.Error(t, err)

	// Test search for string that exists in our test ELF
	result, err := e.SearchString("Hello World", MatchStrategyExact)
	require.NoError(t, err)
	assert.Equal(t, "Hello World", result)

	// Test prefix search
	result, err = e.SearchString("OpenSSL", MatchStrategyPrefix)
	require.NoError(t, err)
	assert.Equal(t, "OpenSSL 1.1.1", result)
}

func TestElfSearchStringClosed(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElfWithRodata(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)

	e.Close()

	_, err = e.SearchString("test", MatchStrategyExact)
	assert.ErrorIs(t, err, ErrFileClosed)
}

func TestElfSearchStringStrategies(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElfWithRodata(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	tests := []struct {
		name     string
		search   string
		strategy MatchStrategy
		wantErr  bool
	}{
		{"Exact match", "Hello World", MatchStrategyExact, false},
		{"Prefix match", "Hello", MatchStrategyPrefix, false},
		{"Suffix match", "World", MatchStrategySuffix, false},
		{"Contains match", "llo Wor", MatchStrategyContains, false},
		{"Not found", "nonexistent_xyz", MatchStrategyExact, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.SearchString(tt.search, tt.strategy)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
