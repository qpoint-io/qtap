# Adding Protocol Support to qtap

This guide provides comprehensive instructions for adding new protocol support to qtap. It is designed to be used by agentic LLMs with human engineer oversight.

## Scope

**This guide covers:**
- Protocol detection in the BPF layer (kernel space)
- Protocol parsing in the Go layer (user space)
- Logging extracted protocol data to stdout

**Out of scope (covered in separate guides):**
- Plugin/stack integration
- Event type extensions
- Metrics, reporting, or other downstream integrations

The end goal of implementing protocol support using this guide is to **detect the protocol and log parsed data to stdout**.

### Build & Verification Constraints

**Important for agentic LLMs implementing this guide:**

- **DO NOT** attempt to compile or verify end-to-end functionality unless explicitly permitted by the human operator
- The build process is complex and requires a custom environment outside the coding agent's scope
- **Only unit tests can be run automatically**: `go test ./pkg/stream/protocols/<name>/...`
- Defer final implementation verification to the human operator
- The human operator will handle BPF compilation and integration testing

---

## Architecture Overview

qtap uses eBPF to tap into network connections and identify/parse application-layer protocols. The architecture has four main layers:

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         KERNEL SPACE (BPF)                              │
├─────────────────────────────────────────────────────────────────────────┤
│  Socket Syscalls ──► protocol.bpf.c ──► detect_protocol() ──► Ring Buffer
│                      (Pattern Match)      (ProtocolEvent)               │
│                                                                         │
│  Data Read/Write ─────────────────────────────────────────► Ring Buffer │
│                                                  (DataEvent)            │
└──────────────────────────────────┬──────────────────────────────────────┘
                                   │
                                   ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                          USER SPACE (Go)                                │
├─────────────────────────────────────────────────────────────────────────┤
│  socket/socket.go ──► connection/manager.go ──► stream/factory.go       │
│  (BPF↔Go Mapping)     (Event Routing)          (Parser Selection)       │
│                                                        │                │
│                                                        ▼                │
│                                         stream/protocols/<proto>/       │
│                                         (Protocol-Specific Parsing)     │
│                                                        │                │
│                                                        ▼                │
│                                                 stdout (logging)        │
└─────────────────────────────────────────────────────────────────────────┘
```

### Data Flow

1. **BPF Layer**: Intercepts socket syscalls, peeks at first bytes to detect protocol
2. **Ring Buffer**: Sends `ProtocolEvent` and `DataEvent` to userspace
3. **Go Bridge**: Maps BPF protocol constants to Go types
4. **Connection Manager**: Routes events to the appropriate connection
5. **Stream Factory**: Creates protocol-specific parsers based on detected protocol
6. **Parser**: Parses protocol data and logs extracted information

---

## Pre-Implementation: TLS/Encryption Analysis

**Before implementing protocol support, analyze how client libraries handle encryption.**

qtap intercepts encrypted traffic by attaching to standard TLS/SSL libraries at the point where data is encrypted/decrypted. This gives access to plaintext data streams. However, this only works if the protocol's client libraries use supported encryption methods.

### Supported TLS/SSL Libraries

qtap has comprehensive support for tapping into these encryption libraries:

| Library | Language/Platform | Status |
|---------|-------------------|--------|
| OpenSSL | C/C++, Python, Ruby, PHP, etc. | Supported |
| Java SSL (JSSE) | Java applications | Supported |
| Go crypto/tls | Go applications | Supported |
| Node.js TLS | Node.js applications | Supported |

### Planning Questions

When adding a new protocol, answer these questions:

**1. Does the protocol use TLS/SSL for encryption?**
- Yes → Continue to question 2
- No (plaintext only) → No encryption concerns, proceed with implementation
- No (custom encryption) → See "Custom Encryption" below

**2. What TLS libraries do popular client implementations use?**

Research the major client libraries for your protocol (example):

| Protocol | Client Library | TLS Implementation | qtap Support |
|----------|----------------|-------------------|--------------|
| Redis | redis-cli | OpenSSL | ✅ Supported |
| Redis | Jedis (Java) | Java SSL | ✅ Supported |
| Redis | go-redis | Go crypto/tls | ✅ Supported |
| MySQL | mysql-client | OpenSSL | ✅ Supported |
| MySQL | JDBC | Java SSL | ✅ Supported |
| PostgreSQL | libpq | OpenSSL | ✅ Supported |

**3. Are there any proprietary or custom encryption implementations?**

Some protocols or client libraries implement their own encryption:
- Custom TLS implementations
- Application-layer encryption (e.g., end-to-end encryption)
- Proprietary protocols with built-in encryption

### Custom Encryption Handling

If a client library uses custom or unsupported encryption:

1. **Document the limitation** in your implementation
2. **Flag for engineering review** - Additional TLS attachment work may be needed
3. **Consider scope** - The protocol may work for plaintext connections only initially

**Example documentation for a protocol with mixed support:**

```
## TLS Support for MyProtocol

| Client Library | TLS Method | qtap Support |
|----------------|------------|--------------|
| official-cli   | OpenSSL    | ✅ Full support |
| myproto-java   | Java SSL   | ✅ Full support |
| myproto-rust   | rustls     | ⚠️ Not yet supported |

Note: Rust applications using rustls will not have TLS traffic decrypted.
Plaintext connections work for all clients.
```

### Research Checklist

Before implementing, complete this research:

- [ ] Identify if the protocol supports/requires TLS
- [ ] List major client libraries for the protocol
- [ ] Determine TLS implementation for each major client
- [ ] Check if qtap supports those TLS implementations (see supported list above)
- [ ] Document any unsupported encryption methods
- [ ] Flag for engineering review if custom TLS work is needed

---

## Implementation Paths

There are two paths for adding protocol support:

| Path | Description | Example | When to Use |
|------|-------------|---------|-------------|
| **A: Detection-Only** | Identify the protocol without parsing | MongoDB | Metrics, filtering, routing decisions |
| **B: Full Parsing** | Detect and parse protocol data | DNS, HTTP | Need to inspect/extract protocol data |

---

## Path A: Detection-Only Implementation

Use this path when you only need to identify traffic type without parsing the contents.

### Step 1: Add BPF Protocol Constant

**File: `bpf/tap/tap.bpf.h`**

Add your protocol to the `PROTOCOL` enum:

```c
enum PROTOCOL {
    P_UNKNOWN,
    P_HTTP1,
    P_HTTP2,
    P_DNS,
    P_MONGODB,
    P_MYPROTOCOL,  // <-- Add here
};
```

### Step 2: Implement BPF Detection Function

**File: `bpf/tap/protocol.bpf.c`**

Create a detection function that examines the first bytes of the connection:

```c
static bool detect_myprotocol(struct conn_info *conn_info,
                              struct buf_info *buf_info, size_t count) {
    // 1. Check minimum size requirement
    if (count < MINIMUM_HEADER_SIZE || !buf_info->buf) {
        return false;
    }

    // 2. Read header bytes from buffer
    unsigned char header[16] = {0};
    if (buf_read((char *)&header, sizeof(header), buf_info, 0) == 0) {
        return false;
    }

    // 3. Check for protocol signature/magic bytes
    // Example: Check for specific byte pattern
    if (header[0] == 0xAB && header[1] == 0xCD) {
        conn_info->protocol = P_MYPROTOCOL;
        return true;
    }

    // Alternative: Check for text prefix
    if (_strncmp((char *)header, "MYPROTO", 7) == 0) {
        conn_info->protocol = P_MYPROTOCOL;
        return true;
    }

    return false;
}
```

**Detection patterns used in existing code:**

| Protocol | Detection Method | Reference |
|----------|------------------|-----------|
| DNS | Port 53 check | `is_dns()` - simple port match |
| HTTP/1 | Method prefixes (GET, POST, etc.) | `detect_http()` - string comparison |
| HTTP/2 | Preface bytes (`PRI * HTTP/2.0...`) | `detect_http()` - byte-by-byte match |
| MongoDB | Wire protocol header + valid opcode | `detect_mongodb()` - header structure validation |
| TLS | Record type 0x16, version 0x03 0x01+ | `detect_tls()` - byte pattern |

### Step 3: Add to Detection Chain

**File: `bpf/tap/protocol.bpf.c`**

Add your detection function to `detect_protocol()`:

```c
static bool detect_protocol(struct conn_info *conn_info,
                           struct buf_info *buf_info, size_t count) {
    conn_info->protocol = P_UNKNOWN;
    bool detected = false;

    // Existing detections...
    if (conn_info->protocol == P_UNKNOWN)
        detected = detect_dns(conn_info);

    if (conn_info->protocol == P_UNKNOWN)
        detected = detect_mongodb(conn_info, buf_info, count);

    if (conn_info->protocol == P_UNKNOWN)
        detected = detect_http(conn_info, buf_info, count);

    // Add your protocol detection
    if (conn_info->protocol == P_UNKNOWN)
        detected = detect_myprotocol(conn_info, buf_info, count);

    return detected;
}
```

**Note on ordering:** Place detection before HTTP if your protocol might be misdetected as HTTP (e.g., binary protocols). Place after if HTTP detection is more critical.

### Step 4: Add Go Protocol Mapping

**File: `pkg/ebpf/socket/socket.go`**

Add to the Protocol constants (must match BPF enum order):

```go
type Protocol uint32

const (
    Protocol_UNKNOWN Protocol = iota
    Protocol_HTTP1
    Protocol_HTTP2
    Protocol_DNS
    Protocol_MONGODB
    Protocol_GRPC
    Protocol_MYPROTOCOL  // <-- Add here (same position as BPF enum)
)
```

Update the `String()` method:

```go
func (p Protocol) String() string {
    switch p {
    // ... existing cases ...
    case Protocol_MYPROTOCOL:
        return "MYPROTOCOL"
    default:
        return fmt.Sprintf("BAD PROTOCOL(%d)", p)
    }
}
```

### Step 5: Add Go Connection Type

**File: `pkg/connection/types.go`**

Add the protocol constant:

```go
const (
    Protocol_UNKNOWN Protocol = "unknown"
    Protocol_HTTP1   Protocol = "http1"
    Protocol_HTTP2   Protocol = "http2"
    Protocol_DNS     Protocol = "dns"
    Protocol_MONGODB Protocol = "mongodb"
    Protocol_GRPC    Protocol = "grpc"
    Protocol_MYPROTOCOL Protocol = "myprotocol"  // <-- Add here
)
```

### Step 6: Update Protocol Mapping Function

**File: `pkg/ebpf/socket/socket.go`**

Find `buildConnectionProtocolEvent()` and add the mapping:

```go
func (e socketProtoEvent) buildConnectionProtocolEvent() connection.ProtocolEvent {
    var p connection.Protocol

    switch e.Protocol {
    case Protocol_UNKNOWN:
        p = connection.Protocol_UNKNOWN
    case Protocol_HTTP1:
        p = connection.Protocol_HTTP1
    // ... existing cases ...
    case Protocol_MYPROTOCOL:
        p = connection.Protocol_MYPROTOCOL  // <-- Add here
    }

    return connection.ProtocolEvent{
        Cookie:      connection.Cookie(e.Cookie),
        TimestampNS: e.TimestampNS,
        Protocol:    p,
        IsTLS:       e.IsTLS,
    }
}
```

### Step 7: Factory Returns nil

**File: `pkg/stream/factory.go`**

For detection-only protocols, the factory returns `nil`:

```go
func (m *StreamFactory) OnConnection(conn *connection.Connection) connection.StreamProcessor {
    // ... existing handlers ...

    // Detection-only: just log and return nil
    if conn.Protocol == connection.Protocol_MYPROTOCOL {
        logger.Debug("MyProtocol connection detected")
        return nil  // No parser needed
    }

    return nil
}
```

---

## Path B: Full Parsing Implementation

Use this path when you need to parse and extract data from the protocol.

### Steps 1-6: Same as Detection-Only

Complete all steps from Path A first.

### Step 7: Create Parser Package

Create a new directory: `pkg/stream/protocols/myprotocol/`

**File: `pkg/stream/protocols/myprotocol/stream.go`**

```go
package myprotocol

import (
    "context"
    "sync"

    "github.com/qpoint-io/qtap/pkg/connection"
    "go.uber.org/zap"
)

// Stream implements connection.StreamProcessor
type Stream struct {
    ctx    context.Context
    logger *zap.Logger
    conn   *connection.Connection
    buffer []byte
    closed bool
    mu     sync.Mutex
}

func NewStream(ctx context.Context, logger *zap.Logger,
               conn *connection.Connection) *Stream {
    return &Stream{
        ctx:    ctx,
        logger: logger.With(zap.String("protocol", "myprotocol")),
        conn:   conn,
        buffer: make([]byte, 0),
    }
}

// Process handles incoming data events
func (s *Stream) Process(event *connection.DataEvent) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    // Accumulate data
    s.buffer = append(s.buffer, event.Data...)

    // Try to parse complete messages
    s.parseMessages(event.Direction)

    return nil
}

func (s *Stream) parseMessages(direction connection.Direction) {
    // Parse protocol-specific messages from s.buffer
    // Log extracted data to stdout

    // Example:
    // for each complete message in buffer {
    //     s.logger.Info("parsed message",
    //         zap.String("direction", string(direction)),
    //         zap.String("type", messageType),
    //         zap.Any("data", parsedData),
    //     )
    //     // Remove parsed bytes from buffer
    // }
}

func (s *Stream) Close() {
    s.mu.Lock()
    defer s.mu.Unlock()

    s.logger.Debug("closing myprotocol stream")
    s.closed = true
}

func (s *Stream) Closed() bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.closed
}
```

### Step 8: Register in Factory

**File: `pkg/stream/factory.go`**

```go
import (
    // ... existing imports ...
    myprotoStream "github.com/qpoint-io/qtap/pkg/stream/protocols/myprotocol"
)

func (m *StreamFactory) OnConnection(conn *connection.Connection) connection.StreamProcessor {
    logger := conn.Logger()

    // ... existing handlers ...

    // Parse myprotocol streams
    if conn.Protocol == connection.Protocol_MYPROTOCOL {
        return myprotoStream.NewStream(conn.Context(), logger, conn)
    }

    return nil
}
```

---

## Parser Complexity Patterns

Existing parsers demonstrate three complexity levels:

### Simple/Stateless (DNS Pattern)

**Reference:** `pkg/stream/protocols/dns/stream.go`

- Buffer accumulation with simple append
- Parse complete messages when available
- No session tracking
- Direct output

```go
func (s *Stream) Process(event *connection.DataEvent) error {
    s.buffer = append(s.buffer, event.Data...)
    s.parseAndLog()
    return nil
}
```

**Best for:** Simple request/response protocols, UDP-based protocols

### Session-Based (HTTP/1 Pattern)

**Reference:** `pkg/stream/protocols/http1/`

- Request/response correlation
- Separate processing for each direction
- Transaction lifecycle tracking
- Uses pipes for streaming data

**Key files:**
- `stream.go` - Entry point, determines request vs response phase
- `session.go` - Manages single HTTP transaction
- `parser.go` - Coordinates parsing with goroutines

**Best for:** TCP protocols with clear request/response pairs

### Multiplexed (HTTP/2 Pattern)

**Reference:** `pkg/stream/protocols/http2/`

- Multiple concurrent streams over one connection
- Stream ID tracking with `map[uint32]*Session`
- Frame-based buffering
- State machine per stream

**Key pattern:**
```go
type Stream struct {
    sessions map[uint32]*Session  // One per stream ID
    buffer   []byte               // Frame accumulator
}
```

**Best for:** Multiplexed protocols (HTTP/2, gRPC, AMQP)

---

## Concrete Example: Redis RESP Protocol

This section walks through implementing Redis RESP protocol support as a complete example.

### Protocol Overview

Redis uses RESP (Redis Serialization Protocol):

```
*3\r\n$3\r\nSET\r\n$5\r\nmykey\r\n$7\r\nmyvalue\r\n   -> Array of 3 bulk strings
+OK\r\n                                              -> Simple string response
```

**Type prefixes:**
- `+` Simple string
- `-` Error
- `:` Integer
- `$` Bulk string (length-prefixed)
- `*` Array

### Step 1: Add BPF Enum

**File: `bpf/tap/tap.bpf.h`**

```c
enum PROTOCOL {
    P_UNKNOWN,
    P_HTTP1,
    P_HTTP2,
    P_DNS,
    P_MONGODB,
    P_REDIS,  // <-- Add
};
```

### Step 2: BPF Detection Function

**File: `bpf/tap/protocol.bpf.c`**

```c
// Redis RESP protocol detection
static bool detect_redis(struct conn_info *conn_info,
                         struct buf_info *buf_info, size_t count) {
    if (count < 4 || !buf_info->buf) {
        return false;
    }

    char header[8] = {0};
    if (buf_read((char *)&header, sizeof(header) - 1, buf_info, 0) == 0) {
        return false;
    }

    // Check for RESP array command (most Redis commands start with *)
    // Format: *<count>\r\n
    if (header[0] == '*' && header[1] >= '1' && header[1] <= '9') {
        conn_info->protocol = P_REDIS;
        return true;
    }

    // Check for RESP response types
    if (header[0] == '+' || header[0] == '-' ||
        header[0] == ':' || header[0] == '$') {
        conn_info->protocol = P_REDIS;
        return true;
    }

    // Check for inline commands (PING, QUIT, etc.)
    if (_strncmp(header, "PING", 4) == 0 ||
        _strncmp(header, "QUIT", 4) == 0 ||
        _strncmp(header, "INFO", 4) == 0) {
        conn_info->protocol = P_REDIS;
        return true;
    }

    return false;
}
```

### Step 3: Add to Detection Chain

**File: `bpf/tap/protocol.bpf.c`**

```c
static bool detect_protocol(struct conn_info *conn_info,
                           struct buf_info *buf_info, size_t count) {
    conn_info->protocol = P_UNKNOWN;
    bool detected = false;

    if (conn_info->protocol == P_UNKNOWN)
        detected = detect_dns(conn_info);

    if (conn_info->protocol == P_UNKNOWN)
        detected = detect_mongodb(conn_info, buf_info, count);

    // Add Redis before HTTP (binary-safe, distinct signatures)
    if (conn_info->protocol == P_UNKNOWN)
        detected = detect_redis(conn_info, buf_info, count);

    if (conn_info->protocol == P_UNKNOWN)
        detected = detect_http(conn_info, buf_info, count);

    return detected;
}
```

### Step 4: Go Protocol Constants

**File: `pkg/ebpf/socket/socket.go`**

```go
const (
    Protocol_UNKNOWN Protocol = iota
    Protocol_HTTP1
    Protocol_HTTP2
    Protocol_DNS
    Protocol_MONGODB
    Protocol_GRPC
    Protocol_REDIS  // <-- Must match BPF enum position
)

func (p Protocol) String() string {
    switch p {
    // ... existing ...
    case Protocol_REDIS:
        return "REDIS"
    default:
        return fmt.Sprintf("BAD PROTOCOL(%d)", p)
    }
}
```

**File: `pkg/connection/types.go`**

```go
const (
    // ... existing ...
    Protocol_REDIS Protocol = "redis"
)
```

### Step 5: Update Mapping

**File: `pkg/ebpf/socket/socket.go`**

```go
func (e socketProtoEvent) buildConnectionProtocolEvent() connection.ProtocolEvent {
    var p connection.Protocol
    switch e.Protocol {
    // ... existing ...
    case Protocol_REDIS:
        p = connection.Protocol_REDIS
    }
    // ...
}
```

### Step 6: Create Redis Parser

**File: `pkg/stream/protocols/redis/stream.go`**

```go
package redis

import (
    "bytes"
    "context"
    "strconv"
    "sync"

    "github.com/qpoint-io/qtap/pkg/connection"
    "go.uber.org/zap"
)

type Stream struct {
    ctx    context.Context
    logger *zap.Logger
    conn   *connection.Connection
    buffer []byte
    closed bool
    mu     sync.Mutex
}

func NewStream(ctx context.Context, logger *zap.Logger,
               conn *connection.Connection) *Stream {
    return &Stream{
        ctx:    ctx,
        logger: logger.With(zap.String("protocol", "redis")),
        conn:   conn,
        buffer: make([]byte, 0),
    }
}

func (s *Stream) Process(event *connection.DataEvent) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    s.buffer = append(s.buffer, event.Data...)
    s.parseMessages(event.Direction)
    return nil
}

func (s *Stream) parseMessages(direction connection.Direction) {
    for {
        if len(s.buffer) == 0 {
            return
        }

        // Parse based on first byte (RESP type)
        switch s.buffer[0] {
        case '*': // Array (command)
            consumed := s.parseArray(direction)
            if consumed == 0 {
                return // Incomplete
            }
            s.buffer = s.buffer[consumed:]

        case '+': // Simple string
            consumed := s.parseSimpleString(direction)
            if consumed == 0 {
                return
            }
            s.buffer = s.buffer[consumed:]

        case '-': // Error
            consumed := s.parseError(direction)
            if consumed == 0 {
                return
            }
            s.buffer = s.buffer[consumed:]

        case ':': // Integer
            consumed := s.parseInteger(direction)
            if consumed == 0 {
                return
            }
            s.buffer = s.buffer[consumed:]

        case '$': // Bulk string
            consumed := s.parseBulkString(direction)
            if consumed == 0 {
                return
            }
            s.buffer = s.buffer[consumed:]

        default:
            // Unknown or inline command - skip to next line
            if idx := bytes.Index(s.buffer, []byte("\r\n")); idx >= 0 {
                line := string(s.buffer[:idx])
                s.logger.Debug("redis inline",
                    zap.String("direction", string(direction)),
                    zap.String("line", line))
                s.buffer = s.buffer[idx+2:]
            } else {
                return // Incomplete
            }
        }
    }
}

func (s *Stream) parseArray(direction connection.Direction) int {
    // Find end of count line
    idx := bytes.Index(s.buffer, []byte("\r\n"))
    if idx < 0 {
        return 0
    }

    // Parse array count
    count, err := strconv.Atoi(string(s.buffer[1:idx]))
    if err != nil {
        return idx + 2 // Skip malformed
    }

    // Parse array elements
    pos := idx + 2
    elements := make([]string, 0, count)

    for i := 0; i < count; i++ {
        if pos >= len(s.buffer) {
            return 0 // Incomplete
        }

        if s.buffer[pos] != '$' {
            return 0 // Expected bulk string
        }

        // Find length line end
        endIdx := bytes.Index(s.buffer[pos:], []byte("\r\n"))
        if endIdx < 0 {
            return 0
        }

        // Parse length
        length, err := strconv.Atoi(string(s.buffer[pos+1 : pos+endIdx]))
        if err != nil {
            return 0
        }

        pos += endIdx + 2 // Move past length line

        // Check if we have full string
        if pos+length+2 > len(s.buffer) {
            return 0 // Incomplete
        }

        elements = append(elements, string(s.buffer[pos:pos+length]))
        pos += length + 2 // Move past string and \r\n
    }

    // Log the parsed command
    if len(elements) > 0 {
        cmd := elements[0]
        args := elements[1:]
        s.logger.Info("redis command",
            zap.String("direction", string(direction)),
            zap.String("command", cmd),
            zap.Strings("args", args))
    }

    return pos
}

func (s *Stream) parseSimpleString(direction connection.Direction) int {
    idx := bytes.Index(s.buffer, []byte("\r\n"))
    if idx < 0 {
        return 0
    }

    value := string(s.buffer[1:idx])
    s.logger.Info("redis response",
        zap.String("direction", string(direction)),
        zap.String("type", "simple_string"),
        zap.String("value", value))

    return idx + 2
}

func (s *Stream) parseError(direction connection.Direction) int {
    idx := bytes.Index(s.buffer, []byte("\r\n"))
    if idx < 0 {
        return 0
    }

    value := string(s.buffer[1:idx])
    s.logger.Info("redis response",
        zap.String("direction", string(direction)),
        zap.String("type", "error"),
        zap.String("value", value))

    return idx + 2
}

func (s *Stream) parseInteger(direction connection.Direction) int {
    idx := bytes.Index(s.buffer, []byte("\r\n"))
    if idx < 0 {
        return 0
    }

    value := string(s.buffer[1:idx])
    s.logger.Info("redis response",
        zap.String("direction", string(direction)),
        zap.String("type", "integer"),
        zap.String("value", value))

    return idx + 2
}

func (s *Stream) parseBulkString(direction connection.Direction) int {
    idx := bytes.Index(s.buffer, []byte("\r\n"))
    if idx < 0 {
        return 0
    }

    length, err := strconv.Atoi(string(s.buffer[1:idx]))
    if err != nil {
        return idx + 2
    }

    // Null bulk string
    if length < 0 {
        s.logger.Info("redis response",
            zap.String("direction", string(direction)),
            zap.String("type", "bulk_string"),
            zap.String("value", "(nil)"))
        return idx + 2
    }

    pos := idx + 2
    if pos+length+2 > len(s.buffer) {
        return 0 // Incomplete
    }

    value := string(s.buffer[pos : pos+length])
    s.logger.Info("redis response",
        zap.String("direction", string(direction)),
        zap.String("type", "bulk_string"),
        zap.String("value", value))

    return pos + length + 2
}

func (s *Stream) Close() {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.logger.Debug("closing redis stream")
    s.closed = true
}

func (s *Stream) Closed() bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.closed
}
```

### Step 7: Register in Factory

**File: `pkg/stream/factory.go`**

```go
import (
    // ... existing imports ...
    redisStream "github.com/qpoint-io/qtap/pkg/stream/protocols/redis"
)

func (m *StreamFactory) OnConnection(conn *connection.Connection) connection.StreamProcessor {
    logger := conn.Logger()

    // ... existing handlers ...

    // Parse redis streams
    if conn.Protocol == connection.Protocol_REDIS {
        return redisStream.NewStream(conn.Context(), logger, conn)
    }

    return nil
}
```

### Expected Output

With the Redis parser in place, you should see output like:

```
{"level":"info","msg":"redis command","protocol":"redis","direction":"egress","command":"SET","args":["user:123","{\"name\":\"alice\"}"]}
{"level":"info","msg":"redis response","protocol":"redis","direction":"ingress","type":"simple_string","value":"OK"}
{"level":"info","msg":"redis command","protocol":"redis","direction":"egress","command":"GET","args":["user:123"]}
{"level":"info","msg":"redis response","protocol":"redis","direction":"ingress","type":"bulk_string","value":"{\"name\":\"alice\"}"}
```

---

## Quick Reference

### Files to Modify

| Layer | File | Purpose |
|-------|------|---------|
| BPF | `bpf/tap/tap.bpf.h` | Add `P_<PROTOCOL>` to enum |
| BPF | `bpf/tap/protocol.bpf.c` | Implement `detect_<protocol>()` |
| Go | `pkg/ebpf/socket/socket.go` | Add Protocol constant + mapping |
| Go | `pkg/connection/types.go` | Add Protocol string constant |
| Go | `pkg/stream/factory.go` | Register parser |
| Go | `pkg/stream/protocols/<name>/` | Parser implementation |

### Implementation Checklist

**Pre-Implementation Research:**
- [ ] Identify if the protocol supports/requires TLS
- [ ] List major client libraries for the protocol
- [ ] Determine TLS implementation for each major client
- [ ] Check if qtap supports those TLS implementations (see supported list in TLS section)
- [ ] Document any unsupported encryption methods
- [ ] Flag for engineering review if custom TLS work is needed

**BPF Layer:**
- [ ] Add `P_<PROTOCOL>` to `enum PROTOCOL` in `tap.bpf.h`
- [ ] Implement `detect_<protocol>()` in `protocol.bpf.c`
- [ ] Add to `detect_protocol()` chain with appropriate ordering

**Go Bridge:**
- [ ] Add `Protocol_<PROTOCOL>` to `socket.go` constants (must match BPF enum order)
- [ ] Update `Protocol.String()` in `socket.go`
- [ ] Add `Protocol_<PROTOCOL>` to `types.go` string constants
- [ ] Update `buildConnectionProtocolEvent()` switch statement

**Parser (Full Parsing Only):**
- [ ] Create `pkg/stream/protocols/<name>/` directory
- [ ] Implement `stream.go` with `StreamProcessor` interface
- [ ] Create `stream_test.go` with comprehensive unit tests
- [ ] Achieve 80%+ test coverage
- [ ] Register in `factory.go`

**Testing Infrastructure:**
- [ ] Create `apps/<protocol>/` directory structure
- [ ] Create `docker-compose.yml` with shared service + client apps
- [ ] Create `Makefile` with standard targets (up, down, logs, test, clean)
- [ ] Implement Ruby, Go, Java, Node.js client apps

### StreamProcessor Interface

```go
type StreamProcessor interface {
    Process(event *DataEvent) error  // Handle incoming data
    Close()                          // Cleanup resources
    Closed() bool                    // Check if closed
}
```

### Helper Functions in BPF

| Function | Purpose |
|----------|---------|
| `buf_read(dst, size, buf_info, offset)` | Read bytes from buffer |
| `_strncmp(s1, s2, n)` | Compare strings |
| `__bpf_htons(port)` | Convert port to network byte order |

---

## Unit Testing

**All parser implementations must have complete unit tests.** This section provides patterns and examples for testing your protocol parser.

### Test File Structure

Create a test file alongside your parser:

```
pkg/stream/protocols/redis/
├── stream.go       # Parser implementation
└── stream_test.go  # Unit tests
```

### Testing Pattern: Table-Driven Tests

Use table-driven tests to cover multiple scenarios efficiently:

**File: `pkg/stream/protocols/redis/stream_test.go`**

```go
package redis

import (
    "context"
    "testing"

    "github.com/qpoint-io/qtap/pkg/connection"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "go.uber.org/zap"
    "go.uber.org/zap/zaptest/observer"
)

// createTestStream creates a stream with an observable logger
func createTestStream(t *testing.T) (*Stream, *observer.ObservedLogs) {
    core, logs := observer.New(zap.DebugLevel)
    logger := zap.New(core)

    stream := &Stream{
        ctx:    context.Background(),
        logger: logger.With(zap.String("protocol", "redis")),
        buffer: make([]byte, 0),
    }

    return stream, logs
}

func TestParseSimpleString(t *testing.T) {
    tests := []struct {
        name          string
        input         string
        wantConsumed  int
        wantValue     string
        wantIncomplete bool
    }{
        {
            name:         "ok response",
            input:        "+OK\r\n",
            wantConsumed: 5,
            wantValue:    "OK",
        },
        {
            name:         "pong response",
            input:        "+PONG\r\n",
            wantConsumed: 7,
            wantValue:    "PONG",
        },
        {
            name:           "incomplete - no terminator",
            input:          "+OK",
            wantConsumed:   0,
            wantIncomplete: true,
        },
        {
            name:           "incomplete - partial terminator",
            input:          "+OK\r",
            wantConsumed:   0,
            wantIncomplete: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            stream, logs := createTestStream(t)
            stream.buffer = []byte(tt.input)

            consumed := stream.parseSimpleString(connection.Ingress)

            assert.Equal(t, tt.wantConsumed, consumed)

            if !tt.wantIncomplete {
                // Verify logged output
                entries := logs.All()
                require.Len(t, entries, 1)
                assert.Equal(t, tt.wantValue, entries[0].ContextMap()["value"])
            }
        })
    }
}

func TestParseArray(t *testing.T) {
    tests := []struct {
        name         string
        input        string
        wantConsumed int
        wantCommand  string
        wantArgs     []string
    }{
        {
            name:         "SET command",
            input:        "*3\r\n$3\r\nSET\r\n$5\r\nmykey\r\n$7\r\nmyvalue\r\n",
            wantConsumed: 39,
            wantCommand:  "SET",
            wantArgs:     []string{"mykey", "myvalue"},
        },
        {
            name:         "GET command",
            input:        "*2\r\n$3\r\nGET\r\n$5\r\nmykey\r\n",
            wantConsumed: 24,
            wantCommand:  "GET",
            wantArgs:     []string{"mykey"},
        },
        {
            name:         "PING command (no args)",
            input:        "*1\r\n$4\r\nPING\r\n",
            wantConsumed: 14,
            wantCommand:  "PING",
            wantArgs:     []string{},
        },
        {
            name:         "incomplete array",
            input:        "*3\r\n$3\r\nSET\r\n",
            wantConsumed: 0, // Incomplete, need more data
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            stream, logs := createTestStream(t)
            stream.buffer = []byte(tt.input)

            consumed := stream.parseArray(connection.Egress)

            assert.Equal(t, tt.wantConsumed, consumed)

            if tt.wantConsumed > 0 {
                entries := logs.All()
                require.Len(t, entries, 1)
                assert.Equal(t, tt.wantCommand, entries[0].ContextMap()["command"])
            }
        })
    }
}

func TestParseBulkString(t *testing.T) {
    tests := []struct {
        name         string
        input        string
        wantConsumed int
        wantValue    string
    }{
        {
            name:         "simple bulk string",
            input:        "$5\r\nhello\r\n",
            wantConsumed: 11,
            wantValue:    "hello",
        },
        {
            name:         "null bulk string",
            input:        "$-1\r\n",
            wantConsumed: 5,
            wantValue:    "(nil)",
        },
        {
            name:         "empty bulk string",
            input:        "$0\r\n\r\n",
            wantConsumed: 6,
            wantValue:    "",
        },
        {
            name:         "incomplete - no data",
            input:        "$5\r\n",
            wantConsumed: 0,
        },
        {
            name:         "incomplete - partial data",
            input:        "$5\r\nhel",
            wantConsumed: 0,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            stream, logs := createTestStream(t)
            stream.buffer = []byte(tt.input)

            consumed := stream.parseBulkString(connection.Ingress)

            assert.Equal(t, tt.wantConsumed, consumed)

            if tt.wantConsumed > 0 {
                entries := logs.All()
                require.Len(t, entries, 1)
                assert.Equal(t, tt.wantValue, entries[0].ContextMap()["value"])
            }
        })
    }
}
```

### Testing Partial Data / Chunked Input

Network data often arrives in chunks. Test that your parser handles partial data correctly:

```go
func TestChunkedInput(t *testing.T) {
    stream, logs := createTestStream(t)

    // Simulate data arriving in chunks
    chunks := []string{
        "*3\r\n$3",        // Partial array
        "\r\nSET\r\n$5",   // More of array
        "\r\nmykey\r\n$7", // Continue
        "\r\nmyvalue\r\n", // Complete
    }

    for _, chunk := range chunks {
        event := &connection.DataEvent{
            Direction: connection.Egress,
            Data:      []byte(chunk),
        }
        err := stream.Process(event)
        require.NoError(t, err)
    }

    // Should have logged one complete command
    entries := logs.All()
    require.Len(t, entries, 1)
    assert.Equal(t, "SET", entries[0].ContextMap()["command"])
}
```

### Testing Multiple Messages in One Event

Test that your parser handles multiple complete messages in a single data event:

```go
func TestMultipleMessages(t *testing.T) {
    stream, logs := createTestStream(t)

    // Multiple responses in one buffer
    input := "+OK\r\n+PONG\r\n:1000\r\n"

    event := &connection.DataEvent{
        Direction: connection.Ingress,
        Data:      []byte(input),
    }
    err := stream.Process(event)
    require.NoError(t, err)

    // Should have logged three responses
    entries := logs.All()
    assert.Len(t, entries, 3)
}
```

### Testing Direction Handling

Verify that request vs response direction is handled correctly:

```go
func TestDirectionHandling(t *testing.T) {
    stream, logs := createTestStream(t)

    // Request (egress)
    reqEvent := &connection.DataEvent{
        Direction: connection.Egress,
        Data:      []byte("*2\r\n$3\r\nGET\r\n$3\r\nfoo\r\n"),
    }
    stream.Process(reqEvent)

    // Response (ingress)
    respEvent := &connection.DataEvent{
        Direction: connection.Ingress,
        Data:      []byte("$3\r\nbar\r\n"),
    }
    stream.Process(respEvent)

    entries := logs.All()
    require.Len(t, entries, 2)

    // Verify directions are logged correctly
    assert.Equal(t, "egress", entries[0].ContextMap()["direction"])
    assert.Equal(t, "ingress", entries[1].ContextMap()["direction"])
}
```

### Testing Edge Cases

Always test edge cases and error conditions:

```go
func TestEdgeCases(t *testing.T) {
    tests := []struct {
        name  string
        input string
    }{
        {"empty buffer", ""},
        {"malformed array count", "*abc\r\n"},
        {"negative array count", "*-1\r\n"},
        {"binary data in bulk string", "$4\r\n\x00\x01\x02\x03\r\n"},
        {"very long string", "$1000\r\n" + string(make([]byte, 1000)) + "\r\n"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            stream, _ := createTestStream(t)
            event := &connection.DataEvent{
                Direction: connection.Ingress,
                Data:      []byte(tt.input),
            }

            // Should not panic
            err := stream.Process(event)
            assert.NoError(t, err)
        })
    }
}
```

### Testing Close/Cleanup

Verify proper cleanup behavior:

```go
func TestCloseStream(t *testing.T) {
    stream, _ := createTestStream(t)

    assert.False(t, stream.Closed())

    stream.Close()

    assert.True(t, stream.Closed())

    // Processing after close should be handled gracefully
    // (implementation dependent - may no-op or return error)
}
```

### Test Coverage Requirements

For parser implementations, aim for:

- **Line coverage:** 80%+ of parser code
- **Branch coverage:** All conditional branches tested
- **Edge cases:** Empty input, malformed data, partial data, very large data

Run tests with coverage:

```bash
go test -cover ./pkg/stream/protocols/redis/
go test -coverprofile=coverage.out ./pkg/stream/protocols/redis/
go tool cover -html=coverage.out  # View coverage report
```

### Implementation Checklist Update

**Parser (Full Parsing Only):**
- [ ] Create `pkg/stream/protocols/<name>/` directory
- [ ] Implement `stream.go` with `StreamProcessor` interface
- [ ] **Create `stream_test.go` with comprehensive unit tests**
- [ ] **Test complete messages parsing**
- [ ] **Test incomplete/chunked data handling**
- [ ] **Test multiple messages in single event**
- [ ] **Test edge cases and error conditions**
- [ ] **Achieve 80%+ test coverage**
- [ ] Register in `factory.go`

---

## Integration Testing

**Note:** Integration testing requires the full build environment and should be performed by the human operator. The agentic LLM should NOT attempt these steps unless explicitly permitted.

### BPF Detection Testing

Use `bpf_printk` for debugging (uncomment existing debug lines in `protocol.bpf.c`):

```c
// bpf_printk("detect_myproto: header[0]=%x header[1]=%x", header[0], header[1]);
```

View output with:
```bash
sudo cat /sys/kernel/debug/tracing/trace_pipe
```

### End-to-End Testing (Human Operator)

The following steps are performed by the human operator and results reported back to the agent:

1. Operator runs qtap with debug logging enabled
2. Operator generates protocol traffic (e.g., `redis-cli` for Redis)
3. Operator verifies parsed output in logs and reports results

### DevTools Testing (Human Operator)

The human operator can use qtap's devtools to observe connection events and verify protocol detection, reporting any issues back to the agent for resolution.

---

## Testing Infrastructure

Each protocol implementation requires a test environment with sample applications to verify the parser works correctly. This section describes how to set up the testing infrastructure.

**Note:** Setting up and running the test infrastructure requires Docker and should be coordinated with the human operator. The agentic LLM should create the files but defer actual execution to the human operator.

### Directory Structure

Test applications live at the repository root under `/apps/<protocol>/`:

```
apps/
└── redis/
    ├── docker-compose.yml    # Orchestrates service + all client apps
    ├── Makefile              # Commands to start/stop containers
    ├── ruby/
    │   ├── Dockerfile
    │   ├── Gemfile
    │   └── app.rb
    ├── go/
    │   ├── Dockerfile
    │   ├── go.mod
    │   └── main.go
    ├── java/
    │   ├── Dockerfile
    │   ├── pom.xml
    │   └── src/main/java/App.java
    └── node/
        ├── Dockerfile
        ├── package.json
        └── app.js
```

### Docker Compose Configuration

Use a shared service instance with multiple client containers:

**File: `apps/redis/docker-compose.yml`**

```yaml
version: '3.8'

services:
  # Shared Redis server
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5

  # Ruby client
  ruby-client:
    build: ./ruby
    depends_on:
      redis:
        condition: service_healthy
    environment:
      - REDIS_HOST=redis
      - REDIS_PORT=6379
      - MAX_ITERATIONS=${MAX_ITERATIONS:-0}
      - SLEEP_DURATION=${SLEEP_DURATION:-1}

  # Go client
  go-client:
    build: ./go
    depends_on:
      redis:
        condition: service_healthy
    environment:
      - REDIS_HOST=redis
      - REDIS_PORT=6379
      - MAX_ITERATIONS=${MAX_ITERATIONS:-0}
      - SLEEP_DURATION=${SLEEP_DURATION:-1}

  # Java client
  java-client:
    build: ./java
    depends_on:
      redis:
        condition: service_healthy
    environment:
      - REDIS_HOST=redis
      - REDIS_PORT=6379
      - MAX_ITERATIONS=${MAX_ITERATIONS:-0}
      - SLEEP_DURATION=${SLEEP_DURATION:-1}

  # Node.js client
  node-client:
    build: ./node
    depends_on:
      redis:
        condition: service_healthy
    environment:
      - REDIS_HOST=redis
      - REDIS_PORT=6379
      - MAX_ITERATIONS=${MAX_ITERATIONS:-0}
      - SLEEP_DURATION=${SLEEP_DURATION:-1}
```

### Environment Variables

All client apps should respect these environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `<PROTO>_HOST` | Service hostname | `localhost` |
| `<PROTO>_PORT` | Service port | Protocol default |
| `MAX_ITERATIONS` | Number of iterations (0 = infinite) | `0` |
| `SLEEP_DURATION` | Seconds between iterations | `1` |

### Sample App Template (Ruby)

**File: `apps/redis/ruby/app.rb`**

```ruby
require 'redis'

host = ENV.fetch('REDIS_HOST', 'localhost')
port = ENV.fetch('REDIS_PORT', 6379).to_i
max_iterations = ENV.fetch('MAX_ITERATIONS', 0).to_i
sleep_duration = ENV.fetch('SLEEP_DURATION', 1).to_f

redis = Redis.new(host: host, port: port)
iteration = 0

loop do
  iteration += 1
  puts "[Ruby] Iteration #{iteration}"

  # Basic operations
  redis.set("ruby:key:#{iteration}", "value-#{iteration}")
  redis.get("ruby:key:#{iteration}")

  # List operations
  redis.lpush("ruby:list", "item-#{iteration}")
  redis.lrange("ruby:list", 0, -1)

  # Hash operations
  redis.hset("ruby:hash", "field-#{iteration}", "value-#{iteration}")
  redis.hgetall("ruby:hash")

  # Pub/sub (publish only)
  redis.publish("ruby:channel", "message-#{iteration}")

  break if max_iterations > 0 && iteration >= max_iterations
  sleep(sleep_duration)
end

puts "[Ruby] Completed #{iteration} iterations"
```

### Sample App Template (Go)

**File: `apps/redis/go/main.go`**

```go
package main

import (
    "context"
    "fmt"
    "os"
    "strconv"
    "time"

    "github.com/redis/go-redis/v9"
)

func main() {
    host := getEnv("REDIS_HOST", "localhost")
    port := getEnv("REDIS_PORT", "6379")
    maxIterations := getEnvInt("MAX_ITERATIONS", 0)
    sleepDuration := getEnvFloat("SLEEP_DURATION", 1.0)

    client := redis.NewClient(&redis.Options{
        Addr: fmt.Sprintf("%s:%s", host, port),
    })
    ctx := context.Background()

    iteration := 0
    for {
        iteration++
        fmt.Printf("[Go] Iteration %d\n", iteration)

        // Basic operations
        key := fmt.Sprintf("go:key:%d", iteration)
        client.Set(ctx, key, fmt.Sprintf("value-%d", iteration), 0)
        client.Get(ctx, key)

        // List operations
        client.LPush(ctx, "go:list", fmt.Sprintf("item-%d", iteration))
        client.LRange(ctx, "go:list", 0, -1)

        // Hash operations
        client.HSet(ctx, "go:hash", fmt.Sprintf("field-%d", iteration), fmt.Sprintf("value-%d", iteration))
        client.HGetAll(ctx, "go:hash")

        // Pub/sub (publish only)
        client.Publish(ctx, "go:channel", fmt.Sprintf("message-%d", iteration))

        if maxIterations > 0 && iteration >= maxIterations {
            break
        }
        time.Sleep(time.Duration(sleepDuration * float64(time.Second)))
    }

    fmt.Printf("[Go] Completed %d iterations\n", iteration)
}

func getEnv(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
    if value := os.Getenv(key); value != "" {
        if i, err := strconv.Atoi(value); err == nil {
            return i
        }
    }
    return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
    if value := os.Getenv(key); value != "" {
        if f, err := strconv.ParseFloat(value, 64); err == nil {
            return f
        }
    }
    return defaultValue
}
```

### Sample App Template (Node.js)

**File: `apps/redis/node/app.js`**

```javascript
const Redis = require('ioredis');

const host = process.env.REDIS_HOST || 'localhost';
const port = parseInt(process.env.REDIS_PORT || '6379', 10);
const maxIterations = parseInt(process.env.MAX_ITERATIONS || '0', 10);
const sleepDuration = parseFloat(process.env.SLEEP_DURATION || '1') * 1000;

const redis = new Redis({ host, port });

async function run() {
    let iteration = 0;

    while (true) {
        iteration++;
        console.log(`[Node] Iteration ${iteration}`);

        // Basic operations
        const key = `node:key:${iteration}`;
        await redis.set(key, `value-${iteration}`);
        await redis.get(key);

        // List operations
        await redis.lpush('node:list', `item-${iteration}`);
        await redis.lrange('node:list', 0, -1);

        // Hash operations
        await redis.hset('node:hash', `field-${iteration}`, `value-${iteration}`);
        await redis.hgetall('node:hash');

        // Pub/sub (publish only)
        await redis.publish('node:channel', `message-${iteration}`);

        if (maxIterations > 0 && iteration >= maxIterations) {
            break;
        }
        await new Promise(resolve => setTimeout(resolve, sleepDuration));
    }

    console.log(`[Node] Completed ${iteration} iterations`);
    await redis.quit();
}

run().catch(console.error);
```

### Sample App Template (Java)

**File: `apps/redis/java/src/main/java/App.java`**

```java
import redis.clients.jedis.Jedis;
import java.util.Map;

public class App {
    public static void main(String[] args) throws InterruptedException {
        String host = System.getenv().getOrDefault("REDIS_HOST", "localhost");
        int port = Integer.parseInt(System.getenv().getOrDefault("REDIS_PORT", "6379"));
        int maxIterations = Integer.parseInt(System.getenv().getOrDefault("MAX_ITERATIONS", "0"));
        double sleepDuration = Double.parseDouble(System.getenv().getOrDefault("SLEEP_DURATION", "1"));

        try (Jedis jedis = new Jedis(host, port)) {
            int iteration = 0;

            while (true) {
                iteration++;
                System.out.printf("[Java] Iteration %d%n", iteration);

                // Basic operations
                String key = "java:key:" + iteration;
                jedis.set(key, "value-" + iteration);
                jedis.get(key);

                // List operations
                jedis.lpush("java:list", "item-" + iteration);
                jedis.lrange("java:list", 0, -1);

                // Hash operations
                jedis.hset("java:hash", "field-" + iteration, "value-" + iteration);
                jedis.hgetAll("java:hash");

                // Pub/sub (publish only)
                jedis.publish("java:channel", "message-" + iteration);

                if (maxIterations > 0 && iteration >= maxIterations) {
                    break;
                }
                Thread.sleep((long) (sleepDuration * 1000));
            }

            System.out.printf("[Java] Completed %d iterations%n", iteration);
        }
    }
}
```

### Makefile

**File: `apps/redis/Makefile`**

```makefile
.PHONY: all up down logs ruby go java node clean

# Default: start all services
all: up

# Start all services
up:
	docker-compose up --build -d

# Stop all services
down:
	docker-compose down

# View logs from all services
logs:
	docker-compose logs -f

# Start only Ruby client
ruby:
	docker-compose up --build -d redis ruby-client

# Start only Go client
go:
	docker-compose up --build -d redis go-client

# Start only Java client
java:
	docker-compose up --build -d redis java-client

# Start only Node client
node:
	docker-compose up --build -d redis node-client

# Run finite iterations (useful for CI)
test:
	MAX_ITERATIONS=10 docker-compose up --build --abort-on-container-exit

# Clean up
clean:
	docker-compose down -v --rmi local
```

### Using the Test Infrastructure

**1. Start all clients for manual testing:**
```bash
cd apps/redis
make up
make logs  # Watch output from all clients
```

**2. Start a single language client:**
```bash
cd apps/redis
make ruby  # Only start Redis + Ruby client
```

**3. Run finite iterations (for CI/automated tests):**
```bash
cd apps/redis
make test  # Runs 10 iterations and exits
```

**4. Human operator verifies with qtap:**

*This step is performed by the human operator:*
```bash
# Terminal 1: Start qtap
sudo ./qtap --log-level=debug

# Terminal 2: Start test clients
cd apps/redis
make up

# Watch qtap output for parsed Redis commands
```

*The operator reports parsing results back to the agent for any fixes needed.*

**5. Clean up:**
```bash
cd apps/redis
make clean
```

### Test App Requirements

Each sample app should:

1. **Cover common operations** - Use the most common protocol commands
2. **Use different data types** - Strings, lists, hashes, etc. (protocol-dependent)
3. **Include metadata** - Prefix keys/channels with language name for tracing
4. **Log iterations** - Print iteration count for debugging
5. **Handle graceful shutdown** - Exit cleanly when MAX_ITERATIONS reached

### Implementation Checklist Update

**Testing Infrastructure (Agent creates files):**
- [ ] Create `apps/<protocol>/` directory
- [ ] Create `docker-compose.yml` with service + clients
- [ ] Create `Makefile` with standard targets
- [ ] Implement Ruby client app
- [ ] Implement Go client app
- [ ] Implement Java client app
- [ ] Implement Node.js client app

**Testing Infrastructure (Human operator verifies):**
- [ ] Test all clients connect and generate traffic
- [ ] Verify qtap parses traffic correctly
- [ ] Report any issues back to agent for resolution
