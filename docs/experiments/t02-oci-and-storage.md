# T02 — OCI, filesystem, and application-network spike

Date: 2026-10-03. Status: **DONE**. Scenarios A, B, C, G, managed persistence, and
filesystem overhead all pass on real hardware with the selected backend. The
initial Firecracker attempt failed the live-bind gate and forced the backend
comparison recorded in [ADR 0005](../adr/0005-platform-qemu-virtiofsd.md).

The plan's gate is: "minimal A/G scenarios pass on real hardware without
privileged host mounts or copy-based bind substitutes." Scenario G passes with
QEMU `microvm` + virtiofsd; the Firecracker failure is retained below as the
reason the backend changed.

## Environment

Same workstation as [T01](t01-boot-spike.md): Linux/amd64, host kernel
`7.2.8-200.fc44.x86_64`, Intel Core Ultra 7 155H, 15 GiB RAM, real/effective UID
1000 (rootless; no sudo), `/dev/kvm` API 12.

**SELinux.** With SELinux **Enforcing**, the rootless topology fails: helper
processes started by `pasta` inside the new user namespace die silently (exit
code 2, empty output), reproducibly 5/5, while the same binaries run correctly
under plain `unshare -Urn` and after `setenforce 0` (5/5 pass). The F0 evidence
below was therefore collected with SELinux **Permissive** (`/sys/fs/selinux/enforce`
= 0). This is a distribution restriction that `doctor` must explain rather than
work around, and the harness now refuses to run under Enforcing with an
actionable message instead of a misleading timeout.

## Artifacts

| Artifact | Pin / digest |
|---|---|
| Guest kernel (Firecracker) | Linux 6.1.188, `vmlinux` sha256 `5e533ec165dacf998ecb699f9d3e0a031218002fd29f168ef8666028ed469f3e` |
| Guest kernel (QEMU) | same source with `CONFIG_VIRTIO_FS`; `bzImage` sha256 `550b5916…` |
| OCI runtime | static `runc` v1.5.2, sha256 `599f6f94ff8c5057241eff0d54c3c74f95c34935b6457b33fe545defc61e9488` |
| OCI image | `docker.io/library/busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e` (exported via podman; no Grillo registry client) |
| virtiofsd | 1.14.0 (`/usr/libexec/virtiofsd`) |
| QEMU | 10.2.2 (`microvm` machine) |
| pasta | `0^20260728.gf8df3f1-2.fc44.x86_64` |

Build/run commands (explicit opt-in; artifacts stay under the ignored
`experiments/artifacts/` tree):

```sh
make oci-guest       # fetch runc + busybox rootfs, build bundles + initramfs
make test-kvm        # Firecracker KVM tests: spike + OCI scenarios
make test-qemu       # QEMU KVM tests: live share, networking, OCI
make test-f0         # full rootless F0 topology, isolation, storage, benchmarks
```

## Scenario A — OCI image execution (PASS)

`TestKVMOCIScenarios` boots the OCI guest and drives runc over vsock. Each check
passed on real hardware (Firecracker and QEMU):

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
rootfs sits on the initramfs (ramfs), which cannot be `pivot_root`-ed.

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
context and localhost.

## Scenario C — two VMs, DNS/egress/host publishing, management isolation (PASS)

`make test-f0` builds a genuinely rootless topology: `pasta` creates the user and
network namespaces, the harness creates two bridges and three TAP devices inside
them, boots three QEMU microVMs (two application VMs on `10.77.0.0/24`, one
isolated VM on `10.78.0.0/24`), and applies a default-deny namespace firewall.
Observed results:

```text
F0 guest get PASS                     # vm1 -> vm0 HTTP over the bridge
F0 guest dns PASS DNS [10.77.0.11]    # fixed fixture resolver on 10.77.0.1
F0 guest egress PASS                  # rootless TCP egress to the uplink
F0 guest deny PASS DENIED 10.78.0.11:8080   # cross-bridge reachability blocked
                  DENIED 10.77.0.11:1024
                  DENIED 10.77.0.1:1024     # management port from the guest
                  DENIED 10.77.0.1:<canary> # host-only service
                  DENIED 10.78.0.1:53
F0 guest deny PASS DENIED 10.77.0.11:8080   # isolated VM cannot reach app net
                  DENIED 10.77.0.1:53
F0 host-loopback-publish PASS 127.0.0.1:36535
```

Management isolation holds because application port publishing crosses only an
operation-private Unix socket with `pasta` automatic forwarding disabled
(`-t/-u/-T/-U none`), and the namespace input policy accepts only the fixture DNS
and established flows. The published endpoint is loopback-only on the host.

## Scenario G — live read/write/read-only binds (PASS on QEMU)

Firecracker exposes no shared-filesystem device (recorded by `make storage-probe`,
exit `3`): `/drives` (virtio-block) and `/pmem` expose block/memory-mapped
devices, not directory semantics, so live directory binds are impossible without a
custom FUSE/9p bridge. QEMU `microvm` + virtiofsd provides them:

```text
STORAGE mount-rw   ok
STORAGE read-host  ok
STORAGE host-watch DEGRADED: no host write event within 2s; polling required
STORAGE host-update ok          # post-mount host write seen with updated content
STORAGE write-host ok           # host sees the guest write
STORAGE rename     ok
STORAGE mount-ro   ok
STORAGE read-ro    ok
STORAGE ro-enforced ok          # EROFS on a write to the read-only share
STORAGE guest-watch ok          # guest-originated inotify
STORAGE RESULT: PASS
```

`host-update` is verified by a real handshake: the guest signals readiness after
mounting, the host then rewrites the file, and the guest must observe the new
content. **Host-originated notifications are explicitly degraded**: no remote
inotify event arrived within the tested window, so development watchers need
polling. Guest-originated events and content coherence both work. virtiofsd ran
with `--sandbox=namespace --seccomp=kill --cache=never
--inode-file-handles=never`.

## Managed persistence and exclusivity (PASS)

A 128 MiB ext4 image is attached through virtio-block to the application VM:

```text
F0 volume-exclusive-lock PASS   # a second writable attachment is refused
F0 guest volume-write PASS      # writes a random token, fsyncs, unmounts
F0 guest volume-read PASS PERSISTENCE PASS   # value survives a full VM restart
F0 ext4-offline-check PASS      # e2fsck -fn is clean after teardown
```

Persistence reuses only the disk image; no guest RAM or snapshot is involved.

## Filesystem overhead (30 samples, µs, per 128-file whole phase)

| Phase | Host baseline | virtiofs (guest) | virtio-block ext4 (guest) |
|---|---|---|---|
| write+fsync | 1297 | 76115 | 49197 |
| read | 816 | 75453 | 1336 |
| rename | 638 | 100307 | 1590 |
| chmod | 237 | 69075 | 380 |
| remove | 374 | 40331 | 1651 |

virtiofs metadata operations are far more expensive than a local block filesystem
(reads ~56×, chmod ~180×, rename ~63×) with the conservative cache settings used
here; write+fsync is within ~1.5× because fsync latency dominates. These are warm-
cache, single-host, 1 vCPU/256 MiB figures, not claims about tuned production
settings; T10/T27 must re-measure with a production cache policy.

## Findings handed to T07/T08/T10/T11

- **VMM lifetime.** If the supervisor is killed without running Stop, Firecracker
  survives as an orphan. The harness sets `Pdeathsig: SIGKILL`; T08 must still
  reconcile orphans by identity rather than parent death alone.
- **Backend.** QEMU `microvm` + virtiofsd is selected; see
  [ADR 0005](../adr/0005-platform-qemu-virtiofsd.md).
- **Guest init.** PID 1 must reap orphaned children: a detached `runc run -d`
  supervisor is reparented to PID 1 and becomes an unreaped zombie. T07 owns it.
- **Detached stdio.** Detached processes must get their own stdio (`/dev/null`),
  not the control channel, or they hold the vsock pipe open.
- **runc and initramfs.** `--no-pivot` is required when the bundle rootfs is a
  ramdisk.
- **DNS fixture.** Real resolvers (including Go) append EDNS0 OPT records;
  protocol fixtures must tolerate additional records instead of rejecting the
  query. A regression test now covers this.
- **Fixture tooling.** `podman rm` also removes the `--cidfile`; the OCI fixture
  script and its offline test were corrected to match real tool behavior.

## Limits

- SELinux must be Permissive (or a suitable policy supplied) on the tested Fedora
  host; `doctor` must surface this.
- Host-originated virtiofs notifications are degraded; polling is required.
- The spike protocol, guest PID 1, DNS fixture, and bundle layout are
  experiment-only and are replaced by T06/T07/T12.
- Containers run as guest root; no user-namespace isolation inside the guest yet.
- Measurements are single-host, warm-cache, 1 vCPU/256 MiB.
