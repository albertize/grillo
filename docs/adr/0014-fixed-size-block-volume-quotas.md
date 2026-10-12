# 0014 — Opt-in fixed-size block volumes for enforced guest-write quotas

- Status: Proposed
- Date: 2026-10-12
- Related tasks: T10, T24
- Supersedes: none

## Context

CAS/rootfs logical quotas and a pre-write host directory-size scan cannot enforce
workload writes through virtiofs. Watchers, polling, reservation counters or
killing a guest after overflow are not hard filesystem quotas. Host bind paths
must remain explicit sharing and must never be migrated/deleted to manufacture
quota evidence. Rootless host operation must not mount a filesystem as root,
configure project quotas, install FUSE/helpers or escalate privileges.

## Options considered

- Host filesystem project/user quotas: useful on a provisioned host, but require
  host-specific privileged policy/configuration; not silently configured here.
- Monitor directory size or cap individual virtiofsd files: not aggregate
  enforcement and subject to timing/many-small-file bypasses.
- Fixed raw ext4 block devices: already supported by QEMU/guest; guest kernels
  perform the mounts while virtual geometry bounds data writes on the host.
- A new filesystem/FUSE server: much larger security-sensitive implementation
  and dependency surface, not warranted for this bounded delivery.

## Decision

Implement **opt-in** fixed-size ext4 images for newly created non-bind
managed/PVC/ephemeral volumes. `grillod --volume-image-bytes N` enables the mode;
zero preserves existing directory behavior. Never automatically migrate or
resize preexisting directory data. Existing legacy non-bind records block use
in bounded mode with an explicit migration diagnostic.

Each volume attaches as a raw virtio-blk device. The guest mounts ext4 and all
containers in the same Pod use that mount; read-only attachments are enforced
both by QEMU and the guest mount. Existing RWO/lease rules remain in force.
Device names limit the mapping to 26 block volumes per Pod; this naming guard
is not evidence for a 26-device hardware/load budget.

The reviewed external formatter is distro-managed **mke2fs 1.47.3**, executed
with argument arrays, sanitized environment, context and 4 KiB output bound.
It is required only when opting into block storage. No fetch or host mount is
performed. Private same-directory images are formatted, checked, fsynced and
published with no-replace rename. Metadata is atomic; an interrupted image is
counted and blocks recreation without being silently deleted.

A private, owned/no-follow/single-link flock serializes admission across
cooperating managers/processes. Flat-store scans stream at most 128 entries
per read; image count and aggregate logical geometry are finite. Images left
by interrupted work count. No automatic eviction. Physical host allocation,
filesystem metadata and unrelated user data are not falsely counted as a
whole-host quota. Sparse images do not reserve actual free disk space.

## Evidence

[Current continuation report](../experiments/t24-pinned-cache-block-quota-sources.md)
records real executor/KVM ENOSPC, fixed host image size, retry after file removal,
persistence across replacement VMs and explicit owned deletion. Repeated race
and child-process tests cover aggregate admission, retained interrupted stages,
cancellation, format failure, geometry changes and legacy data preservation.
Fake-formatter failure proves the cleanup contract only, not real ext4 behavior.

## Consequences and limitations

- Capacity records refer to **image geometry**; usable file payload is smaller
  because ext4 metadata occupies space. Minimum image size is 16 MiB and size
  must be 4096-byte aligned. Exact Kubernetes usable-capacity semantics are not
  claimed; this needs compatibility review before making the mode default.
- The current experimental formatter disables the journal. Explicit sync and
  clean replacement persistence have evidence; hard power-loss/fsck/repair and
  dirty read-only recovery are not acceptance-complete. No automatic repair or
  privileged host mount is added.
- Bind mounts remain virtiofs; their host-data exposure is intentional and is
  **not** made bounded by this mode. Existing directory volume mode also remains
  unbounded for guest writes. Neither is hidden as hard quota enforcement.
- Opt-in policy must be supplied again when starting/restarting the daemon.
  CLI on-demand default startup does not persist these custom flags; directory
  mode refuses attaching recorded block images rather than silently treating
  them as directories. Persisted operator configuration/doctor/UI support is
  still an adoption gate.
- Read-only sharing, multi-Pod concurrency, crash boundaries and persisted
  mode/migration need broader acceptance before adoption as the default.
- Optional formatter versions outside the reviewed pin fail explicitly; ordinary
  optional formatter tests skip when that prerequisite is unavailable. The KVM
  quota target fails on missing prerequisites rather than pretending a pass.

## Workspaces and follow-up

Native `RUN` roots still use writable virtiofs and are **not** covered. Safe
workspace enforcement needs a separate real block-backed build path with bounded
host staging and export, not reusing this proof as a build quota claim.

A coherent follow-up must handle interleaved host `COPY` and guest `RUN`, share
root selection, ownership/whiteouts, bounded authenticated archive export,
traversal/link rejection on host extraction, cancellation/non-reading peers,
per-step VM/image cleanup and failed publication. Container-provided `tar`
cannot be treated as trusted. No speculative protocol/package is implemented
until its contract and tests are scoped. T24 remains BLOCKED for this work,
broader performance/security/load gates and release/source acceptance.
