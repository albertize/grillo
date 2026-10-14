# 0015 — Cumulative host-write budgets for native image builds

- Status: Proposed
- Date: 2026-10-14
- Related tasks: T18, T24
- Supersedes: none

## Context

Per-image extraction limits do not bound the sum of imported bases, multi-stage
copies, repeated overwrites and layer staging in one native build. Checking the
workspace after a write cannot enforce a host-write limit. Guest RUN writes
through virtiofs require a separate filesystem enforcement path, as recorded in
[ADR 0014](0014-fixed-size-block-volume-quotas.md).

## Decision

Use one private, sequential budget per native build for host staging writes.
The defaults are 4 GiB of cumulative regular-file payload plus compressed layer
staging, and 100,000 conservative path-entry charges. Zero selects these finite
defaults; negative limits fail. Internal `NativeBuilder.MaxHostWriteBytes` and
`MaxHostEntries` permit explicit policy selection. No new daemon flag is added.

Reserve declared payload and path components before filesystem effects. Count
overwrites, repeated parent components, failed writes, all stages and imported
images without refunds. The OCI extractor accepts an enclosing `BeforeEntry`
reservation callback, called before payload creation or whiteout effects. Its
existing extraction/archive limits still apply independently.

Charge compressed layer bytes before writing the temporary archive, including
the gzip footer, and apply the per-blob limit there too. CAS publication retains
its separate quota and digest verification. Failed builds do not publish an
image reference; already committed intermediate CAS blobs may remain.

Use Go's `os.Root` for context copy and snapshot/layer access. Stream directory
and glob scans in batches of 128, bound glob scans, snapshots and recursion,
check cancellation during file reads, and avoid following final source-file
symlinks when copying or capturing regular files. Preserve symlink entries;
apply COPY chmod to an open regular-file descriptor. Dockerfile and dockerignore
inputs are each capped at 1 MiB; dockerignore errors other than absence fail.

## Alternatives

- Per-file limits permit many-file and multi-stage accumulation.
- Post-write scans permit disk consumption before rejection.
- Mounting host quota filesystems requires provisioning outside normal rootless
  operation. No host mount, new helper, dependency or sudo is introduced here.
- Block-backed RUN plus trusted authenticated export remains the follow-up;
  these host checks cannot stand in for that implementation or its KVM evidence.

## Consequences and evidence

The budget measures cumulative host writes, not peak workspace usage, physical
disk allocation, free-space reservation, CAS size or whole-host isolation.
Conservative entry counting may reject builds with fewer actual retained files.
Each build has its own budget; concurrent-build aggregate admission is pending.
Snapshots have a separate 100,000-entry cap and recursion is limited to 128 path
components. No hard deadline for arbitrary stalled host filesystem I/O is claimed.
RUN remains writable virtiofs and is not bounded by this policy.

[Delivery evidence](../experiments/t24-native-host-write-budgets.md) records unit,
failure and routine checks. Real build-guest validation requires `/dev/kvm`,
which is absent in this environment. T24 remains BLOCKED.

[ADR 0017](0017-opt-in-block-build-workspaces.md) subsequently adds an opt-in
block runner and geometry charges to this budget. Default virtiofs RUN and the
missing real workspace hardware gate remain outside the original evidence.
