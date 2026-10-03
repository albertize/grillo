# F2 Compose example

A minimal web + worker application that demonstrates the Compose frontend on the
real runtime: one named network, DNS by service name, a managed volume, startup
ordering, and no Docker daemon.

## Prerequisites

- Linux with `/dev/kvm` and unprivileged user/network namespaces (see `grillo doctor`).
- Built binaries: `make build` (produces `bin/grillo`, `bin/grillod`, `bin/grillo-netns`).
- `pasta` and `qemu-system-x86_64` on `PATH`, and the guest artifacts from
  `make t07-guest` and `make oci-guest`.
- No root: everything runs as your user.

## Run

```sh
bin/grillo up examples/f2-compose/compose.yaml
bin/grillo status f2
```

The worker resolves `web` through the in-guest DNS and writes the fetched page to
the managed volume:

```sh
bin/grillo exec f2 worker -- cat /data/result.html   # prints f2-ok
```

## Persistence and cleanup

`down` stops the sandboxes but keeps the volume; `up` again reuses it. Only
`down --volumes` deletes owned volumes:

```sh
bin/grillo down f2
bin/grillo up examples/f2-compose/compose.yaml
bin/grillo down --volumes f2
```

## Notes

- The image is pulled from a registry through `internal/oci`; no Docker daemon is
  involved.
- Multiple distinct named-network topologies are rejected by the compiler because
  the runtime gives one application one network. This example uses a single
  network.
