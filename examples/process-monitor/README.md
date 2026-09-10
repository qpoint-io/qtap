# Process discovery from another Go project

This separate Go module demonstrates `github.com/qpoint-io/qtap/pkg/process/monitor`.
The local replacement in its module manifest uses this checkout. In your own
project, require the QTap version you use and omit that replacement.

From this directory:

```sh
go build -o /tmp/qtap-process-monitor .
sudo /tmp/qtap-process-monitor
```

The example registers an existing-style process observer, prints started,
replaced, and stopped callbacks, and stops when interrupted. It queries the
registry from a started callback and counts a snapshot after startup. It never
starts QTap's command, configuration service, container manager, or capture
managers. It retains QTap's current process model and combined BPF collection.

## Lifecycle and requirements

Call `monitor.New`, register observers using `Observe`, then call `Start`. Always
call the monitor's `Stop`, even when you never start it. Startup failure releases
the owned resources automatically; a deferred `Stop` remains safe. Call lifecycle
methods sequentially. Use the monitor's `Stop`, not its embedded manager's `Stop`,
so that the BPF collection is also released.

Only one monitor is supported per Go process, including successive construction.
Restarting a stopped monitor is unsupported. The existing process manager
registers global metrics; creating a second manager can panic.

Run on Linux with kernel 5.10 or later, BTF available at
`/sys/kernel/btf/vmlinux`, access to procfs and tracefs, and permission to load and
attach QTap's BPF programs (the example uses root). The procfs view must correspond
to the PIDs reported by the probes. The existing combined BPF object is loaded,
so its verifier, resource, and privilege requirements also apply. A nil logger
disables logging. Generated BPF assets are included in QTap.

## Callbacks and queries

`ProcessStarted` includes processes found during startup; `PredatesQpoint`
distinguishes those from live discoveries. `ProcessReplaced` reports executable
changes recognized by the existing manager. `ProcessStopped` includes the
manager's recorded exit code. This is startup enumeration plus exec/exit
observation, not complete process-birth accounting. PID reuse, short-lived
processes, and dropped kernel records retain their existing limitations.

Callbacks run asynchronously and may overlap across observers and lifecycle
transitions. There is no ordered or lossless delivery guarantee. The supplied
`*process.Process` is shared mutable state, not an immutable event snapshot;
retaining it does not preserve a historical value. Optional container metadata
and TLS state may be absent. Observer errors use the manager's existing logging
and error handling; they do not roll back registry changes or reach `Start`.

Register observers before `Start`. There is no unsubscribe operation or atomic
snapshot-and-subscribe boundary. `Stop` joins the source reader and releases its
resources, but already-dispatched callbacks may finish afterward. Applications
own any work their callbacks start and must not rely on discovery resources
remaining available after shutdown.

The embedded manager provides `Get(pid)`, `Await(ctx, pid)`, and
`SnapshotProcesses(fn)`. Use a timeout when waiting for an unknown PID:

```go
ctx, cancel := context.WithTimeout(parent, 5*time.Second)
defer cancel()
proc, err := m.Await(ctx, pid)
```

Waiting is subject to the existing manager's behavior; snapshots are a changing
registry view of process pointers, not point-in-time copies. A stop callback can
overlap registry removal, so consumers should use the callback's process value
when handling exits rather than requiring a lookup to succeed.

## Startup errors and cleanup

`New` returns an error when BPF loading or reader setup fails and releases any
resources it acquired. A failed `Start` detaches process probes already attached
and releases the owned collection. You can report the error and keep your
application running; library code does not exit the process. The example's
`main` chooses to exit on failure, but that is the caller's decision.

After failed startup, do not reuse the monitor. Its deferred `Stop` is safe and
does not close resources a second time. Successful construction always requires
`Stop`, including when startup is never requested. Existing callback work is
still application-owned, as described above.

## Verification

```sh
go test -mod=readonly ./...
sudo go test -mod=readonly -tags integration -v -count=1 ./...
```

The first command verifies compilation from a separate module without loading
BPF. The integration test loads real probes, observes a process that existed
before startup, launches a controlled child, checks registry queries, and asks
the child to replace itself and exit. Each transition is driven after observing
the preceding callback; this tests the supported flow without asserting a new
ordering guarantee. It also checks shutdown. The test requires the runtime
prerequisites and fails rather than silently skipping when discovery cannot
start. Each test program constructs only one manager.

The external failure test runs a consumer without BPF privileges and verifies
that a permission error is returned while the host program continues. Additional
resource-ownership tests can be run from the repository root:

```sh
sudo go test -tags integration -v -count=1 ./pkg/process/monitor
```

These use real BPF resources to force reader setup failure and a failure after
the first process probe has attached. They verify that owned descriptors are
closed and that the first probe no longer keeps its program alive in the kernel.

## QTap's shared collection

QTap uses the same process-source constructor with its already-loaded collection.
That source owns its ring-buffer reader and tracepoint links, but QTap continues
to own the maps and programs used by its other components. Stopping the source
must leave those shared resources available; QTap closes them during its normal
application shutdown. The standalone monitor owns the whole collection because
it creates it. Neither path starts a second process manager or changes existing
QTap observers and configuration handling.

From the repository root, run the shared-resource checks and the affected QTap
process-filtering regression:

```sh
sudo go test -tags integration -count=1 ./pkg/process ./pkg/process/monitor
sudo go test -tags e2e -run '^TestProcessFiltering$' -count=1 ./e2e
```

The repository's e2e workflow also runs the public external consumer and resource
ownership checks. Normal repository tests compile the external module without
loading BPF.
