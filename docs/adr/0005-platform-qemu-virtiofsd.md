# 0005 — Platform: QEMU `microvm` + virtiofsd for the F0 backend

- Status: Proposed
- Date: 2026-10-03
- Related tasks: T02, T03, T08, T10
- Supersedes: none (amends the provisional Firecracker candidate in the plan)

## Context

The plan fixes one microVM per Pod and requires live read/write/read-only host
directory binds (scenario G) without privileged host mounts or copy-based
substitutes. [ADR 0004](0004-shared-filesystem-backend-comparison.md) required a
backend comparison after T02 showed Firecracker exposes no virtio-fs or 9p
device. The corrected T02 probe demonstrates live content sharing on QEMU +
virtiofsd, but not host-originated inotify notifications. T02/T03 remain BLOCKED
on the outstanding plan requirements; this ADR does not reduce their scope.

## Options considered

- **Firecracker (candidate).** Smallest device model and lowest memory, but no
  shared-filesystem device, so scenario G is impossible without writing a custom
  FUSE/9p bridge. Rejected for the product.
- **QEMU `microvm` + `virtiofsd`.** Rootless boot, OCI execution, localhost
  co-location, live virtiofs binds, and rootless networking all verified in this
  repository (`make test-qemu`). Larger dependency and device surface; needs
  hardening.
- **Cloud Hypervisor + virtiofs.** Not installed or evaluated; remains the
  fallback if QEMU overhead or its attack surface proves unacceptable.

## Decision

Propose **QEMU `microvm` + `virtiofsd`** as the initial F0 backend, with Firecracker
retained only for scenarios that need no live sharing. Keep the backend behind the
`internal/sandbox` contract so the choice can change. Confirm this ADR only after
completing T02 evidence, maintainer review, and a documented QEMU hardening pass.

## Evidence

All results are on the T03 host (Linux/amd64, host kernel `7.2.8-200.fc44.x86_64`,
Intel Core Ultra 7 155H, UID 1000, no sudo). See
[the T03 report](../experiments/t03-backend-comparison.md) and `make test-qemu`.

| Capability | Firecracker 1.17.0 | QEMU 10.2.2 microvm + virtiofsd 1.14.0 |
|---|---|---|
| Rootless boot | yes (median 260 ms) | yes (≈400 ms to handshake) |
| OCI run/exec/signal/delete | yes | yes (`vhost-vsock` management) |
| Two containers on localhost | yes | yes |
| Live rw/ro binds, rename | **no device** | yes (post-mount host update verified) |
| Guest-local / host-originated inotify | n/a | guest-local passes; host-originated DEGRADED (no event within 2s) |
| Rootless guest egress + DNS | not wired | yes (user-mode networking) |
| Measured VMM RSS | ≈49 MiB | ≈142 MiB |

Firecracker's storage probe (`make storage-probe`) exits `3` (BLOCKED).

## Consequences

The runtime must manage a larger external dependency (pin, verify, harden, and
document QEMU and virtiofsd) and prefer `microvm` with the smallest device set.
The guest kernel needs `CONFIG_VIRTIO_FS` for QEMU (the Firecracker CI config
omits it). Management uses `vhost-vsock` (host AF_VSOCK) instead of the
Firecracker API socket; guests can also expose virtio-fs. Memory overhead is
roughly 3× Firecracker and must be tracked as a budget. Sharing security now
depends on virtiofsd sandbox settings, which require explicit review.

## Validation and follow-up

- Confirm with maintainer review; run a QEMU/virtiofsd hardening pass (seccomp,
  `--sandbox`, restricted device set, no default NIC or storage).
- Measure cold-cache boot, RSS/PSS at 1/2/4 vCPU and 128/256/512 MiB.
- Evaluate Cloud Hypervisor + virtiofs as the lower-footprint alternative.
- Complete T02's two-VM DNS/egress/host publishing/management-isolation,
  managed-volume persistence, and filesystem-overhead experiments before F0.
- Decide how development watchers handle the observed missing host-originated
  notifications; content coherence alone does not provide event delivery.
