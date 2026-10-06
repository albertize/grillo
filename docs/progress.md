# Implementation progress

## Current state

Grillo has a working experimental Linux/amd64 QEMU/KVM runtime, Compose and
Kubernetes subset compilers, local/exact-version OCI Helm input, and an embedded
React/PatternFly console. Host orchestration runs as the user; there is no
Kubernetes control plane or silent host-container fallback.

T00–T23 (including T18b) have recorded completion evidence. F0/F2 passed their
real gates; **F3/product hardening is not complete**. T24 is next. There is no
packaged release or supported runtime version. Hosted CI has not been executed
in the recorded delivery sessions.

Known limits include guest stdout ingestion, persisted original compiler line
diagnostics, detailed guest/container usage, interactive browser/CLI TTY,
StatefulSet/Job, rolling updates and TLS/CA. Host-originated virtiofs notifications
require polling; the tested Fedora SELinux Enforcing policy blocks pasta.
Concurrent isolated daemons must coordinate host-wide guest CID ranges.
See [compatibility](compatibility.md) and [console limits](ui.md#explicit-mvp-limits).

## Task status

| Task | Title | Status | Evidence or next prerequisite |
|---|---|---|---|
| T00 | Repository scaffold and conventions | DONE | Local checks, clean-source builds, audit, and command smoke tests; [history](history/implementation-log.md) |
| T01 | Rootless VMM and minimal guest spike | DONE | Real boot/exec/stop 30/30, rootless userns/TAP + pasta egress; [boot](experiments/t01-boot-spike.md), [preflight](experiments/t01-preflight.md) |
| T02 | OCI, filesystem, and application-network spike | DONE | `make test-f0`: two-VM DNS/egress/publish/isolation, live binds, ext4 persistence, overhead; host-watch degraded |
| T03 | F0 gate and platform ADR | DONE | F0 gate passed on QEMU `microvm` + virtiofsd; ADR 0005 Proposed with complete evidence |
| T04 | IR, diagnostics, and capabilities | DONE | `internal/model` + `internal/source`; goldens, version/reference/cycle/overflow, import-boundary tests |
| T05 | State, secrets, and recovery primitives | DONE | `internal/state` + `internal/secrets`; lock, atomic snapshots, journal, migration, secret GC tests |
| T06 | Guest protocol and testable client | DONE | `internal/guestproto` + `api/guest-protocol.md`; framing/handshake/client/server, fuzz, partial-I/O, overflow, backpressure, timeout, auth tests |
| T07 | Guest PID 1 and OCI runtime | DONE | `internal/guest` + `cmd/grillo-agent`; `make test-t07` PASS on real KVM (scenario A, init failure, distinct PIDs, no zombies) |
| T08 | Backend lifecycle and process supervision | DONE | `internal/sandbox` + `internal/platform/linux` + `internal/backend/qemu`; `make test-t08` 100 cycles, no leaks |
| T09 | OCI registry and CAS | DONE | `internal/oci`; unit + adversarial + concurrent-dedup tests, and `make test-netreg` live digest-pinned pull PASS |
| T10 | Storage manager | DONE | `internal/storage`; `make test-t10` (scenario G) and a `virtio-blk` ext4 persistence test PASS on real KVM |
| T11 | Production rootless networking and IPAM | DONE | `internal/network` + `internal/netns`; `make test-netns` and `make test-bridged` PASS (egress, unique addresses, isolation, cross-VM, host-loopback denied) |
| T12 | DNS, Service proxy, and Ingress | DONE | Production namespace DNS/VIPs, targetPort, replica balancing, readiness, headless SRV, Ingress and host TCP publication pass real KVM; revalidated 2026-10-06 |
| T13 | Observability and probes | DONE | `internal/observe` + executor probe wiring; startup gating, thresholds, fake-clock, timeout-kill, liveness-restart KVM test |
| T14 | Planner, reconciler, and updates | DONE | `internal/plan` + `internal/reconcile` + `internal/executor`; diff/recreate/route-only, retries, crash replay, idempotent down, bounded workers, `make test-executor` end-to-end on KVM |
| T15 | Local API and daemon lifetime | DONE | `internal/api` + `cmd/grillod`; peer UID, async ops, status/exec endpoints, SSE cursors, disconnect/leak tests |
| T16 | Native-runtime CLI | DONE | Shared explicit capability policy; real CLI Helm lifecycle/inspection and daemon recovery pass KVM, plus parser/terminal/doctor regressions |
| T17 | Compose parser and compiler | DONE | `internal/frontend/compose` + `detect`; interpolation/env precedence, support diagnostics, golden IR, CLI integration |
| T18 | Native build system and image tooling | DONE | `internal/dockerfile` + `internal/build.NativeBuilder`; copy-only and sandboxed `RUN` builds, Podman opt-in, image store/GC, API/CLI (ADR 0006) |
| T18b | Build execution in the guest (protocol) | DONE | `run` guest message + `SandboxRunner`; real KVM evidence |
| T19 | F2 gate: Compose application | DONE | `make test-f2` real KVM: DNS, volume persistence, idempotent re-apply, down/recovery/cleanup; report `docs/experiments/t19-f2-compose.md` |
| T20 | Kubernetes MVP compiler | DONE | `internal/frontend/kubernetes`; goldens, rejections, secret separation, RollingUpdate consent, multi-container KVM; `docs/experiments/t20-kubernetes-compiler.md` |
| T21 | Helm rendering and OCI charts | DONE | Real Helm package/template, local/OCI equivalence, explicit HTTPS fetch, verified atomic cache, stable offline plans, archive/corruption/concurrency/dependency restrictions; `docs/experiments/t21-helm-renderer.md` |
| T22 | Helm gate and compatibility reporting | DONE | Full direct-runtime scenario C plus actual CLI/daemon chart gate PASS; unsupported provisioning blocked; compiler registry/counts and consumer/PVC semantics verified |
| T23 | Web console and secure bridge | DONE | React/PatternFly frontend (ADR 0008), metadata-only views, bounded logs/SSE/non-TTY exec; actual Firefox desktop/mobile and native CLI/daemon/KVM lifetime/cancellation gates PASS; original data limits remain explicit |
| T24 | F3 MVP gate and hardening | TODO | T23 |
| T25 | StatefulSet and Job | TODO | T24 |
| T26 | Extended Tier 1/Tier 2 parity and TLS | TODO | T25 |
| T27 | Measured optimization and packaging | TODO | T24; T26 for complete F4 |
| T28 | Future research, not a prerequisite | TODO | Stable F3 and measured use cases |

Contracts and acceptance criteria remain in the [implementation plan](../IMPLEMENTATION_PLAN.md).
Completion records are dated evidence, not a claim that tests ran on every
subsequent edit or that the whole product is release-ready.

## Evidence and history

- Feasibility: [T01 boot](experiments/t01-boot-spike.md),
  [preflight](experiments/t01-preflight.md), [T02 OCI/storage](experiments/t02-oci-and-storage.md),
  [T03 backend/network/filesystem](experiments/t03-backend-comparison.md).
- Application gates: [Compose F2](experiments/t19-f2-compose.md),
  [Kubernetes compiler](experiments/t20-kubernetes-compiler.md),
  [Helm rendering](experiments/t21-helm-renderer.md), [Helm runtime](experiments/t22-helm-gate.md),
  [console/browser/KVM](experiments/t23-console.md).
- [Implementation history](history/implementation-log.md): original task deliveries,
  failures, corrective work and environments.
- [Completed runtime review](history/runtime-review.md): R1–R8 and verification.
- [Architecture decisions](adr/README.md): review status is unchanged by delivery.

## Latest maintenance

- **Task:** T23 — documentation consolidation after console delivery.
- **Status:** DONE (documentation only; T24 remains TODO).
- **Dependencies verified:** T23 delivery records, existing API/CLI/Make targets;
  user approval for documentation cleanup. No runtime task is being implemented.
- **Files and contracts changed:** Reader-oriented README/getting-started,
  architecture/testing guides; concise current progress with archived history;
  consolidated T23 evidence; current contributor/security/agent guidance.
  Design contracts, task acceptance criteria, ADR status, generated inventories
  and third-party legal notices remain authoritative and unchanged in meaning.
- **Decisions/ADRs:** Editorial organization only; no architecture change.
- **Tests run:** `python3 /tmp/grillo-check-markdown.py` PASS (all 44 project
  Markdown files: local links/anchors and balanced fences); `git diff --check`
  PASS; actual `bin/grillo --help`/`version` and offline Compose/Helm example
  plans PASS; `make check` PASS (frontend checks, Go fmt/vet/unit/race, scripts,
  builds and module verification). Generated compatibility inventories were
  not edited. Verification used the existing Linux/amd64 development host.
- **Tests NOT run and why:** New KVM/browser/load/security campaigns are not
  required for prose-only changes; dated delivery evidence is preserved, not rerun.
- **Integration/benchmark evidence:** Existing reports linked above; no new claim.
- **Known limitations:** Product limits remain; cleanup does not complete T24.
- **Next task:** T24 — F3 gate and hardening.

## Updating progress

Keep this file focused on current status and the latest bounded change. Move
older delivery entries to the tracked history with working links; retain failed
gates, corrections, environments, hashes and blockers. Reproducible acceptance
evidence belongs in `docs/experiments/`, not repeated in every guide. Use the
[agent workflow](../AGENTS.md#8-progress-entry) for the delivery-entry fields.
