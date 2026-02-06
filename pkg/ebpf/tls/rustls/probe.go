// Package rustls implements TLS interception for rustls-based applications.
//
// Unlike OpenSSL which exports dynamic symbols, rustls statically links its
// crypto implementation and strips symbols in release builds. This package
// uses a novel approach:
//
//  1. Parse .eh_frame section to get function boundaries (survives stripping)
//  2. Pattern-match AES-NI instructions to identify crypto functions
//  3. Attach uprobes at discovered offsets
//
// See docs/research/rustls-probe-research.md for detailed research findings.
package rustls

import (
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
)

const Name = "rustls"

var tracer = telemetry.Tracer()

// compile time check that Probe implements tls.Probe
var _ tls.Probe = (*Probe)(nil)

// Probe implements tls.Probe for rustls TLS interception.
type Probe struct {
	logger  *zap.Logger
	probeFn func() []*common.Uprobe
}

// ScanResult contains the results of scanning a binary for rustls.
type ScanResult struct {
	// ContainsRustls indicates if rustls crypto functions were detected.
	ContainsRustls bool

	// SealOffset is the uprobe offset for the TLS encryption function.
	// This is aead_aes_gcm_seal_scatter_impl or equivalent.
	SealOffset uint64

	// OpenOffset is the uprobe offset for the TLS decryption function.
	// This is aead_aes_gcm_open_gather or equivalent.
	OpenOffset uint64

	// CryptoFunctions contains all detected crypto functions with scores.
	CryptoFunctions []ScoredFunction

	// Symbols contains synthetic symbols for the detected functions.
	// These are created from .eh_frame + pattern matching.
	Symbols []elf.Symbol
}

// ProbeName implements tls.ProbeScanResult.
func (r *ScanResult) ProbeName() string {
	return Name
}

// ProbeDetected implements tls.ProbeScanResult.
func (r *ScanResult) ProbeDetected() bool {
	return r.ContainsRustls && (r.SealOffset != 0 || r.OpenOffset != 0)
}

// NewProbe creates a new rustls probe.
func NewProbe(logger *zap.Logger, probeFn func() []*common.Uprobe) *Probe {
	return &Probe{
		logger:  logger,
		probeFn: probeFn,
	}
}

// Name implements tls.Probe.
func (p *Probe) Name() string {
	return Name
}

// vaddrToFileOffset converts a virtual address to a file offset using ELF program headers.
// For PIE binaries, symbol values are virtual addresses, but uprobes need file offsets.
// Returns (offset, true) on success, (0, false) if no matching PT_LOAD segment found.
func vaddrToFileOffset(elfFile *elf.File, vaddr uint64) (uint64, bool) {
	for _, prog := range elfFile.Progs {
		if prog.Type != elf.PT_LOAD {
			continue
		}
		// Check if vaddr falls within this segment
		if vaddr >= prog.Vaddr && vaddr < prog.Vaddr+prog.Memsz {
			// Convert vaddr to file offset
			return prog.Off + (vaddr - prog.Vaddr), true
		}
	}
	// No matching segment found - this indicates an unusual ELF layout
	return 0, false
}

// scanForEVPAEADSymbols looks for aws-lc's EVP_AEAD_CTX_seal_scatter and
// EVP_AEAD_CTX_open_gather symbols. These are present in aws-lc-rs binaries
// and provide more reliable hook points than pattern matching.
func (p *Probe) scanForEVPAEADSymbols(elfFile *elf.File, result *ScanResult) bool {
	symbols, err := elfFile.Symbols()
	if err != nil {
		// Try dynamic symbols
		symbols, err = elfFile.DynamicSymbols()
		if err != nil {
			return false
		}
	}

	// Look for EVP_AEAD_CTX_seal_scatter and EVP_AEAD_CTX_open_gather
	// Symbol names may be prefixed with version like "aws_lc_0_37_0_"
	for _, sym := range symbols {
		// Check for seal_scatter
		if result.SealOffset == 0 {
			if strings.Contains(sym.Name, "EVP_AEAD_CTX_seal_scatter") ||
				strings.HasSuffix(sym.Name, "_seal_scatter") && strings.Contains(sym.Name, "EVP_AEAD") {
				// Convert virtual address to file offset for uprobe
				fileOffset, ok := vaddrToFileOffset(elfFile, sym.Value)
				if !ok {
					continue // Skip symbols we can't resolve
				}
				result.SealOffset = fileOffset
				result.Symbols = append(result.Symbols, elf.Symbol{
					Name:  "rustls_seal",
					Value: fileOffset,
					Size:  1,
				})
			}
		}

		// Check for open_gather
		if result.OpenOffset == 0 {
			if strings.Contains(sym.Name, "EVP_AEAD_CTX_open_gather") ||
				strings.HasSuffix(sym.Name, "_open_gather") && strings.Contains(sym.Name, "EVP_AEAD") {
				// Convert virtual address to file offset for uprobe
				fileOffset, ok := vaddrToFileOffset(elfFile, sym.Value)
				if !ok {
					continue // Skip symbols we can't resolve
				}
				result.OpenOffset = fileOffset
				result.Symbols = append(result.Symbols, elf.Symbol{
					Name:  "rustls_open",
					Value: fileOffset,
					Size:  1,
				})
			}
		}

		// Found both, we're done
		if result.SealOffset != 0 && result.OpenOffset != 0 {
			result.ContainsRustls = true
			return true
		}
	}

	// Partial match - still useful if we found at least one
	if result.SealOffset != 0 || result.OpenOffset != 0 {
		result.ContainsRustls = true
		return true
	}

	return false
}

// scanForRingSymbols looks for ring's versioned assembly symbols.
// Ring prefixes all symbols with ring_core_{version}__ (e.g., ring_core_0_17_14__).
// These are C-compatible assembly functions inherited from BoringSSL.
func (p *Probe) scanForRingSymbols(elfFile *elf.File, result *ScanResult) bool {
	symbols, err := elfFile.Symbols()
	if err != nil {
		// Try dynamic symbols
		symbols, err = elfFile.DynamicSymbols()
		if err != nil {
			return false
		}
	}

	// Look for ring's versioned AES-GCM symbols
	// Priority order (by performance/usage on modern CPUs):
	// 1. aes_gcm_enc_update_vaes_avx2 / aes_gcm_dec_update_vaes_avx2 (VAES+AVX2)
	// 2. aes_hw_ctr32_encrypt_blocks (fallback)
	for _, sym := range symbols {
		// Check for VAES AVX2 optimized encrypt (most common on modern CPUs)
		if result.SealOffset == 0 {
			if strings.HasPrefix(sym.Name, "ring_core_") && strings.HasSuffix(sym.Name, "__aes_gcm_enc_update_vaes_avx2") {
				fileOffset, ok := vaddrToFileOffset(elfFile, sym.Value)
				if !ok {
					continue
				}
				result.SealOffset = fileOffset
				result.Symbols = append(result.Symbols, elf.Symbol{
					Name:  "ring_vaes_enc",
					Value: fileOffset,
					Size:  1,
				})
				p.logger.Debug("found ring aes_gcm_enc_update_vaes_avx2",
					zap.String("symbol", sym.Name),
					zap.Uint64("vaddr", sym.Value),
					zap.Uint64("fileOffset", fileOffset))
			}
		}

		// Check for VAES AVX2 optimized decrypt
		if result.OpenOffset == 0 {
			if strings.HasPrefix(sym.Name, "ring_core_") && strings.HasSuffix(sym.Name, "__aes_gcm_dec_update_vaes_avx2") {
				fileOffset, ok := vaddrToFileOffset(elfFile, sym.Value)
				if !ok {
					continue
				}
				result.OpenOffset = fileOffset
				result.Symbols = append(result.Symbols, elf.Symbol{
					Name:  "ring_vaes_dec",
					Value: fileOffset,
					Size:  1,
				})
				p.logger.Debug("found ring aes_gcm_dec_update_vaes_avx2",
					zap.String("symbol", sym.Name),
					zap.Uint64("vaddr", sym.Value),
					zap.Uint64("fileOffset", fileOffset))
			}
		}

		// Found both VAES functions, we're done
		if result.SealOffset != 0 && result.OpenOffset != 0 {
			result.ContainsRustls = true
			return true
		}
	}

	// Fallback: CTR32 for CPUs without VAES
	for _, sym := range symbols {
		if result.SealOffset == 0 && result.OpenOffset == 0 {
			if strings.HasPrefix(sym.Name, "ring_core_") && strings.HasSuffix(sym.Name, "__aes_hw_ctr32_encrypt_blocks") {
				fileOffset, ok := vaddrToFileOffset(elfFile, sym.Value)
				if !ok {
					continue
				}
				result.SealOffset = fileOffset
				result.OpenOffset = fileOffset // CTR mode is symmetric
				result.Symbols = append(result.Symbols, elf.Symbol{
					Name:  "ring_ctr32",
					Value: fileOffset,
					Size:  1,
				})
				p.logger.Debug("found ring aes_hw_ctr32_encrypt_blocks (fallback)",
					zap.String("symbol", sym.Name),
					zap.Uint64("vaddr", sym.Value),
					zap.Uint64("fileOffset", fileOffset))
			}
		}

		if result.SealOffset != 0 && result.OpenOffset != 0 {
			result.ContainsRustls = true
			return true
		}
	}

	// Partial match - still useful if we found at least one
	if result.SealOffset != 0 || result.OpenOffset != 0 {
		result.ContainsRustls = true
		return true
	}

	return false
}

// Scan implements tls.Probe.
// Analyzes the binary using .eh_frame and AES-NI pattern matching.
func (p *Probe) Scan(ctx context.Context, target *tls.ExeElfScannable) (tls.ProbeScanResult, error) {
	ctx, span := tracer.Start(ctx, "RustlsProbe.Scan")
	defer span.End()

	result := &ScanResult{}

	// Get the underlying elf.File
	elfFile, err := target.Elf.Elf(ctx)
	if err != nil {
		// ELF parsing failure is a real error - propagate it
		p.logger.Debug("failed to get ELF file",
			zap.String("path", target.Path),
			zap.Error(err))
		return nil, fmt.Errorf("getting ELF file: %w", err)
	}

	// Step 0a: Check for EVP_AEAD symbols (aws-lc binaries have these)
	// This is more reliable than pattern matching for aws-lc
	if found := p.scanForEVPAEADSymbols(elfFile, result); found {
		p.logger.Debug("detected aws-lc EVP_AEAD symbols",
			zap.String("path", target.Path),
			zap.Uint64("sealOffset", result.SealOffset),
			zap.Uint64("openOffset", result.OpenOffset))
		return result, nil
	}

	// Step 0b: Check for ring's versioned assembly symbols
	// Ring uses ring_core_{version}__aesni_gcm_encrypt/decrypt
	if found := p.scanForRingSymbols(elfFile, result); found {
		p.logger.Debug("detected ring assembly symbols",
			zap.String("path", target.Path),
			zap.Uint64("sealOffset", result.SealOffset),
			zap.Uint64("openOffset", result.OpenOffset))
		return result, nil
	}

	// Step 1: Parse .eh_frame to get function boundaries
	ehParser := NewEHFrameParser(elfFile)
	functions, err := ehParser.Parse()
	if err != nil {
		// .eh_frame parsing not yet fully implemented
		// For now, return not detected
		p.logger.Debug("failed to parse .eh_frame",
			zap.String("path", target.Path),
			zap.Error(err))
		return result, nil
	}

	if len(functions) == 0 {
		p.logger.Debug("no functions found in .eh_frame",
			zap.String("path", target.Path))
		return result, nil
	}

	// Step 2: Create pattern matcher and find crypto functions
	matcher, err := NewPatternMatcher(elfFile)
	if err != nil || matcher == nil {
		p.logger.Debug("failed to create pattern matcher",
			zap.String("path", target.Path),
			zap.Error(err))
		return result, nil
	}

	// Find functions with crypto patterns (minimum score of 10 = at least 1 AES-NI instruction)
	cryptoFuncs := matcher.FindCryptoFunctions(functions, 10)
	if len(cryptoFuncs) == 0 {
		p.logger.Debug("no crypto functions detected",
			zap.String("path", target.Path))
		return result, nil
	}

	result.ContainsRustls = true
	result.CryptoFunctions = cryptoFuncs

	// Step 3: Identify seal/open functions by characteristics
	// The seal (encrypt) function is typically larger and called more
	// The open (decrypt) function is typically smaller
	for _, cf := range cryptoFuncs {
		size := cf.Bound.Size()

		// seal_scatter_impl is typically ~500 bytes with high AES count
		if size >= 400 && size <= 600 && cf.Score.AESCount >= 2 && result.SealOffset == 0 {
			result.SealOffset = cf.Bound.Start
			continue
		}

		// open_gather is typically smaller, ~40-100 bytes
		if size >= 30 && size <= 150 && cf.Score.AESCount >= 1 && result.OpenOffset == 0 {
			result.OpenOffset = cf.Bound.Start
		}
	}

	// If we couldn't identify specific functions, use the top crypto functions
	if result.SealOffset == 0 && len(cryptoFuncs) > 0 {
		result.SealOffset = cryptoFuncs[0].Bound.Start
	}

	// Create synthetic symbols for the detected functions
	// These will be used by AttachProbes
	// NOTE: Size must be non-zero or the symbol is filtered out!
	if result.SealOffset != 0 {
		result.Symbols = append(result.Symbols, elf.Symbol{
			Name:  "rustls_seal",
			Value: result.SealOffset,
			Size:  1, // Non-zero to pass filter
		})
	}
	if result.OpenOffset != 0 {
		result.Symbols = append(result.Symbols, elf.Symbol{
			Name:  "rustls_open",
			Value: result.OpenOffset,
			Size:  1, // Non-zero to pass filter
		})
	}

	p.logger.Debug("detected rustls crypto functions",
		zap.String("path", target.Path),
		zap.Int("totalCrypto", len(cryptoFuncs)),
		zap.Uint64("sealOffset", result.SealOffset),
		zap.Uint64("openOffset", result.OpenOffset))

	return result, nil
}

// Attach implements tls.Probe.
func (p *Probe) Attach(ctx context.Context, target *tls.ExeLinkAttachable, result tls.ProbeScanResult) (io.Closer, error) {
	ctx, span := tracer.Start(ctx, "RustlsProbe.Attach")
	defer span.End()

	ll := p.logger.With(zap.String("exe", target.Path))

	r, ok := result.(*ScanResult)
	if !ok {
		ll.DPanic("invalid result type", zap.Any("result", result))
		return nil, errors.New("invalid result type: expected *ScanResult")
	}

	if len(r.Symbols) == 0 {
		return nil, errors.New("no symbols to attach")
	}

	// Use offset-based attachment since we don't have real symbols
	return tls.AttachProbes(ctx, ll, target, r.Symbols, binutils.MatchStrategyExact, p.probeFn(), true)
}

// SharedLibraries implements tls.Probe.
// rustls is always statically linked, so we don't scan shared libraries.
func (p *Probe) SharedLibraries() string {
	return "" // No shared library - rustls is statically linked
}

// ScanLibrary implements tls.Probe.
// rustls is always statically linked, so this is a no-op.
func (p *Probe) ScanLibrary(ctx context.Context, ef *binutils.Elf) (tls.ProbeScanResult, error) {
	// rustls is statically linked, not a shared library - matches nodetls/gotls pattern
	return nil, nil
}

// AttachLibrary implements tls.Probe.
// rustls is always statically linked, so this is a no-op.
func (p *Probe) AttachLibrary(ctx context.Context, target *tls.ExeLibraryAttachable, result tls.ProbeScanResult) (io.Closer, error) {
	// rustls is statically linked, not a shared library - matches nodetls/gotls pattern
	return nil, nil
}

// Close implements tls.Probe.
func (p *Probe) Close() error {
	return nil
}
