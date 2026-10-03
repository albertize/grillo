# Implementation Progress

## Current state

**T00 is implemented and locally verified.** The repository contains a Go module, tested command scaffolding, build/check targets, and CI configuration.

**T01 is complete.** A real Firecracker microVM boots as the user, completes a vsock handshake, executes a guest command, streams output, and stops cleanly across 30 measured cycles (boot median 260 ms, total median 391 ms). A rootless networking helper (pasta) was also verified to give a new user+network namespace an address, default route, DNS, and egress.

**T02 is complete.** OCI execution and two-container localhost pass on both backends. Two-VM HTTP/DNS/egress, loopback host publishing, cross-application and guest-to-management denial, live virtiofs binds, a managed ext4 volume with restart persistence, and filesystem overhead were all measured on real hardware (`make test-f0`). Host-originated virtiofs notifications are explicitly degraded (polling).

**T03 is complete; the F0 gate passed on QEMU `microvm` + virtiofsd.** See [ADR 0005](adr/0005-platform-qemu-virtiofsd.md) (Proposed, all required evidence present, awaiting maintainer acceptance). There is still no workload runtime or release. A measured distribution restriction: SELinux Enforcing silently kills `pasta` helpers on the tested Fedora policy, so `doctor` must surface it.

**T04 is complete.** The versioned application IR lives in `internal/model` with `internal/source` for source locations and structured diagnostics. It provides the section 5.1 types, quantity parsing, normalization, canonical hashing, an experimental native JSON manifest, a capability/support registry, validation (unknown version, references, duplicates, cycles, guest budget), and public redaction. The model imports no VMM, network, process, or frontend code.

**T05 is complete.** `internal/state` provides the per-user XDG layout, a single-writer `flock`, atomic snapshots with fsync, pending operations and observations, schema versioning with backup-before-migration, and a bounded rotating NDJSON journal that recovers a truncated last record but rejects mid-file corruption. `internal/secrets` keeps secret values out of the IR and state, versions them with random tokens that change only on real change, and garbage-collects only unreferenced versions.

**T06 is complete.** `internal/guestproto` implements the bounded, versioned, authenticated host/guest protocol and its documented wire format in [api/guest-protocol.md](../api/guest-protocol.md). The framing codec is independent of the transport; an AF_VSOCK dialer/listener and an in-memory plain-conn test transport are provided. The real guest agent (PID 1, OCI runtime) is still T07.

**T07 is complete.** The guest PID 1 agent (`cmd/grillo-agent`) and its runtime
live in `internal/guest`: cgroup/filesystem setup, single-owner child reaping,
OCI bundle generation, a runc adapter, an artifact manifest, and a lifecycle
handler that serves `start`/`stop`/`status`/`exec`/`probe` over the T06 protocol.
The guest image is built by `make t07-guest`, and `make test-t07` boots it under
real KVM/QEMU and passes OCI scenario A plus an init-failure case.

**T08 is complete.** `internal/sandbox` defines the VMM-independent backend contract, `internal/platform/linux` provides PID-reuse-safe process identity, and `internal/backend/qemu` implements the ADR 0005 backend: idempotent `Create`, boot + vsock handshake, `Inspect`, graceful/forced `Stop`, and reverse-order `Delete` with persisted state. `make test-t08` runs 100 real create/start/stop/delete cycles with no leaked VMM process or directory.

**T09 is complete.** `internal/oci` implements reference parsing, the OCI Distribution API subset with bearer/basic auth and bounded HTTP, schema2/OCI manifest and index handling with `linux/amd64` selection, a content-addressed blob store with atomic verified commits and concurrent-pull deduplication, safe gzip/tar layer unpacking with whiteouts and opaque directories, image-config merging, and `diff_id` verification. Adversarial tars and corrupt blobs are rejected.

Hosted CI has not yet been executed.

## Task status

| Task | Title | Status | Evidence or next prerequisite |
|---|---|---|---|
| T00 | Repository scaffold and conventions | DONE | Local checks, clean-source builds, audit, and command smoke tests; evidence below |
| T01 | Rootless VMM and minimal guest spike | DONE | Real boot/exec/stop 30/30, rootless userns/TAP + pasta egress; two T01 reports |
| T02 | OCI, filesystem, and application-network spike | DONE | `make test-f0`: two-VM DNS/egress/publish/isolation, live binds, ext4 persistence, overhead; host-watch degraded |
| T03 | F0 gate and platform ADR | DONE | F0 gate passed on QEMU `microvm` + virtiofsd; ADR 0005 Proposed with complete evidence |
| T04 | IR, diagnostics, and capabilities | DONE | `internal/model` + `internal/source`; goldens, version/reference/cycle/overflow, import-boundary tests |
| T05 | State, secrets, and recovery primitives | DONE | `internal/state` + `internal/secrets`; lock, atomic snapshots, journal, migration, secret GC tests |
| T06 | Guest protocol and testable client | DONE | `internal/guestproto` + `api/guest-protocol.md`; framing/handshake/client/server, fuzz, partial-I/O, overflow, backpressure, timeout, auth tests |
| T07 | Guest PID 1 and OCI runtime | DONE | `internal/guest` + `cmd/grillo-agent`; `make test-t07` PASS on real KVM (scenario A, init failure, distinct PIDs, no zombies) |
| T08 | Backend lifecycle and process supervision | DONE | `internal/sandbox` + `internal/platform/linux` + `internal/backend/qemu`; `make test-t08` 100 cycles, no leaks |
| T09 | OCI registry and CAS | DONE | `internal/oci`; reference/manifest/CAS/unpack/registry/pull tests, 32 cases incl. adversarial tar + concurrent dedup |
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

## T05 state, secrets, and recovery — 2026-10-03

- **Task:** T05 — state, secrets, and recovery primitives
- **Status:** DONE
- **Dependencies verified:** T04 complete; `make check` passes.
- **Files and contracts changed:** `internal/state` (XDG `Layout` with
  ownership/symlink checks and 0700 dirs; single-writer `flock`; `Snapshot` with
  desired application, pending `Operation`s, and `Observation`s; `AtomicWriteFile`
  with per-step failure-injection `Ops`; schema `Version` with backup-before-
  migration; bounded rotating NDJSON `Journal`) and `internal/secrets` (separate
  value store, content-compared random version tokens, public `Refs`, and
  `Collect` for unreferenced versions). Secrets are stored via `state.AtomicWriteFile`.
- **Decisions/ADRs:** None required. The `Ops` injection seam is an atomic-write
  primitive shared by state and secrets, not a speculative abstraction.
- **Tests run:** `make check` PASS; `go test -race ./internal/state
  ./internal/secrets ./internal/model ./internal/source` PASS. Coverage includes:
  XDG resolution and fallbacks, 0700 directory creation, symlink and owner
  rejection, commit/load round trip, 0600 state file, second-writer rejection,
  future-version rejection, legacy v0 migration with `state.v0.bak` backup,
  corrupt-state reporting, stray temp-file tolerance, injected failure at every
  atomic-write step (previous state survives; no temp leaks), directory-fsync
  failure semantics, journal append/read with persistent sequence IDs,
  truncated-last-line recovery, mid-file corruption rejection, rotation bounds
  and sequence continuity, secret put/get, version stability on identical values,
  new version on change, version-mismatch and not-found errors, secret file/dir
  permissions, public refs without values, `Collect` removing only unreferenced
  versions, and a cross-package test that state snapshots and public IR never
  contain a secret value while retaining the version.
- **Tests NOT run and why:** No process-level crash/kill test (single-process
  `flock` and injected step failures cover the recovery contract); no hosted CI.
- **Integration evidence:** Repository-local only; state is a pure local
  primitive and has no hardware dependency.
- **Known limitations:** `flock` is advisory and per-host; secret values are
  stored unencrypted under 0600 (documented for local use; OS keychain is a later
  option). Journal timestamps use wall-clock time.
- **Next task:** T06 (guest protocol) depends on the blocked T03; T17/T20 pure
  frontends and T09 OCI work depend on T04. T07/T08 depend on T06/T05.

## T02/T03 F0 gate — 2026-10-03

- **Task:** T02 — OCI, filesystem, and application-network spike; T03 — F0 gate
  and platform ADR.
- **Status:** DONE (both).
- **Dependencies verified:** T00/T01; real KVM (`/dev/kvm` API 12), QEMU 10.2.2,
  virtiofsd 1.14.0, pasta, `/dev/vhost-vsock`, `/dev/net/tun`, user namespaces,
  nftables, setpriv, and e2fsprogs, all as UID 1000 without sudo. Working tree
  contained T04/T05 as `2ab4244`; unrelated work preserved.
- **Files and contracts changed:** `experiments/boot/qemu/f0guest/` (trusted
  guest payload), `experiments/boot/qemu/fsbench/` (fixed filesystem benchmark),
  `experiments/boot/qemu/run/f0_linux_amd64.go` (rootless topology, isolation,
  storage, supervision, benchmarks), `run/dns_fixture.go` + test (bounded fixture
  resolver), `run/main_linux_amd64.go` (hardened QEMU/virtiofsd flags, wait
  cancellation), `build-f0-guest.sh`, `fetch-oci.sh` cidfile cleanup fix,
  `scripts/fetch_oci_test.sh` (faithful fake podman), `Makefile` (`f0-guest`,
  `test-f0`). No production runtime code was added.
- **Decisions/ADRs:** [0005](adr/0005-platform-qemu-virtiofsd.md) updated to
  Proposed with complete evidence (awaiting maintainer acceptance);
  [0004](adr/0004-shared-filesystem-backend-comparison.md) marked complete.
  No scope reduction was made: the previously deferred T02 requirements were
  executed, not deferred.
- **Tests run:** `make check` PASS; `make test-f0` PASS (full rootless F0 gate,
  re-run twice, `RESULT: PASS (f0)`); `make test-qemu` PASS
  (`TestKVMQEMULiveShare`, `TestKVMQEMUNet`, `TestKVMQEMUOCIScenarios`);
  `make test-kvm` PASS (`TestKVMExecBootStop`, `TestKVMExecMissingCommand`,
  `TestKVMOCIScenarios`); `make qemu-share` PASS; `go test ./experiments/boot/qemu/run
  -run TestFixtureDNS` PASS; `bash scripts/fetch_oci_test.sh` PASS with a fake
  podman that mirrors real cidfile removal. `make oci-guest` regenerated the OCI
  rootfs with real podman (marker + executable busybox verified).
- **Tests NOT run and why:** cold-cache and multi-vCPU/multi-memory benchmarks,
  tuned virtiofs cache performance, and Cloud Hypervisor comparison remain open;
  hosted CI has not run. Full `make test-qemu` initially failed on the stale
  unmarked rootfs and was re-run green after regeneration — not counted as a pass
  before that.
- **Integration evidence:** three rootless QEMU microVMs booted as UID 1000 with
  `CapEff=0`, `NoNewPrivs=1`, `Seccomp=2`, RSS ≈122 MiB, PSS ≈102 MiB; two-VM
  HTTP/DNS/egress PASS; loopback host publish PASS; cross-application,
  guest→management, and isolated-VM denial PASS; virtiofs live content/rw/ro/rename
  PASS with host-originated watch DEGRADED; managed ext4 volume exclusive-lock,
  write, restart-persistence, and `e2fsck` clean PASS; warm boot 30/30, median
  605 ms, p95 606–708 ms; 30-sample filesystem overhead recorded (virtiofs read
  ≈56×, rename ≈63×, chmod ≈180× slower than local ext4). No orphan VMM or work
  directory remained.
- **Known limitations:** Evidence uses SELinux Permissive; with Enforcing the
  `pasta` helper is silently killed on the tested Fedora policy, and the harness
  now refuses that configuration explicitly. Host-originated virtiofs
  notifications need polling. virtiofs metadata overhead is high with
  conservative caching. Containers run as guest root; spike protocol, DNS fixture,
  and guest PID 1 are experiment-only. QEMU RSS budget ≈2.5× Firecracker.
- **Next task:** T03 unblocks T06 (guest protocol, with T04) and later T09/T10/T11.
  T07/T08 remain the next runtime tasks after T06.

## T06 guest protocol and testable client — 2026-10-03

- **Task:** T06 — guest protocol and testable client
- **Status:** DONE
- **Dependencies verified:** T03 (chosen AF_VSOCK/vhost-vsock transport) and T04
  complete; `make check` PASS.
- **Files and contracts changed:** `internal/guestproto` (frames and bounds in
  `frame.go`/`codec.go`, typed messages in `message.go`, handshake in
  `handshake.go`, host client in `client.go`, guest server in `server.go`,
  transport contract in `transport.go`, AF_VSOCK adapter in `vsock_linux.go`) and
  `api/guest-protocol.md`. No runtime, VMM, or guest agent code was added; the
  throwaway T01/T02 spike protocol is untouched and remains experiment-only.
- **Decisions/ADRs:** None required. The framing builds on specification §6.4
  (1 MiB control messages, 64 KiB data frames, length validated before
  allocation) and the T03 transport choice.
- **Tests run:** `make check` PASS; `go test -race -count=1 ./internal/...` PASS;
  `go test -race -count=5 ./internal/guestproto` PASS; `FuzzReadFrame` and
  `FuzzReadMessage` (8 s each, ~2.5M/2.5M executions) PASS with no crash;
  `TestVsockLoopback` PASS on the host AF_VSOCK loopback device. Coverage
  includes frame round trips, short reads/writes (partial I/O), oversized and
  unknown frames rejected before allocation, writer backpressure with a
  non-reading peer (timeout, bounded memory), handshake success, version
  mismatch (server and unit), failed authentication with a wrong per-boot key,
  sandbox mismatch, ping/probe/status/start/stop round trips, streamed exec
  with stdout/stderr/exit, typed error propagation, client timeout, and
  server-side cancellation via a `cancel` message.
- **Tests NOT run and why:** No end-to-end host-to-guest run; that needs the T07
  agent and the T08 backend, and no guest binary speaks this protocol yet. The
  AF_VSOCK adapter was exercised on the local loopback device, not vhost-vsock.
  Hosted CI has not run.
- **Integration evidence:** Repository-local plus an AF_VSOCK loopback test; the
  vsock test skips (never passes) when the host forbids AF_VSOCK.
- **Known limitations:** One in-flight request per connection on the client; the
  server is concurrent but multi-connection session multiplexing and reconnect
  without duplicate starts are deferred to T08. `stdin` frames are not routed
  yet (T07). The per-boot key delivery mechanism is specified, not implemented.
- **Next task:** T07 (guest PID 1 and OCI runtime) depends on T06 and T03.

## T07 guest PID 1 and OCI runtime — 2026-10-03

- **Task:** T07 — guest PID 1 and OCI runtime
- **Status:** DONE
- **Dependencies verified:** T06 complete; the T03 QEMU/vhost-vsock kernel and
  transport are available; real `/dev/kvm` present.
- **Files and contracts changed:** `internal/guest` (`oci.go` OCI bundle and
  config generation, `reaper_linux.go` single-owner `wait4` reaping,
  `runtime_linux.go` runc adapter, `agent_linux.go` lifecycle handler,
  `mounts_linux.go`, `cgroup_linux.go`, `proc_linux.go` zombie count,
  `manifest.go`), `cmd/grillo-agent` (real PID 1 replacing the T00 scaffold),
  `internal/guestproto` (`SandboxSpec`/`ContainerSpec`, `StartRequest.Sandbox`,
  `ExecRequest.Container`, `ProbeRequest.Container`, `StatusResult.Zombies`),
  `guest/README.md`, `guest/build-image.sh`, `guest/manifest`, and the
  `t07-guest` / `test-t07` Makefile targets.
- **Decisions/ADRs:** None required. The OCI config subset deliberately avoids
  adding `runtime-spec` as a dependency (not in the plan's allowed module list);
  the cgroup helper mirrors the enforced subset without importing the protocol.
- **Tests run:** `make check` PASS (fmt, vet, test, race, scripts, build,
  audit); `go test -race -count=5 ./internal/guest` PASS; `make test-t07` PASS
  (real KVM/QEMU, two independent boots). `TestKVMAgentScenarioA` boots the real
  image and verifies an init container writes `/shared/ready` visible to the app,
  the sidecar reaches the app over shared localhost (`wget
  http://127.0.0.1:8080/`), roots stay separate (an app-created marker is absent
  in the sidecar), the app and sidecar have distinct non-zero PIDs, the guest
  reports zero zombies, and stop leaves no running container.
  `TestKVMAgentInitFailure` verifies a failing init container blocks application
  containers. Unit coverage also includes OCI config generation, sandbox and
  container validation, bundle permissions, the reaper against real host
  processes, the artifact manifest, and the agent lifecycle over the real T06
  protocol with a scripted runtime.
- **Tests NOT run and why:** TTY, resize, stdin, live (incremental) log
  streaming, and signal forwarding are not implemented (T13). No multi-connection
  session multiplexing. Hosted CI has not run; no cold-cache benchmark.
- **Integration evidence:** Real KVM/QEMU boot of the built initramfs containing
  the agent, static runc, and busybox root filesystems; scenario A and the
  init-failure case asserted through the production T06 client over AF_VSOCK. The
  artifact manifest records SHA-256 and size for the agent, runc, kernel, and
  initramfs.
- **Known limitations:** One in-flight request per connection; exec and run
  output is captured to files and sent after completion (no live streaming);
  container rootfs materialization is T09; probes run in the guest or target
  container without the readiness/liveness model (T13); shutdown uses
  `reboot(LINUX_REBOOT_CMD_RESTART)` per ADR 0003; the per-boot key is baked into
  the experiment initramfs and production must deliver it per boot over a private
  channel.
- **Next task:** T08 (backend lifecycle) depends on T05/T07; T09 depends on T04.

## T08 backend lifecycle and process supervision — 2026-10-03

- **Task:** T08 — backend lifecycle and process supervision
- **Status:** DONE
- **Dependencies verified:** T05 (state) and T07 (guest agent) complete; real
  `/dev/kvm`, `/dev/vhost-vsock`, qemu 10.2.2, and virtiofsd 1.14.0 present.
- **Files and contracts changed:** `internal/sandbox` (VMM-independent
  `Backend`/`Spec`/`Handle`/`Observation`/`Capabilities` contract),
  `internal/platform/linux` (`Identify`/`Alive`/`Signal`/`SignalGroup` with
  boot-ID + start-time + executable identity), `internal/backend/qemu` (config
  and QEMU/virtiofsd argument rendering, process supervision, persisted sandbox
  state, and the full lifecycle), and the `test-t08` Makefile target.
- **Decisions/ADRs:** Implements ADR 0005 (QEMU microvm + virtiofsd). No new ADR.
- **Tests run:** `make check` PASS (fmt, vet, test, race, scripts, build,
  audit); `go test -race -count=3 ./internal/backend/... ./internal/platform/...`
  PASS; `make test-t08` PASS. `TestKVMCreateStartStopDelete` booted the real T07
  guest through the backend **100 times in 1m25.8s (858 ms/cycle)**, verifying
  `Inspect` returns running with a live guest and zero zombies, repeated `Start`
  does not duplicate resources, `Delete` removes the sandbox, no VMM process
  leaks (identity-checked), and the work directory is empty afterwards. Unit
  tests cover idempotent `Create` by operation ID, `ErrConflict` on ID reuse,
  absent `Stop`/`Delete` success, handshake-failure cleanup (VMM killed), reopen
  loading persisted state, stale-identity kill refusal, capability probing, and
  100 fake-VMM cycles with an empty work dir.
- **Tests NOT run and why:** No virtiofsd share or disk attach is exercised in
  the KVM cycle (the guest agent does not mount them yet; live sharing is T10/T11
  and was verified in the T03 F0 gate). Networking is a pre-rendered `netdev`
  field owned by T11. Hosted CI has not run.
- **Integration evidence:** Real QEMU microVM boots and AF_VSOCK handshakes
  across 100 create/start/stop/delete cycles with no leaked processes or
  directories.
- **Known limitations:** `Stop` sends SIGTERM to the VMM process group after a
  best-effort guest container stop; there is no ACPI/serial shutdown request to
  the guest yet (T11/T14). Dependency cycles and guest status details remain
  T12/T13. The backend serializes operations per sandbox and holds per-sandbox
  locks; global concurrency/limits are T14.
- **Next task:** T09 (OCI registry and CAS) depends on T04 and T03; T11
  (networking) and T10 (storage) depend on T08.

## T09 OCI registry and CAS — 2026-10-03

- **Task:** T09 — OCI registry and CAS
- **Status:** DONE
- **Dependencies verified:** T04 (IR) complete; T03 provided the rootfs
  materialization path; the pure registry/CAS work has no host prerequisites.
- **Files and contracts changed:** `internal/oci` (`reference.go`
  normalization, `manifest.go` schema2/OCI manifests and indexes,
  `registry.go` Distribution API subset with auth/limits, `cas.go`
  content-addressed store, `unpack.go` safe extraction, `config.go` image-config
  merge, `pull.go` orchestration). No VMM, guest, or IR imports.
- **Decisions/ADRs:** None required. The OCI module list is unchanged; the code
  uses only the standard library.
- **Tests run:** `make check` PASS (fmt, vet, test, race, scripts, build, audit);
  `go test -race -count=3 ./internal/oci` PASS (32 test cases). Coverage:
  reference normalization and invalid inputs; OCI and Docker schema2 manifests;
  index platform selection; schema1 and zstd/unimplemented-layer rejection;
  invalid config descriptors; gzip layer unpack; whiteouts and opaque
  directories; setuid clearing; absolute paths, `..`, devices, FIFOs, escaping
  hardlinks, and symlink-parent traversal all refused; `DiffID`; image-config
  merge (entrypoint/cmd/env/user/workdir/stop-signal) with overrides;
  CAS commit/open, digest mismatch, size mismatch, byte-limit, collect, and
  **16 concurrent `Fetch` calls deduplicating to one download**; registry bearer
  auth (in-process `httptest` registry), token failure, manifest size limit,
  cross-host redirect stripping `Authorization`, corrupt blob rejection, and
  `diff_id` mismatch rejection; an end-to-end pull+unpack.
- **Tests NOT run and why:** No real network registry (Docker Hub / GHCR) and no
  credential-helper execution; credential helpers are opt-in and not implemented
  yet. Unpacking maps owners through `MapOwner` and ignores `EPERM` from
  `Lchown`, retaining metadata for guest-side materialization as the plan
  requires; that strategy is verified only for the current-user case. Hosted CI
  has not run.
- **Integration evidence:** Repository-local, with local `httptest` registries
  serving real tar/config/manifest fixtures; no live registry contacted.
- **Known limitations:** `plan`/lockfile integration, pull policies, image GC
  pins, and credential config parsing are T14/T15. Rootless arbitrary-UID
  materialization is recorded but not yet projected into the guest by T10.
- **Next task:** T10 (storage) depends on T05/T08/T09; T11 (networking) on
  T05/T08.

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
