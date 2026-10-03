# 0004 — Compare backends for live filesystem sharing

- Status: Proposed
- Date: 2026-10-03
- Related tasks: T02, T03
- Supersedes: none

## Context

The plan's fixed decision is one microVM per Pod, and the T02 gate requires
"live read/write/read-only binds" without privileged host mounts or copy-based
substitutes. T02 established that the candidate backend cannot meet this:
Firecracker v1.17.0 exposes no virtio-fs or 9p device, and the guest kernel has no
`CONFIG_9P_FS`. Storage is limited to virtio-block and virtio-pmem. See
[the T02 report](../experiments/t02-oci-and-storage.md) and `make storage-probe`.

The plan says explicitly: "If these constraints prevent a rootless product,
compare QEMU `microvm` or a backend with suitable filesystem sharing."

## Options considered

- **Keep Firecracker and build a FUSE- or 9p-over-vsock bridge.** Live binding is
  possible in principle (the guest has `CONFIG_FUSE_FS`), but this is a
  security-sensitive virtualization component. The plan prefers specialized
  external components and forbids writing one without a concrete need and review.
- **QEMU `microvm` machine with `virtiofsd`.** Mature shared-filesystem device
  (`vhost-user-fs`/`virtiofsd`), works rootless with a user namespace; heavier
  than Firecracker and needs its own hardening review.
- **Cloud Hypervisor with virtiofs.** Rust VMM with virtio-fs and a smaller device
  model than full QEMU; another dependency to pin, verify, and harden.
- **Accept copy-based or block-only storage.** Rejected: the plan forbids
  copy-based bind substitutes, and block devices do not provide directory binds.

## Decision

Propose that T03 evaluate **QEMU `microvm` + `virtiofsd`** and **Cloud
Hypervisor + virtiofs** against the T01/T02 criteria (rootless boot, OCI
execution, localhost co-location, rootless networking, live read/write/read-only
binds, rename/watch, measured overhead) and record the result as the platform
ADR. Do not commit to a backend until live sharing is demonstrated on real
hardware. Firecracker remains viable only for scenarios without live binds.

## Evidence

- T02 OCI execution and two-container localhost: PASS on real KVM (Firecracker).
- T02 storage probe: no shared-filesystem device; `make storage-probe` exits 3.
- T01/T02 supply the reuse criteria and harness (`experiments/boot/`).

Not yet gathered: any QEMU/Cloud Hypervisor experiment, virtiofsd behavior,
rootless networking inside a guest, or shared-fs performance.

## Consequences

Backend choice may change, affecting `internal/backend/*`, guest device setup,
and rootfs/storage design — none of which exist yet, so the cost is contained.
Adding QEMU or Cloud Hypervisor adds a larger external dependency to pin, verify,
and document. Until the comparison completes, storage-dependent tasks (T09, T10)
and the F0 gate (T03) stay blocked; pure IR/frontend work (T04, T17, T20) can
proceed.

## Validation and follow-up

**Performed (T03).** The comparison was executed on real hardware:
Firecracker exposes no shared-filesystem device (`make storage-probe` exits 3),
while QEMU `microvm` + virtiofsd passes live read/write, rename, guest-local
inotify, and read-only enforcement. The corrected post-mount host-write probe
observed content updates but no host-originated notification within 2s
(DEGRADED). See [ADR 0005](0005-platform-qemu-virtiofsd.md) and the
[T03 report](../experiments/t03-backend-comparison.md). Cloud Hypervisor remains
untested. T02/T03 are still BLOCKED on the missing topology, persistence, and
overhead evidence; this comparison is partial, not an accepted F0 gate.
