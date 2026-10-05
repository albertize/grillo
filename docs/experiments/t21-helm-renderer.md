# T21 — Controlled Helm rendering and OCI charts

## Result and scope

**T21 DONE.** Local directories/`Chart.yaml` and exact-version OCI charts compile
through official Helm v4.2.2 and the T20 Kubernetes frontend. T22's application
runtime/KVM acceptance gate is separate and has **not** been executed here.

Environment: Linux 7.2.8-200.fc44.x86_64, amd64; Go
`go1.26.8-X:nodwarf5`; `/usr/bin/helm` v4.2.2+gb05881c.
No cluster or kubeconfig is required by rendering.

## Acceptance evidence

| T21 requirement | Evidence |
| --- | --- |
| Controlled external renderer | Version checked before rendering; version mismatch/missing tool tests; [ADR 0007](../adr/0007-controlled-helm-renderer.md) records pin, checksum metadata and license |
| Ordered values and release identity | Real Helm/CLI tests verify later `-f` wins, explicit/default release identity and namespace `default` |
| Fixed offline capabilities | `TestRealHelmOfflineCapabilitiesAndRenderedCRD` verifies Kubernetes v1.30.0, default namespace and built-in apps/v1 capability without inherited kubeconfig |
| Equivalent chart/manifests yield equivalent IR | `TestRealHelmEquivalentStableOrderedValues` compares the typed application; `TestOCIHTTPSRealHelmStableVerifiedOffline` compares local and OCI canonical hashes |
| Repeated inputs are stable | Repeated local/OCI result comparisons and CLI JSON-plan comparisons; cached tags never silently refresh |
| Nondeterminism is diagnosed | Preflight rejects lookup, known random/time/crypto-salt functions and dynamic tpl with `HELM_TEMPLATE_UNSUPPORTED`; scanner is deliberately conservative |
| Errors are redacted | Real Helm fail/invalid-YAML fixtures, registry-error fixtures, version/transport/cache failures use fixed diagnostics, never raw stderr or values |
| Remote fetch is explicit | Cache miss without `--fetch-chart` sends zero requests and creates no cache; consent fetches only manifest/config/chart; cached plans work with the HTTPS server stopped |
| Verified chart/cache/digests | Manifest SHA-256 pin, descriptor media types/sizes/hashes, metadata name/version and archive validation checked before atomic publication and on every reuse |
| Safe bounded extraction | Traversal, absolute paths, symlinks, hardlinks, FIFO, duplicate entries, extra roots, oversize/count/decompression and gzip-checksum fixtures rejected |
| Interrupted/concurrent cache operations | Partial/wrong-digest downloads never publish; retry works; six concurrent compilers publish a complete agreed record; conflicts never replace a pin; lock wait honors cancellation |
| No implicit cluster/plugin/dependency execution | Direct template/client-only invocation, isolated Helm/plugin/HOME/kubeconfig paths; locked dependencies rejected without fetching or modifying the archive; CRDs and hooks rejected |
| Private secrets and shared API path | CLI creation plans do not write the secret store or publish content hashes; apply passes random-versioned references to the API test double |

## Commands actually run

- `go test -count=1 -v ./internal/frontend/helm ./internal/cli`: PASS, real
  Helm tests executed rather than skipped.
- `go test -race -count=3 ./internal/frontend/helm ./internal/cli`: PASS.
- `make check`: PASS (format, vet, unit/race tests, script fixtures, builds,
  module verification/list/tidy).
- Freshly built `bin/grillo plan examples/helm/demo --release demo`: PASS,
  isolated XDG workspace remained empty.
- `git diff --check` and local Markdown-link checks: PASS.

The CLI OCI integration test runs **official Helm package**, serves that package
using an in-process HTTPS OCI Distribution fixture, fetches it through the
production registry client, renders with **actual Helm**, and exercises offline
CLI planning and cached apply wiring. The fixture transport trusts only the test
server certificate; no host CA or configuration was changed.

## Cache and trust contract

`--version` accepts exact SemVer only (including prerelease/build metadata;
Helm's `+` to `_` OCI tag convention is applied). `--chart-digest sha256:...`
optionally supplies an independently trusted manifest pin. Without it, the first
HTTPS resolution establishes a trust-on-first-use pin; digest checks ensure
integrity, **not provenance signatures or publisher authenticity**.

Cache entries live under `$XDG_CACHE_HOME/grillo/helm/charts`, in private
owner-checked directories and mode-0600 atomic records. Records contain the
manifest/config/chart archive, with bounded sizes, and are keyed by full
registry/repository/version. Every read revalidates the graph and extracted
metadata. A corrupt cache is blocked, not silently repaired; consent never
refreshes a previously pinned version. Linux flock serializes publication and
rejects concurrent pin disagreement. Source charts may embed secrets, so the
cache is private even though it is not the secret store.

The default chart transport is HTTPS-only, including redirects and token realms,
with no inherited proxy credentials. Authentication is anonymous/public-registry
bearer auth; no private-registry credential CLI is provided. No tools are fetched
or installed automatically.

## Intentional restrictions and checks not run

- Self-contained v2 charts only. Dependencies/subcharts are rejected, even with
  Chart.lock; no dependency download/update command is exposed, and lockfiles
  are never rewritten. This implements T21's restricted dependency policy, not
  dependency-resolution parity.
- One Helm chart-content layer with a Helm config in an OCI image manifest.
  Additional layers (including provenance artifacts), indexes and signatures
  are not supported. SHA-256 integrity is not signature verification.
- Values schemas, hooks/test hooks, CRDs, lookup, DNS templates, dynamic tpl,
  known nondeterministic templates, non-default namespaces and unsupported T20
  workload semantics block compilation/apply. Conservative scanning can reject
  tokens in comments or strings; it is not an OS sandbox for an untrusted Helm
  binary.
- Offline plans preview creation against empty observations; secret versions use
  `unresolved-offline` and do not report live secret-update diffs. Normal plan
  is read-only; explicit `--fetch-chart` permits cache writes, not runtime state
  or secret-store writes.
- Cache publication is Linux-only. Non-Linux local rendering is not a verified
  platform-support claim.
- Public Internet registry pulls, private credential flows, hosted CI, and T22
  runtime/KVM execution were not run. HTTPS protocol/rendering integration is
  fixture-backed; no claim of a public chart or deployed Helm release is made.

## Next

T22 — run the realistic Helm subset on the actual Grillo runtime, exercise
persistence/update/unsupported-input gates, and publish compatibility evidence.
