# Implementation Progress

## Current state

**T00 is implemented and locally verified.** The repository contains a Go module, tested command scaffolding, build/check targets, and CI configuration.

**T01 is complete.** A real Firecracker microVM boots as the user, completes a vsock handshake, executes a guest command, streams output, and stops cleanly across 30 measured cycles (boot median 260 ms, total median 391 ms). A rootless networking helper (pasta) was also verified to give a new user+network namespace an address, default route, DNS, and egress.

**T02 remains BLOCKED on outstanding acceptance evidence.** OCI execution and two-container localhost pass on Firecracker and QEMU. The corrected QEMU probe verifies post-mount host writes, guest writes, rename, and read-only enforcement. Guest-local inotify passes, but host-originated notifications were not observed within 2 seconds (`DEGRADED`; polling needed).

**T03 remains BLOCKED; QEMU is a proposed candidate, not a completed F0 gate.** Two-VM DNS/egress/publishing/management isolation, managed-volume persistence, and filesystem overhead remain unverified T02 requirements. See [ADR 0005](adr/0005-platform-qemu-virtiofsd.md). There is still no workload runtime or release.

**T04 is complete.** The versioned application IR lives in `internal/model` with `internal/source` for source locations and structured diagnostics. It provides the section 5.1 types, quantity parsing, normalization, canonical hashing, an experimental native JSON manifest, a capability/support registry, validation (unknown version, references, duplicates, cycles, guest budget), and public redaction. The model imports no VMM, network, process, or frontend code.

Hosted CI has not yet been executed.

## Task status

| Task | Title | Status | Evidence or next prerequisite |
|---|---|---|---|
| T00 | Repository scaffold and conventions | DONE | Local checks, clean-source builds, audit, and command smoke tests; evidence below |
| T01 | Rootless VMM and minimal guest spike | DONE | Real boot/exec/stop 30/30, rootless userns/TAP + pasta egress; two T01 reports |
| T02 | OCI, filesystem, and application-network spike | BLOCKED | Partial real evidence; two-VM topology, managed storage, and overhead missing; host-watch degraded |
| T03 | F0 gate and platform ADR | BLOCKED | Proposed QEMU candidate; complete T02 evidence and review ADR 0005 before confirming F0 |
| T04 | IR, diagnostics, and capabilities | DONE | `internal/model` + `internal/source`; goldens, version/reference/cycle/overflow, import-boundary tests |
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

- User reported running the bootstrap; local dependency files are present.
  Actual VMM execution and guest boot were verified later (see the T01 boot
  spike entry below).
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

## T01 boot spike — 2026-10-03

- **Task:** T01 — Rootless VMM and minimal guest spike
- **Status:** DONE (boot spike portion; helper covered by the completion entry below)
- **Dependencies verified:** T00; bootstrap provisioned pinned Firecracker 1.17.0
  and Linux 6.1.188 sources; `make check` passes.
- **Files and contracts changed:** `experiments/boot/` (`guestinit`, `guestcmd`,
  `run`, `build-guest.sh`), `Makefile` (`guest`, real `test-kvm`, `bench-t01`),
  `go.mod`/`go.sum` now require `golang.org/x/sys v0.48.0` (plan-approved; guest
  vsock only). Report: [t01-boot-spike](experiments/t01-boot-spike.md).
- **Decisions/ADRs:** Throwaway experiment protocol and guest-initiated
  `reboot(RESTART)` shutdown (host `SendCtrlAltDel` is inert with the pinned
  guest config, which has `CONFIG_VT` but no `SERIO`/`I8042`). Recorded for
  T03/T08; no backend selected. `golang.org/x/sys` was already a planned module.
- **Tests run:** `make test-kvm` PASS (`TestKVMExecBootStop`,
  `TestKVMExecMissingCommand`) on real KVM; `make bench-t01` / 30 cycles PASS,
  30/30, boot median 260 ms p95 265 ms, total median 391 ms p95 400 ms;
  `make check` PASS; `make vulncheck` PASS (no vulnerabilities); `git diff --check` PASS.
- **Tests NOT run and why:** Networking-helper forwarding (pasta/slirp4netns),
  OCI execution, storage sharing, cold-cache and competing-workload benchmarks,
  and hosted CI were not run.
- **Integration evidence:** real boot, vsock handshake, guest exec, output
  streaming, and clean stop on `/dev/kvm` as UID 1000; artifacts and hashes in
  the report.
- **Cleanup evidence:** no `firecracker` processes, no `/tmp/grillo-t01-*`
  directories, and no Grillo mounts remained after the runs.
- **Known limitations:** experiment protocol/binaries are replaced by T06/T07; no
  Pod/OCI/container semantics; measurements are single-host, warm-cache,
  1 vCPU/256 MiB; guest is a trusted image; helper networking unproven.
- **Next task:** finish T01 helper-networking verification or proceed to T02
  (OCI, filesystem, application networking) with the backend still provisional.

## T01 completion — rootless networking helper

- **Task:** T01 — final acceptance item (helper without root)
- **Status:** DONE (T01 complete)
- **Dependencies verified:** T00; bootstrap and boot spike committed.
- **Files and contracts changed:** `experiments/boot/netns-helper.sh`,
  `Makefile` (`net-helper`), `experiments/boot/README.md`, and the updated
  [T01 report](experiments/t01-boot-spike.md).
- **Decisions/ADRs:** None new. Only `pasta` is exercised; `slirp4netns` is not
  implemented or claimed. No backend selected.
- **Tests run:** `sh experiments/boot/netns-helper.sh` PASS (pasta 0^20260728,
  UID 1000, address + default route + DNS + HTTP 200 egress, no leftover
  process); offline-egress case INCONCLUSIVE exit 2; unsupported-helper case
  BLOCKED exit 1; `make check` PASS; `git diff --check` PASS.
- **Tests NOT run and why:** `slirp4netns` absent; guest `virtio-net` not wired
  to the helper (T02/T11); hosted CI not run.
- **Integration evidence:** real rootless namespace connectivity on the host;
  distribution restrictions (`max_user_namespaces=62055`, SELinux `Enforcing`,
  no `unprivileged_userns_clone`, `/dev/net/tun` present) recorded in the report.
- **Cleanup evidence:** no `pasta` process remained; host interface count unchanged.
- **Known limitations:** helper validated in isolation, not from inside a VM;
  port forwarding and multi-VM routing untested.
- **Next task:** T02 — OCI execution through guest runc, two-VM application
  networking, and live bind mounts, with the backend still provisional.

## T02 spike — 2026-10-03

- **Task:** T02 — OCI, filesystem, and application-network spike
- **Status:** BLOCKED (scenario A + B pass; scenario G fails; scenario C not attempted)
- **Dependencies verified:** T01 complete; bootstrap and OCI fixtures present;
  `make check` passes.
- **Files and contracts changed:** `experiments/boot/spike/` (reusable session),
  `run/` rewritten as a CLI, `guestinit` (cgroup2/devpts/shm/run mounts, loopback,
  detached-start command), `fetch-oci.sh`, `build-oci-guest.sh`, `storage-probe.sh`,
  `oci/` harness + kvm test, `Makefile` (`oci-guest`, `storage-probe`, extended
  `test-kvm`). Report: [t02-oci-and-storage](experiments/t02-oci-and-storage.md).
- **Decisions/ADRs:** [0004](adr/0004-shared-filesystem-backend-comparison.md)
  (proposed): compare QEMU microvm + virtiofsd and Cloud Hypervisor + virtiofs
  before T03; Firecracker has no shared-filesystem device.
- **Tests run:** `make test-kvm` PASS — `TestKVMExecBootStop`,
  `TestKVMExecMissingCommand`, `TestKVMOCIScenarios` (scenarios A and B) on real
  KVM; `make check` PASS; `make storage-probe` exits 3 (BLOCKED, expected);
  `git diff --check` PASS. OCI runtime static runc v1.5.2; image
  `busybox:1.37@sha256:bdf5...`.
- **Tests NOT run and why:** scenario C (two-VM DNS/egress/host publishing,
  management isolation) not attempted — needs a namespace/TAP/helper harness;
  live-bind and filesystem-overhead measurements impossible with Firecracker.
- **Integration evidence:** real guest runc start/state/exec/TERM/delete and a
  client container reaching a server container on `127.0.0.1`
  (`grillo-oci-localhost-ok`).
- **Known limitations:** Firecracker cannot do live host-directory binds; guest
  PID 1 does not yet reap orphaned children (spike uses `runc delete --force`);
  detached stdio and `--no-pivot` requirements recorded for T07; containers run
  as guest root; protocol/bundles are experiment-only.
- **Next task:** T03 — backend comparison and platform ADR (do not start
  storage/backend-dependent tasks first). Pure IR work (T04/T17/T20) is independent.

## T03 backend comparison — 2026-10-03

**Historical entry, corrected by the review follow-up below:** the original
completion/watch claims exceeded the tests' coverage. The current task table
and review follow-up supersede those claims; the original measurements remain
historical evidence only.

- **Task:** T03 — F0 gate and platform ADR
- **Status:** DONE (decision evidence complete; adoption pending ADR review)
- **Dependencies verified:** T00–T02; QEMU 10.2.2 and virtiofsd 1.14.0 installed by
  the user; `make check` passes.
- **Files and contracts changed:** `experiments/boot/qemu/` (kernel/initramfs
  builds, storage and network probes, harness), `experiments/boot/spike` (AF_VSOCK
  support and a nil-session cleanup fix), `experiments/boot/oci` (QEMU backend),
  `Makefile` (`qemu-guest`, `test-qemu`, `qemu-share`, test filters). Reports:
  [t03-backend-comparison](experiments/t03-backend-comparison.md).
- **Decisions/ADRs:** [0005](adr/0005-platform-qemu-virtiofsd.md) (Proposed):
  QEMU `microvm` + virtiofsd as the F0 backend; Firecracker rejected for the
  product because it has no shared-filesystem device.
- **Tests run:** `make test-qemu` PASS (`TestKVMQEMULiveShare`,
  `TestKVMQEMUNet`, `TestKVMQEMUOCIScenarios`); `make test-kvm` PASS
  (`TestKVMExecBootStop`, `TestKVMExecMissingCommand`, `TestKVMOCIScenarios`);
  `make storage-probe` exits 3 (BLOCKED, expected); `make check` PASS;
  `git diff --check` PASS.
- **Integration evidence:** QEMU virtiofs live rw/read-only/rename/watch; QEMU
  user-mode guest TCP egress and DNS; OCI run/exec/signal/delete and two-container
  localhost on QEMU via `vhost-vsock` (boot ≈404 ms, VMM RSS ≈142 MiB vs
  Firecracker ≈49 MiB). No orphan VMM processes after runs.
- **Tests NOT run and why:** cold-cache/multi-vCPU benchmarks, managed
  virtio-block volume overhead, two-VM DNS/host publishing (T11), QEMU hardening
  pass, and Cloud Hypervisor comparison.
- **Known limitations:** virtiofsd ran with `--sandbox=none`; QEMU device surface
  not yet hardened; guest PID 1 reaping and VMM orphan reconciliation belong to
  T07/T08; ADRs 0004/0005 await maintainer review.
- **Next task:** T04 (IR, diagnostics, capabilities) is independent and can start;
  storage/backend work (T09/T10) should follow the QEMU hardening pass.

## T01–T03 review follow-up — 2026-10-03

- **Task:** T02/T03 — repair experiment safety and acceptance evidence.
- **Status:** BLOCKED (code fixes implemented; full feasibility gate still open).
- **Dependencies verified:** T00/T01 implementation, existing pinned guest
  artifacts, UID 1000, accessible KVM and vhost-vsock, QEMU and virtiofsd.
- **Files and contracts changed:** OCI fixture creation now owns a unique
  container ID, serializes builders, publishes validated rootfs atomically, and
  rejects unmarked caches. Cached runc is verified before execution. Host exec
  has total deadlines, cancellation, 64 KiB frames and 4 MiB aggregate output;
  failed exchanges close the channel. QEMU probes propagate wait failures and
  cancellation. Storage probes distinguish post-mount content coherence from
  host-originated and guest-local inotify. Added offline regression tests and
  short preventive rules to `AGENT.md`.
- **Decisions/ADRs:** ADRs 0004/0005 evidence narrowed; no acceptance or scope
  reduction approved. T02/T03 reverted to BLOCKED, not silently deferred.
- **Tests run:** `make qemu-share` PASS for content/rw/ro/rename/guest-watch;
  host-watch explicitly DEGRADED (no event within 2s). Direct existing-artifact
  test command `go test -tags kvm -count=1 -v -run 'TestKVMQEMU'
  ./experiments/boot/qemu/run/ ./experiments/boot/oci/` PASS: three real tests,
  including networking and OCI. Offline OCI failure/retry/cache/concurrency
  tests PASS. Initial `make check` failed on a prematurely closed net.Pipe
  fixture; repeated race tests then exposed a frame-limit boundary waiting for
  more input. Both failures were corrected, not treated as successful checks.
  Final `make check` PASS; `go test -race -count=10
  ./experiments/boot/spike ./experiments/boot/qemu/run` PASS. Existing-artifact
  Firecracker command `go test -tags kvm -count=1 -v -run
  'TestKVMExecBootStop|TestKVMExecMissingCommand|TestKVMOCIScenarios'
  ./experiments/boot/spike/ ./experiments/boot/oci/` PASS (three real tests).
  `bash -n experiments/boot/fetch-oci.sh scripts/fetch_oci_test.sh` and
  `git diff --check` PASS; process inspection found no remaining QEMU,
  virtiofsd, or Firecracker processes.
- **Tests NOT run and why:** Full `make test-qemu` provisioning not run: existing
  OCI artifacts were reused to avoid downloads and deleting/replacing the old
  unmarked rootfs. Fresh real Podman export remains unverified; offline doubles
  test script behavior only. No cold-cache/30-cycle benchmark or hosted CI run.
- **Integration evidence:** updated storage initramfs SHA-256
  `ab2df454d2d8fdb3f88d603b769ad63cb0fc7be8cd4a9bce3e7e0ba5e49bf5f8`;
  real QEMU host-update visibility with degraded remote notifications.
- **Known limitations:** two-VM networking/isolation, managed persistent storage,
  and filesystem overhead remain required evidence; virtiofsd sandbox and guest
  PID 1 reaping remain unhardened. Protocol is still a trusted-image experiment,
  not the authenticated production protocol. Legacy rootfs directories must be
  reviewed and moved aside explicitly, never automatically deleted.
- **Next task:** finish the missing T02 experiments and assess the host-watch
  limitation before closing T03; pure T04 work remains independent.

## T04 IR and diagnostics — 2026-10-03

- **Task:** T04 — IR, diagnostics, and capabilities
- **Status:** DONE
- **Dependencies verified:** T00 (module, checks); T02/T03 remain blocked and are
  independent of this pure work.
- **Files and contracts changed:** `internal/model` (types, quantities,
  capabilities/support registry, normalization, canonical hashing, validation,
  redaction, experimental native manifest) and `internal/source` (source kinds,
  positions/spans, source map, severity/compatibility, structured diagnostics).
  IR version is `grillo.dev/v1alpha1`.
- **Decisions/ADRs:** None required. The model boundary is enforced by
  `TestModelImportBoundary`: it imports only the standard library and
  `internal/source`.
- **Tests run:** `make check` PASS; `go test ./internal/...` PASS;
  `go test -race ./internal/model ./internal/source` PASS. Coverage includes a
  deterministic canonical JSON golden, hash determinism and volatile-field
  insensitivity (source path, identity ID, route endpoint) plus sensitivity to
  secret-version and image-digest changes, quantity overflow/invalid inputs,
  order-insensitive normalization, unknown version, missing references,
  duplicates, dependency cycles, negative quantities, guest-budget aggregation,
  unsupported capabilities, env value/valueFrom conflict, duplicate ports, native
  manifest decode/trailing-data rejection and round-trip, and public redaction.
- **Tests NOT run and why:** No KVM/runtime tests; the IR is pure and has no
  effectful path. Frontend compilers (T17/T20) and the state store (T05) are not
  implemented yet, so no end-to-end compile test exists.
- **Integration evidence:** None required for a pure package; the done-when
  gate is deterministic goldens plus version/reference/cycle/overflow tests and
  the import boundary, all present.
- **Known limitations:** `Workload.DependsOn` is an experimental native-manifest
  ordering hint, not a Kubernetes/Compose concept; capability data is described
  conservatively until real runtime integration. Secret values never enter the
  IR; redaction covers insensitive config entries and arbitrary text.
- **Next task:** T05 — state, secrets, and recovery primitives (depends on T04).

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
