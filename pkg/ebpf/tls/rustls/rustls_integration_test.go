//go:build integration

package rustls

import (
	"debug/elf"
	"os"
	"testing"
)

func TestRustlsBinaryDetection(t *testing.T) {
	// Path to our test rustls binary
	rustlsBinary := "/home/openclaw/research/tls-probes/rustls/experiments/target/release/rustls-probe-test"
	if _, err := os.Stat(rustlsBinary); os.IsNotExist(err) {
		t.Skip("rustls test binary not found")
	}

	f, err := elf.Open(rustlsBinary)
	if err != nil {
		t.Fatalf("failed to open ELF: %v", err)
	}
	defer f.Close()

	// Test .eh_frame parsing
	parser := NewEHFrameParser(f)
	functions, err := parser.Parse()
	if err != nil {
		t.Fatalf("failed to parse .eh_frame: %v", err)
	}

	t.Logf("Found %d functions in .eh_frame", len(functions))
	if len(functions) < 1000 {
		t.Errorf("expected many functions in rustls binary, got %d", len(functions))
	}

	// Test pattern matching
	matcher, err := NewPatternMatcher(f)
	if err != nil {
		t.Fatalf("failed to create pattern matcher: %v", err)
	}

	cryptoFuncs := matcher.FindCryptoFunctions(functions, 10)
	t.Logf("Found %d crypto functions", len(cryptoFuncs))
	
	if len(cryptoFuncs) == 0 {
		t.Error("expected to find crypto functions in rustls binary")
	}

	// Log top 5 crypto functions
	limit := 5
	if len(cryptoFuncs) < limit {
		limit = len(cryptoFuncs)
	}
	for i, cf := range cryptoFuncs[:limit] {
		t.Logf("  %d: 0x%x - 0x%x (size: %d, score: %d, aes: %d)",
			i, cf.Bound.Start, cf.Bound.End, cf.Bound.Size(),
			cf.Score.Score, cf.Score.AESCount)
	}
}
