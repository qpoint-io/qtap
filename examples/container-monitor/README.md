# Container discovery from another Go project

This separate Go module demonstrates `github.com/qpoint-io/qtap/pkg/container`.
Its local module replacement uses this checkout. In your own project, require
the QTap version you use and omit the replacement.

From this directory, on Linux:

```sh
go build -mod=readonly -o /tmp/qtap-container-monitor .
sudo /tmp/qtap-container-monitor
```

Root is only needed if your user cannot access the runtime sockets. This example
does not load BPF or start QTap's process, capture, configuration, or telemetry
services. It registers container callbacks and prints started, stopped, and
restarted events until Ctrl-C or SIGTERM.

Runtime connection logs and the startup message go to standard error. Event
blocks go to standard output, so they can be redirected independently.

## Displayed container details

Each callback prints one block:

```text
started runtime="docker" id="abcdef1234567890"
  name:      "web"
  image:     "nginx:latest"
  digest:    "sha256:abc"
  root_pid:  4242
  rootfs:    "/var/lib/docker/overlay2/example/merged"
  pod:       unavailable
  namespace: unavailable
  pod_uid:   unavailable
  labels:    map["app":"web"]
```

Stopped and restarted callbacks show the same fields with a different event
name. Empty strings, a zero root PID, and missing labels print as `unavailable`.
Container names omit Docker's leading slash. IDs are shown in full. All string
values, including label keys and values, are quoted so embedded newlines remain
inside their field. One write per block keeps output from concurrent runtime
callbacks together.

Pod name, namespace, and UID come directly from runtime labels. The example does
not call the lazy pod accessor or query CRI from a callback, and it does not
modify callback records. Image digest, root filesystem, and PID availability
depend on the runtime and its configuration. The PID and other fields in a stop
event are the last cached metadata, not a fresh inspection of a running task.

## Runtime endpoints

Empty endpoint flags preserve the library's runtime probing and defaults:

```sh
/tmp/qtap-container-monitor \
  -docker-endpoint unix:///var/run/docker.sock \
  -containerd-endpoint /run/containerd/containerd.sock \
  -cri-endpoint unix:///run/containerd/containerd.sock
```

Docker also supports its existing environment configuration, including
`DOCKER_HOST`. The containerd argument is a socket filesystem path. The process
must have permission to access these sockets; when running inside a container,
mount the host runtime sockets as needed. The manager probes Docker, containerd,
and CRI; these flags select their endpoints, not which integrations are enabled.

Construction logs successful connections and skips unavailable runtimes. With
no connected runtime, startup can still succeed and wait without producing
events. The startup message means the manager started, not that every runtime
connected. Inspect the connection logs when output is empty. Existing
containerd discovery skips the `default` namespace during initial enumeration.

## Event and lifecycle semantics

`started` includes containers found during startup. There is no marker that
distinguishes initial discovery from a later insertion into the cache. Updating
a cached record does not emit another start. `stopped` reports cache removal;
repeated removal does not produce another event. `restarted` uses the existing
Docker restart notifications; containerd does not emit restart callbacks.

Callbacks run synchronously after cache locks are released and can overlap
between runtime accessors. These are discovery reporting events, not an ordered
or lossless runtime audit log. A container visible through both runtimes can
appear more than once, with its runtime identified in each block. Slow standard
output slows the corresponding runtime callback.

Interrupting the example cancels the watcher context and exits the program.
The library currently has no manager-wide close or wait operation. Construction
and CRI enrichment do not consistently use the watcher context. This example
retains those existing lifecycle limits; process exit releases remaining clients.

## Verification

```sh
go test -mod=readonly -race ./...
```

Formatting tests use in-memory container records and require no runtime or
privileges. Normal repository tests also compile and test this external module.
To watch a real lifecycle, leave the monitor running and use another terminal:

```sh
docker run -d --name container-monitor-demo alpine:3.21 sleep 300
docker restart container-monitor-demo
docker stop container-monitor-demo
docker rm container-monitor-demo
```

The module requires the same Go version as the checkout, currently Go 1.27.1.
