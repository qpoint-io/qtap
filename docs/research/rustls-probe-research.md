# rustls eBPF Probe Research Findings

## Executive Summary

**Goal:** Intercept rustls TLS traffic using eBPF, even in stripped binaries.

**Result:** ✅ VIABLE - Multiple creative approaches identified and validated.

## Key Discoveries

### 1. .eh_frame Survives Stripping
The `.eh_frame` section, required for exception handling, is NEVER stripped.
It contains function boundaries (start/end addresses) for ALL functions.

```bash
# Works on stripped binaries!
readelf --debug-dump=frames <binary> | grep "pc="
# Output: pc=00000000001d2c50..00000000001d2e55 (our target function!)
```

### 2. AES-NI Instruction Patterns Are Detectable
Crypto functions contain AES-NI instructions that survive compilation:
- `aesenc`, `aesenclast`, `aesdec`, `aesdeclast`
- `pclmulqdq`, `vpclmulqdq` (for GCM mode)

These can be found via disassembly even without symbols.

### 3. aws-lc-rs Uses Pregenerated Assembly
rustls (via aws-lc-rs) uses pregenerated BoringSSL assembly for crypto.
This makes instruction patterns highly consistent across builds.

### 4. Function Signatures Are Stable
Key crypto functions have identifiable characteristics:
- `aead_aes_gcm_seal_scatter_impl`: ~0x205 bytes, specific call pattern
- `aead_aes_gcm_open_gather`: ~0x2A bytes wrapper
- Called functions: `CRYPTO_gcm128_setiv`, `CRYPTO_gcm128_encrypt_ctr32`, etc.

## Viable Implementation Approaches

### Approach A: .eh_frame + Pattern Matching (Recommended)
1. Parse `.eh_frame` to get function boundaries
2. Scan each function for AES-NI instructions
3. Match call patterns to identify crypto functions
4. Attach uprobes at discovered offsets

**Pros:** Works on any stripped binary, no version database needed
**Cons:** Requires binary analysis at probe attach time

### Approach B: Signature Database
1. Pre-build signatures for known rustls/aws-lc versions
2. Match binary hash or instruction fingerprints
3. Look up pre-computed offsets

**Pros:** Fast, no runtime analysis
**Cons:** Requires maintaining version database

### Approach C: Syscall + Context Correlation
1. Hook `write()` syscall on TLS sockets
2. Correlate with plaintext buffers in memory
3. Use stack traces to identify TLS paths

**Pros:** Version-independent
**Cons:** Complex, potential performance impact

## Proof of Concept Files

- `experiments/`: Test rustls binary (with and without symbols)
- `crypto_finder.py`: Script to find crypto functions in binaries
- `poc_hook.bt`: bpftrace script for hook verification
- `design/PROBE_DESIGN.md`: Detailed implementation design

## Hook Points Identified

| Function | Offset | Purpose |
|----------|--------|---------|
| `aead_aes_gcm_seal_scatter_impl` | 0x1d2c50 | TLS record encryption |
| `aead_aes_gcm_open_gather` | 0x1d3750 | TLS record decryption |
| `aws_lc_0_37_0_CRYPTO_gcm128_encrypt_ctr32` | 0x1d2700 | Low-level encryption |

## Data Extraction Strategy

At `seal_scatter_impl` entry:
- Plaintext pointer accessible via stack offsets
- Length available from function arguments
- BPF can read user memory to capture plaintext

## Comparison with Other TLS Libraries

| Library | Symbol Stability | .eh_frame | Pattern Match | Difficulty |
|---------|-----------------|-----------|---------------|------------|
| OpenSSL | ✅ Dynamic symbols | ✅ | N/A | Easy |
| BoringSSL | ❌ Static | ✅ | ✅ | Medium |
| GnuTLS | ✅ Dynamic symbols | ✅ | N/A | Easy |
| **rustls** | ❌ Static | ✅ | ✅ | Medium |
| Node.js TLS | ✅ Offsets known | ✅ | N/A | Easy |
| Go TLS | ✅ .gopclntab | ✅ | N/A | Easy |

## Next Steps for Implementation

1. **Implement .eh_frame parser in Go** for qtap integration
2. **Build pattern matcher** for AES-NI instruction detection
3. **Create signature database** for common rustls versions
4. **Implement BPF probe** to capture plaintext at hook points
5. **Test on real applications** (not just our test binary)

## References

- aws-lc-rs: https://github.com/aws/aws-lc-rs
- rustls: https://github.com/rustls/rustls
- ELF .eh_frame: https://refspecs.linuxfoundation.org/LSB_5.0.0/LSB-Core-generic/LSB-Core-generic/ehframechpt.html
