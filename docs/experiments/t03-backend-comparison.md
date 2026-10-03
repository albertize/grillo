# T03 — F0 gate and platform decision

Date: 2026-10-03. Status: **DONE — F0 gate passed on QEMU `microvm` + virtiofsd.**
The decision is recorded in [ADR 0005](../adr/0005-platform-qemu-virtiofsd.md).

The plan's gate: confirm VMM/network/sharing/rootfs/supervision with versions and
commands, and record risks, doctor prerequisites, measured budgets, and a support
matrix. Native fresh-host reproduction remains T04–T28 work; this report captures
what was actually executed here.

## Environment and versions

| Item | Value |
|---|---|
| Host | Linux/amd64, `7.2.8-200.fc44.x86_64`, Intel Core Ultra 7 155H, 15 GiB |
| User | real/effective UID 1000, no sudo; mapped namespace root is UID 1000 |
| SELinux | **Permissive** for this evidence (Enforcing silently kills pasta helpers; see below) |
| KVM | `/dev/kvm` API 12; `/dev/vhost-vsock`, `/dev/vhost-net`, `/dev/net/tun` present |
| QEMU | `qemu-system-x86_64` 10.2.2, `microvm` machine, `-sandbox on` |
| virtiofsd | 1.14.0, `--sandbox=namespace --seccomp=kill --cache=never --inode-file-handles=never` |
| Firecracker | v1.17.0 (boot/OCI only; no shared filesystem) |
| pasta | `0^20260728.gf8df3f1-2.fc44.x86_64` |
| Guest kernel | Linux 6.1.188; QEMU build adds `CONFIG_VIRTIO_FS` |
| OCI runtime | static runc 1.5.2 (`599f6f…`); image `busybox:1.37@sha256:bdf57e…` |

## Rootless topology

`pasta` creates a user + network namespace and maps namespace root to host UID
1000 (`/proc/self/uid_map` is asserted to be exactly `0 1000 1`). Inside it the
harness creates `br0`/`br1`, three TAP devices, an nftables default-deny policy,
and a fixed DNS fixture, then boots three QEMU microVMs. QEMU is launched through
`setpriv --bounding-set=-all --inh-caps=-all --ambient-caps=-all --no-new-privs`
and with `-sandbox on,obsolete=deny,elevateprivileges=deny,spawn=deny,resourcecontrol=deny`.

Every VMM is re-checked from `/proc/<pid>/status` and `/proc/<pid>/smaps_rollup`:

```text
CapEff:     0000000000000000
NoNewPrivs: 1
Seccomp:    2
VmRSS:      ~122500 kB
Pss:        ~102000–111000 kB
```

## Gate results

| Capability | Firecracker | QEMU microvm + virtiofsd | Command |
|---|---|---|---|
| Rootless boot | yes, 260 ms median | yes | `make test-kvm` / `test-qemu` |
| Guest console + stop | yes | yes | harnesses |
| OCI run/exec/signal/delete | yes | yes | `make test-kvm` / `test-qemu` |
| Two containers on localhost | yes | yes | scenario B |
| Two VMs + DNS + egress | not wired | yes | `make test-f0` |
| Host publishing (loopback) | n/a | yes | `make test-f0` |
| Guest→management / cross-app | n/a | denied | `make test-f0` |
| Live rw/ro binds, rename | **no shared-fs device** | yes | `make storage-probe` (exit 3) vs `qemu-share` |
| Host-originated inotify | n/a | **DEGRADED** (no event within 2s) | corrected `qemu-share` |
| Managed ext4 volume + persistence | virtio-block | virtio-block | `make test-f0` |
| Measured VMM RSS | ≈49 MiB | ≈122 MiB (PSS ≈102 MiB) | `/proc/<pid>` |

## Build, storage, and crash behaviour

- Guest rootfs is built from the pinned busybox image via podman export; the
  fixture cache is published atomically and rejected if unverified. Cached runc
  is checksum-verified before execution.
- ext4 images are created without privileged host mounts (`mke2fs` on a file); a
  second writable attachment is refused through an exclusive `flock`, and
  `e2fsck -fn` is clean after teardown.
- VMM helpers set `Pdeathsig: SIGKILL` and are killed by process group; the run
  left no orphan QEMU, virtiofsd, or pasta process and no work directory.
- The experiment refuses to run as root and refuses to run under SELinux
  Enforcing.

## Measured budgets (warm cache, 1 vCPU/256 MiB)

- Warm simple VM boot to vsock handshake: median **605 ms**, p95 **606–708 ms**
  over 30 samples, 0 failures.
- VMM RSS ≈122 MiB, PSS ≈102 MiB per idle VM (≈2.5× the Firecracker figure).
- virtiofs vs local ext4 metadata overhead: read ≈56×, rename ≈63×, chmod ≈180×;
  write+fsync ≈1.5×. See the [T02 report](t02-oci-and-storage.md).

## Risks and doctor prerequisites

- **SELinux.** On the tested Fedora policy, SELinux Enforcing causes processes
  spawned by `pasta` to die silently (exit 2, no output). `doctor` must detect and
  explain this; the runtime must not silently fall back or escalate.
- **virtiofsd sandbox.** Production must keep an explicit sandbox/seccomp policy;
  the spike uses `--sandbox=namespace --seccomp=kill` with conservative caching.
- **QEMU surface.** Prefer `microvm`, `-nodefaults`, no monitor, and `-sandbox on`;
  pin and checksum QEMU and virtiofsd.
- **Device permissions.** `/dev/kvm`, `/dev/vhost-vsock`, and `/dev/net/tun` must
  be usable by the user; distributions with stricter userns/SELinux policy need a
  documented doctor path.
- **Host-originated notifications.** Development watchers need polling until this
  is solved (T11/T27).
- **Overhead.** QEMU RSS is ≈2.5× Firecracker and must be tracked as a budget.
- **Guest PID 1.** Reaping and VMM orphan reconciliation belong to T07/T08.

## Support matrix (F0 evidence scope)

| Dimension | Supported in evidence |
|---|---|
| Host | Linux/amd64 with KVM, SELinux Permissive |
| Hypervisor | QEMU `microvm` (selected); Firecracker (boot/OCI only) |
| Sharing | virtiofs live binds (content/rw/ro/rename); host-originated watch degraded |
| Networking | pasta user/net namespace, TAP + bridges, nftables default-deny, user-mode egress |
| Storage | virtio-block ext4 managed volume with persistence and exclusive lock |
| Guest | Linux 6.1.188, static runc, busybox image |
| Management | vsock (AF_VSOCK) |
| Out of scope here | arm64, macOS/Windows, GPU, snapshots, multi-node, certificates |

## Helm and state deviations

The plan's provisional choices remain: invoke the pinned official Helm binary via
`os/exec` (no shell) and record the deviation in an ADR when Helm begins (T21);
use atomic JSON snapshots plus a bounded NDJSON journal instead of SQLite (T05).
No T03 evidence invalidates either.

## Reproduce

```sh
make test-f0      # full rootless F0 topology, isolation, storage, benchmarks
make test-qemu    # QEMU KVM tests: live share, networking, OCI
make test-kvm     # Firecracker KVM tests: boot/exec/stop, OCI
make qemu-share   # serial console for the virtiofs live-share probe
```

Each target builds the needed guest artifacts. A missing `/dev/kvm` is reported by
the tests as a SKIP, never as a pass; SELinux Enforcing is refused with an explicit
message.

## Not done / next

- Cold-cache and multi-vCPU/multi-memory benchmarks; teardown disk accounting.
- virtiofs performance with a production cache policy and PSS/DSS accounting.
- Cloud Hypervisor + virtiofs as a lower-footprint alternative (ADR 0005).
- Two-application network isolation beyond the tested single-app topology is T11.
