# T24 continuation: opt-in block native build integration

Date: 2026-10-14. **T24/F3/D0 remain BLOCKED.** The native block path is now wired
in software; guest-write enforcement is not accepted without its real KVM gate.

## Environment and dependencies

Linux/amd64, kernel `7.2.9-200.fc44.x86_64`, Go 1.27.2, mke2fs 1.47.3. Existing
source checkout and frontend dependencies; `/dev/kvm` is absent. Changes remain
uncommitted. Inspected T06 protocol, T09 OCI/CAS, T10 ext4/virtio-blk, T18/T18b
native build and T23 daemon contracts plus the preceding
[host budgets](t24-native-host-write-budgets.md) and
[export primitive](t24-agent-owned-build-export.md).

The existing `/usr/bin/mke2fs` SHA-256 observed here was
`23faf37790816bafce8c8e50a5c528dc7c11534399c5172d835395b7c15aa9a8`.
This is a local receipt, not an independent provenance/security acceptance.
Its tar-population probe returned exit 1 with libarchive support unavailable.
Owned `/tmp` probe files were removed by that operation. The selected path
formats empty images and populates them with trusted agent code instead.
No dependency installation, libarchive addition, host mount, privilege escalation,
network provisioning or release publication was performed.

## Delivered path

The daemon selects BlockRunner when `--build-workspace-bytes N` is nonzero;
zero keeps legacy virtiofs. The formatter path comes from `--mke2fs` or reviewed
PATH selection. Each RUN reserves fixed raw geometry, creates a checksummed
bounded tar seed, formats an owned disk and boots a guest with that disk plus a
read-only seed share. The agent requires a fresh ext4 target, verifies the seed
and populates it before running a command. A separate offline backend avoids
the build-network launcher when `network=none`.

The host adopts a validated exported root only after guest cleanup. Subsequent
COPY/RUN sees prior additions/deletions and numeric ownership; root metadata is
kept for subsequent seeds and OCI layers. Seed/spool/extraction/layer staging and
image geometry share the host budget. RUN output has a 4 MiB aggregate bound and
the step has a five-minute ceiling. Cleanup errors override success; unresolved
state is retained, image references are not published, and native admission is
blocked while retained/interrupted scratch remains. Existing image references
and managed/bind volumes are not automatically removed.

## Verification

- Focused `go test ./internal/build ./internal/guest ./internal/guestproto
  ./internal/oci ./cmd/grillod` PASS. An initial compile failed on an undeclared
  selected-backend variable; corrected before the passing checks.
- Real mke2fs formats 32 MiB images in unit tests; size/privacy/ext4 magic are
  checked. This proves formatting only, not mounted guest behavior or ENOSPC.
- Orchestration fixtures cover COPY/RUN interleaving, deletions, root/file owner
  metadata, offline selection, old-agent rejection, boot/start/stop failures,
  incomplete export, retained live-disk contract, before-boot reservation,
  failed publication and aggregate output limits. They are software contracts.
- Guest import tests exercise actual extraction into owned host test directories:
  checksums, count, path/whiteout rejection, source/device/overlap policy,
  cancellation, special-mode handling and refusal to delete preexisting data.
  These are not mounted ext4/KVM tests or proof of guest-root chown.
- Protocol tests reject mismatched RUN sessions/streams/completion; daemon tests
  exercise queued cancellation, cleanup latch and retained-state refusal.
- `go test -tags kvm -run '^$' ./internal/build` compiles hardware-tagged tests
  without running them; the final compile also PASS.
- Final `make check` PASS: frontend sources/tests, formatting, vet, unit/race,
  script tests, host/agent rebuild and module audit. Earlier passing runs cover
  successive changes; none is substituted for hardware evidence.
- `make fmt`, changed-document Markdown fences/214 local links and
  `git diff --check` PASS. No generated guest, binary or frontend assets are in
  the diff. No new fuzz campaign was run; the prior low-coverage export smoke
  remains historical, not proof for the new guest seed import.
- `make test-t24-build-workspace` **FAIL/BLOCKED**, exit 2:
  `BLOCKED: readable/writable /dev/kvm required`. It stopped before guest rebuild
  or execution; no hardware pass/skip is substituted.

## Reproducible hardware gate and limits

After deliberately provisioning the existing guest/OCI/KVM prerequisites:

```sh
make test-t24-build-workspace
```

The target rebuilds the agent initramfs before exercising 32 MiB native RUN,
guest ENOSPC followed by deletion/retry, COPY/RUN alternation, metadata and owned
cleanup. Missing KVM, reviewed formatter or artifacts fails the gate. Failed
test-owned state is retained for review rather than deleting a possibly live disk.

Hardware correctness, crash/repair/recovery, full metadata parity, performance,
resource/guest load, independent-host receipts and release/source acceptance
remain incomplete. Current block images have no journal and are sparse. The
default native RUN path is still unbounded for guest writes. No advisory rerun
or runc/Helm replacement was performed; prior findings remain unresolved.
See [ADR 0017](../adr/0017-opt-in-block-build-workspaces.md).
