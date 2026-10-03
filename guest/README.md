# Guest image and agent

This directory holds the guest-side build inputs for the Grillo microVM. The
runtime code lives in `internal/guest`; the PID 1 entry point is
`cmd/grillo-agent`.

## Layout of a built image

```
/init                grillo-agent (PID 1, static, CGO_ENABLED=0)
/runc                static runc used for OCI containers
/rootfs/<container>  per-container root filesystems (busybox or OCI layers)
/etc/grillo/key      base64 per-boot handshake key (delivered by the host)
/run/grillo/...      bundles, logs, and runc state (created at runtime)
```

The kernel is the QEMU `microvm` guest kernel with `CONFIG_VIRTIO_FS`,
`CONFIG_VSOCKETS`, cgroup v2, and the namespaces runc needs. Kernel, runc, and
rootfs versions are pinned and recorded in the artifact manifest.

## Building

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
a `/shared` directory, and a per-boot key at `/etc/grillo/key`, then writes the
artifact manifest. `make test-t07` boots the image with QEMU `microvm` and
`vhost-vsock` and drives scenario A through the T06 client. A missing `/dev/kvm`
or qemu is a documented SKIP, never a pass.

The key is baked in for the local experiment because the host controls the
image. A production backend must deliver a fresh key per boot over a private
channel (for example a virtio device or fw_cfg), never on the kernel command
line or in logs.

## Artifact manifest

`internal/guest` provides `BuildManifest`, `Manifest.Verify`, and
`ReadManifest`. The manifest records each component's name, version, path,
SHA-256, and size, sorted by name for reproducibility:

```json
{
  "artifacts": [
    {"name": "grillo-agent", "version": "0.1.0-t07", "path": "bin/grillo-agent", "sha256": "...", "size": 12345}
  ]
}
```

Boot code must verify the manifest before starting the VMM; `Manifest.Verify`
re-hashes each file and reports the first mismatch.

## Agent behavior

The agent is PID 1: it mounts `/proc`, `/sys`, `/dev`, cgroup v2, devpts, shm,
and a runtime tmpfs; enables the `cpu`, `memory`, and `pids` cgroup controllers;
and reaps all children through a single `wait4` loop. It listens on a vsock port,
performs the versioned/authenticated handshake, and serves `start`, `stop`,
`status`, `exec`, and `probe` by running OCI bundles with runc. Network and IPC
namespaces are omitted so containers share the guest's Pod network and IPC while
keeping separate root filesystems and PID namespaces.
