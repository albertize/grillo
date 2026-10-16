# Implementation progress

## Current state

Grillo has a working experimental Linux/amd64 QEMU/KVM runtime, Compose and
Kubernetes subset compilers, local/exact-version OCI Helm input, and an embedded
React/PatternFly console. Host orchestration runs as the user; there is no
Kubernetes control plane or silent host-container fallback.

T00–T23 (including T18b) have recorded completion evidence. F0/F2 passed their
real gates; **F3/product hardening is not complete**. T24 is BLOCKED after the
local hardening campaign; missing §28 contracts and external-host evidence are
listed in the [current T24 report](experiments/t24-interactive-metrics-artifacts.md). There is no
packaged release or supported runtime version. [User-supplied hosted CI logs](experiments/t24-ci-log-review.md)
now record Ubuntu unit/build checks PASS but Go stdlib advisory FAIL; no hosted
run was started by the coding agent. Prior local vendor-version zero-finding
scans do not clear stdlib vulnerabilities. [Actual pinned Go 1.26.9 local rebuilds](experiments/t24-go126-toolchain.md)
passed advisory/check/fuzz/browser/KVM gates at that pin. The current pin is
**Go 1.27.2**, matching the user's installed upstream toolchain; `make check`
passes locally. Bounded [Chrome/browser Terminal and dependency KVM gates](experiments/t24-browser-terminal-compose-dependencies.md)
now pass on 1.27.2. The new [autonomous local campaign](experiments/t24-autonomous-local-campaign.md)
records current advisory/check/race, five 5-minute fuzz targets and sequential
KVM gates PASS, plus new API/TCP admission and resource collectors. Its final
30-repeat resource gate failed on registry HTTP 429 (earlier batch PASS); that
failure is retained. The [continuation](experiments/t24-pinned-cache-block-quota-sources.md)
now records a real live fixture pull, verified digest-only offline reuse, actual
opt-in ext4 volume ENOSPC/persistence and 30-repeat seeded local budgets PASS.
The [host-write continuation](experiments/t24-native-host-write-budgets.md) now
bounds cumulative native host copies/imports/layer staging with finite defaults.
The [agent export prerequisite](experiments/t24-agent-owned-build-export.md) now
adds authenticated bounded snapshots and private host extraction. The
[opt-in block integration](experiments/t24-opt-in-block-build-workspaces.md) now
wires per-step disks, agent population and COPY/RUN snapshot adoption in software.
Its [real workspace KVM gate](experiments/t24-block-workspace-kvm.md) now PASS
outside the session sandbox after fixture corrections; KVM/vsock devices are
available on the host but hidden inside the sandbox.
Legacy/bind volume writes and default native RUN remain unbounded; full
load/performance/source/release acceptance remains BLOCKED. The
[fault/load continuation](experiments/t24-workspace-faults-advisory-review.md)
passes actual RUN cancellation, owned VMM SIGKILL and output exhaustion with
fresh-disk retries in ten sequential race-detector repetitions. Fresh binary
advisory scans still FAIL (runc 14, Helm 15); a Helm source comparison reports
eight called advisories from three modules and does not clear the original
binary. Hosted rerun/complete independent clean-host acceptance remain pending;
[partial second-host evidence](experiments/d0-second-host-fedora44.md) exists.
The [isolated candidate campaign](experiments/t24-external-candidates.md) now
builds patched runc/Helm with explicit source/native inputs, passes source/binary
called-vulnerability scans (Helm retains one module-only advisory), real installed
and isolated workspace gates, and 30/30 seeded resource budgets. Originals and
normal selection remain unchanged. One combined-load workspace retry timeout is
retained; formal promotion, native/source/license review and remaining F3 gates
are not cleared by the candidate passes.
The [timeout investigation](experiments/t24-reaper-timeout.md) fixes a reproduced
lost exit delivery in the guest reaper; corrected guest load/KVM/installed gates
pass. The historical hardware timeout remains unproven as to cause, and software
load exposed separate reproducible DNS TCP bind collisions.

Container stdout/stderr now reaches the bounded guest ring and host spool, with
explicit guest-retention gaps; [real pipeline evidence](experiments/t24-guest-logs.md).
CLI stdin/TTY/resize, actual guest/cgroup counters and verified private boot
snapshots/fresh keys now have [local evidence](experiments/t24-interactive-metrics-artifacts.md).
Known limits include persisted original compiler line
diagnostics, PSS/cache/CPU percentages, full-screen browser terminal emulation,
StatefulSet/Job, rolling updates and TLS/CA. Pod Terminal now has real browser
stdin/PTY/resize with a bounded text-only VT subset. Host-originated virtiofs notifications
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
| T23 | Web console and secure bridge | DONE | React/PatternFly (ADR 0008), metadata-only views, bounded logs/SSE/captured Exec; [interactive Pod Terminal + Chrome/KVM](experiments/t24-browser-terminal-compose-dependencies.md) now PASS; original Firefox evidence remains historical, full terminal emulation/data limits explicit |
| T24 | F3 MVP gate and hardening | BLOCKED | [100 verified boots](experiments/t24-closure-campaign.md) PASS; [volume/seeded-budget/source evidence](experiments/t24-pinned-cache-block-quota-sources.md), [host budgets](experiments/t24-native-host-write-budgets.md), [opt-in build integration](experiments/t24-opt-in-block-build-workspaces.md); original binary advisories FAIL; [candidates](experiments/t24-external-candidates.md) pass bounded local gates; combined-load/crash/source and hosted/clean-host gates remain |
| T25 | StatefulSet and Job | TODO | T24 |
| T26 | Extended Tier 1/Tier 2 parity and TLS | TODO | T25 |
| T27 | Measured optimization and packaging | TODO | T24; T26 for complete F4 |
| T28 | Future research, not a prerequisite | TODO | Stable F3 and measured use cases |
| D0 | Packaging-ready runtime layout | BLOCKED | [Runtime-only guest and installed Compose/Helm/UI](experiments/d0-installed-runtime.md) local KVM gates PASS; T24, clean-host, license/source/provenance and release lifecycle remain |

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

## Latest delivery

- **Task:** T24 — guest reaper race and combined-load workspace timeout investigation.
- **Status:** BLOCKED overall; reproduced lost exit delivery fixed. Historical
  hardware timeout is not conclusively attributed; separate DNS bind failures remain.
- **Dependencies verified:** T06 protocol, T07 guest, T18/T18b build runtime and
  candidate portable-guest/installed gates; actual host KVM API 12/vsock as UID
  1000 outside the sandbox, with SELinux already Permissive.
- **Files and contracts changed:** Atomic waiter lookup/retained exit insertion,
  concurrent regression, bounded private phase/guest log retention before owned
  hardware-test cleanup, diagnostic regression and evidence/testing docs. No
  timeout, protocol, dependency or normal guest selection changes.
- **Decisions/ADRs:** No architectural change; existing candidate and opt-in
  workspace ADRs remain Proposed.
- **Tests run:** Regression FAIL before fix (stranded waiter at PID 117); ten
  reaper/race repetitions PASS after fix (200,000 synthetic races). Tagged
  diagnostics/compilation PASS; actual rebuilt old and corrected portable guests
  each pass initial plus ten KVM/race workspace runs during software load.
  T07 and corrected installed runtime KVM PASS. Final `make check` PASS.
  First full ten-repeat software/race load PASS; second FAIL on three network
  bind collisions. Focused network repeat also FAIL on two bind collisions.
  Commands, hashes and timings are in the [report](experiments/t24-reaper-timeout.md).
- **Tests NOT run and why:** New fuzz/advisory campaigns, independent-host,
  persistent-volume/daemon crash and release/source/license promotion gates are
  outside this bounded fix; dependencies were unchanged.
- **Integration/benchmark evidence:** Actual corrected guest rebuilt and verified
  by immutable inventory; fault/retry and installed Compose/Helm gates PASS.
  Old guest also passed, so no exclusive cause for the historical timeout or
  whole-host saturation performance is claimed.
- **Known limitations:** DNS UDP-selected ephemeral ports can collide on TCP;
  that separate issue remains reproducible. Original artifact advisories and
  candidate promotion/F3/release gates remain. No commit or publication.
- **Next task:** Separate DNS dual-protocol port allocation correction; inspect
  retained phase/guest logs if the historical workspace timeout recurs. Remaining
  crash/source/clean-host gates follow; no T25 start.

## Updating progress

Keep this file focused on current status and the latest bounded change. Move
older delivery entries to the tracked history with working links; retain failed
gates, corrections, environments, hashes and blockers. Reproducible acceptance
evidence belongs in `docs/experiments/`, not repeated in every guide. Use the
[agent workflow](../AGENTS.md#8-progress-entry) for the delivery-entry fields.
