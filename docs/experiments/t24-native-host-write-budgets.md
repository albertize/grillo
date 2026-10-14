# T24 continuation: native host-write budgets

Date: 2026-10-14. **T24/F3/D0 remain BLOCKED.** This delivery bounds host staging
in native builds; it does not provide a block-backed RUN workspace or a release.

## Environment and dependencies

Linux/amd64, kernel `7.2.9-200.fc44.x86_64`, Go 1.27.2, source checkout with
uncommitted changes. `/dev/kvm` is absent. Existing frontend dependencies were
used; no installation, host mount, security-policy change or publication.
Dependencies inspected: T09 OCI extraction/CAS, T18 native builder, T18b runner
and T23 daemon integration. The production daemon constructs NativeBuilder with
zero policy fields, so the finite defaults apply there too.

## Contract delivered

One budget covers all host copies, imported image extraction and compressed
layer staging in a build. Defaults: 4 GiB cumulative writes and 100,000
conservative path-entry charges. Overwrites and all stages count; rejected
writes are checked before creating parents/files. A failed build preserves an
existing image reference and removes its owned scratch directory. Intermediate
verified CAS blobs may remain under the existing CAS policy.

Context copies and snapshot/layer reads use `os.Root`. Glob and directory scans
are streamed; snapshots, glob scans and recursion are bounded. File reads check
cancellation and copy only the opened file's declared size. COPY chmod acts on
the regular-file descriptor; symlinks retain their link semantics. Dockerfile
and dockerignore reads are capped at 1 MiB; unreadable/invalid dockerignore
inputs fail rather than silently disabling exclusions.

See [ADR 0015](../adr/0015-native-host-write-budgets.md) for precise accounting
and [previous evidence](t24-pinned-cache-block-quota-sources.md) for volume quotas,
seeded budgets, external binary findings and source/provenance blockers.

## Verification

- Initial `go test ./internal/build ./internal/oci` failed because the sandbox
  made the existing Go build cache read-only. The authorized rerun passed.
- `go test ./internal/build ./internal/oci` passed with the new regressions:
  exact copy/snapshot boundaries, over-budget admission before creation,
  overwrite and multi-stage accounting, imported-base accounting, bounded glob
  scans, cancellation, rooted copy behavior, symlink preservation, metadata
  limits, per-blob pre-write rejection, scratch cleanup and prior-image retention.
- OCI tests verify the enclosing reservation runs before regular payload and
  whiteout effects and persists across separate image extractions.
- Two `make check` runs passed: frontend build/tests/source consistency,
  formatting, Go vet/unit/race, script tests, host/agent builds and module audit.
  The second includes corrected root WORKDIR handling.
- Final `go test -race ./internal/build ./internal/oci` passed after making the
  exact layer-boundary test independent of wall-clock timestamps. The test
  accepts the complete compressed archive size and rejects one byte less,
  including trailer writes, while cleaning temporary archives.
- `make fmt`, Markdown fences and 179 local links in changed documentation,
  and `git diff --check` passed. No generated frontend or binary artifacts were
  included in the diff.

## Missing gates and limits

`make test-builder-kvm` was not run: `/dev/kvm` is absent. Unit tests prove the
host builder/extractor contract, not real guest behavior. No guest code or
protocol changed. No new advisory scan, benchmark, hosted/independent-host run
or external runc/Helm replacement was performed in this bounded delivery.
The prior reported external binary findings remain unresolved.

RUN writes through virtiofs remain unbounded. The host policy is per build,
not aggregate concurrent admission, physical free-space reservation or a deadline
for arbitrary stalled filesystem I/O. A complete native workspace needs real
guest-write enforcement, interleaved COPY/RUN coherence and trusted bounded
authenticated export/extraction, followed by rebuilt guest/KVM evidence.
