# Implementation Progress

## Current state

**T00 is implemented and locally verified.** The repository now contains a Go module, tested command scaffolding, build/check targets, and CI configuration. There is still no workload runtime, guest init implementation, or release.

T01 prerequisite probes verified unprivileged KVM API access and private user/network namespace TAP operations. No VM boot or runtime benchmarks have been performed. Hosted CI has not yet been executed.

## Task status

| Task | Title | Status | Evidence or next prerequisite |
|---|---|---|---|
| T00 | Repository scaffold and conventions | DONE | Local checks, clean-source builds, audit, and command smoke tests; evidence below |
| T01 | Rootless VMM and minimal guest spike | BLOCKED | Prerequisite probes pass; no VMM in PATH or verified guest artifacts |
| T02 | OCI, filesystem, and application-network spike | TODO | T01 |
| T03 | F0 gate and platform ADR | TODO | T02 and measured evidence |
| T04 | IR, diagnostics, and capabilities | TODO | T00 |
| T05 | State, secrets, and recovery primitives | TODO | T04 |
| T06 | Guest protocol and testable client | TODO | T03, T04 |
| T07 | Guest PID 1 and OCI runtime | TODO | T06 |
| T08 | Backend lifecycle and process supervision | TODO | T05, T07 |
| T09 | OCI registry and CAS | TODO | T04, T03 |
| T10 | Storage manager | TODO | T05, T08, T09 |
| T11 | Production rootless networking and IPAM | TODO | T05, T08 |
| T12 | DNS, Service proxy, and Ingress | TODO | T11, T04 |
| T13 | Observability and probes | TODO | T05, T07, T12 |
| T14 | Planner, reconciler, and updates | TODO | T08–T13 |
| T15 | Local API and daemon lifetime | TODO | T14 |
| T16 | Native-runtime CLI | TODO | T15 |
| T17 | Compose parser and compiler | TODO | T04; final integration T16 |
| T18 | Builder and image/volume/network tooling | TODO | T09, T10, T11, T17 |
| T19 | F2 gate: Compose application | TODO | T12–T18 |
| T20 | Kubernetes MVP compiler | TODO | T04; runtime verification T14 |
| T21 | Helm rendering and OCI charts | TODO | T20 |
| T22 | Helm gate and compatibility reporting | TODO | T19, T21 |
| T23 | Web console and secure bridge | TODO | T15, T22 |
| T24 | F3 MVP gate and hardening | TODO | T23 |
| T25 | StatefulSet and Job | TODO | T24 |
| T26 | Extended Tier 1/Tier 2 parity and TLS | TODO | T25 |
| T27 | Measured optimization and packaging | TODO | T24; T26 for complete F4 |
| T28 | Future research, not a prerequisite | TODO | Stable F3 and measured use cases |

Task contracts and full acceptance criteria live in [IMPLEMENTATION_PLAN.md](../IMPLEMENTATION_PLAN.md). The table is a status summary, not a replacement for those contracts.

## T00 delivery — 2026-10-03

- **Task:** T00 — Repository scaffold and conventions
- **Status:** DONE
- **Dependencies verified:** None; initial working tree was clean and no remote was configured.
- **Files and contracts changed:** `go.mod`, `cmd/grillo`, `cmd/grillo-agent`,
  `internal/command`, `Makefile`, `.github/workflows/ci.yml`, and contributor docs.
  Exit codes: 0 success, 1 unavailable functionality/output failure, 2 invalid usage.
  Unknown arguments are not echoed. No host inspection, mutations, downloads, or
  workload execution occur in either command. Existing `.gitignore` already
  excludes `bin/` and runtime artifacts.
- **Decisions/ADRs:** [0001](adr/0001-scaffold-and-dependencies.md), proposed pending
  maintainer review. Go 1.26.8; local module identity; standard library only.
  Maintained YAML v3.0.5 path/license verified for future use, not imported.
  No external runtime modules, so no go.sum yet. No backend choice made.
- **Tests run:** Linux/amd64, `go1.26.8-X:nodwarf5`:
  - `make check`: PASS (fmt, vet, unit/race tests, host and Linux/amd64 agent
    builds, `go mod verify`, `go list -m all`, `go mod tidy -diff`).
  - Repeated `make check` in a fresh temporary source copy excluding ignored
    files/build artifacts: PASS. This is not a remote checkout or hosted CI run.
  - `make vulncheck`: PASS, pinned v1.8.0 reports no vulnerabilities found.
  - `go test -cover ./internal/command`: PASS, 100% statement coverage for the
    small dispatcher only; not a runtime coverage claim.
  - Built binary smoke tests: version exits 0, doctor exits 1, unknown `up`
    exits 2, agent without arguments exits 1, as intended.
  - `git diff --check`: PASS.
  - `make test-kvm`: expected BLOCKED/nonzero (Make exits 2); no tests exist yet.
- **Tests NOT run and why:** Hosted GitHub Actions awaits repository push.
  Real KVM/guest/network tests await T01 artifacts and test implementation.
  `/dev/kvm` exists locally, but access or functionality was not tested.
- **Integration/benchmark evidence:** CLI binary smoke tests only; no VM evidence.
- **Known limitations:** Doctor performs no readiness checks; agent is not PID 1;
  versions are development scaffold identifiers. No daemon, frontends, API, or
  workload semantics exist. Compatibility support is unchanged.
- **Next task:** T01 — rootless VMM and minimal guest spike; T04 is independently
  eligible for pure model work. Do not substitute scaffold results for F0.

## T01 start — 2026-10-03

- **Task:** T01 — Rootless VMM and minimal guest spike
- **Status:** BLOCKED (started; boot acceptance gates unperformed)
- **Dependencies verified:** T00 committed as `d2bf8a4`; `make check` passes.
- **Files and contracts changed:** `experiments/boot/` prerequisite probes and
  tests, `Makefile` formatting coverage, and [experiment report](experiments/t01-preflight.md).
  No production doctor or backend behavior changed.
- **Decisions/ADRs:** No backend selected and no new dependency introduced.
- **Tests run:** `go run ./experiments/boot/preflight` PASS (real KVM API 12,
  UID/EUID 1000); `sh experiments/boot/namespaces.sh` PASS (private namespace TAP
  create/up/delete); `make check` PASS including negative probe unit/race tests;
  `make vulncheck` PASS, no vulnerabilities found; `git diff --check` PASS.
  Post-probe interface/process inspection found no leftover experiment objects.
- **Tests NOT run and why:** VM boot, vsock, guest command/logging, stop, helper
  networking, and 30 cycles require a VMM and verified guest artifacts that are
  absent. No tool/artifact installation was authorized or performed.
- **Integration/benchmark evidence:** Real host prerequisite probes only; see report.
- **Known limitations:** KVM API success is not VM feasibility. Namespace UID 0
  maps to host UID 1000. Helper presence does not prove connectivity. No suitable
  guest kernel/rootfs is selected or verified.
- **Next task:** Continue T01 after explicit provisioning authorization; do not
  proceed to T02 or report F0 success. T04 remains independently eligible.

## T01 tooling — explicit dependency bootstrap

- **Task:** T01 — dependency provisioning support
- **Status:** BLOCKED (boot acceptance gates still unperformed)
- **Dependencies verified:** T00 checks continue to pass; existing T01 probe
  changes were preserved. User requested an installation script, not a host
  package installation during this session.
- **Files and contracts changed:** `scripts/bootstrap.sh`, offline shell tests,
  `scripts/README.md`, `Makefile` script-test target, README and experiment docs.
  Default is dry-run; package installation and non-root artifact downloads are
  separate explicit modes. No automatic sudo, overwrite, or downloaded execution.
- **Decisions/ADRs:** [0002](adr/0002-explicit-dependency-bootstrap.md), proposed.
  Pins cover Go 1.26.8, candidate Firecracker 1.17.0, Linux source 6.1.188, and
  upstream kernel config; future runtime tools are deliberately not installed.
- **Tests run:** `make test-scripts` and `make check` PASS, including offline
  checksum success/mismatch, transfer failure, fixture extraction/activation,
  special-character paths, existing/symlink target refusal, and cleanup tests.
  `make vulncheck` PASS, no vulnerabilities found. `bash scripts/bootstrap.sh
  --dry-run` and `git diff --check` PASS. Metadata/checksum provenance inspected
  over HTTPS; no independent signature verification claimed.
- **Tests NOT run and why:** Real bootstrap downloads/extraction, dnf package
  installation, guest build, VM boot, and 30-cycle gate were not executed.
  ShellCheck is not installed; Bash syntax and offline behavior were tested.
- **Integration/benchmark evidence:** Installer fixtures only; not upstream
  archive installation or hardware evidence. Existing probe evidence unchanged.
- **Known limitations:** Fedora system packages are distribution-managed, not
  pinned. Script refuses existing installs instead of updating them. Guest init,
  bootable kernel, and initramfs still need implementation. No future backend,
  OCI, Helm, or networking helper dependencies are preselected by the script.
- **Next task:** Run explicit bootstrap on the target host, then continue T01
  guest implementation and real hardware acceptance checks.

## Pre-commit verification after user provisioning

- User reported running the bootstrap; local dependency files are now present.
  Actual VMM execution and guest boot remain unverified.
- Initial `make check` FAILED because formatting traversed the downloaded Go
  compiler's intentionally invalid test fixtures. No artifact files were reformatted.
- Restricted formatting to project source directories, preserving formatter
  failure exit codes. Bootstrap now creates a nested module boundary to keep
  `go ... ./...` and `go mod tidy` out of third-party artifact sources; added an
  offline assertion for that boundary. Added the same ignored boundary file to
  the existing local installation without changing downloaded source files.
- Repeated `make check` and `git diff --check`: PASS with dependencies installed.
- README now includes the user-provided illustration with descriptive alt text;
  the URL-decoded image path resolves. Downloaded artifacts remain ignored.

## Updating this file

Use `TODO`, `IN_PROGRESS`, `BLOCKED`, or `DONE`. Explain blocked prerequisites and separate skipped tests from successful ones. Do not mark tasks complete based only on mocks when their acceptance criteria require real execution.

Append a delivery entry:

```text
Task: Txx — title
Status:
Dependencies verified:
Files and contracts changed:
Decisions/ADRs:
Tests run (command, environment, result):
Tests NOT run and why:
Integration/benchmark evidence:
Known limitations:
Next task:
```
