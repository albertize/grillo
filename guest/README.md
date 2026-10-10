# Guest image and agent

This directory holds the guest-side build inputs for the Grillo microVM. The
runtime code lives in `internal/guest`; the PID 1 entry point is
`cmd/grillo-agent`.

## Runtime-only base

`make runtime-guest` builds a separate production-shaped base from explicitly
provisioned inputs. It selects only the static agent/runc, BusyBox networking
utility and pinned glibc ELF dependencies, plus essential directories. Build
metadata is an adjacent host-side inventory. The image contains no application
fixture roots or reusable authentication
key. The existing private fresh-key overlay is still required for each boot.

The builder publishes verified, content-identity, no-replace store entries under
`experiments/artifacts/runtime/`; the optional selection file is developer-only.
Bounded archive scanning rejects unknown namespace entries, duplicates, extra
gzip streams, fixture/key files and explicitly supplied forbidden markers.
Opaque binary scanning is not publisher authentication or a proof against all
unknown embedded secrets. Build/source trust and redistribution review remain
required. See [runtime layout](../docs/runtime-layout.md) and
[actual installed-prefix KVM evidence](../docs/experiments/d0-installed-runtime.md).

## Layout of a development fixture

```
/init                grillo-agent (PID 1, static, CGO_ENABLED=0)
/runc                static runc used for OCI containers
/rootfs-{setup,app,sidecar}  development fixture container roots
/etc/grillo/key      base64 per-boot handshake key (delivered by the host)
/run/grillo/...      bundles, logs, and runc state (created at runtime)
```

The kernel is the QEMU `microvm` guest kernel with `CONFIG_VIRTIO_FS`,
`CONFIG_VSOCKETS`, cgroup v2, and the namespaces runc needs. Kernel, runc, and
rootfs versions are pinned and recorded in the artifact manifest.

## Building

Follow [getting started](../docs/getting-started.md) for host tools, frontend build
dependencies, kernel sources and the QEMU kernel. These are development artifacts,
not a packaged release. The initial OCI fixture export can require Podman even
though the native workload/build runtime does not.

The agent is built by the repository build target:

```sh
make build          # produces bin/grillo and bin/grillo-agent
```

The full guest image is built from the T02 OCI fixture (pinned static runc and
busybox rootfs):

```sh
make oci-guest      # fetch runc + busybox rootfs (T02 fixture, cached)
make t07-guest      # build experiments/artifacts/t07/{initramfs-agent.cpio.gz,key,manifest.json}
make test-t07       # real KVM/QEMU scenario A against the built image
```

`make t07-guest` builds the static agent, packs it as `/init` with `/runc`, one
busybox rootfs per container (`/rootfs-setup`, `/rootfs-app`, `/rootfs-sidecar`),
a `/shared` directory, and a private legacy fixture key at `/etc/grillo/key`, then writes the
artifact manifest. `make test-t07` boots the image with QEMU `microvm` and
`vhost-vsock` and drives scenario A through the T06 client. A missing `/dev/kvm`
or qemu is a documented SKIP, never a pass.

Direct component gates use the baked fixture key; fixture key/images are 0600.
The daemon's runtime and native-build QEMU paths instead require a trusted
manifest, verify kernel/initramfs into private snapshots before launch and append
a private gzip/newc overlay replacing `/etc/grillo/key` with 32 fresh random bytes
on each new boot. Idempotent live starts retain their credentials. The daemon
ignores legacy `--key-file`; no boot key appears in arguments, logs or public DTOs.
See [ADR 0009](../docs/adr/0009-verified-boot-and-private-key-overlay.md) and the
[T24 evidence](../docs/experiments/t24-interactive-metrics-artifacts.md). Inventories
are local integrity expectations, not publisher authentication or release SBOMs.

## Artifact manifest

`internal/guest` provides `BuildManifest` for legacy development inventories,
`BuildPortableManifest` for schema 1 boot inventories, `Manifest.Verify` and
`ReadBootManifest`. Installed daemon defaults require schema 1 with exact guest
ABI/platform metadata and manifest-relative kernel/initramfs paths. Explicit
`--artifact-manifest` / `GRILLO_GUEST_MANIFEST` developer overrides permit legacy
inventories; declared incompatible metadata is never bypassed.

See [portable schema and builder](manifest-schema.md) and
[real moved-directory boot evidence](../docs/experiments/d0-portable-manifest.md).
The current `make t07-guest` still generates the legacy fixture inventory below,
recording each component's name, version, path, SHA-256 and size, sorted by name:

```json
{
  "artifacts": [
    {"name": "grillo-agent", "version": "0.1.0-t07", "path": "bin/grillo-agent", "sha256": "...", "size": 12345}
  ]
}
```

Boot code must verify the manifest before starting the VMM; `Manifest.Verify`
re-hashes bounded regular files and reports the first mismatch. Portable inventory
paths are anchored to the manifest directory and confined with `os.Root`, not CWD.

## Agent behavior

The agent is PID 1: it mounts `/proc`, `/sys`, `/dev`, cgroup v2, devpts, shm,
and a runtime tmpfs; enables the `cpu`, `memory`, and `pids` cgroup controllers;
and reaps all children through a single `wait4` loop. It listens on a vsock port,
performs the versioned/authenticated handshake, and serves `start`, `stop`,
`status`, `exec`, and `probe` by running OCI bundles with runc. Network and IPC
namespaces are omitted so containers share the guest's Pod network and IPC while
keeping separate root filesystems and PID namespaces.
