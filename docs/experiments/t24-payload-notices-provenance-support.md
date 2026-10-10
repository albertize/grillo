# T24 — payload notice/provenance hardening and host support boundaries

**Status: local bounded delivery PASS; T24/D0 remain BLOCKED overall.**
Dependencies verified: T23/T24's established embedded UI, runtime-only image and
Stage/installed-prefix contracts, earlier license inventory and partial user-run
Fedora 44 evidence. No new module/tool/ADR or release-support decision.

> Advisory correction: [hosted-CI review](t24-ci-log-review.md) reproduces 10 Go
> 1.26.8 stdlib findings fixed in 1.26.9. The historical local zero-finding result
> used an unrecognized vendor suffix and does not clear stdlib vulnerabilities.
> Notice/staging/KVM evidence stays scoped. Subsequent [actual Go 1.26.9 local
> gates](t24-go126-toolchain.md) PASS; old payloads are not patched and hosted
> rerun/overall T24 remain pending.

## Contracts changed

1. Stage now requires the generated frontend notice text and the four existing
   Font Awesome/Red Hat font notice files, copies them **verbatim** into
   `share/doc/grillo/third-party/` and records their SHA-256/size in VERSION.json.
   Explicit allowlist only: no arbitrary checkout or host file collection.
2. Source notices must be regular/no-follow files, nonempty and <=1 MiB each,
   <=4 MiB aggregate, within the existing total payload bound. Missing, empty,
   symlink, FIFO/special or oversized notices fail staging. Failed Stage removes
   only its unique temporary prefix; no existing output is replaced.
3. VERSION.json now includes `binary_provenance` extracted using standard-library
   Go buildinfo from the **copied CLI/daemon/netns/Helm bytes**, not from the staging
   tool's dependency graph. Missing Go metadata/oversized identities fail closed;
   records include toolchain, main module, dependency versions/sums/replacements.
   Metadata dependencies cap at 2,048, identity lengths are bounded. Arbitrary
   build settings/linker text is omitted, local replacement paths are redacted
   including unversioned relative replacements.
4. `notice_coverage` explicitly states **partial**. Frontend/font text does not
   complete Go runtime/dependency, Helm or guest corresponding-source/notices
   review. `redistribution_review` remains pending. Embedded metadata is producer
   supplied, potentially forged; neither it nor hashes verify publisher identity.
5. A standalone [host support guide](../host-support.md) ships beside runtime-layout
   and VERSION.json. It separates local and user-run Permissive observations,
   recorded Enforcing blockers and unverified hosts, provides safe reporting and
   operational limits, and does not invent support ranges/contacts/SLA or URLs.
   Source-only evidence links are described as source paths, not broken staged
   hyperlinks. README/runtime-layout reference the guide.

## Actual checks

```sh
go test ./internal/distribution ./scripts/stage
make payload HELM_BINARY=/usr/bin/helm \
  HELM_SHA256=deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d
make check
make test-installed-runtime HELM_BINARY=/usr/bin/helm \
  HELM_SHA256=deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d
make audit vulncheck
go test -race -count=100 \
  -run 'TestBinaryProvenance|TestModuleProvenance|TestRuntimeNotices' ./internal/distribution
```

PASS. Positive and negative regressions cover actual test-binary metadata, absent
metadata, identity bounds/private replacement redaction, verbatim text/hash copying,
explicit partial status, missing/empty/symlink/FIFO/oversized notices and cancellation.
The repeated selected race suite passes (1.90 seconds); full checks pass. Official
pinned govulncheck v1.8.0 reports **No vulnerabilities found** for Grillo's graph;
this does not audit the separately built Helm/guest/host dependencies.

Installed KVM gate PASS without SKIP (final 7.35 seconds; earlier 7.67 seconds) after rebuilding tested binaries:
real Compose/Helm/HTTP/DNS/UI and cleanup, with new assertions for four actual
binary records, partial notice coverage and shipped Font Awesome notice.
Same recorded local Fedora/UID 1000/QEMU 10.2.2/already-Permissive environment;
no security policy changed. It does not upgrade the second-host/default-policy gate.

Actual payload before final guide refinements:
`experiments/artifacts/payloads/grillo-test.8gIStkJg/`; archive SHA-256
`21a807eb4b884ada865447e5e4b53ecb6bb98fdec0e02482fbae602c38f4d3c3`.
Archive inspection verified all five notice digests/sizes. Final rebuilt payload
with guide refinements: `experiments/artifacts/payloads/grillo-test.SZg5rwSJ/`,
SHA-256 `e5078b611596ce75b4b4ce64c08e481684f406b8e5af28708d4a81132d089a6e`.
All 21 inventoried file hashes/sizes and four provenance records verified inside
that archive; guide has explicit unsupported-release and no bootstrap credential. Actual executable
metadata observed:

| Binary | Embedded Go toolchain | Declared dependency modules |
|---|---|---:|
| grillo | go1.26.8-X:nodwarf5 | 4 |
| grillod | go1.26.8-X:nodwarf5 | 2 |
| grillo-netns | go1.26.8-X:nodwarf5 | 2 |
| Helm | go1.26.4-X:nodwarf5 | 106 |

The distinct Helm toolchain and 106-module graph demonstrate why Grillo's Go
license/advisory checks cannot certify that bundled executable. Fedora-built
Helm lacks upstream sums for many declared dependencies; its version/module list
alone does not prove exact source/build lineage or patch set. Obtain/review exact
SRPM/build and full dependency/source/notices before redistribution.

## User-run support evidence update

The user subsequently confirms UI functionality on their second Fedora 44 host.
That joins reported Compose up/down/up and Helm Ingress HTTP observations as
external **functional** PASS under Permissive, not a guessed result. The
[sanitized second-host record](d0-second-host-fedora44.md) preserves incomplete
process/lifetime/DNS/late-token/clean-target evidence and null endpoint diagnostics.
No bootstrap credential is copied into this report or payload guide.

## Remaining gates and next task

- Assemble exact kernel/runc/BusyBox/glibc corresponding sources/config/build
  recipes and complete Go/toolchain/Helm notices, then legal/provenance review.
- Actual publisher/signature/trust, source-to-binary/patch identity and SBOM/
  attestation/release channels still need verified inputs and maintainer decisions.
- No supported runtime release, working dedicated security contact/SLA or
  default-policy Fedora Enforcing acceptance is created by this guide.
- Remaining cache families/volumes, physical/idle/cold/peak budgets, longer hostile
  load/fault/slow-input and hosted CI gates remain. No new external runs performed.

Next bounded work: complete prepared Go/toolchain notice collection and obtain
explicit corresponding-source inputs for guest/Helm; investigate missing endpoints
and lifecycle presentation separately. No commit, push, package publication,
source/tool auto-fetch, automatic sudo or host-policy changes performed.
