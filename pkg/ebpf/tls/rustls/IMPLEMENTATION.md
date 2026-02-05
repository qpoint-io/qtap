# rustls TLS Probe Implementation Plan

## Overview

Implement eBPF-based TLS interception for rustls, even in stripped binaries,
using .eh_frame parsing and AES-NI instruction pattern matching.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    rustls Probe Flow                        │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  1. Process Attach                                          │
│     ├── Read target binary ELF                              │
│     ├── Parse .eh_frame section                             │
│     └── Get function boundaries                             │
│                                                             │
│  2. Function Discovery                                      │
│     ├── Scan functions for AES-NI patterns                  │
│     ├── Match call graph signatures                         │
│     └── Identify seal/open functions                        │
│                                                             │
│  3. Probe Attachment                                        │
│     ├── Calculate uprobe offsets                            │
│     └── Attach BPF programs                                 │
│                                                             │
│  4. Data Capture                                            │
│     ├── Read plaintext from function args                   │
│     └── Send to userspace via ringbuf                       │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## Components

### 1. EH Frame Parser (`ehframe.go`)
- Parse ELF .eh_frame section
- Extract function boundaries (start, end addresses)
- Handle different CIE/FDE formats

### 2. Pattern Matcher (`pattern.go`)
- Disassemble function bytes
- Detect AES-NI instructions (aesenc, pclmulqdq, etc.)
- Score functions by crypto-relevance

### 3. Signature Database (`signatures.go`)
- Pre-computed signatures for known rustls versions
- Function size, prologue bytes, call offsets
- Fast lookup for common binaries

### 4. Probe Manager (`probe.go`)
- Implement tls.Probe interface
- Coordinate discovery and attachment
- Handle multiple rustls instances

### 5. BPF Program (`rustls.bpf.c`)
- Uprobe at seal_scatter entry
- Read plaintext pointer from stack
- Copy data to ringbuf

## Implementation Phases

### Phase 1: EH Frame Parser (Week 1)
- [ ] Implement .eh_frame section reader
- [ ] Parse CIE (Common Information Entry)
- [ ] Parse FDE (Frame Description Entry)
- [ ] Extract PC ranges (function boundaries)
- [ ] Unit tests with known binaries

### Phase 2: Pattern Matcher (Week 1-2)
- [ ] Integrate capstone-go for disassembly
- [ ] Implement AES-NI instruction detection
- [ ] Score functions by crypto patterns
- [ ] Build call graph analysis
- [ ] Validate against known binaries

### Phase 3: Probe Integration (Week 2-3)
- [ ] Implement tls.Probe interface
- [ ] Wire into target_scanner.go
- [ ] Handle probe attachment/detachment
- [ ] Test with rustls test binary

### Phase 4: BPF Data Capture (Week 3)
- [ ] Write BPF program for seal_scatter
- [ ] Figure out argument extraction
- [ ] Handle different function layouts
- [ ] Test data capture accuracy

### Phase 5: Testing & Polish (Week 4)
- [ ] E2E tests with real rustls apps
- [ ] Performance benchmarking
- [ ] Documentation
- [ ] PR review and merge

## Dependencies

```go
// New dependencies needed
"github.com/go-delve/delve/pkg/dwarf/frame" // .eh_frame parser (validated!)
"debug/elf"                                  // ELF parsing (stdlib)
```

## Validated by Research

**Prior art confirms this approach is sound:**

1. **Delve debugger** - Pure Go .eh_frame parser we can use directly
2. **Ghidra** - Uses .eh_frame as primary method for function discovery in stripped binaries
3. **Academic research** - 72% of ELF binaries have >90% function coverage via .eh_frame
4. **pyelftools** - Python reference implementation

Key insight: This is a **well-known technique** in the security/RE community.

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Offset changes between versions | Medium | High | Signature database |
| Pattern matching false positives | Low | Medium | Multiple signal validation |
| Argument extraction complexity | High | High | Prototype first |
| Performance overhead | Medium | Medium | Lazy scanning |

## Open Questions

1. **Argument extraction**: How do we reliably read plaintext from stack?
   - Need to reverse engineer calling convention
   - May vary by rust compiler version

2. **Multi-version support**: How many rustls versions to support?
   - Start with latest (0.23.x)
   - Add older versions based on usage data

3. **Static vs dynamic**: Should we pre-scan binaries or scan on attach?
   - Tradeoff: startup time vs maintenance burden

## Success Criteria

- [ ] Can hook rustls 0.23.x TLS connections
- [ ] Works on stripped binaries
- [ ] Captures plaintext HTTP data
- [ ] < 5% performance overhead
- [ ] E2E tests pass in CI

## References

- Research findings: `docs/research/rustls-probe-research.md`
- OpenSSL probe (pattern): `pkg/ebpf/tls/openssl/`
- EH Frame spec: LSB 5.0.0 Core Generic
