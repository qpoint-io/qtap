//go:build integration

package rustls

import (
	"debug/elf"
	"os"
	"testing"
)

func TestStrippedRustlsBinaryDetection(t *testing.T) {
	// Path to our STRIPPED rustls binary
	rustlsBinary := "/home/openclaw/research/tls-probes/rustls/experiments/rustls-stripped"
	if _, err := os.Stat(rustlsBinary); os.IsNotExist(err) {
		t.Skip("stripped rustls test binary not found")
	}

	f, err := elf.Open(rustlsBinary)
	if err != nil {
		t.Fatalf("failed to open ELF: %v", err)
	}
	defer f.Close()

	// Verify no symbols in stripped binary
	syms, _ := f.Symbols()
	dynSyms, _ := f.DynamicSymbols()
	t.Logf("Symbols: %d, Dynamic symbols: %d", len(syms), len(dynSyms))
	if len(syms) > 0 {
		t.Log("Warning: binary has symbols - may not be fully stripped")
	}

	// Test .eh_frame parsing - THIS IS THE KEY!
	parser := NewEHFrameParser(f)
	functions, err := parser.Parse()
	if err != nil {
		t.Fatalf("failed to parse .eh_frame: %v", err)
	}

	t.Logf("Found %d functions in .eh_frame (STRIPPED BINARY!)", len(functions))
	if len(functions) < 1000 {
		t.Errorf("expected many functions even in stripped binary, got %d", len(functions))
	}

	// Test pattern matching
	matcher, err := NewPatternMatcher(f)
	if err != nil {
		t.Fatalf("failed to create pattern matcher: %v", err)
	}

	cryptoFuncs := matcher.FindCryptoFunctions(functions, 10)
	t.Logf("Found %d crypto functions (STRIPPED BINARY!)", len(cryptoFuncs))
	
	if len(cryptoFuncs) == 0 {
		t.Error("expected to find crypto functions even in stripped binary")
	}

	// Log top 5 crypto functions
	limit := 5
	if len(cryptoFuncs) < limit {
		limit = len(cryptoFuncs)
	}
	for i, cf := range cryptoFuncs[:limit] {
		t.Logf("  %d: 0x%x (size: %d, aes: %d) - HOOK POINT!",
			i, cf.Bound.Start, cf.Bound.Size(), cf.Score.AESCount)
	}
}
