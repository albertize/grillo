# T24 — isolated patched runc and Helm candidates

Date: 2026-10-16. **T24/F3/D0 remain BLOCKED.** Two local candidates were built
and evaluated without replacing /usr/bin/helm, the original runc fixture or the
normal runtime selection. They are not official upstream releases or cleared
distribution artifacts. [ADR 0018](../adr/0018-reviewed-external-artifact-candidates.md)
remains Proposed.

The subsequent [timeout investigation](t24-reaper-timeout.md) fixes a reproduced
guest reaper race, adds retained diagnostics and records old/new-guest KVM load
passes. It preserves the failure below without claiming conclusive attribution;
separate repeated DNS bind collisions remain documented.

## Environment and dependencies

Dependencies verified: T09 OCI/CAS, T10 storage, T18/T18b build runtime, T21 Helm,
the existing installed-prefix gate and preceding block-workspace fault tests.
Linux/amd64, kernel 7.2.9-200.fc44.x86_64, Go 1.27.2, GCC 16.2.1-2.fc44,
Rust/Cargo 1.98.1-1.fc44, QEMU 10.2.2, virtiofsd 1.14.0 and mke2fs 1.47.3.
Host SELinux read-only probe: **Permissive**. The sandbox separately reports
Disabled, which is not the host policy used for hardware acceptance.

All candidate files live below ignored
`experiments/artifacts/t24-candidates-20261016-ae096cr9`. Existing sources/vendor
were extracted with Python's data filter and entry/expanded-size bounds. Only
unsafe symlink test fixtures under testdata were omitted. No upstream source
test requiring those fixtures is claimed. Original sources remain retained.
Go dependency downloads use a private GOMODCACHE and sum.golang.org; module
verification passed for both source trees. No Go family upgrade, system package
installation, sudo, CA or host-policy change occurred.

## Inputs, patches and outputs

The [candidate lock](t24-external-candidate-lock.json) records source archive,
native package, output and patch SHA-256 values. Exact module changes are in the
[runc patch](t24-runc-candidate-module.patch) and
[Helm patch](t24-helm-candidate-module.patch). Neither changes Grillo's go.mod.

| Candidate | Source / changes | SHA-256 |
| --- | --- | --- |
| runc 1.5.2+grillo.1 | Revision 29dd3dc2b13b4123162e5fe132504bb4b15569f1; x/net 0.60.0, x/sys 0.48.0, cilium/ebpf 0.22.0; Go 1.27.2 | `5bca7accb4a212f748899ac54dee87a470be9af5b8f34a84110c7cc425c045fb` |
| Helm upstream version 4.2.2, dirty build | Revision b05881cf967a5a09e19866799d0edfd40675803a; x/crypto 0.57.0, x/net 0.60.0, x/sys 0.48.0, x/term 0.46.0, x/text 0.42.0, x/sync 0.23.0, oras-go/v2 2.6.2; transitive mod/tools updates in patch | `b82ff5240c4a1385a2ebc4e967fc497593bfe0bfdb98835aea19fb573ea444b9` |

Both binaries retain debug/symbol information for precise scanner analysis.
runc is static PIE with no DT_NEEDED dependencies; Helm is CGO-disabled/static.
runc --version and features report seccomp 2.6.1 and libpathrs **0.2.5+dev**.
The collected libpathrs archive/tag is v0.2.6, but its Cargo manifest/internal
version says 0.2.5+dev; that discrepancy is preserved rather than relabeled.

### Native preparation

83 existing crate archives were verified against the provided Cargo.lock, safely
unpacked into a Cargo directory source, and given per-file/package checksum
metadata. The source Cargo.lock exactly matches the previously collected lock.
No Cargo registry access or nightly toolchain was used. Build command from the
private libpathrs tree (candidate_root denotes the artifact directory above):

```sh
CARGO_HOME="$candidate_root/cargo-home" LIBPATHRS_CAPI_BUILDMODE=staticlib \
  RUSTFLAGS='-C panic=abort' \
  cargo rustc --offline --frozen --release -j 2 --lib --features capi --crate-type staticlib
```

PASS. The static library and existing header were copied to native/lib and
native/include. A private pathrs.pc records the actual 0.2.5+dev version.

The host lacked glibc/libseccomp static archives and libpathrs pkg-config data.
dnf5 download fetched glibc-static-2.43-9.fc44.x86_64 and
libseccomp-static-2.6.1-2.fc44.x86_64 into the private tree, with private dnf
cache/state/log paths. rpmkeys --checksig reported digests/signatures OK using
existing Fedora keys **before extraction**. A bounded newc payload reader
selected only regular usr/lib64/*.a members, rejecting unsafe names/types; it
did not run package scripts. libm.a's absolute linker-script paths were relocated
to the private extracted libraries and the change recorded in a local receipt.
Matching glibc/libseccomp SRPMs were also downloaded and their signatures verified.

The new static runc embeds native **glibc 2.43** inputs; this is distinct from
BusyBox's unchanged glibc 2.41 guest libraries. Retained signed SRPMs identify
source inputs, not independently reproduced RPM/compiler outputs or completed
source/license clearance. Compiler/libstd source lineage remains incomplete.

### Go dependency resolution and build commands

GOTOOLCHAIN=local and a private GOMODCACHE were used for explicit go get updates
listed in the patches, followed by go mod vendor and go mod verify. Normal
minimum-version selection also updated Helm's x/mod and x/tools requirements.
The runc source's go directive advanced from 1.25.0 to 1.26.0; actual builds use
the unchanged installed Go 1.27.2. No implicit toolchain download was allowed.

Helm command from its source tree:

```sh
GOTOOLCHAIN=local GOPROXY=off GOFLAGS='-mod=vendor -buildvcs=false' \
  GOMAXPROCS=2 CGO_ENABLED=0 go build -trimpath \
  -ldflags '-X helm.sh/helm/v4/internal/version.version=v4.2.2 -X helm.sh/helm/v4/internal/version.gitCommit=b05881cf967a5a09e19866799d0edfd40675803a -X helm.sh/helm/v4/internal/version.gitTreeState=dirty' \
  -o ../helm-candidate ./cmd/helm
```

A private writable GOCACHE was selected for this sandbox build. runc command
from its source tree, with native library/include flags derived from the private
pkg-config entry:

```sh
GOTOOLCHAIN=local GOPROXY=off GOFLAGS='-mod=vendor -buildvcs=false' \
  GOMAXPROCS=2 CGO_ENABLED=1 PKG_CONFIG_PATH="$candidate_root/native/lib/pkgconfig" \
  CGO_LDFLAGS="-L$candidate_root/native/lib -ldl -lpthread -lm -lutil -lrt" \
  go build -trimpath -buildmode=pie \
  -tags 'seccomp urfave_cli_no_docs libpathrs netgo osusergo' \
  -ldflags '-X main.gitCommit=29dd3dc2b13b4123162e5fe132504bb4b15569f1-grillo-candidate -X main.extraVersion=+grillo.1 -linkmode external -extldflags -static-pie' \
  -o ../runc-candidate .
```

Initial runc sandbox compilation failed resolving vendor packages even though
they existed; the host build passed. An initial candidate suffix lacked the
SemVer '+' separator; the final candidate was rebuilt with the honest local
version above. Initial runc features execution failed on the sandbox's read-only
XDG_RUNTIME_DIR; --root pointing at a newly owned private directory passed.
Initial focused Grillo tests failed on sandbox cache writes; host rerun passed.
These failures were not converted to successes without reruns.

Local receipts under candidate_root/receipts retain module patches, archive and
native-library inventories, build info, source scans and output hashes. These
commands/patches document the build; no independent byte-for-byte reproduction
in a second path or on another host is claimed.

## Advisory results

The same cached govulncheck v1.8.0, compiled with Go 1.27.2, queried the live
public database. Binary scans ran on the actual candidates, and source scans
used each updated vendor tree (runc with its native pkg-config/link inputs).

| Scan | Result |
| --- | --- |
| Final runc binary | PASS, exit 0, no vulnerabilities found |
| runc source | PASS, exit 0, no vulnerabilities found |
| Helm binary and source | PASS at symbol/package scope, exit 0; zero called/imported-package findings, **one module-only advisory remains** |

Helm's verbose binary scan identifies the residual as GO-2026-5932, the old
golang.org/x/crypto/openpgp advisory with no fixed version. The module is needed
for other crypto packages, while the old OpenPGP package is absent from the
linked package findings. This is an explicit disposition, not an allowlist,
advisory suppression, vendored package deletion or universal zero-finding claim.
Native Rust/C dependencies, compiler vulnerabilities and exploitability are not
cleared by Go scans. Original installed binaries still have their prior 14/15
findings; they were not replaced or retrospectively cleared.

## Integration and software verification

- Focused guest/runtime-image tests PASS, including explicit pin metadata,
  immutable old guest/default-pin preservation, wrong hash, incomplete pair,
  malformed/oversized/path/control-character identity rejection before publication.
- Hardware-tagged block test compilation PASS without execution.
- PATH explicitly selecting the candidate Helm: go test -count=1 -v
  ./internal/frontend/helm PASS (1.242 s), including real local and OCI rendering,
  stable ordered values, redaction/rejections, corruption, concurrency and cancellation.
- Candidate runtime guest built with paired explicit version/hash, candidate
  inputs, separate store and separate selection. Inventory identity:
  `37150904e748f166af74b79053a66e13300b76ddd4acdd0856d0f27eaac59d5c`.
  Host/agent build identity explicitly carries dev-t24-candidate and dirty commit.
- make test-installed-runtime with candidate selection, Helm binary/hash and the
  existing verified seeded CAS: **PASS**, exit 0, package 3.331 s. Actual KVM
  moved/read-only prefix, standalone Compose/Helm HTTP/DNS, exec, CLI/UI lifetime,
  embedded UI without Node, runtime PATH without build tools and owned teardown.
  This is seeded local evidence, not a fresh registry pull or clean external host.
- make check PASS outside sandbox after builder/portable-guest changes.
- Final make check vulncheck PASS after phase diagnostics, including all routine
  checks and a current Grillo source scan with Go 1.27.2. No new fuzz campaign
  or full upstream runc/Helm test suite was run.

### Candidate workspace load failure

GRILLO_TEST_BUILD_GUEST_SELECTION selects the candidate portable guest and
verified fresh-key snapshots for the existing make test-t24-build-workspace-load.
The first rebuilt single gate PASS (6.867 s), but its ten race repetitions
**FAIL**, exit 2, package 155.628 s. One output-limit scenario's subsequent fresh
build returned context deadline exceeded after about 30 seconds; other repetitions
passed. Failed owned state remains at /tmp/grillo-block-kvm-3895773466. Its backend
and scratch directories were empty on inspection; logs had already been removed
by successful cleanup, so the precise timed-out phase was not established.

That run overlapped routine checks; causation by host load is **not established**.
No deadline or acceptance threshold was relaxed. BlockRunner now wraps boot,
RUN transport and export errors with phase context while retaining errors.Is
causes; regression tests cover those failure paths. A separate isolated batch
is required and does not erase this combined-load failure.

The isolated rerun with phase diagnostics PASS, exit 0: rebuilt single gate
7.369 s and ten race repetitions 128.817 s. The selected portable guest is still
inventory 37150904e748f166af74b79053a66e13300b76ddd4acdd0856d0f27eaac59d5c;
the Make prerequisite also rebuilt the legacy fixture, which is not the guest
used by this explicit selection. No failure recurred in that isolated batch;
the cause and combined-load acceptance remain unresolved.

### Candidate installed resource budgets

After the isolated workspace batch, make test-t24-resource-budget ran with the
same explicit candidate input tree/version/hash/store/selection, candidate Helm
binary/hash and T24_SEED_CAS pointing at the existing verified fixture.
**PASS**, exit 0, 30/30 installed iterations, no skips, package 187.379 s.
Full local receipt: /tmp/grillo-t24-budget.DQWN6U. The
[recorded scalar summary](t24-candidate-resource-budgets.json) is generated from
that completed receipt by the existing checker; no thresholds changed.

| Seeded CAS phase | Median HTTP readiness | Nearest-rank p95 | Existing p95 bound |
| --- | --- | --- | --- |
| Fresh rootfs | 0.835579 s | 0.839913 s | 2.0 s |
| Warm rootfs | 0.733798 s | 0.736763 s | 1.8 s |

Maximum sampled owned-process RSS was 254,988,288 bytes (bound 268,435,456),
allocated owned-directory bytes 603,684,864 (bound 734,003,200), logical bytes
602,523,791 (same bound), idle CPU 4 USER_HZ ticks (bound 8). These are sampled
local seeded regression budgets, not instantaneous peaks, online registry-pull
latency, whole-host pressure or portable SLOs. The earlier workspace timeout is
a different load scenario and is not cleared by this successful batch.

### Reusing the local candidates deliberately

From the repository root, after reviewing the lock, patches and candidate files:

```sh
candidate_root="$PWD/experiments/artifacts/t24-candidates-20261016-ae096cr9"
export GUEST_INPUTS="$candidate_root/inputs"
export RUNTIME_RUNC_VERSION=1.5.2+grillo.1
export RUNTIME_RUNC_SHA256=5bca7accb4a212f748899ac54dee87a470be9af5b8f34a84110c7cc425c045fb
export RUNTIME_STORE="$candidate_root/runtime"
export RUNTIME_SELECTION="$candidate_root/runtime-selected.txt"
export VERSION=dev-t24-candidate
export COMMIT=1bda63156002a6862a028a303468d484c23d9019-dirty
export HELM_BINARY="$candidate_root/helm-candidate"
export HELM_SHA256=b82ff5240c4a1385a2ebc4e967fc497593bfe0bfdb98835aea19fb573ea444b9
export T24_SEED_CAS="$PWD/experiments/artifacts/t24-fixture-20261011/cas"

GRILLO_TEST_SEED_CAS="$T24_SEED_CAS" make test-installed-runtime
GRILLO_TEST_BUILD_GUEST_SELECTION="$RUNTIME_SELECTION" make test-t24-build-workspace-load
make test-t24-resource-budget
```

These are opt-in developer commands, not a public artifact retrieval or release
installation flow. Run hardware/measurement targets sequentially unless testing
combined load deliberately. The selection's immutable inventory identifies the
guest actually used; rebuilding the unrelated legacy fixture does not do so.

## Remaining gates

Candidates are available for deliberate local evaluation only. Full native
source/notices/compiler lineage, a native advisory review, artifact promotion,
independent reproduction, persistent-volume crash repair, daemon/host interruption,
combined-load timeout diagnosis, broader peak/concurrency budgets, hosted CI and
clean external/default-policy acceptance remain open. No publication, supported
release, commit, upstream identity or maintainer security contact is invented.

Next bounded work: diagnose the retained combined-load failure with phase
context and retained boot diagnostics, then persistent-volume/daemon crash gates.
