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

**T09 is complete.** `internal/oci` implements reference parsing, the OCI Distribution API subset with bearer/basic auth and bounded HTTP, schema2/OCI manifest and index handling with `linux/amd64` selection, a content-addressed blob store with atomic verified commits and concurrent-pull deduplication, safe gzip/tar layer unpacking with whiteouts and opaque directories, image-config merging, and `diff_id` verification. Adversarial tars and corrupt blobs are rejected. A `netreg`-tagged test pulls a digest-pinned busybox from Docker Hub, verifies the CAS, and unpacks it (network failure is a documented SKIP).

**T10 is complete.** `internal/storage` manages volumes with validated bind paths, managed/PVC/ephemeral lifecycles, leases with owner/operation records, access modes, explicit UID/GID mapping, crash-safe lease reconciliation, and deletion that never touches bind or external data. The guest agent mounts virtiofs and block shares before starting containers, and `make test-t10` passes acceptance scenario G on real KVM: live bind sharing both ways and enforced read-only. A KVM test also attaches a raw ext4 image as `virtio-blk-device` and persists a file across two sandboxes (the same-disk attachment is exclusive).

**T11 is complete.** `internal/network` provides IPAM with persistent owner-bound leases and reconciliation, host loopback port reservations with privileged-port and collision rejection, application isolation policy, a rootless publishing proxy, and a supervised pasta helper that runs a child in a fresh user+network namespace. `make test-netns` verifies egress, distinct namespaces, mutually isolated same-port binds, and host-loopback management unreachability with real pasta, and asserts each sandbox receives its own IPAM address via `pasta -a`. The production backend now runs each application in a dedicated `internal/netns` supervisor: one pasta user+network namespace, a bridge with the application gateway, one TAP per sandbox, and nftables forwarding with masqueraded egress. `make test-bridged` verifies cross-VM reachability, real DNS addresses, guest egress, and application isolation on real KVM.

**T12 is complete.** `internal/network` adds an application DNS resolver and UDP/TCP server (all four Kubernetes service names resolve, NXDOMAIN for in-zone misses, REFUSED rather than acting as an open resolver), a Service registry with a dedicated VIP pool, a readiness-aware round-robin balancer and TCP service proxy, and an Ingress matcher/reverse proxy with segment-aware Prefix, Exact, sanitized forwarded headers, and bounded backend timeouts. `golang.org/x/net` (dnsmessage) is now a pinned dependency. The resolver runs inside the guest: the sandbox spec carries service records, the agent serves them on 127.0.0.1:53 and writes `/etc/resolv.conf`, every container bind-mounts it, and the four Kubernetes names resolve end to end on real KVM. With the production bridge, service records carry the real per-replica sandbox addresses, so replicas resolve and reach each other across VMs.

**T13 is complete.** `internal/observe` provides a sequenced event stream (reusing the state journal's rotation and sequence IDs) with `Follow` and gap detection, a bounded log spool with follow, `/proc`-based resource snapshots with CPU deltas, and exec/HTTP/TCP probes with startup/readiness/liveness roles, thresholds, injected-clock scheduling, and nonoverlapping ticks. A bounded exit watcher emits an event only on container state change. The executor wires the probes and restarts a container whose liveness probe fails, verified on real KVM by a PID change.

**T14 is complete.** `internal/plan` diffs a desired IR against observed state into an ordered, typed action list (create/recreate/scale/drain/stop/delete, volume preparation, endpoint updates) with per-template hashing so identical and route-only applies reboot nothing and Jobs do not restart forever. `internal/reconcile` applies plans with deterministic operation IDs, bounded per-application workers, classified permanent/transient retries, partial-progress persistence, crash-replay, idempotent `down`, and a `NativeExecutor` dispatcher over sandbox/volume controllers. `internal/executor` implements those controllers against the QEMU backend, storage manager, and guest agent, so a native manifest applies end to end; `make test-executor` boots a sandbox, starts a container, reports status, execs a command, and tears down on real KVM.

**T15 is complete.** `internal/api` serves the local Unix-socket API with `SO_PEERCRED` UID checks, bounded bodies, the `{code,message,resource,retryable,details}` error DTO, asynchronous operations on the daemon lifetime context, SSE events with `Last-Event-ID` and gap records, log streaming, and distinct daemon-shutdown/application-down endpoints. `cmd/grillod` holds the single-instance state lock, builds the QEMU backend, storage manager, and `internal/executor`, and serves status and exec endpoints.

**T16 is complete.** `internal/cli` implements the native-runtime command line with a parser for flags before/after positionals, repeated `-f`, and the exec `--` terminator; offline `plan`; `up` (daemon autostart + async apply), `down`, `status`, `inspect`, `logs`, `events`, `exec` against a live container, and a read-only `doctor` that never requires root. `cmd/grillo` is wired to it. It also provides `ps`, `restart`, and a `shell` that streams `/bin/sh`, and a `ui` command that serves a secure loopback web console (`internal/ui`) with a bootstrap token exchanged for an HttpOnly cookie, Host/Origin checks, a strict CSP, and text-node rendering.

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
| T09 | OCI registry and CAS | DONE | `internal/oci`; unit + adversarial + concurrent-dedup tests, and `make test-netreg` live digest-pinned pull PASS |
| T10 | Storage manager | DONE | `internal/storage`; `make test-t10` (scenario G) and a `virtio-blk` ext4 persistence test PASS on real KVM |
| T11 | Production rootless networking and IPAM | DONE | `internal/network` + `internal/netns`; `make test-netns` and `make test-bridged` PASS (egress, unique addresses, isolation, cross-VM, host-loopback denied) |
| T12 | DNS, Service proxy, and Ingress | DONE | `internal/network` + in-guest resolver with real replica addresses; cross-VM resolve/reach, balancing, readiness, timeout, path-segment tests |
| T13 | Observability and probes | DONE | `internal/observe` + executor probe wiring; startup gating, thresholds, fake-clock, timeout-kill, liveness-restart KVM test |
| T14 | Planner, reconciler, and updates | DONE | `internal/plan` + `internal/reconcile` + `internal/executor`; diff/recreate/route-only, retries, crash replay, idempotent down, bounded workers, `make test-executor` end-to-end on KVM |
| T15 | Local API and daemon lifetime | DONE | `internal/api` + `cmd/grillod`; peer UID, async ops, status/exec endpoints, SSE cursors, disconnect/leak tests |
| T16 | Native-runtime CLI | DONE | `internal/cli` + `cmd/grillo`; plan, up/down/status/inspect/ps/restart/logs/events/exec/shell/ui, terminal restore, read-only doctor |
| T17 | Compose parser and compiler | TODO | T04; final integration T16 |
| T18 | Builder and image/volume/network tooling | TODO | T09, T10, T11, T17 |
| T19 | F2 gate: Compose application | TODO | T12–T18 |
| T20 | Kubernetes MVP compiler | TODO | T04; runtime verification T14 |
| T21 | Helm rendering and OCI charts | TODO | T20 |
| T22 | Helm gate and compatibility reporting | TODO | T19, T21 |
| T23 | Web console and secure bridge | IN_PROGRESS | Bridge + minimal console in `internal/ui` and `grillo ui`; full views pending |
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
- **Tests NOT run and why:** No credential-helper execution; credential helpers
  are opt-in and not implemented yet. Unpacking maps owners through `MapOwner`
  and ignores `EPERM` from `Lchown`, retaining metadata for guest-side
  materialization as the plan requires; that strategy is verified only for the
  current-user case. Hosted CI has not run.
- **Integration evidence:** Local `httptest` registries serve real
  tar/config/manifest fixtures, and `make test-netreg` pulls and unpacks a real
  digest-pinned image from Docker Hub (network failure is a documented SKIP).
- **Known limitations:** `plan`/lockfile integration, pull policies, image GC
  pins, and credential config parsing are T14/T15. Rootless arbitrary-UID
  materialization is recorded but not yet projected into the guest by T10.
- **Next task:** T10 (storage) depends on T05/T08/T09; T11 (networking) on
  T05/T08.

## T10 storage manager — 2026-10-03

- **Task:** T10 — storage manager
- **Status:** DONE
- **Dependencies verified:** T05 (state), T08 (backend), T09 (OCI) complete;
  real `/dev/kvm`, qemu, and virtiofsd present.
- **Files and contracts changed:** `internal/storage` (volume model, manager,
  bind validation, leases, access modes, UID/GID mapping, reconciliation,
  safe deletion); `internal/guestproto` (`ShareSpec` and `SandboxSpec.Shares`);
  `internal/guest` (`MountShares`, called before containers start); `test-t10`
  Makefile target.
- **Decisions/ADRs:** None required. Bind paths are canonicalized with
  `EvalSymlinks`; `/` and `$HOME` are refused as bind sources.
- **Tests run:** `make check` PASS; `go test -race -count=2 ./internal/storage
  ./internal/guest ./internal/guestproto` PASS; `make test-t10` PASS (real
  KVM/QEMU). `TestKVMBindPersistence` boots the backend with a virtiofs bind
  share and asserts, through the production protocol: the guest reads a host
  sentinel, a host write after mount is visible in the guest (a live bind, not an
  initial copy), a guest write appears on the host, and a read-only share rejects
  a write both in the guest and on the host. Unit tests cover managed-volume
  persistence across reopen, idempotent create, RWO/ROX/RWX access modes,
  read-only enforcement, attachment UID/GID mapping, detach/release, lease
  reconciliation for gone sandboxes, `down --volumes` skipping bind and foreign
  volumes, and deletion refusing bind and leased volumes.
- **Tests NOT run and why:** No multi-Pod shared-volume rejection beyond the
  access-mode checks, and no `e2fsck` after the block test. Hosted CI has not run.
- **Integration evidence:** Real QEMU microVMs with virtiofsd and virtio-blk
  shares: bidirectional bind sharing, read-only denial, and a raw ext4 image
  persisting a file across two sandboxes verified end to end.
- **Known limitations:** PVC capacity and storage class are recorded but not
  quota-enforced on the host; `subPath` is not implemented; GC/pins and
  `inspect` projection into the API are later tasks.
- **Next task:** T11 (networking) depends on T05/T08; T12 (DNS) on T11/T04.

## T11 production rootless networking and IPAM — 2026-10-03

- **Task:** T11 — production rootless networking and IPAM
- **Status:** DONE
- **Dependencies verified:** T05 (state) and T08 (backend) complete; pasta,
  user namespaces, and a non-loopback address available; SELinux Permissive.
- **Files and contracts changed:** `internal/network` (IPAM leases, port
  reservations, isolation policy, loopback publishing proxy, and the pasta
  helper) and the `test-netns` Makefile target.
- **Decisions/ADRs:** None required. Rootless egress uses pasta with inbound
  forwarding disabled; publishing is a host loopback TCP proxy owned by T12's
  routing; multiple logical networks per sandbox are rejected rather than
  flattened.
- **Tests run:** `make check` PASS; `go test -race -count=2 ./internal/network`
  PASS; `make test-netns` PASS (real pasta, ~4 s). Coverage: address allocation
  and reuse, `/30` exhaustion, lease persistence across reopen, reconciliation
  of gone sandboxes; port reservation idempotency, collision (`ErrPortInUse`),
  privileged-port rejection with a remap suggestion, non-loopback rejection,
  detection of an already-bound host port, release, and reconciliation; isolation
  policy; loopback proxying; and the namespace integration test, which starts
  two concurrent pasta helpers and asserts distinct netns inodes, working guest
  egress, denial of a host canary endpoint, and same-port bind isolation.
- **Tests NOT run and why:** Guest DNS, Service VIPs/proxies, and Ingress are T12.
  The publishing proxy forwards to a target address but is not yet wired to a
  guest through a relay; the T03 F0 gate covered the full host→guest publish
  path in the experiment harness. No multi-VM TAP-in-namespace test here.
- **Integration evidence:** Real pasta helpers under UID 1000 with user
  namespaces; kernel netns inodes differ from the host and from each other;
  same loopback port binds in both; egress to `1.1.1.1:443` succeeds; a host
  canary listener is unreachable from inside.
- **Known limitations:** Per-port egress allow/deny policy is a blanket forward
  to the uplink (no per-destination policy yet); TLS and hostname management are
  out of scope.
- **Next task:** T12 (DNS, Service proxy, Ingress) depends on T11/T04.

## T12 DNS, Service proxy, and Ingress — 2026-10-03

- **Task:** T12 — DNS, Service proxy, and Ingress
- **Status:** DONE
- **Dependencies verified:** T11 (rootless namespaces, IPAM) and T04 (IR)
  complete.
- **Files and contracts changed:** `internal/network` (`dns.go`, `service.go`,
  `ingress.go`) and `go.mod`/`go.sum` (adds the allowed module
  `golang.org/x/net v0.59.0` for `dns/dnsmessage`).
- **Decisions/ADRs:** None required. The resolver is authoritative for the
  application zone and returns REFUSED for external names rather than acting as
  an open resolver; a TCP proxy cannot serve UDP, so UDP Services are rejected
  with `ErrUnsupported`.
- **Tests run:** `make check` PASS (fmt, vet, test, race, scripts, build, audit,
  module tidy diff); `go test -race -count=2 ./internal/network` PASS. Coverage:
  all four service names (`postgres`, `postgres.default`,
  `postgres.default.svc`, `postgres.default.svc.cluster.local`) resolve an A
  record over both UDP and TCP; in-zone misses return NXDOMAIN and external
  names return REFUSED; SRV records resolve; service removal; VIP allocation in a
  separate pool; round-robin balancing across three ready backends with an
  unready backend receiving zero connections; UDP Service rejection; balancing
  with no ready endpoints; Ingress Exact-vs-Prefix precedence, segment-aware
  prefix (`/api` not matching `/apix`), host-port stripping, and no-match cases;
  reverse-proxy forwarding; client-supplied `X-Forwarded-For` being overwritten;
  a slow backend returning 502 within the configured response-header timeout;
  and no-match/unavailable status codes.
- **Tests NOT run and why:** TCP Service load balancing is exposed as a proxy
  and DNS; the VIP is not yet programmed as a guest interface alias. External
  forwarding is not implemented (REFUSED), and IPv6/AAAA, headless Services
  beyond A records, and wildcard Ingress hosts are out of scope.
- **Integration evidence:** Real UDP and TCP DNS servers exercised with the
  `dnsmessage` client; the in-guest resolver answers the four Kubernetes names on
  real KVM (`make test-executor`); real TCP backends and a real reverse proxy with an
  `httptest` origin.
- **Known limitations:** UDP Services are explicitly unsupported by the TCP
  proxy. VIPs are allocated but not yet attached to sandbox interfaces. Localhost
  Ingress fallback and TLS are T14. No SRV `_proto` variants beyond TCP.
- **Next task:** T13 (observability and probes) depends on T05/T07/T12.

## T13 observability and probes — 2026-10-03

- **Task:** T13 — observability and probes
- **Status:** DONE
- **Dependencies verified:** T05 (state journal), T07 (guest agent), T12
  (Service/probe context) complete.
- **Files and contracts changed:** `internal/observe` (`events.go`,
  `spool.go`, `metrics.go`, `probe.go`, `guest_prober.go`, `exitwatch.go`) and
  `internal/state` (optional `Source`/`Reason`/`Fields` on journal events).
- **Decisions/ADRs:** None required. Events reuse the state journal so sequence
  IDs, rotation, and truncation recovery are shared; probes are in
  `internal/observe`; exec probes for containers delegate to the guest agent
  (`GuestProber`) so they run in the right network context.
- **Tests run:** `make check` PASS (fmt, vet, test, race, scripts, build,
  audit); `go test -race -count=2 ./internal/observe ./internal/state` PASS.
  Coverage: emit/list/filter/follow and gap detection; log append/list/follow
  and line truncation; `/proc` sampling and CPU deltas; startup gating readiness
  and liveness; success/failure thresholds; startup failure; injected-clock
  ticks; nonoverlapping scheduling (one probe per tick); direct exec probe
  timeout killing its child (verified with `kill(pid, 0)`); HTTP and TCP probes;
  liveness callback firing only for the intended container; readiness changing
  state without restart; a slow follower not blocking the producer; an event
  carrying a source mapping; and the exit watcher emitting once per transition.
- **Tests NOT run and why:** Metrics are per-process via `/proc`, not cgroup
  aggregation; probes are wired through the executor rather than the reconciler.
  Hosted CI has not run.
- **Integration evidence:** The direct exec timeout test uses a real process tree
  and confirms no surviving child; `make test-executor` runs a failing liveness
  probe on real KVM and asserts the container PID changes.
- **Known limitations:** CPU accounting assumes `USER_HZ=100`; source mapping is
  a passthrough string populated by callers from IR locations; `Follow` polls the
  journal, so there is a bounded delivery latency. Metric gaps are reported by
  omission (no zero snapshots), not yet by an explicit unavailable reason.
- **Next task:** T14 (daemon, reconcile, API) depends on several tasks; T15/T16
  and the pure frontends T17/T20 remain.

## T14 planner, reconciler, and updates — 2026-10-03

- **Task:** T14 — planner, reconciler, and updates
- **Status:** DONE
- **Dependencies verified:** T08–T13 complete.
- **Files and contracts changed:** `internal/plan` (typed action list, template
  and route hashing, diff/build, restart policy), `internal/reconcile`
  (`Executor`/`Store` contracts, memory store, retry classification, operation
  IDs, `Apply`/`Down`/`ApplyAll`, `NativeExecutor`), and `internal/executor`
  (IR to `sandbox.Spec`/guest `SandboxSpec`, container/volume/agent mapping, and
  `SandboxController`/`VolumeController` adapters).
- **Decisions/ADRs:** None required. Plans are an ordered linearization of the
  typed action DAG. Operation IDs are deterministic (`sha256` of application,
  revision, action kind, and resource) so replayed actions deduplicate.
- **Tests run:** `make check` PASS; `go test -race -count=3 ./internal/plan
  ./internal/reconcile` PASS; `make test-executor` PASS (real KVM). Coverage
  includes the unit cases above plus the executor's IR mapping (user, resources,
  env/config resolution, volume mounts, argument merge) and the end-to-end KVM
  apply: the executor boots a sandbox with a virtiofs rootfs, starts the
  container, reports it running, execs a command, and `down` tears it down.
- **Tests NOT run and why:** No rollback on failed replacement is tested (desired
  state stays new and the failure is reported). Networking/endpoint attachment
  remains a separate task, so `UpdateEndpoints` is a no-op here.
- **Integration evidence:** Real KVM/QEMU end-to-end apply through the planner,
  reconciler, executor, backend, storage, and guest agent (`make test-executor`).
- **Known limitations:** No automatic rollback on failed replacement (desired
  state stays new and the failure is reported); endpoint/route wiring is a no-op
  pending network attachment.
- **Next task:** T15 (local API and daemon lifetime) depends on T14.

## T15 local API and daemon lifetime — 2026-10-03

- **Task:** T15 — local API and daemon lifetime
- **Status:** DONE
- **Dependencies verified:** T14 (planner/reconciler) complete.
- **Files and contracts changed:** `internal/api` (`api.go` server/DTOs/ops,
  `listener_linux.go` peer-UID Unix listener, `client.go` + daemon bootstrap),
  `cmd/grillod` (foreground daemon), and `api/local-api.md`.
- **Decisions/ADRs:** None required. Operations derive from the daemon lifetime
  context so client disconnects do not cancel accepted work; the daemon `shutdown`
  endpoint is intentionally separate from application `down`.
- **Tests run:** `make check` PASS; `go test -race -count=2 ./internal/api` PASS.
  Coverage: version/health; malformed JSON returns 400 and the server keeps
  serving; oversized bodies rejected; an apply accepted before the client
  disconnects still succeeds; explicit cancel transitions to `canceled`; a peer
  from an unexpected UID is rejected; SSE delivers a sequenced event; status and
  exec endpoints; concurrent clients; and closing twenty event streams does not
  grow goroutines (session-leak check).
- **Tests NOT run and why:** `EnsureDaemon` autostart is exercised only for the
  already-healthy path; spawning the real `grillod` binary from a test is not
  done. Hosted CI has not run.
- **Integration evidence:** Real Unix-socket HTTP over a peer-credential-checked
  listener; the daemon builds the QEMU backend, storage manager, and executor,
  which is exercised end to end by `make test-executor`.
- **Known limitations:** No request-level rate limiting beyond body bounds; no
  application list endpoint yet; operations are in-memory and not recovered
  across a daemon restart (the reconciler replays from desired state instead).
  Exec captures captured output rather than a framed stream.
- **Next task:** T16 (native-runtime CLI) depends on T15.

## T16 native-runtime CLI — 2026-10-03

- **Task:** T16 — native-runtime CLI
- **Status:** DONE
- **Dependencies verified:** T15 (API/daemon) complete.
- **Files and contracts changed:** `internal/cli` (`flags.go` interspersed parser,
  `cli.go` commands, `doctor.go`, `terminal.go`), `cmd/grillo` wired to the CLI,
  and `go.mod`/`go.sum` (pins `golang.org/x/term v0.46.0`).
- **Decisions/ADRs:** None required. Uses the standard `flag` package after a
  documented split that allows flags before and after positionals and honors
  `--`; exec puts the local terminal in raw mode and always restores it.
- **Tests run:** `make check` PASS; `go test -race -count=2 ./internal/cli` PASS.
  Coverage: interspersed and repeated `-f`; flags after positionals; the exec
  `--` terminator; unknown-flag and missing-value errors; offline `plan` prints
  actions and rejects invalid manifests; `up` sends the application and waits for
  the operation; `down --volumes`; `exec` runs against the daemon, returns the
  container exit code, and restores the terminal on failure; `status`/`inspect`;
  doctor exit codes and well-formed checks; version and unknown-command exit
  codes.
- **Tests NOT run and why:** Image/volume/network inventory is not implemented;
  `shell` streams `/bin/sh` without a full TTY (TTY is optional per the plan);
  daemon autostart is not integration-tested by spawning the binary. Hosted CI
  has not run.
- **Integration evidence:** The CLI drives the real API client types; `doctor`
  probes the real host read-only; end-to-end container execution is covered by
  `make test-executor` at the executor layer.
- **Known limitations:** No `--output=json` on every command, no resource-notation
  resolution (`deployment/backend`), and no interactive TTY stream yet.
- **Next task:** T17 (Compose parser and compiler) depends on T04; T20
  (Kubernetes) depends on T04.

## T23 web console and secure bridge (in progress) - 2026-10-03

- **Task:** T23 - web console and secure bridge
- **Status:** IN_PROGRESS (bridge and a minimal console delivered)
- **Dependencies verified:** T15 (API) complete; full T23 also lists T22.
- **Files and contracts changed:** `internal/ui` (embedded `index.html`/`app.js`,
  bootstrap-token session exchange, peer-independent loopback Host check,
  Origin check on mutations, strict CSP, no CDN, text-node rendering) and the
  `grillo ui` command in `internal/cli`.
- **Decisions/ADRs:** None required. The console binds loopback only, requires an
  HttpOnly SameSite session cookie obtained from a one-time bootstrap token, and
  proxies data from the daemon API; it is never a host file server.
- **Tests run:** `make check` PASS; `go test -race ./internal/ui ./internal/cli`
  PASS. Coverage: public index with a strict CSP; data endpoints require a
  session; a wrong bootstrap token is rejected; the correct token sets an
  HttpOnly cookie; an authorized status request succeeds; a foreign Host header
  is rejected; and the events proxy streams with a valid session. The CLI `ui`
  command serves until its context is canceled.
- **Tests NOT run and why:** Full console views (logs, metrics, mounts, routes,
  topology), TLS, and explicit secret reveal are not implemented. Hosted CI has
  not run.
- **Integration evidence:** Repository-local HTTP over `httptest` with the real
  handler; the UI reads the real API client types through the bridge's `Core`.
- **Known limitations:** Minimal single-page status/events view; no logs/metrics
  panes, no topology SVG, no local TLS or dev CA.
- **Next task:** Complete T23 views and wire the browser console to the running
  bridge; T17/T20 frontends remain.

## Production rootless network topology - 2026-10-03

- **Task:** close the remaining T11/T12 networking limitation (Service/VIP
  datapath and cross-VM routing).
- **Status:** DONE
- **Files and contracts changed:** `internal/netns` (supervisor + client:
  bridge, gateway, per-sandbox TAP, nftables forwarding/masquerade, launch
  protocol), `cmd/grillo-netns` (supervisor entry point), `internal/backend/qemu`
  (optional `Config.Launch` to start the VMM through the supervisor, external
  process tracking), `internal/executor` (per-application supervisor, IPAM
  addresses, `NetworkConfig`, real service DNS addresses, `SandboxInfo`/`ExecRuntime`),
  `internal/guestproto` (`NetworkConfig`), and `internal/guest` (agent configures
  the interface with busybox `ip`).
- **Decisions/ADRs:** Implements the T03 F0 topology in production: one pasta
  namespace per application, a bridge with the application gateway, one TAP per
  sandbox, `virtual-net-device`, and nftables. Depends on ADR 0005.
- **Tests run:** `make check` PASS; `make test-bridged` PASS (real KVM + pasta):
  two replicas get distinct IPAM addresses, one fetches the other's page across
  the bridge, DNS returns both real addresses, the guest dials `1.1.1.1:443` via
  the agent probe (egress), and a second application at the same address cannot
  reach the first (isolation). `make test-t07`, `make test-executor`,
  `make test-netns`, `make test-netreg`, and the block-device test all PASS.
- **Tests NOT run and why:** No per-destination egress policy test beyond the
  blanket uplink forward; no VIP interface-alias test (services are reached via
  DNS/proxy). Hosted CI has not run.
- **Integration evidence:** Real QEMU microVMs inside a pasta namespace, on a
  bridge, doing cross-VM HTTP and DNS resolution with IPAM addresses.
- **Known limitations:** The bridge subnet is fixed at `10.77.0.0/24` per
  application (isolated namespaces); egress is a blanket uplink forward; TAP
  setup uses `ip`/`nft` binaries; the guest interface is configured by the agent,
  not the kernel.
- **Next task:** T17 (Compose parser and compiler).

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
