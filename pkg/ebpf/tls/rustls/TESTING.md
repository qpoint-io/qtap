# Testing rustls TLS Probe

## Prerequisites

1. Linux with kernel 5.4+ (for BPF)
2. Root/privileged access
3. clang 14 (for BPF compilation)
4. Docker (for testing)

## Quick Test with bpftrace

The fastest way to verify the hooks work:

```bash
# Terminal 1: Start bpftrace (needs root)
sudo bpftrace -e '
uprobe:/path/to/rustls-probe-test:0x1bbe00 {
    printf("EVP_AEAD_CTX_seal_scatter called! pid=%d\n", pid);
    printf("  out=%p (ciphertext)\n", arg1);
    // plaintext is on stack at sp+8
}
'

# Terminal 2: Run the test binary
./rustls-probe-test
```

## Test with Commercial qtap

The commercial qpoint/qtap already has TLS probes. To test:

```bash
# Build and run qtap with debug logging
docker run --privileged --pid=host \
  us-docker.pkg.dev/qpoint-edge/public/qtap:v0 \
  --log-level debug

# In another terminal, run the rustls app
./rustls-probe-test

# Check logs for TLS visibility
```

## Building the BPF Probe

If you have clang 14:

```bash
cd /path/to/qtap
go generate ./internal/tap/...
go build ./cmd/qtap
```

## Hook Points Discovered

These offsets are for rustls 0.23.36 + aws-lc-rs 0.37.0:

| Function | Offset | Purpose |
|----------|--------|---------|
| `EVP_AEAD_CTX_seal_scatter` | 0x1bbe00 | Encryption entry |
| `EVP_AEAD_CTX_open_gather` | 0x1bbf50 | Decryption entry |
| `aead_aes_gcm_seal_scatter_impl` | 0x1d2c50 | Low-level encrypt |
| `aead_aes_gcm_open_gather` | 0x1d3750 | Low-level decrypt |

## Verifying the Discovery Works

```bash
# Run the integration tests
go test -v -tags=integration ./pkg/ebpf/tls/rustls/...
```

Expected output:
```
=== RUN   TestStrippedRustlsBinaryDetection
    Symbols: 0           ← Confirmed stripped!
    Functions: 4,338     ← From .eh_frame
    Crypto hooks: 22     ← AES-NI pattern matched
--- PASS
```

## E2E Test Script

```bash
#!/bin/bash
# Run in privileged Docker container

# 1. Start qtap watching for processes
./qtap --log-level debug &
QTAP_PID=$!
sleep 2

# 2. Run rustls test app
./rustls-probe-test

# 3. Check for TLS visibility in logs
# Should see: "detected rustls crypto functions"
# Should see: HTTP request/response data

kill $QTAP_PID
```
