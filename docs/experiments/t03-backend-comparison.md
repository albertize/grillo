# T03 — F0 backend comparison and platform decision

Date: 2026-10-03. Status: **BLOCKED; partial QEMU evidence, proposed decision.**
Starting commit: `7c1dc81` (T02); T03 changes follow.

The plan's gate: confirm VMM/network/sharing/rootfs/supervision with versions and
commands, and record risks, doctor prerequisites, budgets, and a support matrix.
The comparison supports QEMU as a candidate, not completion of F0. Review found
missing T02 topology/storage/overhead evidence and overbroad watch claims; no
approved scope reduction permits deferring those requirements to T10/T11.

## Environment and versions

| Item | Value |
|---|---|
| Host | Linux/amd64, `7.2.8-200.fc44.x86_64`, Intel Core Ultra 7 155H, 15 GiB |
| User | real/effective UID 1000, no sudo, SELinux `Enforcing` |
| KVM | `/dev/kvm` API 12; `/dev/vhost-vsock`, `/dev/vhost-net` present |
| Firecracker | v1.17.0 (pinned download) |
| QEMU | `qemu-system-x86_64` 10.2.2, `microvm` machine |
| virtiofsd | 1.14.0 (`/usr/libexec/virtiofsd`) |
| Guest kernel | Linux 6.1.188; Firecracker build (`5e533ec…`), QEMU build adds `CONFIG_VIRTIO_FS` (`647c4b5d…` vmlinux / bzImage `550b5916…`) |
| OCI runtime | static runc 1.5.2 (`599f6f…`); image `busybox:1.37@sha256:bdf57e…` |

## Comparison

| Capability | Firecracker | QEMU microvm + virtiofsd | Command |
|---|---|---|---|
| Rootless boot | yes, 260 ms median | yes, ≈400 ms to handshake | `make test-kvm` / `make test-qemu` |
| Guest console + stop | yes (serial + guest reboot) | yes (serial + `-no-reboot`) | harnesses |
| OCI run/exec/signal/delete | yes | yes | `make test-kvm` / `test-qemu` |
| Two containers on localhost | yes | yes | scenario B |
| Live rw host→guest→host | **no shared-fs device** | yes | `make storage-probe` (exit 3) vs `qemu-share` |
| Read-only bind enforced | n/a | yes (`EROFS`) | `qemu-share` |
| Rename / guest-local inotify | n/a | yes | `qemu-share` |
| Host-originated inotify | n/a | DEGRADED: no event within 2s | corrected `qemu-share` |
| Rootless guest egress + DNS | not wired | yes (user-mode net) | `-scenario net` |
| Management channel | Firecracker API + vsock UDS | `vhost-vsock` (host AF_VSOCK) | — |
| VMM RSS after boot | ≈49 MiB | ≈142 MiB | `/proc/<pid>/status` |

Firecracker rootless prerequisites were verified in T01 (KVM API, user/network
namespace TAP, `pasta` egress). QEMU networking uses user-mode SLIRP, which needs
no host TAP or privileges.

## Commands

```sh
make storage-probe     # records the Firecracker shared-fs gap (exit 3 = BLOCKED)
make qemu-guest        # build the QEMU bzImage and probe/OCI initramfs
make qemu-share        # QEMU + virtiofsd live-bind probe
make test-qemu         # real KVM tests: live share, networking, OCI on QEMU
make test-kvm          # Firecracker tests: boot/exec/stop and OCI
make net-helper        # rootless helper (pasta) connectivity
```

Original QEMU live-share serial evidence (the old `watch` was guest-local only;
its pre-boot sentinel did not prove live host updates):

```text
STORAGE mount-rw   ok
STORAGE host-sentinel "host-sentinel-v1\n"
STORAGE write-host ok
STORAGE rename     ok
STORAGE mount-ro   ok
STORAGE ro-enforced ok
STORAGE watch      ok
STORAGE RESULT: PASS
```

QEMU networking and OCI evidence:

```text
NET egress-tcp ok
NET dns        ok
NET RESULT: PASS
qemu-oci: boot=404ms rss=145328 KiB
== A. OCI image execution ==  (all PASS)
== B. Two containers over localhost == (PASS, grillo-oci-localhost-ok)
```

## Decision

Propose **QEMU `microvm` + `virtiofsd`** as the initial F0 backend; reject
Firecracker for the product because it cannot do live host-directory binds. See
[ADR 0005](../adr/0005-platform-qemu-virtiofsd.md) (Proposed). Cloud Hypervisor +
virtiofs remains the untested lower-footprint alternative.

## Risks and doctor prerequisites

- **virtiofsd sandbox.** The spike uses `--sandbox=none`; production needs a
  reviewed sandbox/seccomp posture. Do not claim isolation it does not provide.
- **QEMU surface.** Prefer `microvm` with the minimal device set; avoid default
  NIC/storage; document pinned QEMU/virtiofsd versions and checksums.
- **Kernel option.** The guest kernel must include `CONFIG_VIRTIO_FS` (and
  `CONFIG_VIRTIO_VSOCKETS` for the management channel).
- **Device permissions.** `/dev/kvm`, `/dev/vhost-vsock`, and (for TAP helpers)
  `/dev/net/tun` must be accessible to the user; `doctor` must explain
  distribution restrictions rather than silently escalate.
- **Overhead.** QEMU RSS is ≈3× Firecracker; treat tens-to-~150 MiB as a tracked
  budget, not a promise.
- **Unreaped orphan children.** The guest PID 1 does not yet reap all reparented
  containers; T07 owns this.
- **Parent death.** VMM helpers use `Pdeathsig`; T08 must still reconcile orphans
  by identity (boot time, executable) rather than PID alone.

## Support matrix (F0 evidence scope)

| Dimension | Supported in evidence |
|---|---|
| Host | Linux/amd64 with KVM |
| Hypervisor | QEMU `microvm` (proposed), Firecracker (boot/OCI only) |
| Sharing | virtiofs live binds (QEMU); none (Firecracker) |
| Networking | QEMU user-mode SLIRP; `pasta` helper standalone |
| Guest | Linux 6.1.188, static runc, busybox image |
| Management | vsock (Firecracker API UDS or QEMU `vhost-vsock`) |
| Out of scope here | arm64, macOS/Windows, GPU, snapshots, multi-node |

## Helm and state deviations

The plan's provisional choices remain: invoke the pinned official Helm binary via
`os/exec` (no shell) and record the deviation in an ADR when Helm work begins
(T21); use atomic JSON snapshots plus a bounded NDJSON journal instead of SQLite
(T05). No evidence in T03 invalidates either; both stay as planned.

## Review rerun: live updates versus notifications

The corrected `make qemu-share` waits for the guest to read the initial sentinel
and register its watch, then writes a new value from the host. The guest checks
the exact updated content. On the same host, real KVM results were:

```text
STORAGE read-host  ok
STORAGE host-watch DEGRADED: no host write event within 2s; polling required
STORAGE host-update ok
STORAGE write-host ok
STORAGE rename     ok
STORAGE ro-enforced ok
STORAGE guest-watch ok
STORAGE RESULT: PASS
```

PASS covers content/rw/ro/rename and guest-local events, not remote event support
or the full F0 gate. Host-originated notifications remain explicitly degraded.
Storage initramfs SHA-256:
`ab2df454d2d8fdb3f88d603b769ad63cb0fc7be8cd4a9bce3e7e0ba5e49bf5f8`.
The existing-artifact QEMU KVM tests (live share, networking, OCI) were rerun and
passed. No new performance baseline is inferred from this correctness run.

## Not done / next

- Cold-cache, multi-vCPU, and multi-memory benchmarks; PSS and teardown disk use.
- Managed persistent volume (virtio-block) test and filesystem overhead.
- Two-VM DNS, host publishing, and management isolation (still required by T02,
  not deferred to T11).
- QEMU/virtiofsd hardening pass and Cloud Hypervisor comparison.
