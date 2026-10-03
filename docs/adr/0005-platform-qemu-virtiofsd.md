# 0005 — Platform: QEMU `microvm` + virtiofsd for the F0 backend

- Status: Proposed (all required T02/T03 evidence now exists; awaiting maintainer acceptance)
- Date: 2026-10-03
- Related tasks: T02, T03, T08, T10, T11
- Supersedes: none (amends the provisional Firecracker candidate in the plan)

## Context

The plan fixes one microVM per Pod and requires live read/write/read-only host
directory binds (scenario G) without privileged host mounts or copy-based
substitutes. [ADR 0004](0004-shared-filesystem-backend-comparison.md) required a
backend comparison after T02 showed Firecracker exposes no virtio-fs or 9p
device. T02/T03 have now completed the comparison with real hardware evidence.

## Options considered

- **Firecracker (candidate).** Smallest device model and lowest memory, but no
  shared-filesystem device, so scenario G is impossible without writing a custom
  FUSE/9p bridge. Rejected for the product; retained only for scenarios that need
  no live sharing.
- **QEMU `microvm` + `virtiofsd`.** Rootless boot, OCI execution, localhost
  co-location, live virtiofs binds, rootless networking, managed ext4 volumes,
  and enforced isolation all verified in this repository (`make test-f0`,
  `make test-qemu`). Larger dependency and device surface; needs hardening.
- **Cloud Hypervisor + virtiofs.** Not installed or evaluated; remains the
  fallback if QEMU overhead or its attack surface proves unacceptable.

## Decision

Propose **QEMU `microvm` + `virtiofsd`** as the initial F0 backend. Keep it behind
the `internal/sandbox` contract so the choice can change. Adopt only after
maintainer acceptance; the required evidence is complete.

## Evidence

Measured on the T03 host (Linux/amd64, kernel `7.2.8-200.fc44.x86_64`, Intel Core
Ultra 7 155H, UID 1000, no sudo, SELinux **Permissive**). See the
[T02](../experiments/t02-oci-and-storage.md) and
[T03](../experiments/t03-backend-comparison.md) reports.

| Capability | Firecracker 1.17.0 | QEMU 10.2.2 microvm + virtiofsd 1.14.0 |
|---|---|---|
| Rootless boot | yes (median 260 ms) | yes (median 605 ms to handshake) |
| OCI run/exec/signal/delete | yes | yes (`vhost-vsock` management) |
| Two containers on localhost | yes | yes |
| Two VMs + DNS + egress | not wired | yes |
| Host publishing (loopback) | n/a | yes |
| Live rw/ro binds, rename | **no device** | yes (post-mount host update verified) |
| Host-originated inotify | n/a | **DEGRADED** (no event within 2s; polling) |
| Managed ext4 volume + persistence | virtio-block | virtio-block (exclusive lock, e2fsck clean) |
| VMM RSS / PSS after boot | ≈49 MiB | ≈122 MiB / ≈102 MiB |

Every QEMU launch was re-checked to have `CapEff=0`, `NoNewPrivs=1`, and
`Seccomp=2`; guests are confined by a default-deny namespace firewall with
host-only publishing.

Distribution restriction: with SELinux **Enforcing**, processes spawned by `pasta`
die silently (exit 2, empty output) on the tested Fedora policy. `doctor` must
detect and explain this; evidence above was collected with SELinux Permissive.

## Consequences

The runtime must manage a larger external dependency (pin, verify, harden, and
document QEMU and virtiofsd) and prefer `microvm` with the smallest device set
(`-nodefaults`, no monitor, `-sandbox on`). The guest kernel needs `CONFIG_VIRTIO_FS`
(the Firecracker CI config omits it). Management uses `vhost-vsock` (host
AF_VSOCK). Memory overhead is ≈2.5× Firecracker and must be tracked as a budget.
Sharing security depends on virtiofsd sandbox settings, which require explicit
review. Host-originated file-watch events are not delivered reliably, so
development watchers must poll.

## Validation and follow-up

- Accept after maintainer review; the required T02/T03 evidence exists.
- Measure cold-cache boot and RSS/PSS at 1/2/4 vCPU and 128/256/512 MiB.
- Re-measure virtiofs with a production cache policy; the reported metadata
  overhead used `--cache=never` and is a conservative upper bound.
- Evaluate Cloud Hypervisor + virtiofs as the lower-footprint alternative.
- Solve host-originated notifications or document polling as the supported
  behavior (T11/T27).
