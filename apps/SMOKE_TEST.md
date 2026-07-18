# Qtap Protocol Smoke Test

A happy-path smoke test for an AI agent (or human) to validate that a freshly
built `qtap` captures and decodes real protocol traffic end-to-end. Each app in
this directory (`kafka`, `mysql`, `redis`) runs a server plus clients in several
languages that generate finite, deterministic traffic while `qtap` taps the host.

**What this validates:** eBPF load, container/process attribution, the protocol
parsers (redis/mysql/kafka), and — for the MySQL/Redis TLS variants — the TLS
probes (openssl, gotls, nodetls, javassl) decrypting encrypted traffic.

---

## Prerequisites

- Linux host, **run everything as root** (eBPF requires it).
- Docker running (`docker ps` succeeds).
- Toolchain to build qtap: Go, clang/llvm, and openjdk-17 (for the Java TLS agent).
- Working directory is the repo root (`.../qtap`).

---

## Step 1 — Build qtap

```bash
make build          # runs: generate (eBPF) + build-jvm (Java agent) + build-go
```

Success: `bin/qtap` exists. If `build-jvm` fails, ensure `openjdk-17-jdk-headless`
is installed — the `javassl` probe embeds a Java agent and will not compile without it.

## Step 2 — Start qtap tapping (background)

The **default embedded config** already taps `http`, `redis`, `mysql`, `kafka`,
and `grpc`, writes events to stdout, and enables all TLS probes — no `--config` needed.

```bash
./bin/qtap --log-level info --log-encoding console > /tmp/qtap-smoke.log 2>&1 &
QTAP_PID=$!
sleep 8   # allow eBPF programs to load
grep -E "starting TLS Probes" /tmp/qtap-smoke.log
```

Expected (within ~5s of "loading BPF programs and maps"):

```
INFO  loading BPF programs and maps
INFO  starting TLS Probes  {"probes": "nodetls,openssl,gotls,javassl"}
```

If you see `requires kernel version 5.10 or greater` or a permission error, you
are not root or the kernel is too old — stop here.

## Step 3 — Generate traffic (per protocol)

Each app's `make test` runs the clients for a finite number of iterations
(`MAX_ITERATIONS`, default 5–10) and exits cleanly (`--abort-on-container-exit`).

### Redis (plaintext)

```bash
cd apps/redis
MAX_ITERATIONS=5 docker compose up --build --abort-on-container-exit
docker compose down -v
cd ../..
```

Expected client output (each language completes its iterations):

```
go-client-1    | [Go] Completed 5 iterations
java-client-1  | [Java] Completed 5 iterations
node-client-1  | [Node] Completed 5 iterations
ruby-client-1  | [Ruby] Completed 5 iterations
```

### MySQL (plaintext)

```bash
cd apps/mysql && MAX_ITERATIONS=5 docker compose up --build --abort-on-container-exit
docker compose down -v && cd ../..
```

### Kafka

```bash
cd apps/kafka && make test        # MAX_ITERATIONS=10
make clean && cd ../..
```

### TLS variants (MySQL / Redis) — validates the TLS probes

These generate **encrypted** traffic; capturing decoded statements proves the
gotls/nodetls/javassl/openssl probes are decrypting. Certs must be generated first
(the Makefiles' `test-tls`/`up-tls` targets do this via `make certs`):

```bash
cd apps/redis && make test-tls    # generates certs, runs clients over TLS
make clean && cd ../..

cd apps/mysql && make test-tls
make clean && cd ../..
```

> If a TLS run logs `Failed to load certificate: /certs/server.crt: No such file`,
> run `make certs` in that app dir first (the `test-tls` target normally does this).

## Step 4 — Verify qtap captured the traffic

While/after each app runs, inspect the qtap log:

```bash
grep -iE '"l7Protocol":"redis"|databaseType":"redis"' /tmp/qtap-smoke.log | head
```

### Pass criteria (Redis example — real captured output)

You should see, for each client language:

1. A **Connection** event tagged with the protocol and full attribution:

```json
{"type": "*eventstore.Connection", "item": {
  "l7Protocol":"redis","socketProtocol":"tcp","direction":"egress-internal",
  "tags":{"protocol":["redis"],"bin":["ruby"]},
  "source":{"exe":"/usr/local/bin/ruby","pid":142284,
    "container":{"name":"redis-ruby-client-1","image":"redis-ruby-client"}},
  "destination":{"address":{"ip":"172.18.0.2","port":6379}}}}
```

2. **DatabaseRequest** events with the actual decoded statements, plus a
   human-readable console decode line:

```
 ■ redis-server ← lrange ruby:list 0 -1 OK
{"type": "*eventstore.DatabaseRequest", "item": {
  "databaseType":"redis","statement":"hset ruby:hash field-2 value-2",
  "resultType":"integer","isError":false}}
```

**The test passes if, for each app:**

- [ ] A `Connection` event appears with the correct `l7Protocol`
      (`redis` / `mysql` / `kafka`) and `socketProtocol: tcp`.
- [ ] The connection is attributed to the right **container** (`...-client-1` /
      the server image) and **process** (`exe`, `pid`).
- [ ] For DB protocols, `DatabaseRequest` events contain **real decoded
      statements** (e.g. `lrange`, `hset`, `publish` for redis; `SELECT`/`INSERT`
      for mysql) with `isError:false`.
- [ ] For the **TLS variants**, the same decoded statements appear even though the
      wire traffic is encrypted, and connections show a `tlsProbeTypesDetected`
      entry — this confirms the probes decrypted the traffic.

Per-protocol grep helpers:

```bash
grep '"l7Protocol":"mysql"'  /tmp/qtap-smoke.log | head
grep '"l7Protocol":"kafka"'  /tmp/qtap-smoke.log | head
grep 'tlsProbeTypesDetected' /tmp/qtap-smoke.log | head   # TLS runs only
```

## Step 5 — Teardown

```bash
kill $QTAP_PID
for a in redis mysql kafka; do (cd apps/$a && docker compose down -v --rmi local 2>/dev/null); done
```

---

## Notes / expected non-failures

- `tls probe error ... searching symbols ...` lines for **unrelated host
  processes** (editors, snapd, etc.) are benign — the probes try to attach to
  every process and skip ones they can't parse.
- `requested Go version exceeds supported range, using newest supported version`
  is a `gotls` offset-scan warning; it falls back to the newest known layout and
  still captures.
- `redis stream closed with pending commands` at shutdown is expected when
  clients exit mid-stream.
- The default config's `direction: all` captures both the client's
  `egress-internal` side and the server's `ingress` side of each connection, so
  you will see two Connection events per client conversation — that is correct.
