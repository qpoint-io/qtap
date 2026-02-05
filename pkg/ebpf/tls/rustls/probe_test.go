package rustls

import (
	"debug/elf"
	"os"
	"testing"
)

// TestEHFrameParser tests parsing .eh_frame from a real binary.
func TestEHFrameParser(t *testing.T) {
	// Use /bin/ls as a test binary (always available)
	testBinary := "/bin/ls"
	if _, err := os.Stat(testBinary); os.IsNotExist(err) {
		t.Skip("test binary not found")
	}

	f, err := elf.Open(testBinary)
	if err != nil {
		t.Fatalf("failed to open ELF: %v", err)
	}
	defer f.Close()

	parser := NewEHFrameParser(f)
	functions, err := parser.Parse()
	if err != nil {
		t.Fatalf("failed to parse .eh_frame: %v", err)
	}

	// We should find some functions
	if len(functions) == 0 {
		t.Error("expected to find functions in .eh_frame")
	}

	t.Logf("found %d functions in %s", len(functions), testBinary)

	// Verify function bounds look reasonable
	for i, fn := range functions[:min(5, len(functions))] {
		if fn.Start >= fn.End {
			t.Errorf("function %d: invalid bounds start=%x end=%x", i, fn.Start, fn.End)
		}
		if fn.Size() > 10*1024*1024 {
			t.Errorf("function %d: suspiciously large size=%d", i, fn.Size())
		}
		t.Logf("  function %d: 0x%x - 0x%x (size: %d)", i, fn.Start, fn.End, fn.Size())
	}
}

// TestPatternMatcher tests crypto pattern detection.
func TestPatternMatcher(t *testing.T) {
	// Use /bin/ls as a test binary - should have NO crypto patterns
	testBinary := "/bin/ls"
	if _, err := os.Stat(testBinary); os.IsNotExist(err) {
		t.Skip("test binary not found")
	}

	f, err := elf.Open(testBinary)
	if err != nil {
		t.Fatalf("failed to open ELF: %v", err)
	}
	defer f.Close()

	matcher, err := NewPatternMatcher(f)
	if err != nil {
		t.Fatalf("failed to create pattern matcher: %v", err)
	}
	if matcher == nil {
		t.Skip("no .text section")
	}

	parser := NewEHFrameParser(f)
	functions, err := parser.Parse()
	if err != nil {
		t.Fatalf("failed to parse .eh_frame: %v", err)
	}

	// Look for crypto functions (should find none in /bin/ls)
	cryptoFuncs := matcher.FindCryptoFunctions(functions, 1)

	// /bin/ls shouldn't have significant crypto patterns
	if len(cryptoFuncs) > 0 {
		t.Logf("found %d functions with crypto patterns in %s", len(cryptoFuncs), testBinary)
		for i, sf := range cryptoFuncs[:min(3, len(cryptoFuncs))] {
			t.Logf("  %d: 0x%x score=%d (aes=%d gcm=%d vex=%d)",
				i, sf.Bound.Start, sf.Score.Score,
				sf.Score.AESCount, sf.Score.GCMCount, sf.Score.VEXAESCount)
		}
	}
}

// TestCountPattern tests the pattern counting helper.
func TestCountPattern(t *testing.T) {
	tests := []struct {
		data    []byte
		pattern []byte
		want    int
	}{
		{[]byte{1, 2, 3, 2, 3, 2, 3}, []byte{2, 3}, 3},
		{[]byte{1, 2, 3, 4, 5}, []byte{6, 7}, 0},
		{[]byte{1, 1, 1, 1}, []byte{1, 1}, 3},
		{[]byte{}, []byte{1}, 0},
		{[]byte{1, 2, 3}, []byte{}, 0},
	}

	for i, tt := range tests {
		got := countPattern(tt.data, tt.pattern)
		if got != tt.want {
			t.Errorf("test %d: countPattern got %d, want %d", i, got, tt.want)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
