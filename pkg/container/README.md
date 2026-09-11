# Container discovery

Import `github.com/qpoint-io/qtap/pkg/container` to discover Docker and containerd
containers, look up cached metadata by ID, and enrich pod metadata through CRI.
The package imports no other qtap packages and registers no qtap metrics.
It remains part of qtap's Go module, so consumers inherit its dependency versions
and Go requirement (currently Go 1.27.1).

## Use without telemetry

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

manager := container.NewManager(zap.NewNop(), "", "", "", container.Callbacks{})
if err := manager.Start(ctx); err != nil {
    return err
}
if c := manager.GetByID(containerID); c != nil {
    fmt.Println(c.ID, c.TidyName(), c.Image)
}
```

See the [compilable examples](example_test.go) for complete imports and explicit
handling of a missing container. Lookup accepts a full or 12-character ID and
returns `nil` when the container has not been discovered. Treat returned records
and their maps as read-only.

Supply a non-nil Zap logger; `zap.NewNop()` disables logging. The three endpoint
arguments are Docker, containerd, and CRI, in that order. Empty endpoints retain
automatic runtime probing and Docker's environment configuration. The process
needs permission to access the runtime sockets, including host sockets when it
runs inside a container. CRI enriches discovered containers; it is not a separate
container discovery backend.

Construction probes runtimes and logs skipped integrations. Starting a manager
with no connected runtimes can succeed with an empty cache. Keep the startup
context alive for ongoing event watching. Context cancellation signals watchers
to stop, but there is currently no manager-wide close or wait operation, and
construction and pod enrichment do not consistently use that context.

## Optional reporting

Pass only the handlers your application needs:

```go
callbacks := container.Callbacks{
    Started: func(c *container.Container, runtime string) {
        fmt.Println("discovered", runtime, c.ID)
    },
}
manager := container.NewManager(logger, dockerEndpoint, containerdEndpoint, criEndpoint, callbacks)
```

`Started` reports newly cached containers, including initial discovery. Cache
updates do not emit another start. `Stopped` reports removal of a cached
container. `Restarted` reports the existing Docker restart notifications;
containerd does not emit restart notifications. Runtime names are `docker` and
`containerd`. These are reporting events, not an authoritative lifecycle stream.

Handlers run synchronously after cache locks are released. A handler can look up
a container: starts see the inserted record and stops see its removal. Handlers
must return promptly, treat records as read-only, and allow concurrent calls from
different runtime accessors. Nil handlers are ignored. There is no event queue or
delivery guarantee.

## Constructor migration

`NewManager`, `NewDockerAccessor`, and `NewContainerdAccessor` now require a final
`Callbacks` argument. Append `container.Callbacks{}` for discovery without
reporting. Lookup and metadata APIs are unchanged.

Qtap creates its telemetry adapter once with its product registry and passes the
resulting callbacks to the manager. Metric registration happens during that
explicit initialization, preserving the existing `qtap_container_*` names.

## Checks

From the repository root:

```sh
go test -race -timeout 120s ./pkg/container ./pkg/telemetry/containermetrics ./pkg/process
go test ./pkg/container -run '^TestNoQTapDependencies$'
```

Tests use a local HTTP stub and in-memory container records; they require no live
runtime or telemetry setup. The examples compile with the tests and are not run
against the host. The dependency check inspects the production Go dependency
graph, including transitive imports.
