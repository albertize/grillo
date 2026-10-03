# T02 — OCI, filesystem, and application-network spike

Date: 2026-10-03. Status: **BLOCKED on the live-bind gate (scenario G)**.
Scenario A passes; two containers share localhost (B); scenario C was not
completed. Starting commit: `62c6a10` (T01 complete); T02 changes follow.

The plan's gate is: "minimal A/G scenarios pass on real hardware without
privileged host mounts or copy-based bind substitutes. Otherwise BLOCKED pending
backend comparison." A passes; G cannot pass with Firecracker. Per the plan, this
forces a backend comparison before T03 rather than a workaround.

## Environment

Same workstation as [T01](t01-boot-spike.md): Linux/amd64, host kernel
`7.2.8-200.fc44.x86_64`, Intel Core Ultra 7 155H, 15 GiB RAM, real/effective UID
1000 (rootless; no sudo), `/dev/kvm` API 12, SELinux `Enforcing`.

## Artifacts

| Artifact | Pin / digest |
|---|---|
| Guest kernel | Linux 6.1.188, `vmlinux` sha256 `5e533ec165dacf998ecb699f9d3e0a031218002fd29f168ef8666028ed469f3e` |
| OCI runtime | static `runc` v1.5.2, sha256 `599f6f94ff8c5057241eff0d54c3c74f95c34935b6457b33fe545defc61e9488` |
| OCI image | `docker.io/library/busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e` (exported via podman; no Grillo registry client) |
| OCI initramfs | `initramfs-oci.cpio.gz` (PID 1 + runc + three bundles) |

Build/run commands (explicit opt-in; artifacts stay under the ignored
`experiments/artifacts/` tree):

```sh
make oci-guest       # fetch runc + busybox rootfs, build bundles + initramfs
make test-kvm        # real KVM tests: spike + OCI scenarios
go run ./experiments/boot/oci
```

## Scenario A — OCI image execution (PASS)

`TestKVMOCIScenarios` boots the OCI guest and drives runc over vsock. Each check
passed on real hardware:

```text
PASS  guest runc version
PASS  start container
PASS  container running
PASS  exec in container      (/bin/echo exec-in-container)
PASS  signal container       (runc kill TERM)
PASS  delete container       (runc delete --force)
```

The container runs the busybox rootfs through runc with PID/IPC/UTS/mount
namespaces and cgroup v2. `runc run` needed `--no-pivot` because the bundle
rootfs sits on the initramfs (`rootfs`/ramfs), which cannot be `pivot_root`-ed;
runc's own help documents this for ramdisk rootfs.

## Scenario B — two containers over localhost (PASS)

Two separate runc containers (server `httpd`, client `wget`), both started
without a network namespace so they share the guest network, communicate on
`127.0.0.1`:

```text
PASS  start server container
PASS  server container listed
PASS  client reaches server on localhost
      client output: grillo-oci-localhost-ok
```

This demonstrates Pod-style co-location: distinct containers, one network
context and localhost. The guest PID 1 brings up `lo`.

## Scenario C — two VMs, DNS/egress/host publishing, management isolation (NOT COMPLETED)

Not attempted. It requires wiring Firecracker's virtio-net to a rootless helper
(TAP creation inside a user/network namespace, `pasta` for egress, IPAM, DNS, and
host port publishing), which is a substantial harness beyond this spike. T01
proved the helper works rootless in isolation; T02 did not connect it to a guest.
This remains a feasibility item, not evidence of support.

## Scenario G — live read/write/read-only binds (BLOCKED)

Live host-directory sharing requires a shared-filesystem device. `make
storage-probe` records, reproducibly, that neither the candidate VMM nor the
guest kernel provides one:

- Firecracker v1.17.0 API devices: `/drives/{id}` (virtio-block), `/pmem/{id}`
  (virtio-pmem), `/network-interfaces/{id}`, `/vsock`, `/balloon`, `/entropy`,
  `/serial`, memory hotplug. **No virtio-fs and no 9p device.**
- Guest kernel has `CONFIG_FUSE_FS=y`, `CONFIG_NFS_FS=y`, `CONFIG_VIRTIO_PMEM=y`,
  `CONFIG_VIRTIO_BLK=y`, but **no `CONFIG_9P_FS`** (and no virtio-fs).

virtio-block exposes a whole block device, not a directory, and mounting one
ext4 image read-write from both host and guest is unsafe (no cluster
filesystem). virtio-pmem exposes a memory-mapped file region, not directory
semantics. So live bidirectional writes with rename/watch and read-only
enforcement are not achievable with Firecracker without a custom FUSE-over-vsock
or 9p-over-vsock bridge — a security-sensitive component the plan says not to
write when a specialized external one exists.

`make storage-probe` exits `3` (BLOCKED) with this finding.

## Findings handed to T03/T07

- **VMM lifetime.** If the supervising process is killed without running Stop,
  Firecracker survives as an orphan and its temp directory leaks. The harness now
  sets `Pdeathsig: SIGKILL` on the VMM; verified by killing the harness mid-boot
  (1 VMM before, 0 after). T08 must still reconcile orphans by identity rather
  than relying on parent death alone.
- **Backend.** Firecracker fails the live-sharing gate; compare QEMU `microvm`
  (virtiofsd) or Cloud Hypervisor (virtiofs) before T03. See
  [ADR 0004](../adr/0004-shared-filesystem-backend-comparison.md). Managed
  persistent volumes can still use virtio-block, but scenario G needs shared-fs.
- **Guest init.** PID 1 must reap orphaned children: a detached (`runc run -d`)
  container's supervisor is reparented to PID 1; without reaping it remains a
  zombie that runc still reports as running. The spike worked around this with
  `runc delete --force`; T07 must implement real reaping.
- **Detached stdio.** Detached processes must get their own stdio (`/dev/null`),
  not the control channel, or they hold the vsock pipe open. The spike added a
  detached-start command for this.
- **runc and initramfs.** `--no-pivot` is required when the bundle rootfs lives
  on a ramdisk.

## Limits

- Scenario C not done; no guest DNS/egress/host-publishing evidence.
- The spike protocol, guest PID 1, and bundle layout are experiment-only and are
  replaced by T06/T07.
- Single host, warm cache; OCI scenarios ran in ~1.4 s including one boot each
  (boot ~0.25 s as in T01).
- Containers run as guest root; no user-namespace isolation inside the guest yet.
- Measured filesystem overhead for binds was not possible because binds are not
  supported by the candidate backend.
