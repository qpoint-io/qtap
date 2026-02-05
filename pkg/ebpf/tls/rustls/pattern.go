// Package rustls implements TLS interception for rustls-based applications.
package rustls

import (
	"debug/elf"
)

// AES-NI instruction opcodes we look for
// Reference: Intel SDM, Volume 2
var aesniOpcodes = map[string][]byte{
	// AESENC - AES encrypt one round
	"aesenc": {0x66, 0x0f, 0x38, 0xdc},
	// AESENCLAST - AES encrypt last round
	"aesenclast": {0x66, 0x0f, 0x38, 0xdd},
	// AESDEC - AES decrypt one round
	"aesdec": {0x66, 0x0f, 0x38, 0xde},
	// AESDECLAST - AES decrypt last round
	"aesdeclast": {0x66, 0x0f, 0x38, 0xdf},
	// AESIMC - AES inverse mix columns
	"aesimc": {0x66, 0x0f, 0x38, 0xdb},
	// AESKEYGENASSIST - AES key generation assist
	"aeskeygenassist": {0x66, 0x0f, 0x3a, 0xdf},
}

// GCM-related instruction opcodes (PCLMULQDQ for carry-less multiplication)
var gcmOpcodes = map[string][]byte{
	// PCLMULQDQ - Carry-less multiplication (used in GCM mode)
	"pclmulqdq": {0x66, 0x0f, 0x3a, 0x44},
}

// VEX-encoded AES-NI instructions (AVX)
// These have variable prefixes, so we look for the core opcodes
var vexAesOpcodes = map[string][]byte{
	// VAESENC (VEX prefix + 0x38 0xdc)
	"vaesenc_core": {0x38, 0xdc},
	// VAESENCLAST (VEX prefix + 0x38 0xdd)
	"vaesenclast_core": {0x38, 0xdd},
}

// EVEX-encoded AES-NI instructions (AVX-512)
// EVEX prefix: 0x62 followed by payload bytes
var evexMarker = byte(0x62)

// PatternMatcher identifies crypto functions by instruction patterns.
type PatternMatcher struct {
	file *elf.File

	// Text section data and base address
	textData []byte
	textBase uint64
}

// NewPatternMatcher creates a new pattern matcher for the given ELF file.
func NewPatternMatcher(f *elf.File) (*PatternMatcher, error) {
	text := f.Section(".text")
	if text == nil {
		return nil, nil // No text section
	}

	data, err := text.Data()
	if err != nil {
		return nil, err
	}

	return &PatternMatcher{
		file:     f,
		textData: data,
		textBase: text.Addr,
	}, nil
}

// CryptoScore represents how likely a function is to be crypto-related.
type CryptoScore struct {
	// Number of AES-NI instructions found
	AESCount int
	// Number of GCM-related instructions found
	GCMCount int
	// Number of VEX/EVEX encoded AES instructions
	VEXAESCount int
	// Total score (higher = more likely crypto)
	Score int
}

// AnalyzeFunction scans a function's bytes for crypto instruction patterns.
func (m *PatternMatcher) AnalyzeFunction(fn FunctionBound) CryptoScore {
	if m.textData == nil {
		return CryptoScore{}
	}

	// Calculate offsets into text section
	if fn.Start < m.textBase || fn.End < m.textBase {
		return CryptoScore{}
	}

	startOff := fn.Start - m.textBase
	endOff := fn.End - m.textBase

	if startOff >= uint64(len(m.textData)) || endOff > uint64(len(m.textData)) {
		return CryptoScore{}
	}

	funcBytes := m.textData[startOff:endOff]

	var score CryptoScore

	// Count AES-NI instructions
	for _, opcode := range aesniOpcodes {
		score.AESCount += countPattern(funcBytes, opcode)
	}

	// Count GCM instructions
	for _, opcode := range gcmOpcodes {
		score.GCMCount += countPattern(funcBytes, opcode)
	}

	// Count VEX-encoded AES (look for VEX prefix patterns)
	// VEX 2-byte: 0xC5
	// VEX 3-byte: 0xC4
	for i := 0; i < len(funcBytes)-4; i++ {
		if funcBytes[i] == 0xC5 || funcBytes[i] == 0xC4 {
			// Check if followed by AES opcode pattern
			for _, core := range vexAesOpcodes {
				if i+4 < len(funcBytes) {
					// VEX prefix can be 2 or 3 bytes, then the opcode
					if containsAt(funcBytes[i+2:], core) || containsAt(funcBytes[i+3:], core) {
						score.VEXAESCount++
					}
				}
			}
		}
	}

	// Count EVEX-encoded AES
	for i := 0; i < len(funcBytes)-6; i++ {
		if funcBytes[i] == evexMarker {
			// EVEX prefix is 4 bytes, then opcode
			for _, core := range vexAesOpcodes {
				if i+6 < len(funcBytes) && containsAt(funcBytes[i+4:], core) {
					score.VEXAESCount++
				}
			}
		}
	}

	// Calculate total score
	// Weight: AES instructions are most important, GCM next, VEX least
	score.Score = score.AESCount*10 + score.GCMCount*5 + score.VEXAESCount*3

	return score
}

// FindCryptoFunctions returns functions with crypto patterns, sorted by score.
func (m *PatternMatcher) FindCryptoFunctions(functions []FunctionBound, minScore int) []ScoredFunction {
	var results []ScoredFunction

	for _, fn := range functions {
		// Skip tiny functions
		if fn.Size() < 50 {
			continue
		}

		score := m.AnalyzeFunction(fn)
		if score.Score >= minScore {
			results = append(results, ScoredFunction{
				Bound: fn,
				Score: score,
			})
		}
	}

	// Sort by score (highest first)
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score.Score > results[i].Score.Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	return results
}

// ScoredFunction is a function with its crypto score.
type ScoredFunction struct {
	Bound FunctionBound
	Score CryptoScore
}

// Helper: count occurrences of pattern in data
func countPattern(data, pattern []byte) int {
	if len(pattern) == 0 || len(data) < len(pattern) {
		return 0
	}

	count := 0
	for i := 0; i <= len(data)-len(pattern); i++ {
		match := true
		for j := 0; j < len(pattern); j++ {
			if data[i+j] != pattern[j] {
				match = false
				break
			}
		}
		if match {
			count++
		}
	}
	return count
}

// Helper: check if pattern exists at start of data
func containsAt(data, pattern []byte) bool {
	if len(data) < len(pattern) {
		return false
	}
	for i := 0; i < len(pattern); i++ {
		if data[i] != pattern[i] {
			return false
		}
	}
	return true
}
