# Implementation Progress

## Current state

The repository contains design and contributor documentation only. **No implementation task has been completed.** Adding this tracker, contribution guidelines, and license does not complete T00, which also requires a Go module, build targets, CI, and tested command scaffolding.

No runtime tests, KVM experiments, or benchmarks have been performed as part of this documentation work.

## Task status

| Task | Title | Status | Evidence or next prerequisite |
|---|---|---|---|
| T00 | Repository scaffold and conventions | TODO | First implementation task; documentation exists, code/tooling do not |
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
