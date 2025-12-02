# TLS Probe Architecture V2 - Implementation Plan

## Overview

This document outlines the redesign of the TLS probe architecture for improved performance, reliability, caching, and lifecycle management. The key goals are:

1. **Decouple scanning from process lifecycle** - Continue scanning even if the process exits
2. **Concurrent scanning and attachment** - Run all probe scanners in parallel
3. **Unified interface** - Standard interface for all TLS probes
4. **Better caching** - Binary-hash-based caching that survives process restarts

---

## Current Architecture Analysis

### Probe Implementations

We analyzed four TLS probe implementations:

#### OpenSSL (`qtap/pkg/ebpf/tls/openssl/`)

- **Detection**: Searches for `SSL_read`, `SSL_write`, `SSL_read_ex`, `SSL_write_ex` symbols
- **Two modes**:
  - **Shared library**: Scans containers for `libssl.so`, attaches uprobes at container level
  - **Static linking**: Detects statically linked OpenSSL via non-zero symbol Value/Size in SYMTAB
- **Offset strategy**: None needed — hooks at function boundaries
- **Caching**: 5-min TTL in-memory cache keyed by `proc.CacheKey()`

#### NodeTLS (`qpoint/internal/tap/tls/nodetls/`)

- **Detection**: Searches for TLSWrap mangled C++ symbols (`_ZN4node7TLSWrapC2E` for <v15, `_ZN4node6crypto7TLSWrapC2E` for >=v15)
- **Version detection**: Two strategies - exec `node --version` (chroot for containers) or binary string search for "node.js/v"
- **Offset strategy**: Hardcoded version-specific struct offsets pushed to eBPF map keyed by PID
- **Caching**: 5-min TTL cache storing version, symaddrs, symbols

#### GoTLS (`qpoint/internal/tap/tls/gotls/`)

- **Detection**: Uses `gobin` package to identify Go binaries and extract Go version
- **Symbol search**: `crypto/tls.(*Conn).Read/Write` + itab symbols for interface type resolution
- **Offset strategy**: Precomputed via `gobin.GetOffset()` for struct fields
- **Special handling**: Finds multiple return offsets (Go's non-standard calling convention)
- **Caching**: 5-min TTL cache storing version, symaddrs, function offsets

#### JavaSSL (`qpoint/internal/tap/tls/javassl/`)

- **Detection**: Regex for `java`/`javaw` executables OR linked libs (`libjvm.so`, `libjava.so`, `libjli.so`)
- **Fundamentally different**: Uses Java Attach API for bytecode instrumentation
- **Correlation challenge**: SSLEngine decouples encryption from I/O, correlates via xxhash of first 16 bytes
- **Multiple eBPF maps**: JavaProcessPidMap, SessionIgnoreMap, SyscallCorrelatedMap, UprobeCorrelatedMap

### Common Patterns

| Pattern             | Implementation                                                                   |
| ------------------- | -------------------------------------------------------------------------------- |
| Manager + Target    | All use a Manager tracking per-process/container Targets                         |
| Process Observer    | All embed `process.DefaultObserver`, implement `ProcessStarted/Stopped/Replaced` |
| TTL Cache           | All use `synq.TTLCache` with 5-min expiration                                    |
| Cache Key           | All use `proc.CacheKey()` (binary path + mtime combo)                            |
| Target Versions Map | Most track `targetVersions` to detect `exec()` replacements                      |
| ELF Symbol Search   | All use `binutils.Elf.SearchSymbols()`                                           |
| Uprobe Attachment   | All use `link.OpenExecutable()` + `probe.Attach()`                               |

### Key Divergences

| Aspect              | OpenSSL             | NodeTLS                 | GoTLS                    | JavaSSL                  |
| ------------------- | ------------------- | ----------------------- | ------------------------ | ------------------------ |
| **Scope**           | Container + Process | Process only            | Process only             | Process only             |
| **Detection Cost**  | Low                 | Medium (exec or search) | High (gobin analysis)    | Low (regex + ldd)        |
| **Offset Strategy** | None                | Hardcoded version map   | Precomputed + runtime    | None (agent-based)       |
| **Probe Points**    | Entry/exit          | Entry/exit              | Entry + multiple returns | Native bridge entry/exit |
| **FD Correlation**  | Direct              | Direct                  | Direct                   | Indirect (hash matching) |

---

## Problems Identified

### Problem 1: Scan Tied to Process Lifecycle

If a process exits during scan (100ms+ for large binaries), work is wasted and cache isn't populated.

**Solution**: Keep file descriptor open during scan. Linux keeps an inode alive as long as any FD references it. The process can exit, but the FD keeps the binary accessible.

```go
type BinaryRef struct {
    Path    string
    FD      *os.File   // Open FD keeps inode alive
    ElfFile *elf.File  // Parsed from FD
}
```

### Problem 2: Ephemeral In-Memory Cache

Container restarts lose all cached data; repeated scans for identical binaries.

**Solution**: Two-level cache with binary hash as key:

- L1: In-memory LRU (current implementation, enhanced)
- L2: On-disk persistent cache (future)

### Problem 3: Sequential Probe Execution

Current `TlsManager.ProcessStarted` iterates through probes sequentially.

**Solution**: Run all scanners concurrently since:

- Scanning is pure read operations - no process interaction
- Uprobe attachment is thread-safe at the kernel level
- Probes hook different functions, no conflicts

### Problem 4: No Unified Interface

Each probe has different method signatures and behaviors.

**Solution**: Standard `Scanner` interface with `Detect`, `Scan`, `Attach` phases.

### Problem 5: Thread Safety in binutils

Current `binutils.Elf` has race conditions:

- `Elf()` method lazily initializes `ef` field without synchronization
- `readString()` uses `Seek()` which is position-dependent

**Solution**:

- Add `sync.Once` for lazy initialization
- Replace `Seek`-based reads with `ReadAt`
- Pre-parse symbol tables into immutable struct

### Problem 6: Conflict Resolution Unnecessary

We considered conflict resolution for overlapping probes, but analysis shows probes hook completely different functions:

- OpenSSL: `SSL_read`, `SSL_write`
- NodeTLS: `_ZN4node...TLSWrap...`
- GoTLS: `crypto/tls.(*Conn).Read/Write`
- JavaSSL: Custom bridge functions

**Decision**: Attach all matching probes, no conflict resolution needed.

---

## Proposed Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Process Monitor                               │
│   (fanotify/netlink → ProcessStarted/Stopped/Replaced events)       │
└───────────────────────────────┬─────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         TLS Manager                                  │
│                    (process.Observer impl)                           │
└───────────────────────────────┬─────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        Orchestrator                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │   OpenSSL    │  │   NodeTLS    │  │    GoTLS     │  ...         │
│  │   Scanner    │  │   Scanner    │  │   Scanner    │              │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘              │
│         └─────────────────┼─────────────────┘                       │
│                           ▼                                         │
│                  ┌─────────────────┐                                │
│                  │  ParsedBinary   │ ◀──── Shared, immutable        │
│                  └────────┬────────┘                                │
└───────────────────────────┼─────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         Scan Cache                                   │
│                    (L1: memory, L2: disk*)                          │
│                        * L2 is future                                │
└─────────────────────────────────────────────────────────────────────┘
```

### Core Interfaces

```go
// Scanner is implemented by each TLS probe type
type Scanner interface {
    Name() string
    Detect(ctx context.Context, pb *binutils.ParsedBinary) bool
    Scan(ctx context.Context, pb *binutils.ParsedBinary) (ScanResult, error)
    Attach(ctx context.Context, proc *process.Process, result ScanResult) (Detacher, error)
}

// ScanResult is returned by Scanner.Scan, each probe defines its own concrete type
type ScanResult interface {
    ProbeType() string
}

// Detacher is returned by Scanner.Attach for cleanup
type Detacher interface {
    Close() error
}
```

### ParsedBinary (Thread-Safe ELF Wrapper)

```go
// ParsedBinary holds pre-parsed ELF data, immutable after construction
type ParsedBinary struct {
    Path     string
    Hash     string
    fd       *os.File       // Kept open for uprobe attachment
    elf      *elf.File

    // Pre-parsed for thread safety (stdlib DynamicSymbols has lazy init race)
    Symtab   []elf.Symbol
    Dynsym   []elf.Symbol
    Sections []*elf.Section
}

// Generic helper methods
func (pb *ParsedBinary) FindSymbols(predicate func(elf.Symbol) bool) []elf.Symbol
func (pb *ParsedBinary) SectionData(name string) ([]byte, error)
func (pb *ParsedBinary) HasSymbol(name string) bool
func (pb *ParsedBinary) Close() error
```

### Concurrent Execution Flow

```go
func (o *Orchestrator) Process(ctx context.Context, proc *process.Process) ([]Detacher, error) {
    // 1. Parse binary once (handles DynamicSymbols thread-safety)
    pb, err := binutils.ParseBinary(proc.PidExe)
    // pb.fd keeps binary accessible even if process exits

    // 2. Check cache
    cacheKey := pb.Hash
    if cached := o.cache.Get(cacheKey); cached != nil {
        return o.attachFromCache(ctx, proc, pb, cached)
    }

    // 3. Run all scanners concurrently
    var wg sync.WaitGroup
    results := make(chan scanResult, len(o.scanners))

    for _, scanner := range o.scanners {
        scanner := scanner
        wg.Add(1)
        go func() {
            defer wg.Done()
            if scanner.Detect(ctx, pb) {
                if result, err := scanner.Scan(ctx, pb); err == nil {
                    // Attach immediately
                    detacher, _ := scanner.Attach(ctx, proc, result)
                    results <- scanResult{scanner.Name(), result, detacher}
                }
            }
        }()
    }

    // 4. Collect results and cache
    wg.Wait()
    close(results)
    // ...
}
```

---

## Implementation Plan

### Phase 1: Enhance `binutils` Package ✅

#### 1.1 Fix Thread-Safety Issues in `binutils/elf.go`

- [x] Add `sync.Once` for lazy `elf.File` initialization in `Elf()` method
- [x] Replace `readString` with position-independent reads using `ReadAt`
- [x] Refactor `searchSymbol` to use `ReadAt` instead of `Seek`
- [x] Make buffer usage per-call rather than shared across concurrent operations

#### 1.2 Add `ParsedBinary` Type (`binutils/parsed.go`)

- [x] Struct holding pre-parsed ELF data (symtab, dynsym, sections)
- [x] Constructor that parses everything upfront (single-threaded)
- [x] All fields immutable after construction
- [x] Keeps FD open for uprobe attachment
- [x] Generic helper methods: `FindSymbols(predicate)`, `SectionData(name)`, `HasSymbol(name)`

#### 1.3 Add Hash Computation (`binutils/hash.go`)

- [x] Add method to compute binary hash (SHA256 sampling from start/middle/end + size)
- [x] This becomes the cache key

---

### Phase 2: Create Scanner Interface in `pkg/ebpf/tls` ✅

#### 2.1 Define Core Types (`pkg/ebpf/tls/scanner.go`)

- [x] `Scanner` interface with `Name`, `Detect`, `Scan`, `Attach`
- [x] `ScanResult` interface
- [x] `Detacher` interface (with `MultiDetacher` and `NoopDetacher` helpers)

#### 2.2 Create Scan Cache (`pkg/ebpf/tls/cache.go`)

- [x] In-memory LRU cache (L1)
- [x] Key: binary hash
- [x] Value: map of probe name → scan result (via `CachedResults`)
- [x] TTL-based expiration with refresh on access
- [x] Thread-safe via `sync.RWMutex`

#### 2.3 Create Orchestrator (`pkg/ebpf/tls/orchestrator.go`)

- [x] Holds list of registered scanners
- [x] `Process(ctx, *process.Process) error`:
  1. Get or create `ParsedBinary`
  2. Check cache
  3. Run detect → scan → attach concurrently for uncached probes
  4. Store results in cache
  5. Manage detachers internally

#### 2.4 Update `TlsManager` (`pkg/ebpf/tls/manager.go`)

- [x] Wrap `Orchestrator` (via `WithOrchestrator` / `WithScanners` options)
- [x] Implement same `process.Observer` interface
- [x] Handle process lifecycle → orchestrator calls
- [x] Manage detacher cleanup on process stop (via orchestrator)
- [x] Keep existing metrics working + added cache metrics

<!-- TODO: Metrics suggestions for follow-up:
     - scan_duration_seconds (histogram by probe type)
     - cache_hit_total / cache_miss_total (counter by probe type)
     - concurrent_scans_in_flight (gauge)
     - attach_duration_seconds (histogram by probe type)
     - active_attachments (gauge by probe type)
-->

---

### Phase 3: Migrate OpenSSL Probe

#### 3.1 Create `pkg/ebpf/tls/openssl/scanner.go`

- [ ] `OpenSSLScanner` implementing `Scanner`
- [ ] `Detect`: Check for `SSL_read`, `SSL_write` symbols
- [ ] `Scan`: Find all SSL symbols, calculate uprobe addresses
- [ ] `Attach`: Bind uprobes to target binary
- [ ] `OpenSSLScanResult`: Symbols, addresses, static vs shared info

#### 3.2 Handle Container-Level Shared Library Attachment

- [ ] Keep existing container tracking logic for `libssl.so`
- [ ] Scanner detects shared library case and returns appropriate result
- [ ] Orchestrator handles container-scoped vs process-scoped attachment

#### 3.3 Remove Old Files

- [ ] Delete `manager.go`, `container.go`, `target.go` after migration complete

---

### Phase 4: Migrate NodeTLS Probe (qpoint repo)

#### 4.1 Create `internal/tap/tls/nodetls/scanner.go`

- [ ] `NodeTLSScanner` implementing `Scanner`
- [ ] `Detect`: Check for TLSWrap symbols
- [ ] `Scan`: Detect version via binary search, get symaddrs
- [ ] `Attach`: Push symaddrs to eBPF map, bind uprobes
- [ ] `NodeTLSScanResult`: Version, symaddrs, symbols

#### 4.2 Version Detection

- [ ] Keep `getNodeVersionByProc` (exec-based) as fallback
- [ ] Primary: `getNodeVersionBySearch` (binary string search)

<!-- NOTE: Consider removing exec-based version detection (getNodeVersionByProc) in future.
     Binary search is more reliable and works even if process exits during scan.
     Exec-based detection requires chroot for containers and can fail in various ways.
-->

#### 4.3 Remove Old Files

- [ ] Delete `manager.go`, `target.go` after migration complete

---

### Phase 5: Migrate GoTLS Probe (qpoint repo)

#### 5.1 Create `internal/tap/tls/gotls/scanner.go`

- [ ] `GoTLSScanner` implementing `Scanner`
- [ ] `Detect`: Check for Go binary markers
- [ ] `Scan`: Get Go version, compute symaddrs, find function offsets
- [ ] `Attach`: Push symaddrs to eBPF map, bind uprobes at entry + return offsets
- [ ] `GoTLSScanResult`: Version, symaddrs, function offsets

#### 5.2 Keep `gobin` Package

- [ ] Keep `gobin` package as-is for Go-specific analysis
- [ ] Scanner calls into `gobin` during `Scan` phase

#### 5.3 Remove Old Files

- [ ] Delete `manager.go`, `attach.go`, `scan.go` after migration complete

---

### Phase 6: Migrate JavaSSL Probe (qpoint repo)

#### 6.1 Create `internal/tap/tls/javassl/scanner.go`

- [ ] `JavaSSLScanner` implementing `Scanner`
- [ ] `Detect`: Check exe regex or linked libs
- [ ] `Scan`: Return minimal result (Java detection is mostly runtime)
- [ ] `Attach`: Install agent, inject into JVM, attach uprobes to bridge

#### 6.2 Keep SSLEngine Manager

- [ ] `SslEngineManager` handles runtime correlation (unchanged)
- [ ] Scanner's `Attach` creates and returns a detacher that wraps cleanup

#### 6.3 Remove Old Files

- [ ] Delete `manager.go`, `target.go` after migration complete

---

### Phase 7: Testing & Cleanup

#### 7.1 Unit Tests

- [ ] Test `ParsedBinary` parsing and thread-safety
- [ ] Test each scanner's `Detect`, `Scan`, `Attach` independently
- [ ] Test orchestrator concurrent execution
- [ ] Test cache hit/miss behavior

#### 7.2 Integration Tests

- [ ] End-to-end test with real binaries
- [ ] Concurrent process startup tests
- [ ] Process exit during scan tests

#### 7.3 Remove Legacy Code

- [ ] Delete old manager files
- [ ] Remove deprecated types
- [ ] Update any external references

---

## Final Package Structure

```
pkg/
├── binutils/
│   ├── elf.go           # Existing (with thread-safety fixes)
│   ├── parsed.go        # NEW: ParsedBinary type
│   └── hash.go          # NEW: Binary hashing
│
├── ebpf/
│   └── tls/
│       ├── scanner.go       # NEW: Scanner interface + ScanResult + Detacher
│       ├── cache.go         # NEW: ScanCache (L1 only initially)
│       ├── orchestrator.go  # NEW: Orchestrator
│       ├── manager.go       # UPDATED: Wraps orchestrator
│       │
│       └── openssl/
│           └── scanner.go   # NEW: OpenSSLScanner

internal/tap/tls/  (qpoint repo)
├── nodetls/
│   └── scanner.go   # NEW: NodeTLSScanner
│
├── gotls/
│   ├── scanner.go   # NEW: GoTLSScanner
│   └── gobin/       # KEEP: Go binary analysis
│
└── javassl/
    ├── scanner.go   # NEW: JavaSSLScanner
    └── engine.go    # KEEP: SSLEngine correlation
```

---

## Migration Strategy

1. **Phase 1-2**: Can be done first, no breaking changes
2. **Phase 3**: Migrate OpenSSL, keep old interface working via adapter
3. **Phase 4-6**: Migrate remaining probes one at a time
4. **Phase 7**: Remove adapters and legacy code

Each phase can be merged independently. Probes can coexist during migration.

---

## Follow-Up Items

- [ ] **On-disk cache (L2)**: Add persistent cache layer for scan results that survives restarts
- [ ] **Container image caching**: Add image-digest-based caching for shared library detection (many containers use identical base images)
- [ ] **Enhanced metrics**: Add scan duration histograms, cache hit rates, concurrent scan gauges (see metrics comment above)
- [ ] **Remove exec-based Node version detection**: Rely solely on binary string search for better reliability

---

## Technical Notes

### Thread Safety in stdlib `debug/elf`

| Method             | Thread-Safe | Reason                             |
| ------------------ | ----------- | ---------------------------------- |
| `Symbols()`        | ✅ Yes      | Only reads, fresh allocations      |
| `DynamicSymbols()` | ❌ **No**   | Lazy init of `gnuNeed`/`gnuVersym` |
| `Section(name)`    | ✅ Yes      | Just returns pointer from slice    |
| `Section.Data()`   | ✅ Yes      | Fresh allocation via `ReadAt`      |

**Solution**: Call `DynamicSymbols()` once during `ParsedBinary` construction (single-threaded), then concurrent access to the cached results is safe.

### File Descriptor Lifecycle

Keeping an FD open prevents the inode from being garbage collected:

```go
fd, _ := os.Open(procPath)  // Open FD
// ... process can exit here ...
// ... container can be deleted ...
ef, _ := elf.NewFile(fd)    // Still works! FD keeps inode alive
```

This is the key insight that allows scanning to continue even after a process exits.

### Concurrent Attachment Safety

Uprobe attachment via `link.OpenExecutable()` + `Uprobe()` is thread-safe at the kernel level. Each call creates independent state, so multiple probes can attach concurrently without synchronization.
