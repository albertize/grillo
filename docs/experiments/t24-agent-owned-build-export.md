# T24 continuation: agent-owned build export and private host extraction

Date: 2026-10-14. **T24/F3/D0 remain BLOCKED.** This is a software prerequisite
for block-backed native RUN, not a completed workspace or hardware quota gate.

## Environment and scope

Linux/amd64, kernel `7.2.9-200.fc44.x86_64`, Go 1.27.2; existing source checkout
and frontend dependencies. `/dev/kvm` is absent. Work remains uncommitted.
No provisioning, host mount, formatter invocation, policy change or publication.
Reviewed T06 guest framing/auth/deadlines, T18/T18b runner and
[host-write budget delivery](t24-native-host-write-budgets.md).

## Delivered contract

The guest advertises optional `build-export` and produces full tar snapshots in
agent code. Requests select only a known mounted tag in a started build sandbox.
RUN/export cannot overlap. Numeric metadata and symlinks are represented without
executing workload tools. Byte/entry/depth bounds and cancellation apply during
export; terminal counts and checksum include the tar trailer.

The client checks capability, session/stream, aggregate bytes and terminal
receipt before accepting success. Failures close the connection. Cancellation
also closes guest stream writes, including non-reading peers. Failed RUN cleanup
overrides command success and leaves the build sandbox busy pending recreation.

The host receiver charges tar staging and extraction to one budget, validates
the full archive, then creates a private root. Numeric owners remain OCI metadata
without host chown. Unsafe paths/links, duplicate names, special files,
whiteouts, incomplete trailers, nonzero trailing data and inconsistent receipts
fail. Owned temporary archives and failed roots are removed. The existing native
builder is not wired to this receiver yet.

## Verification

- `go test ./internal/guestproto ./internal/guest ./internal/build` PASS.
  A sandbox attempt failed on the read-only Go cache; the authorized rerun
  passed.
- Protocol tests use real per-boot-key handshake and framing over `net.Pipe`:
  successful transfer, capability rejection, byte/hash/count/frame failures,
  output-writer failure, absent completion, request timeout and non-reading peer.
- Agent tests cover declared root selection, ordinary/not-started/busy sandbox
  rejection, numeric/symlink metadata, exact archive/trailer limits, cancellation,
  entry limits and success followed by failed RUN cleanup.
- Host tests cover payload/owner/link preservation, private roots, malicious
  names/types/link parents, duplicates, reserved whiteouts, checksum/count
  mismatch, truncation/trailing data, cancellation, quota refusal, partial
  completion and combined staging/extraction accounting.
- Two `make check` runs PASS, including frontend, formatting, vet, unit/race,
  scripts, rebuilt host/agent binaries and module audit. The final run includes
  nonblocking/no-follow directory access and deadline-close error normalization.
- `go test ./internal/build -run '^$' -fuzz '^FuzzUnpackBuildExport$'
  -fuzztime=5s -parallel=2` PASS: 2 seeds, only 5 executions in approximately
  6 seconds, no new interesting inputs. The low execution count/stall limits
  coverage; this is not a longer or complete fuzz campaign.
- `make fmt`, changed-document Markdown fences/195 local links and
  `git diff --check` PASS. No generated binaries, guest artifacts or frontend
  assets are included in the diff.

## Missing gates

`make test-builder-kvm` was not run because `/dev/kvm` is absent. The agent binary
is rebuilt by routine checks; no guest initramfs or KVM run is claimed.
No new advisory scan, external binary replacement, benchmark or hosted/clean-host
gate was performed. Prior runc/Helm findings remain unresolved.

RUN still uses writable virtiofs. Block image formatting/population/attachment,
interleaved COPY/RUN state adoption, ownership/privilege-bit compatibility and
per-step cleanup require the next bounded integration and hardware evidence.
Neither the authenticated receipt nor unit tests make guest content trusted.
See [ADR 0016](../adr/0016-agent-owned-build-export.md) for limits and follow-up.
