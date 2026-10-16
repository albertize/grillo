# 0017 — Opt-in per-step block workspaces for native RUN

- Status: Proposed
- Date: 2026-10-14
- Related tasks: T10, T18, T18b, T24
- Supersedes: none

## Context and formatter evidence

[Host budgets](0015-native-host-write-budgets.md) cannot enforce guest writes
through virtiofs. [Agent export](0016-agent-owned-build-export.md) supplies the
bounded return path. The existing reviewed mke2fs 1.47.3 on this host rejects
`-d root.tar` because it was built without libarchive; the actual probe failed.
No new formatter dependency or implicit fallback is introduced to hide that.

## Decision

`grillod --build-workspace-bytes N` selects an experimental block runner for
native RUN. Zero retains the existing virtiofs runner. Geometry must be at least
16 MiB and divisible by 4096. `--mke2fs` selects the same trusted 1.47.3
formatter already used for opt-in block volumes. Each RUN uses a fresh private
raw ext4 image; mke2fs only formats it, never mounts it or reads workload files.
The current images disable journaling, matching the experimental volume policy.

The host creates a full tar seed from its owned staging root, including numeric
ownership and root metadata. It charges image geometry and all seed/export/
extraction/layer staging against the cumulative per-build host budget. Only the
seed directory is exposed through read-only virtiofs. QEMU attaches the workspace
as one raw disk; the agent mounts `/dev/vda` at the build root.

The guest must advertise `block-build` and `build-export`. `SandboxSpec.buildSeed`
selects disjoint read-only input and writable ext4 shares, with explicit archive
size/count/checksum. The agent verifies and extracts `root.tar` itself before
reporting ready. It removes only the empty formatter-created lost+found directory;
other preexisting data refuses population. No workload executable populates or
exports the root. Old guests fail with a rebuild diagnostic, without fallback.

After successful RUN, the host receives and validates the full export. It stops
and deletes the guest before adopting that root through renames with rollback
on failed replacement. Every subsequent host COPY and RUN sees the adopted state.
Root UID/GID/mode and file ownership are retained for the next seed and OCI layer
metadata. Private build extraction preserves special modes without host chown;
ordinary OCI extraction still clears privilege bits. No staged workload file is
executed on the host. Inaccessible directory layouts may still fail explicitly.

RUN has a five-minute ceiling and a 4 MiB aggregate stdout/stderr limit even
without a progress consumer. The block runner honors offline requests through
a distinct backend without the build-network launcher. Native builds serialize
per daemon, with cancellation while queued. No host-wide concurrency/reservation
or throughput claim follows from this admission policy.

All guest cleanup and owned scratch removal precede image-reference publication.
Unresolved cleanup retains disk/input state and blocks further native builds.
Interrupted scratch also refuses new builds after daemon restart; operator
review, stopping owned guests and explicit cleanup are required. No automatic
recursive cleanup of unknown state, managed volumes or bind paths is added.

## Evidence and remaining acceptance

The [fault/load continuation](../experiments/t24-workspace-faults-advisory-review.md)
adds real RUN cancellation, owned VMM SIGKILL and output-limit failures with
prior-reference preservation, VMM death, scratch cleanup and fresh successful
builds. Ten sequential race-detector repetitions pass. Persistent-volume
repair, daemon-crash recovery and whole-host pressure remain unverified.

The [2026-10-16 KVM follow-up](../experiments/t24-block-workspace-kvm.md)
passes the bounded hardware gate after correcting the dynamically linked
BusyBox fixture and ENOSPC shell quoting. Published root/output metadata and
successful scratch cleanup are now asserted. KVM/vsock are accessible outside
the session sandbox. This supersedes the hardware-unavailable disposition
below; crash/recovery/load and overall T24 acceptance remain incomplete.

[Delivery report](../experiments/t24-opt-in-block-build-workspaces.md) records real
formatter calls, software orchestration/import/export/metadata/failure tests,
routine checks and the missing hardware prerequisite.

`make test-t24-build-workspace` requires accessible KVM before rebuilding the
agent guest. The actual test requires reviewed formatter and artifacts, and
checks guest ENOSPC/retry, interleaved COPY/RUN and cleanup without a skip.
The initial delivery was BLOCKED because `/dev/kvm` was absent inside the session
sandbox. Later host execution passes the bounded gate as linked above. Test
compilation and fake-backend orchestration alone are not KVM evidence.

The path remains opt-in with bounded hardware fault/retry evidence. Daemon/host
crash, persistent-volume fsck/repair, full metadata/load/performance and
independent-host acceptance remain incomplete.
Sparse images do not reserve physical free space; capacity is raw geometry,
not usable payload. Geometry is a reservation charge, not cumulative guest-write
traffic. Custom policy is not persisted by CLI on-demand startup. T24/F3/D0
remain BLOCKED; no supported release or external advisory clearance is claimed.
