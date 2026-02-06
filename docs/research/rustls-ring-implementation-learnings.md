# rustls Ring Backend Implementation Learnings

## Summary
Successfully implemented eBPF probes for ring's AES-GCM encryption, achieving full HTTP visibility for ~55% of Rust TLS applications.

## Key Discovery: VAES AVX2 Path

### Initial Assumption (WRONG)
We initially tried to hook `ring_core_*__aesni_gcm_encrypt` because:
1. Research showed ring uses BoringSSL-derived assembly
2. Function name suggested it was the main encryption path

**Result:** Probes attached but never fired.

### The Fix
Used GDB to trace actual function calls:
```bash
gdb -batch -x script.txt ./ring-test
# Breakpoint on aesni_gcm_encrypt: NEVER HIT
# Breakpoint on aes_gcm_enc_update_vaes_avx2: HIT!
```

**Discovery:** On modern CPUs with VAES+AVX2 support (most x86_64 from ~2019+), ring uses optimized paths:
- `ring_core_*__aes_gcm_enc_update_vaes_avx2` - Encryption
- `ring_core_*__aes_gcm_dec_update_vaes_avx2` - Decryption

The `aesni_gcm_encrypt` function has a 288-byte minimum and is rarely used.

## Key Discovery: Capture Timing

### Initial Approach (WRONG)
Following the OpenSSL pattern, we captured data on uretprobe (function return):
```c
SEC("uretprobe/ring_vaes_enc")
int ring_probe_ret_vaes_enc(struct pt_regs *ctx) {
    // Capture args->buf here
}
```

**Result:** Captured ciphertext, not plaintext. Protocol detection failed.

### The Fix
VAES uses in-place XOR encryption. By the time uretprobe fires, the buffer has been XOR'd with the keystream.

**Solution:** Capture the INPUT buffer on ENTRY (before XOR):
```c
SEC("uprobe/ring_vaes_enc")
int ring_probe_entry_vaes_enc(struct pt_regs *ctx) {
    // Capture in_buf HERE - it's still plaintext
    uint64_t in_buf = (uint64_t)PT_REGS_PARM2(ctx);
    // ... call process_data() NOW, not on return
}
```

For decrypt, capture OUTPUT buffer on RETURN (after XOR produces plaintext):
```c
SEC("uretprobe/ring_vaes_dec")
int ring_probe_ret_vaes_dec(struct pt_regs *ctx) {
    // Capture out_buf HERE - XOR has produced plaintext
}
```

## Key Discovery: Small Chunk Filtering

### Problem
Initial captures showed 16-byte chunks confusing protocol detection:
```
ring/vaes_enc_entry: len=16  # TLS record header?
ring/vaes_enc_entry: len=16  # More fragments
ring/vaes_enc_entry: len=1072  # Actual HTTP data
```

The 16-byte chunks were being processed first, and since they weren't HTTP headers, protocol stayed UNKNOWN.

### The Fix
Filter out small chunks (<32 bytes) in the BPF probe:
```c
if (len < 32) {
    return 0;  // Skip TLS record headers, handshake fragments
}
```

## Debugging Techniques Used

### 1. GDB Breakpoint Verification
```bash
# Verify which functions actually get called
gdb -batch -x /tmp/script.txt ./ring-test

# script.txt:
break ring_core_*__aesni_gcm_encrypt
break ring_core_*__aes_gcm_enc_update_vaes_avx2
run
info break
```

### 2. BPF Trace Output
```c
bpf_printk("ring/vaes_enc: conn_info=%p is_open=%d", ci, ci ? ci->is_open : -1);
```
Then: `cat /sys/kernel/debug/tracing/trace`

### 3. Symbol Inspection
```bash
nm /path/to/binary | grep "ring_core.*aes"
objdump -d /path/to/binary | grep -A5 "aes_gcm_enc_update_vaes_avx2"
```

### 4. Verify Byte Alignment
```bash
# Compare objdump vaddr with uprobe file offset
objdump -d binary | grep "<symbol>:"  # Shows vaddr
od -A x -t x1 -j OFFSET -N 20 binary   # Verify bytes match
```

## Implementation Checklist for Future TLS Probes

1. **Identify actual code paths** - Don't assume from function names. Use GDB to verify.
2. **Understand capture timing** - XOR-based ciphers need entry capture for encrypt, return capture for decrypt.
3. **Check CPU features** - Modern CPUs have optimized paths (VAES, AVX2, etc.).
4. **Filter noise** - Small chunks (record headers, handshake) can confuse protocol detection.
5. **Verify with actual data** - Check `l7Protocol` and `tlsProbeIntrospected` in logs.

## Function Signatures

### VAES AVX2 Encrypt
```c
void aes_gcm_enc_update_vaes_avx2(
    uint8_t *out,           // rdi - output (ciphertext)
    const uint8_t *in,      // rsi - input (PLAINTEXT) ← capture this on ENTRY
    size_t len,             // rdx - length in bytes
    const AES_KEY *key,     // rcx
    uint8_t *ivec,          // r8
    uint8_t *Xi             // r9 - GHASH state
);
```

### VAES AVX2 Decrypt
```c
void aes_gcm_dec_update_vaes_avx2(
    uint8_t *out,           // rdi - output (PLAINTEXT) ← capture this on RETURN
    const uint8_t *in,      // rsi - input (ciphertext)
    size_t len,             // rdx - length in bytes
    ...
);
```

## Timeline
- Started: ~22:30 UTC
- Ring scanner working: ~23:30 UTC
- Probes attaching: ~00:00 UTC
- Discovered VAES path: ~00:15 UTC
- Full HTTP visibility: ~00:40 UTC
- Verification complete: ~00:55 UTC

Total: ~2.5 hours from "probes don't fire" to "both backends working"
