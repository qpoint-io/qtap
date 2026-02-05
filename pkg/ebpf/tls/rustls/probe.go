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
	"debug/elf"
	"fmt"

	"go.uber.org/zap"
)

// Probe implements TLS interception for rustls binaries.
type Probe struct {
	logger *zap.Logger

	// Discovered hook points for this binary
	sealOffset uint64 // aead_aes_gcm_seal_scatter_impl
	openOffset uint64 // aead_aes_gcm_open_gather
}

// ProbeConfig contains configuration for the rustls probe.
type ProbeConfig struct {
	Logger *zap.Logger
}

// NewProbe creates a new rustls probe instance.
func NewProbe(cfg ProbeConfig) *Probe {
	return &Probe{
		logger: cfg.Logger,
	}
}

// CanProbe checks if the given binary appears to be a rustls application.
// Returns true if we can identify rustls crypto functions.
func (p *Probe) CanProbe(path string) (bool, error) {
	f, err := elf.Open(path)
	if err != nil {
		return false, fmt.Errorf("failed to open ELF: %w", err)
	}
	defer f.Close()

	// Look for .eh_frame section (required for our approach)
	ehFrame := f.Section(".eh_frame")
	if ehFrame == nil {
		p.logger.Debug("no .eh_frame section found", zap.String("path", path))
		return false, nil
	}

	// Parse function boundaries from .eh_frame
	functions, err := p.parseEHFrame(f, ehFrame)
	if err != nil {
		return false, fmt.Errorf("failed to parse .eh_frame: %w", err)
	}

	p.logger.Debug("parsed .eh_frame",
		zap.String("path", path),
		zap.Int("functions", len(functions)))

	// Look for crypto functions using pattern matching
	cryptoFuncs, err := p.findCryptoFunctions(f, functions)
	if err != nil {
		return false, fmt.Errorf("failed to find crypto functions: %w", err)
	}

	if len(cryptoFuncs) == 0 {
		p.logger.Debug("no rustls crypto functions found", zap.String("path", path))
		return false, nil
	}

	// Store discovered offsets
	for _, cf := range cryptoFuncs {
		switch cf.role {
		case "seal":
			p.sealOffset = cf.start
		case "open":
			p.openOffset = cf.start
		}
	}

	p.logger.Info("identified rustls crypto functions",
		zap.String("path", path),
		zap.Uint64("sealOffset", p.sealOffset),
		zap.Uint64("openOffset", p.openOffset))

	return p.sealOffset != 0 || p.openOffset != 0, nil
}

// functionBounds represents a function's address range from .eh_frame.
type functionBounds struct {
	start uint64
	end   uint64
	size  uint64
}

// cryptoFunction represents an identified crypto function.
type cryptoFunction struct {
	start    uint64
	end      uint64
	role     string // "seal" or "open"
	aesCount int    // number of AES-NI instructions
	gcmCount int    // number of GCM-related instructions
}

// parseEHFrame extracts function boundaries from the .eh_frame section.
func (p *Probe) parseEHFrame(f *elf.File, section *elf.Section) ([]functionBounds, error) {
	// TODO: Implement .eh_frame parsing
	// See: https://refspecs.linuxfoundation.org/LSB_5.0.0/LSB-Core-generic/LSB-Core-generic/ehframechpt.html
	//
	// For now, return empty - will be implemented in Phase 1
	return nil, fmt.Errorf("not implemented")
}

// findCryptoFunctions scans functions for AES-NI patterns.
func (p *Probe) findCryptoFunctions(f *elf.File, functions []functionBounds) ([]cryptoFunction, error) {
	// TODO: Implement pattern matching
	// - Disassemble each function
	// - Look for: aesenc, aesenclast, aesdec, aesdeclast, pclmulqdq
	// - Score by crypto instruction density
	//
	// For now, return empty - will be implemented in Phase 2
	return nil, fmt.Errorf("not implemented")
}

// Attach attaches the probe to the target process.
func (p *Probe) Attach(pid int, path string) error {
	// TODO: Implement probe attachment
	// - Load BPF program
	// - Attach uprobes at sealOffset and openOffset
	//
	// For now, return error - will be implemented in Phase 3
	return fmt.Errorf("not implemented")
}

// Detach detaches the probe from the target process.
func (p *Probe) Detach() error {
	// TODO: Implement probe detachment
	return fmt.Errorf("not implemented")
}
