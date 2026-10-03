# Implementation Progress

## Current state

**T00 is implemented and locally verified.** The repository now contains a Go module, tested command scaffolding, build/check targets, and CI configuration. There is still no workload runtime, guest init implementation, or release.

No KVM experiments or runtime benchmarks have been performed. Hosted CI has not yet been executed.

## Task status

| Task | Title | Status | Evidence or next prerequisite |
|---|---|---|---|
| T00 | Repository scaffold and conventions | DONE | Local checks, clean-source builds, audit, and command smoke tests; evidence below |
| T01 | Rootless VMM and minimal guest spike | TODO | T00 |
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
