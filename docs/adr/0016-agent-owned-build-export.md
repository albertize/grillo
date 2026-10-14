# 0016 — Agent-owned bounded build snapshot export

- Status: Proposed
- Date: 2026-10-14
- Related tasks: T18b, T24
- Supersedes: none

## Context

The block-backed native workspace follow-up in
[ADR 0014](0014-fixed-size-block-volume-quotas.md) needs a trusted way to recover
filesystem changes after RUN. A workload's tar executable is untrusted and may
be absent. Host extraction must treat guest filenames, links, ownership and
stream data as untrusted even after authentication.

## Decision

Add the optional `build-export` capability to protocol 1.1. `export` selects an
already-mounted share by tag in a started build sandbox. It cannot select an
arbitrary guest path or a workload sandbox. `export_result` reports complete
uncompressed tar byte count, entry count and SHA-256 over all bytes, including
the trailer. The existing per-boot authenticated connection carries both data
and the terminal receipt. A checksum detects mismatched/incomplete transfers;
it is not a separate signature or proof that guest data is safe.

The agent generates tar in Go with rooted/no-follow access, batches of 128
directory entries and a 128-level traversal guard. Allowed filesystem entries
are directories, regular files and symlinks. Numeric owners and symlink metadata
are preserved; account names are omitted. Special files and reserved `.wh.*`
snapshot names fail explicitly. RUN and export are serialized per agent.
Failed RUN cleanup latches the build sandbox busy until it is recreated.

Requests must explicitly bound tar bytes (1 KiB to 8 GiB) and entries (1 to
100,000). Limits apply before output writes, including tar padding/trailer. The
guest caps each export at five minutes even for an unbounded caller. Client
calls use the existing whole-request default deadline unless explicitly bounded
by the caller. Cancellation closes the transfer connection to interrupt socket
writes to a non-reading peer. Failed transfers discard the connection.

The client requires advertised capability, validates session/stream/aggregate
bytes and the terminal receipt, and rejects short local writes. The host receiver
charges archive staging and extraction to the same cumulative host-write budget
from [ADR 0015](0015-native-host-write-budgets.md). It verifies the receipt, tar
trailer, names, entry count, numeric owners, duplicate paths and permitted types
before extracting into a new private root. It rejects snapshot whiteouts,
hardlinks, special entries and symlink parents; links themselves remain opaque
container metadata. No host chown is performed. Failed staging is removed;
the caller owns a successfully returned private root and its later adoption.

## Alternatives

- Running container-provided tar cannot establish the required trusted producer.
- Copying guest block files through debugfs or a new mount/FUSE helper adds
  external dependencies and security boundaries not needed for this primitive.
- Writable virtiofs alone preserves coherence but does not enforce guest-write
  quotas; it remains the existing RUN path until block integration is complete.

## Evidence and follow-up

[Software evidence](../experiments/t24-agent-owned-build-export.md) covers actual
authenticated in-memory transport, malformed/failed transfer, non-reading peers,
agent root selection/busy state, host extraction and cumulative spool/payload
limits. These tests do not prove a real block-backed workspace or KVM behavior.
The new agent binary is rebuilt by routine checks; a runnable guest initramfs
must also be rebuilt before later hardware tests.

The native builder does not yet call this primitive. Per-step block-image
creation/population, guest attachment, COPY/RUN coherence, metadata adoption,
VM/image cleanup and failed-publication hardware gates remain open. Existing
virtiofs shares may still be modified by an external host actor; this is not an
atomic filesystem snapshot. Local filesystem stalls are not given a hard I/O
deadline. Existing extraction clears privilege bits and rejects inaccessible
directory layouts rather than promising complete metadata parity. Root-directory
metadata adoption also remains pending. T24 remains
BLOCKED, with no new supported runtime, security acceptance or release claim.

The subsequent [ADR 0017](0017-opt-in-block-build-workspaces.md) wires this primitive
to opt-in native block RUN and root metadata adoption. Its software checks pass;
real KVM acceptance remains blocked. The statements above describe this ADR's
original prerequisite delivery, not a claim that integration is still unwritten.
