package binutils

import (
	"errors"
	"path/filepath"
	"testing"
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
	e.isClosed.Store(true)

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

	// Test that the file is marked as closed
	if !e.isClosed.Load() {
		t.Errorf("Elf.isClosed = %v, want true", e.isClosed.Load())
	}
}

func TestElfSearchString(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElfWithRodata(t)

	e, err := NewElf(ctx, elfPath, "", false)
	if err != nil {
		t.Fatalf("NewElf() error = %v", err)
	}
	defer e.Close()

	// Test search for non-existent string
	_, err = e.SearchString("definitely_nonexistent_string_xyz123", MatchStrategyExact)
	if err == nil {
		t.Error("SearchString() should error when string not found")
	}

	// Test search for string that exists in our test ELF
	result, err := e.SearchString("Hello World", MatchStrategyExact)
	if err != nil {
		t.Errorf("SearchString() error = %v", err)
	}
	if result != "Hello World" {
		t.Errorf("SearchString() = %q, want 'Hello World'", result)
	}

	// Test prefix search
	result, err = e.SearchString("OpenSSL", MatchStrategyPrefix)
	if err != nil {
		t.Errorf("SearchString() prefix error = %v", err)
	}
	if result != "OpenSSL 1.1.1" {
		t.Errorf("SearchString() prefix = %q, want 'OpenSSL 1.1.1'", result)
	}
}

func TestElfSearchStringClosed(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElfWithRodata(t)

	e, err := NewElf(ctx, elfPath, "", false)
	if err != nil {
		t.Fatalf("NewElf() error = %v", err)
	}

	e.Close()

	_, err = e.SearchString("test", MatchStrategyExact)
	if !errors.Is(err, ErrFileClosed) {
		t.Errorf("SearchString() on closed file error = %v, want ErrFileClosed", err)
	}
}

func TestElfSearchStringStrategies(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElfWithRodata(t)

	e, err := NewElf(ctx, elfPath, "", false)
	if err != nil {
		t.Fatalf("NewElf() error = %v", err)
	}
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
			if (err != nil) != tt.wantErr {
				t.Errorf("SearchString(%q, %v) error = %v, wantErr %v", tt.search, tt.strategy, err, tt.wantErr)
			}
		})
	}
}
