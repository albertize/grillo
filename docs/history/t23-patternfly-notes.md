# Archived PatternFly delivery notes

Historical session record, retained for provenance rather than everyday use.
See [consolidated T23 acceptance](../experiments/t23-console.md).

## Original PatternFly follow-up

Status: **DONE for the requested frontend migration**, 2026-10-06. T24/F3 remains
TODO. This report supplements, not replaces, the
[original T23 acceptance evidence](../experiments/t23-console.md).

## Direction and contracts

The user explicitly requested PatternFly React after reviewing the original UI.
[ADR 0008](../adr/0008-patternfly-react-console.md) records the deliberate change
to the framework-free delivery plan (formal maintainer ADR review pending).

React/React DOM 19.3.0, PatternFly core/table/icons 6.6.1 and build-only esbuild
0.28.2 are exact integrity-locked npm dependencies. Node 24.18.0/npm 11.16.0 are
build tools, not installed/runtime prerequisites for users of the Go binary.
Explicit `make ui-deps` disables install scripts. There is no CDN/dev server,
new Go module, runtime SDK, raw HTML insertion or weakened CSP.

The frontend now uses PatternFly masthead/sidebar, focused pages, status cards,
resource tables/detail, configuration tabs, alerts/forms and empty/loading
states. Existing public DTO, bootstrap auth, API, stream/exec quotas, cursor/gap
recovery and workload-lifetime ownership remain. Filtering retains at most
256 KiB of UTF-8 text, including multibyte output; events also cap at 200.
Unavailable data is still labelled unavailable. License notices include MIT
packages, nested notices, Red Hat OFL fonts and Font Awesome attribution.

## Actual checks

Same Linux/amd64 UID 1000 host as the original report, SELinux already Permissive.
No host policy change, privilege escalation, browser download or driver install.

- `make ui-deps ui-check`: PASS; 22 installed platform-appropriate packages,
  five Node tests and deterministic generated-output comparison.
- `make check`: PASS after final source changes (frontend build/unit/freshness,
  Go format/vet/unit/race, script checks, binary builds and module checks).
- `GRILLO_UI_SCREENSHOT=/tmp/grillo-patternfly-preview.png make test-ui-browser`:
  PASS with actual Firefox 157.0. Checks PatternFly/local fonts, no CSP/runtime
  errors, all views, literal HTML output, metadata-only response/DOM, filters,
  SSE cursor/gap/reconnect, exec and **visible** 390px mobile navigation.
  Screenshots are ephemeral local artifacts, not hardware evidence.
- `make test-ui`: PASS twice after CID isolation, with rebuilt guest and freshly
  built actual CLI/daemon. Native three-microVM/seven-container Helm chart;
  real Firefox UI bootstrap, views and VMM metrics; real exec stdout/stderr/exit;
  cancellation verifies the recorded guest exec PID is gone and a subsequent
  exec succeeds. Browser closure and CLI SIGINT leave the same live VMM PIDs
  and working API exec. Reapply/down/persistence/daemon recovery also run.
  Latest run: 35.585s for the entire gate, 5.30s browser subtest (not benchmarks).
- `cd web && npm audit --json`: PASS, zero known advisories at execution time;
  this is not a dependency security review or publisher-authenticity proof.
- `node --check scripts/ui-browser-smoke.mjs`, `git diff --check`: PASS.
- Local-link/balanced-fence check across nine updated Markdown documents: PASS.
  Initial check found preexisting `AGENT.md` links in README/plan; corrected to
  the actual tracked `AGENTS.md`. An ad-hoc demo-inspect summary initially
  assumed the wrong JSON shape and failed; CLI inspect itself succeeded, then
  the corrected summary showed five running containers/two completed init
  containers. The demo endpoint remained HTTP 200 and its three VMM process
  identities were unchanged after stopping only its verified UI bridge.
  The replacement bridge served byte-identical current JS/CSS/notices under
  strict CSP, without consuming its review bootstrap. The first ad-hoc URL
  parser expected `#bootstrap` instead of the actual `#token` and failed;
  correcting the check passed, with no bridge/runtime failure.

The first esbuild build failed while inspecting uninstalled optional packages
for other platforms; it now ignores ENOENT only for lock entries marked optional.
A strengthened mobile visibility assertion exposed a real issue: unmanaged
PatternFly Page was not observing resize, leaving the menu translated offscreen.
Enabling its documented resize callback and closing inert navigation at the
mobile breakpoint fixed it; merely programmatically clicking hidden links is
not counted as mobile evidence.

The first KVM migration run **failed** with `guest-cid=20: Address already in
use`, because the isolated review demo was still running. It was not stopped.
`grillod` now accepts validated explicit `-vsock-cid-base` and
`-build-vsock-cid-base` configuration (defaults unchanged: 20/200). The gate uses
separate test ranges derived from its PID. Real collisions still fail; this is
not a global allocator or a guarantee arbitrary ranges are free. Unit tests
cover reserved/out-of-range/equal bases before state preparation. Subsequent
complete hardware gates passed with the review demo's three VMMs still alive.

Latest tested artifact SHA-256:

- kernel: `550b5916433e758e814039f8324a9f1cdfdd2b3b8e4a1e44cc6836cdc92d5942`
- guest: `89c3f43ea235ceaee5150de99fb24f9d0c99c184182170ecdfe5edbe56754e3d`

Raw build sizes (not compressed/network/startup measurements): JS **565,652 B**,
CSS **1,790,834 B**, 15 generated assets totalling **2,680,695 B**; the built CLI
was **14,877,065 B**. No performance improvement claim is made.

## Limits and next gate

Hosted CI was updated for pinned Node/dependency setup but not executed here.
Other browsers/hosts, accessibility audit, load/performance campaigns, full T24
hardening and T27 release packaging were not run. This frontend change does not
add guest stdout ingestion, persisted original compiler line diagnostics,
guest/container actual resource metrics, browser TTY, secret reveal or TLS/CA.
Default CID allocation still needs explicit coordination for concurrent isolated
daemons/other host VMMs. Next task: **T24**.
