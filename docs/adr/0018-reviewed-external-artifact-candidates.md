# 0018 — Explicit reviewed external artifact candidates

- Status: Proposed
- Date: 2026-10-16
- Related tasks: T24, D0
- Supersedes: none

## Context

The existing runc/Helm artifacts have failing advisory scans. Rebuilding Grillo
does not change their embedded Go standard library or transitive dependencies.
Disabling seccomp/libpathrs to simplify a runc rebuild would change its security
boundary. Patched Helm sources must not be described as an official unmodified
release. [ADR 0007](0007-controlled-helm-renderer.md) remains the renderer baseline;
this proposal permits explicit local candidate evaluation, not silent selection
or upstream-release attribution.

The user requested autonomous work on these blockers. Isolated source/vendor
updates, native-library preparation, builds, scans and hardware evaluation are
within that scope. Publication, supported-release claims and system package
installation are not part of this delivery.

## Decision

Build candidates in an operation-private artifact tree. Retain the original
fixtures and /usr/bin/helm. Go dependency changes belong only to each external
source module; no Helm SDK, CGO or Rust dependency is added to Grillo itself.
Keep seccomp and libpathrs in runc and use static native libraries. Source and
module hashes, compiler identities, native RPM signatures/hashes and local
patches are recorded separately from claims of source/license clearance.

Candidate executables retain symbols for meaningful binary scans. Identify
runc as a modified build and Helm with upstream version/commit plus dirty tree
state. Version strings alone do not establish trust. Grillo's existing Helm
staging path still requires an explicit executable SHA-256.

The runtime-image build command gains paired -runc-version/-runc-sha256 options
(Make RUNTIME_RUNC_VERSION/RUNTIME_RUNC_SHA256). With both empty it keeps the
repository pin. A replacement requires both fields, a bounded version without
paths/control characters, a lowercase 64-digit hash and actual file digest
verification through existing rooted artifact reads. No other guest input pin
changes. Selected version/hash enter the immutable guest inventory and store
identity. Neither the daemon nor untrusted manifests can set this build policy.

The block-build KVM gate can explicitly consume a portable guest selection via
GRILLO_TEST_BUILD_GUEST_SELECTION. It verifies the manifest and uses verified
boot snapshots/fresh keys; invalid selection fails without legacy fallback.
Default fixture execution remains available as a separate test mode.

## Evidence and validation

[Candidate build and acceptance report](../experiments/t24-external-candidates.md)
records actual hashes, builds, advisory results, renderer regressions and real
installed/workspace gates. Unit tests reject incomplete/malformed/mismatched
selection, preserve default pins/old immutable guests and record replacement
identity. Generic runtime-image interruption/corruption/concurrency tests remain.

## Consequences and remaining gates

No current fixture or system tool is automatically replaced. Candidate selection
is deliberate and local, not a supported installation or new default. A zero
called-vulnerability result does not clear native libraries, uncalled modules,
publisher provenance, notices or corresponding-source obligations. The current
libpathrs source tag's internal version is 0.2.5+dev and must remain accurately
recorded. The new runc native glibc input is distinct from guest BusyBox's glibc.

Formal artifact promotion, reproducible native/compiler source lineage,
independent-host/default-policy gates and release/security-response decisions
remain separate. This ADR remains Proposed pending review.
