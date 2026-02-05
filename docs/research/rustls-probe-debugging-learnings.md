# rustls Probe Debugging Learnings

This document captures the debugging process and lessons learned while implementing the rustls TLS probe.

## Executive Summary

Successfully implemented TLS introspection for rustls applications using aws-lc-rs backend. The key breakthrough was discovering that PIE binaries require virtual address to file offset conversion for uprobe attachment.

## The Journey

### Phase 1: Research & Approach Selection

**Initial approach (failed):** Pattern matching for AES-NI instructions
- Found crypto functions via `.eh_frame` parsing + AES-NI byte patterns
- Problem: Low-level assembly routines (`_aesni_encrypt2`) don't have plaintext in registers/stack

**Winning approach:** Hook high-level EVP_AEAD API
- aws-lc-rs uses BoringSSL-compatible `EVP_AEAD_CTX_seal_scatter` / `open_gather`
- These functions have plaintext as parameters, stable ABI across versions

### Phase 2: Probe Implementation

Built BPF probes to capture plaintext before encryption (seal) and after decryption (open).

**Challenge 1: FD correlation**
- Crypto functions don't know which socket they're operating on
- Solution: Thread-local tracking via `rustls_active_fd` BPF map
- Populated by syscall exit probes (connect, write, etc.)

**Challenge 2: Finding the right functions**
- aws-lc version-prefixes symbols: `aws_lc_0_37_0_EVP_AEAD_CTX_seal_scatter`
- Scanner looks for `EVP_AEAD_CTX_seal_scatter` substring

### Phase 3: The PIE Binary Bug (The Breakthrough)

**Symptoms:**
- Probes attached at correct-looking addresses
- `bpftool link list` showed attachment
- BPF trace output: EMPTY
- Binary worked fine (TLS handshake succeeded)

**Debugging steps:**
1. Added `bpf_printk` at probe entry - still nothing
2. Verified probe attachment with `bpftool` - showed attached
3. Dumped bytes at target address with `od` - **WRONG BYTES!**

**Root cause:**
```
ELF .text section:
  VAddr:      0x000e9b80
  FileOffset: 0x000e8b80
  Difference: 0x1000 (4096 bytes)

Symbol from nm:  0x1be9f0  (virtual address)
Uprobe needs:    0x1bd9f0  (file offset)
```

**The fix:**
```go
func vaddrToFileOffset(elfFile *elf.File, vaddr uint64) uint64 {
    for _, prog := range elfFile.Progs {
        if prog.Type != elf.PT_LOAD {
            continue
        }
        if vaddr >= prog.Vaddr && vaddr < prog.Vaddr+prog.Memsz {
            return prog.Off + (vaddr - prog.Vaddr)
        }
    }
    return vaddr
}
```

## Key Learnings

### 1. uprobes Need File Offsets, Not Virtual Addresses

For PIE binaries, `nm` and `objdump` report virtual addresses. The kernel's uprobe mechanism needs file offsets. These differ when ELF sections aren't page-aligned with their virtual addresses.

**How to verify:**
```bash
# Check section alignment
readelf -S binary | grep "\.text"
#   VAddr: 0x000e9b80  Offset: 0x000e8b80  <- Different!

# Verify bytes at offset
od -A x -t x1 -j $FILE_OFFSET -N 16 binary
# Should match objdump output for that function
```

### 2. bpf_printk is Essential for Debugging

Even when probes appear attached, they may not fire. Always add debug prints at function entry to verify execution.

### 3. Different Crypto Backends Need Different Probes

| rustls Backend | Probe Strategy | Status |
|----------------|----------------|--------|
| aws-lc-rs | Hook EVP_AEAD_CTX_seal/open_scatter | ✅ Working |
| ring | Would need ring-specific hooks | Detection only |
| BoringSSL | Similar to aws-lc | Untested |

### 4. Test Container Setup Matters

Required for eBPF testing in containers:
```bash
docker run --privileged --pid=host \
  -v /sys/kernel/debug:/sys/kernel/debug:rw \
  ...
```

## Testing Checklist

For any new TLS probe:

1. [ ] Verify function exists in target binary (`nm`)
2. [ ] Check if binary is PIE (`file` or `readelf -h`)
3. [ ] Calculate file offset if PIE
4. [ ] Verify bytes at offset match expected instruction (`od`)
5. [ ] Add `bpf_printk` at function entry
6. [ ] Check BPF trace output (`/sys/kernel/debug/tracing/trace`)
7. [ ] Verify `tlsProbeIntrospected: true` in connection report
8. [ ] Verify `l7Protocol` shows expected protocol (http1, http2, etc.)

## Files Modified

- `pkg/ebpf/tls/rustls/probe.go` - Scanner with vaddr→offset conversion
- `bpf/tap/rustls.bpf.c` - BPF probes for seal/open capture
- `bpf/tap/socket.bpf.c` - FD tracking via `rustls_track_active_fd()`

## Commit History

- `c22fee2` - fix(rustls): Convert symbol vaddr to file offset for PIE binaries
- `319f4f5` - wip(rustls): FD correlation via connect() tracking  
- `e847dfb` - feat(rustls): Data capture BPF + probe attachment fix
