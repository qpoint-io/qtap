# Process discovery from another Go project

This separate Go module demonstrates `github.com/qpoint-io/qtap/pkg/process/monitor`.
The local replacement in its module manifest uses this checkout. In your own
project, require the QTap version you use and omit that replacement.

From this directory:

```sh
go build -o /tmp/qtap-process-monitor .
sudo /tmp/qtap-process-monitor
```

The example registers an existing-style process observer, starts startup
enumeration and live Linux discovery, and stops when interrupted. It never
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

## Verification

```sh
go test -mod=readonly ./...
sudo go test -mod=readonly -tags integration -v -count=1 ./...
```

The first command verifies compilation from a separate module without loading
BPF. The integration test loads real probes, observes a process that existed
before startup, and checks shutdown. It requires the runtime prerequisites and
fails rather than silently skipping when discovery cannot start.
