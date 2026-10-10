# Implementation history

Archived on 2026-10-06 during documentation consolidation. Entries and the
old status snapshot are preserved as historical records, including failures and
superseded summaries. They are not current support claims; use
[current progress](../progress.md) and [compatibility](../compatibility.md).


## Archived T24 supplied hosted-CI log review

BLOCKED; [supplied archive review](../experiments/t24-ci-log-review.md) identifies
55be94c Ubuntu unit/build make check PASS but upstream Go 1.26.8 advisory FAIL,
ten called-symbol stdlib advisories fixed in 1.26.9; subsequent hosted fuzz not
executed. Bounded inspection/hash outside repo, no archived credentials/raw logs
tracked; user-supplied bytes not independent authenticated run provenance.
Actual local vendor-version make vulncheck exit 0 vs explicit upstream 1.26.8
baseline Make exit 2 reproduced the discrepancy. Installed scanner rejects the
Fedora `-X:nodwarf5` suffix; stdlib-clearance conclusion withdrawn, historical
output preserved. Security/progress/evidence corrections, no pins/system/tools/
processes changed in that log-review step. Docs/diff PASS. No fixed toolchain
install/rebuild/browser/KVM/remote hosted rerun in that step; next was deliberate
fixed-source upgrade and fail-closed coverage regressions. Older live-stack
availability was not reverified during log review. T24/D0 remain BLOCKED.

## Archived T23 live full-stack review setup

IN_PROGRESS for live user review; actual local build/HTTP/guest-network checks PASS,
intentionally left running. Native Compose/DNS/publication/verified guest/T23
contracts verified; new `examples/ui-stack` static UI, bounded standard-library Go
notes API/tests and Nginx gateway, pinned native copy-only builds. No dependency/
policy/sudo/fallback or release waiver. Unique owned private XDG session/CID ranges,
no unrelated state recovery/cleanup. Backend tests / full make check PASS; three
native image builds, plan/up/status/snapshots, UI assets/health/API GET/POST/readback
and actual Nginx/UI guest exec to backend DNS/HTTP PASS. Three microVMs confirmed;
management console HTTP shell PASS. Initial missing ENTRYPOINT-derived command and
premature Nginx upstream DNS failures fixed with explicit commands/bounded guest
readiness wait and preserved in [evidence](../experiments/t23-full-stack-review.md).
New demo browser form not automated; HTTP assets/API are not browser proof. Down/
post-down refusal/daemon cleanup not performed because user requested it stay live;
no new default-policy/security/source/other-host/full-console gate. Three 512 MiB/
1-vCPU allocations are not measured aggregate usage. Null endpoint/expose DTO
finding remains, volatile unauthenticated demo and image/source/advisory review
pending; bootstrap credential not in tracked evidence. Next was live dashboard/
topology review and workload master/detail/scoped logs. T24/D0 BLOCKED overall.

## Archived T23 first visual/topology revision

Bounded local iteration DONE; user visual review next and T24/D0 BLOCKED. Verified
T15/T22/T23 public snapshot/session/exec/chart/PatternFly/security/lifetime contracts.
Reworked shared petrol/teal/mint shell, dashboard actual/unavailable guest memory
rings, bounded declaration-only topology + responsive Resources/Details inspector,
keyboard/filter/zoom/close-focus interactions and regressions. No new dependency,
API, copied upstream branding or invented capacity/traffic. Full make check /
10 Node tests PASS. Final real Firefox 9.07s and rebuilt CLI/daemon/KVM/UI gate
39.11s PASS without SKIP. Initial BiDi key encoding FAIL fixed, default-blue token/
stacked mobile header corrections preserved in [evidence](../experiments/t23-console-redesign.md).
Actual logs/metrics/exec/cancellation and UI-independent VMM lifetime retained on
local already-Permissive Fedora. No other-browser/CI/accessibility/cold-render/
external-host proof, prior transfer payloads not rebuilt. Other resource pages
retain functionality under shared styling; no drag/free-pan/full-browser TTY.
User then found missing libexec daemon from incomplete plain-bin review command;
corrected explicit checkout helper/guest overrides in UI guide, doctor preflight
PASS with expected first-run/support WARNs and targeted cached tests PASS. No
user-context daemon startup falsely inferred from those read-only checks.
Next: actual user review / workload master-detail / scoped logs, without waiving
remaining hardening/source/provenance gates. No commit/push in that UI iteration.

## Archived T24 payload notices/provenance/support delivery

Overall T24/D0 BLOCKED; bounded local Stage delivery PASS. Added mandatory verbatim
frontend/font notice copying with bounded/no-follow inputs, four copied-binary
Go metadata records, private replacement redaction, partial coverage status and
staged host support guide. Verified T23/Stage/runtime-only/installed contracts and
prior license/user-run Permissive evidence. No dependency/ADR/support waiver.
Full make check, real payload builds/archive digest inspection, 100 selected race
suites, audit/vulncheck PASS (Grillo graph only). Rebuilt installed KVM PASS without
SKIP, final 7.35s. Helm actually declares Go 1.26.4 / 106 dependencies; metadata
is not exact source/patch or publisher trust. Five notice texts ship, complete
Go/Helm/guest notice/source review remains pending. No new external/default-policy
or hosted CI evidence. User-confirmed UI on second Fedora host remains scoped
functional evidence, with cleanup/lifetime gaps preserved. Known Enforcing/quota/
performance/source/provenance gates remain. Next was prepared source/notice
collection and remaining hardening, not automatic release acceptance.
[Detailed report](../experiments/t24-payload-notices-provenance-support.md).
User then authorized commit `55be94c` of accumulated work; no push performed.

## Archived D0/T24 partial second-host evidence entry

Overall BLOCKED. User reports Fedora 44 Workstation/Linux 7.2.9-200.fc44 under
Permissive, installed Compose HTTP up/down/up, Helm Ingress HTTP/down and later UI
functionality confirmation. External functional PASS attributed to user, not coding
agent execution or full clean-host/default-policy support. Preserved first 401 and
endpoint/ps diagnostics; no timing or token-expiry recovery inferred. Process regex
contains qemu-system comma, so QEMU cleanup remains unproven; closing UI/terminal
lifetime, DNS, delayed retry, clean-target and byte/helper identities remain missing.
Docs-only link/fence/diff checks PASS; no Go/KVM/remote runs for this entry. UI
bootstrap credential omitted from all reports. [Sanitized record](../experiments/d0-second-host-fedora44.md).
Next was missing checks, separate presentation and continuing hardening/source/
provenance blockers; [payload notices/support delivery](../experiments/t24-payload-notices-provenance-support.md)
addresses a bounded part without clearing the gates.

## Archived D0/T24 transfer payload delivery

Overall BLOCKED; established Make/runtime-guest/Stage/Helm pin dependencies
verified. Added `make payload`, unique private output per invocation, archive and
relative verified checksum, owned-stage failure cleanup and offline script tests
in `make check`; no installer/transfer/implicit pin/daemon termination. Shell/full
checks PASS; two actual unique payloads and extracted CLI version/doctor PASS with
expected state/support warnings. No new wrapper-specific KVM/remote acceptance
claim; preceding installed-prefix KVM evidence remains separate. SIGKILL can retain
incomplete output; dirty dev identities/hashes are not release attestations. No
commit/publication. [Payload report](../experiments/d0-transfer-payload.md).
Next was transfer/new-daemon up/down/up and remaining external checks; subsequent
[user-submitted Fedora 44 observations](../experiments/d0-second-host-fedora44.md)
record partial progress without closing the gate.

## Archived T24 registry rejected-token delivery

T24 BLOCKED overall; T09/Puller/CAS and installed-prefix dependencies verified.
User's second host reports first Compose HTTP success followed by manifest 401
on later up, empty container inspection and registered name retained by ps.
Kernel 7.2.9-200.fc44, OS/MAC not established: partial external evidence only.
Code review verified indefinite cached Bearer reuse after rejection. Added one
refresh from original challenge, conditional invalidation and service/scope keys,
without JWT trust/login/credentials/cleanup/policy changes. Registry/selected
race/full checks PASS; 100 auth race campaigns PASS (3.81s), rebuilt installed
KVM Compose/Helm/UI PASS without SKIP (7.30s) on unchanged local environment.
Remote expiry cause and updated-daemon recovery not yet verified; null endpoint
and stopped-app presentation remain separate. No new release/dependency/commit.
[Full report](../experiments/t24-registry-token-refresh.md); next was safe fresh
payload transfer and daemon restart/retest, now assisted by
[the payload target](../experiments/d0-transfer-payload.md).

## Archived T24 aggregate cache/load/license review delivery

T24 BLOCKED overall. T09/T18, per-image OCI budgets and installed D0 dependencies
verified. CAS/rootfs finite logical quotas, cross-process admission, CAS prune,
rootfs publication/reopen tests and bounded streaming scans delivered. Full check,
100 selected cache race suites (80,000 attempted writes plus child-process lock
checks), independent resolver and Python notice suites PASS. Installed KVM and
ten-cycle campaign (20 workloads/75.28s) PASS without SKIP on the unchanged local
Fedora/Permissive environment. First failed-stage assertion rejected the new lock;
corrected to allow only that exact file. Later streaming ReadDir lstat failed due
to synthetic File.Name; corrected real path, reran all gates PASS. Explicitly
materialized two existing Go source pins, produced seven-entry Go/47-entry npm
notice inventory and identified missing guest/helper corresponding-source/notices.
No redistribution clearance, global/physical/volume quota or active-rootfs GC;
no full external/hosted-CI/performance evidence. Full scope/failures/limitations:
[cache/load/license report](../experiments/t24-cache-load-license-review.md).
Next was remaining quotas/budgets/source/release decisions; user subsequently
reported second-host registry authorization failure, tracked in
[registry refresh](../experiments/t24-registry-token-refresh.md).

## Archived T24 OCI extraction budget delivery

T24 BLOCKED overall; T09/T18 and installed-runtime dependencies verified.
Finite per-image payload/entry/archive defaults span layers; bounded diff_id,
CRC/cancellation and cache failure/retry contracts delivered. CAS duplicate-fetch
race observed during full checks, corrected by admitted-leader recheck and
regressions. Unit/complete checks, 100 selected race suites, 60-second OCI fuzz,
installed Compose/Helm/UI and rebuilt legacy native RUN/multistage/network KVM
all final PASS without SKIP. Fuzz stalls still not latency evidence.
Same Fedora fc44/Linux 7.2.9/UID 1000/unchanged Permissive; no global/physical quota,
source/provenance clearance, hosted CI or external-host claim. User deferred
external-host checks. Full failure/limits/hash evidence:
[OCI budgets](../experiments/t24-oci-extraction-budgets.md); next was aggregate
quota/admission, load and source/notice review, now tracked in the
[subsequent report](../experiments/t24-cache-load-license-review.md).

## Archived T24 verified lifecycle/advisory delivery

Task T24, BLOCKED overall; dependencies T23/D0 and ADR 0009 verified.
Added verified runtime-only lifecycle measurements and pure regressions, patched
existing official x/net to v0.60.0 for five module-only advisories, no new module
or release claim. Complete check/advisory/npm audit, five 60-second fuzz targets,
100 real verified boot cycles twice and installed Compose/Helm/UI passed.
Initial QMP socket measurement rejection was fixed and recorded.
Fedora fc44/Linux 7.2.9, UID 1000, SELinux already Permissive and unchanged;
final verified-start median/p95 419,497/422,478 µs with zero retained tracked VMMs.
Missing global budgets/quota/load, source/provenance/release and hosted CI remain;
fuzz stalls are not latency evidence. User explicitly deferred second-host checks,
not acceptance requirements. Full measurements, failed gates, advisory IDs and
next steps: [campaign report](../experiments/t24-closure-campaign.md).
Subsequent [OCI budget work](../experiments/t24-oci-extraction-budgets.md) addresses
one local resource boundary without closing T24/D0.

## Archived D0 runtime-only/installed delivery

- **Task/status:** D0 — runtime-only guest, coherent staging and installed
  lifecycle; BLOCKED overall, local gates PASS.
- **Dependencies:** Prior layout/inventory, T07/T08/T11/T15, Compose/Helm,
  ADR 0009; T24 remained BLOCKED without acceptance waiver.
- **Contracts:** Runtime builder/scanner, staging, standalone examples/guide,
  shared identity, first-run private directories/log safety, root guards,
  doctor capability/remediation, endpoint inspection and developer harnesses.
- **Decisions:** Explicit provisioned inputs; no new dependency, accepted ADR,
  publisher/support or release claim. License/source review pending.
- **Checks:** Unit/focused races, `make check`, scanner fuzz, runtime/stage targets,
  real installed Compose/Helm/UI and legacy `make test-helm`, all final PASS;
  first-run/output-limit/compile/harness failures and corrections preserved in
  [full evidence](../experiments/d0-installed-runtime.md).
- **Environment:** Fedora fc44/Linux 7.2.9, QEMU 10.2.2, UID 1000; SELinux already
  Permissive and unchanged. Moved/read-only prefix, arbitrary CWD, no checkout/
  build tools on runtime PATH, HTTP/DNS/UI/lifetime and cleanup; online OCI pull.
- **Not run/limits:** Clean external host, Enforcing, release/source/licenses/
  signing, native package lifecycle, hosted CI, cold benchmark, supported ranges
  and retention GC. Historical T07 fixture/key remains development-only.
- **Next then:** T24/maintainer review before D1 and dependent D2/D3/D4. Subsequent
  [T24 campaign](../experiments/t24-closure-campaign.md) adds measurements/advisory
  fixes without closing external gates.

## Archived D0 portable inventory delivery

- **Task:** D0 — portable guest inventory and ABI validation.
- **Status:** BLOCKED overall; bounded inventory/real boot contract delivered.
- **Dependencies verified:** Prior D0 layout, T07/T08 and T24 verified snapshots/
  fresh-key overlay; T24 acceptance remained BLOCKED. Continuation was not a
  waiver of external-host/release gates.
- **Files and contracts changed:** Versioned inventory, confined/bounded reads,
  strict JSON, portable manifest builder, QEMU installed-metadata/snapshot policy,
  daemon/doctor wiring, tests and `make test-d0-manifest`;
  [evidence](../experiments/d0-portable-manifest.md).
- **Decisions/ADRs:** Existing ABI/protocol and ADR 0009 retained; exact metadata,
  explicit legacy overrides. No dependency, publisher authentication,
  supported-host claim or ADR acceptance.
- **Tests run:** Focused unit/race and `make check` PASS; real
  `make test-d0-manifest` PASS without SKIP after rebuilding T07. Actual builder/
  moved-read-only-prefix doctor smoke from `/tmp`, links/fences/diff checks PASS.
- **Tests NOT run then:** Fixture-free guest/secret scan and installed CLI/daemon
  Compose+Helm lifecycle still pending. No clean external host, packages,
  upgrade/uninstall, provenance/license or benchmark gate.
- **Integration evidence:** Fedora fc44/kernel 7.2.8, QEMU 10.2.2, SELinux already
  Permissive. Two authenticated boots with distinct fresh keys, VMM/resource
  teardown verified. Development fixture only, not a distribution/network gate.
- **Known limitations then:** T07 fixture roots/legacy key, local expectations not
  publisher identity/opaque-image inspection, helper integrity/ranges and fuller
  doctor still open. T24 release blockers preserved.
- **Next task then:** Runtime-only guest builder, content/key scan and actual
  installed Compose/Helm; these are delivered in the
  [subsequent local D0 report](../experiments/d0-installed-runtime.md), not a T24
  or clean-host acceptance waiver.

## Status snapshot before consolidation

**T00 is implemented and locally verified.** The repository contains a Go module, tested command scaffolding, build/check targets, and CI configuration.

**T01 is complete.** A real Firecracker microVM boots as the user, completes a vsock handshake, executes a guest command, streams output, and stops cleanly across 30 measured cycles (boot median 260 ms, total median 391 ms). A rootless networking helper (pasta) was also verified to give a new user+network namespace an address, default route, DNS, and egress.

**T02 is complete.** OCI execution and two-container localhost pass on both backends. Two-VM HTTP/DNS/egress, loopback host publishing, cross-application and guest-to-management denial, live virtiofs binds, a managed ext4 volume with restart persistence, and filesystem overhead were all measured on real hardware (`make test-f0`). Host-originated virtiofs notifications are explicitly degraded (polling).

**T03 is complete; the F0 gate passed on QEMU `microvm` + virtiofsd.** See [ADR 0005](../adr/0005-platform-qemu-virtiofsd.md) (Proposed, all required evidence present, awaiting maintainer acceptance). There is still no workload runtime or release. A measured distribution restriction: SELinux Enforcing silently kills `pasta` helpers on the tested Fedora policy, so `doctor` must surface it.

**T04 is complete.** The versioned application IR lives in `internal/model` with `internal/source` for source locations and structured diagnostics. It provides the section 5.1 types, quantity parsing, normalization, canonical hashing, an experimental native JSON manifest, a capability/support registry, validation (unknown version, references, duplicates, cycles, guest budget), and public redaction. The model imports no VMM, network, process, or frontend code.

**T05 is complete.** `internal/state` provides the per-user XDG layout, a single-writer `flock`, atomic snapshots with fsync, pending operations and observations, schema versioning with backup-before-migration, and a bounded rotating NDJSON journal that recovers a truncated last record but rejects mid-file corruption. `internal/secrets` keeps secret values out of the IR and state, versions them with random tokens that change only on real change, and garbage-collects only unreferenced versions.

**T06 is complete.** `internal/guestproto` implements the bounded, versioned, authenticated host/guest protocol and its documented wire format in [api/guest-protocol.md](../../api/guest-protocol.md). The framing codec is independent of the transport; an AF_VSOCK dialer/listener and an in-memory plain-conn test transport are provided. The real guest agent (PID 1, OCI runtime) is still T07.

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

**T12 is complete after production revalidation on 2026-10-06.** Application
namespace DNS now supplies normal Service VIPs, headless ready-Pod addresses and
named SRV records. TCP proxies resolve named target ports, balance real replicas,
and exclude unready/draining sandboxes. Ingress publishes stable loopback HTTP
fallbacks; `inspect` includes public route endpoints. Fixed TCP host ports are
reserved before VM boot and released on removal/down. Namespace DNS forwarding
has bounded UDP/TCP exchanges through pasta's explicit DNS forwarding address;
external DNS availability still depends on the host resolver. Compose names use
headless DNS rather than pretending to be Kubernetes ClusterIP Services.
`make test-helm`, `make test-f2`, `make test-k8s` and `make check` pass. The original
missing-datapath failures remain recorded as historical evidence in the T22 report.

**T13 is complete.** `internal/observe` provides a sequenced event stream (reusing the state journal's rotation and sequence IDs) with `Follow` and gap detection, a bounded log spool with follow, `/proc`-based resource snapshots with CPU deltas, and exec/HTTP/TCP probes with startup/readiness/liveness roles, thresholds, injected-clock scheduling, and nonoverlapping ticks. A bounded exit watcher emits an event only on container state change. The executor wires the probes and restarts a container whose liveness probe fails, verified on real KVM by a PID change.

**T14 is complete.** `internal/plan` diffs a desired IR against observed state into an ordered, typed action list (create/recreate/scale/drain/stop/delete, volume preparation, endpoint updates) with per-template hashing so identical and route-only applies reboot nothing and Jobs do not restart forever. `internal/reconcile` applies plans with deterministic operation IDs, bounded per-application workers, classified permanent/transient retries, partial-progress persistence, crash-replay, idempotent `down`, and a `NativeExecutor` dispatcher over sandbox/volume controllers. `internal/executor` implements those controllers against the QEMU backend, storage manager, and guest agent, so a native manifest applies end to end; `make test-executor` boots a sandbox, starts a container, reports status, execs a command, and tears down on real KVM.

**T15 is complete.** `internal/api` serves the local Unix-socket API with `SO_PEERCRED` UID checks, bounded bodies, the `{code,message,resource,retryable,details}` error DTO, asynchronous operations on the daemon lifetime context, SSE events with `Last-Event-ID` and gap records, log streaming, and distinct daemon-shutdown/application-down endpoints. `cmd/grillod` holds the single-instance state lock, builds the QEMU backend, storage manager, and `internal/executor`, and serves status and exec endpoints.

**T21 (Helm rendering and OCI charts) is complete.** `internal/frontend/helm`
uses official Helm v4.2.2 with a private environment, bounded local inputs,
ordered values, fixed offline capabilities and stable release identity. The CLI
accepts local and exact-version OCI charts; remote fetch is explicit, and private
atomic cache records verify digests/metadata/archives on every reuse. Dependencies,
hooks/CRDs/lookup/dynamic tpl and known nondeterministic templates are rejected.
Actual Helm packaging/rendering plus HTTPS Distribution fixture and CLI tests
pass; this is not the T22 real-workload gate. See
[the T21 report](../experiments/t21-helm-renderer.md).

**T20 (Kubernetes MVP compiler) is complete.** `internal/frontend/kubernetes` compiles multi-document YAML/`List` for Pod, Deployment, Service, Ingress, ConfigMap, Secret, and PVC into the IR, rejects privileged/hostNetwork/Tier3/NodePort and unknown fields, keeps Secret values out of the public IR, and discloses the default RollingUpdate as a Recreate downgrade requiring consent. `make test-k8s` runs a compiled multi-container Pod on KVM. See `docs/experiments/t20-kubernetes-compiler.md` and `docs/compatibility.md`.

**T19 (F2 gate) is complete.** A complete Compose application (web + worker, one named network, service DNS, a managed volume) runs on the real runtime: `make test-f2` proves cross-VM fetch by service name, volume persistence across `down`/`up`, idempotent re-apply, and clean `down --volumes`. The gate also fixed three runtime defects (per-workload DNS filtering, volume metadata exposure, and PID-namespace VMM tracking). See `docs/experiments/t19-f2-compose.md`.

**T18 and T18b are complete (ADR 0006).** Grillo has a native build system as the default and required path: `internal/dockerfile` parses a supported Dockerfile subset and `.dockerignore`, `internal/build.NativeBuilder` assembles the image without any external container engine, and `RUN` executes inside a sandboxed guest that shares the build root (T18b). Podman is an explicit opt-in (`--podman`) only. The Podman backend, `internal/oci.ImportLayout`, and `internal/image` (inventory/inspect/pin/GC, API/CLI) are retained.

**T17 is complete.** `internal/frontend/compose` parses Compose YAML with source maps, interpolation (`$VAR`, `${VAR}`, defaults, required, alternatives, `$$`), `.env`/host environment precedence, `env_file`, ports, volumes, healthchecks, `depends_on`, restart policies, resources, and networks, and compiles to the IR with field-level support diagnostics and a golden fixture. `internal/frontend/detect` identifies Compose, native, Kubernetes, and Helm input, and the CLI now accepts Compose files.

**T16 is complete after capability-policy revalidation on 2026-10-06.** CLI,
executor and daemon share an explicit native implementation whitelist rather
than empty or blanket capabilities. Supported storage, init/multiple containers,
probes and bridged routes/host ports are admitted; privileged, StatefulSet/Job
and unknown features remain unavailable. Offline applicability describes
implementation semantics, not host feasibility. Real CLI Helm plan/up/inspect/
down through the actual daemon now passes, with secret separation and recovery.
`internal/cli` implements the native-runtime command line with a parser for flags before/after positionals, repeated `-f`, and the exec `--` terminator; offline `plan`; `up` (daemon autostart + async apply), `down`, `status`, `inspect`, `logs`, `events`, `exec` against a live container, and a read-only `doctor` that never requires root. `cmd/grillo` is wired to it. It also provides `ps`, `restart`, and a `shell` that streams `/bin/sh`, and a `ui` command that serves a secure loopback web console (`internal/ui`) with a bootstrap token exchanged for an HttpOnly cookie, Host/Origin checks, a strict CSP, and text-node rendering.

**T23's MVP web console and user-requested PatternFly React follow-up are
complete.** Embedded React/PatternFly views use an
allowlisted public API snapshot: overview/topology, VM/container/probe detail,
VMM samples versus guest budget, mounts/volumes/routes, config/Secret metadata,
source/runtime diagnostics, filtered spool logs, resumable SSE and non-TTY exec.
Real Firefox fixture and actual CLI UI/daemon/KVM gates pass, including unchanged
VMM PIDs and successful exec after browser/bridge closure. Missing compiler
line diagnostics, log ingestion and detailed guest/container metrics are labelled
unavailable; full browser TTY is deferred. See [the T23 report](../experiments/t23-console.md).

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

Task contracts and full acceptance criteria live in [IMPLEMENTATION_PLAN.md](../../IMPLEMENTATION_PLAN.md). The table is a status summary, not a replacement for those contracts.

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
- **Decisions/ADRs:** [0001](../adr/0001-scaffold-and-dependencies.md), proposed pending
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
  tests, `Makefile` formatting coverage, and [experiment report](../experiments/t01-preflight.md).
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
- **Decisions/ADRs:** [0002](../adr/0002-explicit-dependency-bootstrap.md), proposed.
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
  vsock only). Report: [t01-boot-spike](../experiments/t01-boot-spike.md).
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
  [T01 report](../experiments/t01-boot-spike.md).
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
  `test-kvm`). Report: [t02-oci-and-storage](../experiments/t02-oci-and-storage.md).
- **Decisions/ADRs:** [0004](../adr/0004-shared-filesystem-backend-comparison.md)
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
  [t03-backend-comparison](../experiments/t03-backend-comparison.md).
- **Decisions/ADRs:** [0005](../adr/0005-platform-qemu-virtiofsd.md) (Proposed):
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
- **Decisions/ADRs:** [0005](../adr/0005-platform-qemu-virtiofsd.md) updated to
  Proposed with complete evidence (awaiting maintainer acceptance);
  [0004](../adr/0004-shared-filesystem-backend-comparison.md) marked complete.
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

## T18 Builder and image tooling - 2026-10-03

- **Task:** T18 - Native build system and image tooling
- **Status:** DONE
- **Dependencies verified:** T07 (guest agent), T09 (OCI/CAS), T10 (storage), T11 (network), T17 (Compose) complete.
- **Files and contracts changed:** `internal/dockerfile` (Dockerfile parser and
  `.dockerignore` matcher), `internal/build` (`Builder` contract with `Request`/
  `Result`, `NativeBuilder`, `SandboxRunner`, `GuestBoot`, and `PodmanBuilder`),
  `internal/oci/layout.go` (`ImportLayout`, `LoadPulled`, `CAS.Read`) and
  `internal/oci/config.go` (image-config history),
  `internal/image` (`Store` with `Import`/`List`/`Get`/`Pin`/`PinDigest`/`Prune`/
  `Verify` and `ErrNotFound`), `internal/api/images.go` and `client.go` (image and
  build endpoints), `cmd/grillod` (wires the image store, the native builder with
  a sandbox runner, and the opt-in Podman builder), `internal/cli`
  (`build --podman`, `image ls|inspect|pin|unpin|prune`), `api/local-api.md`.
- **Decisions/ADRs:** ADR 0006 - native build system required, Podman opt-in.
- **Tests run:** `make check` PASS. `make test-builder-kvm` PASS on real KVM:
  `TestKVMBuildGuestRun` proves commands execute in a booted build guest, writes
  reach the shared root over virtiofs, the guest is reused, and exit codes are
  reported; `TestKVMBuildNativeBuilderRun` proves the full native pipeline
  (`FROM scratch` -> `COPY` -> sandboxed `RUN` -> publish -> unpack). `make
  test-builder` PASS confirms the opt-in Podman path (scratch build,
  `.dockerignore`, OCI-layout import, cancellation, missing-tool error). Unit
  tests cover the parser, `.dockerignore`, `ARG`/`ENV` expansion, `COPY`/`ADD`
  with `--chown`/`--chmod`, `FROM scratch`/local-base/multi-stage builds,
  whiteouts, unsupported-instruction rejection, `RUN` requiring a runner,
  `SandboxRunner` boot reuse and exit-code handling, build egress through pasta
  (`TestKVMBuildGuestNetwork` proves the guest interface is configured and can
  reach the internet; it SKIPs when the host has no internet), image
  import/list/pin/verify
  and GC, OCI-layout import/corruption, API endpoints, and CLI `build`/`image`
  (`--podman` opt-in asserted). `TestPlanDoesNotBuild` enforces that planning
  never imports a builder.
- **Tests NOT run and why:** hosted CI has not run. CI without `/dev/kvm` or the
  guest artifacts SKIPs the `kvm` build test; CI without Podman SKIPs the
  `builder` test. Both are documented, not passes.
- **Integration evidence:** Real native build with sandboxed `RUN` executed on
  this host (Linux/amd64, kernel `7.2.8-200.fc44.x86_64`, KVM API 12) in 11.4 s;
  the guest agent initramfs was rebuilt before testing.
- **Known limitations:** per-instruction layer caching is not yet implemented
  (one layer per stage); non-numeric `USER`/`--chown` names and `ADD` URLs/tar
  extraction are rejected; `prune` preserves images referenced by applications
  applied in the current daemon session only (not persisted across restarts);
  build egress requires the `grillo-netns` supervisor and `pasta`, and is
  disabled with a warning when they are unavailable.
- **Next task:** T19 (F2 gate: Compose application).

## T20 Kubernetes MVP compiler - 2026-10-03

- **Task:** T20 - Kubernetes MVP compiler
- **Status:** DONE
- **Dependencies verified:** T04 (IR), T14 (reconciler), T06/T07 (guest) complete.
- **Files and contracts changed:** `internal/frontend/kubernetes` (parser,
  support registry, compiler, tests, fixtures, golden),
  `internal/frontend/detect` and `internal/cli` (Kubernetes input, secret
  persistence, `--allow-degraded`), `internal/executor` (`SecretResolver` and
  `secretKeyRef` env resolution), `cmd/grillod` (secret store),
  `docs/compatibility.md`, `docs/experiments/t20-kubernetes-compiler.md`,
  `Makefile` (`test-k8s`).
- **Decisions/ADRs:** No new ADR. Uses the existing IR and source diagnostics.
- **Tests run:** `make test-k8s` PASS on real KVM (init + app + sidecar, shared
  localhost, `emptyDir` write). Compiler unit tests: deployment golden,
  secret-leak check, RollingUpdate consent gate, explicit Recreate,
  privileged/hostNetwork/Tier3/NodePort rejection, multi-container Pod, `List`,
  namespace rejection, unknown field, selector mismatch. CLI tests:
  `TestPlanKubernetesInput`, `TestPlanKubernetesRejectsPrivileged`. `make check`
  PASS.
- **Tests NOT run and why:** hosted CI has not run; the KVM test SKIPs without
  `/dev/kvm`, `pasta`, or the guest artifacts.
- **Integration evidence:** `docs/experiments/t20-kubernetes-compiler.md`.
- **Known limitations:** StatefulSet/Job/CronJob (F4), NetworkPolicy, and
  projected/configMap/secret/downwardAPI volumes are rejected; `fieldRef` and
  scheduling fields are rejected; Secret values are written by the CLI to the
  shared per-user secret store.
- **Next task:** T21 (Helm rendering and OCI charts).

## T19 F2 gate: Compose application - 2026-10-03

- **Task:** T19 - F2 gate: Compose application
- **Status:** DONE
- **Dependencies verified:** T12–T18 complete.
- **Files and contracts changed:** `internal/frontend/compose` (a Service per
  service for DNS; multi-network topologies rejected with `compose.multi_network`),
  `internal/executor` (local image store resolution, project-relative bind
  resolution, all-service DNS records), `internal/storage` (share only
  `<volume>/data`), `internal/sandbox`/`internal/netns`/`internal/backend/qemu`
  (`sandbox.VMM` handle so externally launched VMMs are not tracked by
  namespace-local PID), `examples/f2-compose`, `docs/experiments/t19-f2-compose.md`,
  `Makefile` (`test-f2`).
- **Decisions/ADRs:** ADR 0006 (native builds). No new ADR.
- **Tests run:** `make test-f2` PASS on real KVM (web + worker, DNS by name,
  cross-VM fetch, managed volume, idempotent re-apply, down/up recovery, cleanup);
  `make test-bridged`, `make test-executor`, `make test-builder-kvm`, and
  `make check` PASS. Compose unit tests cover the single-network success and the
  multi-network rejection.
- **Tests NOT run and why:** hosted CI has not run; the KVM gate SKIPs without
  `/dev/kvm`, `pasta`, or the guest artifacts.
- **Integration evidence:** `docs/experiments/t19-f2-compose.md` with the
  in-guest resolver configuration and fetched content, and the pass output.
- **Known limitations:** one network per application (multi-network rejected);
  Compose `build:` is not yet wired to run automatically from `up`; the gate uses
  prebuilt images.
- **Next task:** T21 (Helm rendering and OCI charts).

## T18b Build execution in the guest - 2026-10-03

- **Task:** T18b - Build execution in the guest (protocol)
- **Status:** DONE
- **Dependencies verified:** T06 (protocol), T07 (guest agent), T18 (native builder).
- **Files and contracts changed:** `internal/guestproto` (`run`/`run_result`
  message types, `RunRequest`/`RunResult`, `SandboxSpec.Build`, `SandboxClient`
  surface), `internal/guest` (`RunStreaming` on the runtime, the agent `run`
  handler that resolves the share target itself, and `stderrWriter`),
  `internal/build` (`SandboxRunner`, `GuestBoot`, `BuildNetwork`). `make build`
  now also builds `bin/grillod` and `bin/grillo-netns`.
- **Decisions/ADRs:** ADR 0006 (option A: execute `RUN` inside the sandbox).
- **Tests run:** `make test-builder-kvm` PASS; `make check` PASS. The agent's
  `run` handler rejects an unknown share and requires a share and command.
- **Tests NOT run and why:** hosted CI has not run; the KVM test SKIPs without
  `/dev/kvm` or the guest artifacts.
- **Integration evidence:** repeated `RUN` commands ran against one booted guest;
  output and exit codes were correct; writes propagated to the host share.
- **Known limitations:** streaming is delivered per read chunk and bounded by the
  runtime capture limit. Build egress is provided by `build.BuildNetwork` (a
  pasta namespace with a TAP and IPAM lease); the guest writes the host
  nameservers into `/etc/resolv.conf`, and it is disabled with a warning when
  `grillo-netns`/`pasta` are unavailable.
- **Next task:** T19 (F2 gate: Compose application).

## T17 Compose parser and compiler - 2026-10-03

- **Task:** T17 - Compose parser and compiler
- **Status:** DONE
- **Dependencies verified:** T04 (IR) complete; integrated with T16 (CLI).
- **Files and contracts changed:** `internal/frontend/compose` (YAML node
  helpers, `interpolate.go`, `support.go`, `compile.go`), `internal/frontend/detect`
  (format detection), and `internal/cli` (Compose-aware `plan`/`up` with
  `--format` and `.env`/host environment precedence). Adds the allowed module
  `go.yaml.in/yaml/v3 v3.0.5`.
- **Decisions/ADRs:** None required. A Compose string `command`/`entrypoint` is
  split with a shell-like lexer, never wrapped in `/bin/sh -c`; each service maps
  to one workload with one container.
- **Tests run:** `make check` PASS; `go test -race ./internal/frontend/...
  ./internal/cli` PASS. Coverage: the full fixture compiles to the expected IR
  (replicas, restart, depends_on, resources, healthcheck probe, ports, volumes,
  networks); a canonical-JSON golden; unknown and unsupported fields produce
  structured diagnostics; interpolation forms and required-variable failure;
  empty document and absent-vs-empty command/environment; a diagnostic does not
  echo a secret value; the project-relative bind source is resolved; format
  detection for compose/native/kubernetes/helm and ambiguity; and the CLI
  compiles Compose for `plan`/`up` and rejects unsupported fields. The optional
  `docker compose config` comparison SKIPs because Docker is not installed.
- **Tests NOT run and why:** `build` is `VALIDATE_ONLY` (the image builder is
  T18); Compose secrets/configs/profiles/extends/devices/privileged/host-network
  are rejected with diagnostics; Kubernetes and Helm inputs are detected but not
  compiled (T20/T21). Hosted CI has not run.
- **Integration evidence:** Repository-local; the frontend imports only
  `model`/`source`/YAML, enforced by a `go list` import-boundary test.
- **Known limitations:** Bind sources are kept project-relative for reproducible
  goldens and must be resolved by the runtime; `build` is a diagnostic only;
  `env_file` supports the `KEY=VALUE` subset.
- **Next task:** T18 (builder and image/volume/network tooling).

## Runtime review remediation - 2026-10-04

- **Task:** corrective workstream R1–R8 from the runtime code review (T07–T10, T14–T16, T19, T20). Tracker: [remediation.md](runtime-review.md).
- **Status:** DONE for implementation and real-KVM verification of all eight findings.
- **Dependencies verified:** T07/T08/T09/T10, T13/T14/T15/T16, T17/T19/T20 complete; `/dev/kvm`, `/dev/vhost-vsock`, `qemu-system-x86_64`, `pasta`, `ip`, `nft` present on this host.
- **Files and contracts changed:** `internal/model` (per-container `SecurityProfile`); `internal/frontend/kubernetes` (effective security policy per container, Pod label fix, PVC alias resolution, `subPath` rejection); `internal/executor` (`security.go`, millicore→CFS quota with 1 s period, read-only image shares, private roots, deterministic exec selection); `internal/guest` (`rootfs_linux.go` per-container overlay, OCI root `readonly`); `internal/guestproto` (container-name validation); `internal/backend/qemu` (QMP `SO_PEERCRED` identity, `recovery_linux.go`); `internal/netns` (supervisor shutdown); `internal/reconcile` (`persistent.go` durable intent/progress); `cmd/grillod` (network wiring, mutation lock, startup recovery, native-build scratch dir).
- **Decisions/ADRs:** No new ADR. Writable container roots use guest-local overlayfs with a private upper layer; unsupported security semantics fail closed; recovery restarts sandboxes (implementation plan §4.1) and never kills an unverified process.
- **Tests run:** `make check` PASS; `make test-executor`, `make test-f2`, `make test-k8s` PASS on real KVM/QEMU; `go test -tags kvm -run TestKVMDaemonRemediation ./cmd/grillod` PASS against the real `grillod` + `grillo-netns` process and API; `go test -race` PASS on the changed packages.
- **Tests NOT run and why:** Hosted CI has not run. KVM targets SKIP without `/dev/kvm`, `pasta`, or the guest artifacts.
- **Integration evidence:** The daemon gate proves cross-VM Service DNS/fetch, private per-container writes not visible in a sibling or the shared image cache, `runAsUser` enforced as UID 1000, read-only root write rejection, in-guest `cpu.max` of `100000 1000000` for 100m, and recreation of durable desired state after daemon `SIGKILL` while a stopped application stays stopped.
- **Known limitations:** Overlay upper layers live in guest tmpfs (bounded by guest RAM, no per-container disk quota); capability overrides and custom seccomp are rejected, not emulated; recovery restarts rather than adopts VMs; the mutation lock is global per daemon; `subPath` and ReadWriteMany remain unsupported.
- **Next task:** continue T21 (Helm rendering and OCI charts).

## T21 local Helm renderer — 2026-10-05

- **Task:** T21 — Helm rendering and OCI charts (bounded local-renderer increment).
- **Status:** IN_PROGRESS; not a completed T21 or T22 gate.
- **Dependencies verified:** T20 completed, existing Kubernetes compiler and
  private secret result contract inspected; clean initial working tree. User
  installed Helm during the session; `/usr/bin/helm` reports v4.2.2+gb05881c.
- **Files and contracts changed:** `internal/frontend/helm` exposes `Compile`
  for local chart directories and ordered values, explicit release identity,
  default namespace, fixed offline capabilities, private snapshots/environment,
  rooted file reads, input/output/deadline limits, and redacted tool diagnostics.
  It delegates to T20 and returns secrets separately. Compatibility and ADR
  index updated; no Go dependency or CLI behavior changed.
- **Decisions/ADRs:** [0007](../adr/0007-controlled-helm-renderer.md), Proposed;
  official executable instead of the specification's library preference.
  Exact version is v4.2.2, matching the user-provisioned renderer. Upstream
  checksum metadata inspected; no binary installation or verification claimed.
- **Tests run:** `go test -count=1 -v ./internal/frontend/helm` PASS with real
  Helm (no skips); `go test -race -count=1 ./internal/frontend/helm` PASS;
  `make check` PASS (format, vet, unit/race, script fixtures, builds, module audit).
  Positive coverage includes equivalent chart/manifest IR, repeated stable
  rendering, values order, and secret separation. Negative coverage includes
  hooks, lookup/random/tpl, dependencies, schemas, CRDs, symlinks, oversized
  files, version mismatch, output overflow, environment isolation, cancellation,
  and redacted failures. Initial tests failed on a pointer-to-slice test access,
  Helm rejecting invalid YAML before T20, and an embedded `bytes.Buffer`
  promoting `ReadFrom` and bypassing the output cap; all were corrected and
  regressions pass. `git diff --check` PASS.
- **Tests NOT run and why:** T22/KVM Helm workload gate is not available yet;
  CLI Helm input, remote OCI fetching/cache/digests and lockfile dependencies
  are not implemented. Hosted CI not run.
- **Integration evidence:** Actual Helm template processes without inherited
  kubeconfig or credentials, then the production Kubernetes compiler; no VM
  or application execution is claimed by these tests.
- **Known limitations:** Local chart adapter only; CLI still rejects Helm.
  Token preflight is conservative (including comments/strings), not a host
  sandbox; dependencies, schemas and dynamic `tpl` are blocked to avoid implicit
  fetches or opaque/nondeterministic rendering. No T21 completion claim.
- **Next task:** continue T21 with CLI integration and secret persistence, then
  explicit verified OCI fetching/cache and lockfile-aware dependencies.

## T21 local Helm CLI integration — 2026-10-05

- **Task:** T21 — Helm rendering and OCI charts (local CLI increment).
- **Status:** IN_PROGRESS; OCI/cache/dependencies and T22 remain open.
- **Dependencies verified:** T20 and previous T21 local renderer; Helm v4.2.2
  available. Existing uncommitted renderer/documentation changes were preserved.
- **Files and contracts changed:** `internal/cli/cli.go`, CLI Helm tests,
  `internal/frontend/helm/helm.go`, `examples/helm`, README, compatibility and
  ADR 0007. `plan`/`up` detect chart directories or `Chart.yaml`, accept ordered
  values with a positional chart, `--release` and `--namespace default`, and
  propagate code-specific degradation consent to T20. Non-Helm input rejects
  Helm-only options rather than silently ignoring them. File-backed manifests
  are now read with the same 8 MiB bound as stdin.
- **Decisions/ADRs:** ADR 0007 remains Proposed. Helm and Kubernetes secrets are
  persisted only on apply, after validation; preview plans use the deterministic
  `unresolved-offline` marker instead of publishing compiler content hashes.
  This corrects the existing Kubernetes preview's secret-store mutation too.
  Unsafe special files are rejected before opening chart/values inputs.
- **Tests run:** `go test -count=1 ./internal/cli ./internal/frontend/helm` PASS;
  `go test -race -count=1 ./internal/cli ./internal/frontend/helm` PASS;
  `make check` PASS. New CLI cases use actual Helm for values ordering, stable
  plans, identity, Chart.yaml/directories, degradation consent, private secret
  persistence with unchanged version on reapply, and redacted failures. Additional
  cases prove no state creation on preview/rejection, no daemon/Apply on rejected
  input, missing Helm, FIFO refusal, invalid namespace/release, no implicit OCI
  fetch and the manifest size bound. Kubernetes secret-preview and rejected-apply
  regressions pass. No failures occurred in these verification runs.
- **Integration evidence:** freshly built `bin/grillo plan examples/helm/demo
  --release demo` PASS; isolated XDG workspace remained empty. CLI apply tests
  invoke real Helm and the real secret store but use the API test double, not
  KVM or the daemon runtime.
- **Tests NOT run and why:** real Helm workload/KVM gate T22 awaits completed
  T21; OCI fetching/digest-cache/lockfile tests await implementation. Hosted CI
  not run.
- **Known limitations:** local charts only, no dependencies or values schemas;
  offline plans target empty observations and do not compare live secret changes.
  Existing renderer restrictions remain. No Helm runtime completion claim.
- **Next task:** finish T21's explicit exact-version OCI fetch, verified cache
  and lockfile-aware dependency policy; then execute T22 on real KVM.

## T21 completion — exact-version OCI charts — 2026-10-05

- **Task:** T21 — Helm rendering and OCI charts.
- **Status:** DONE. All T21 acceptance criteria are evidenced in the
  [T21 report](../experiments/t21-helm-renderer.md); T22 remains TODO.
- **Dependencies verified:** T20 complete; existing OCI Distribution client,
  T05 atomic file primitive and official Helm v4.2.2 inspected/reused. Existing
  uncommitted local renderer/CLI changes preserved and completed, not replaced.
- **Files and contracts changed:** `internal/frontend/helm/oci.go`, Linux cache
  lock/nonblocking input helpers, Helm/OCI/adversarial/CLI tests,
  `internal/cli/cli.go` (`--version`, `--chart-digest`, `--fetch-chart`),
  `examples/helm`, README, compatibility, ADR 0007 and T21 acceptance report.
  Exact SemVer OCI charts fetch only with consent when missing; cached versions
  never refresh. Manifest/config/content hashes, sizes, metadata, archive safety
  and private owner-checked cache records are verified before atomic publication
  and on every reuse. Corrupt caches/conflicting pins block without overwrite.
  Default transport refuses HTTPS downgrades (including token realms) and does
  not inherit proxy credentials. Source provenance identifies Helm input.
- **Decisions/ADRs:** ADR 0007 remains Proposed. T21 implements a restricted
  self-contained chart subset: dependencies/subcharts are rejected whether
  locked or unlocked; no dependency downloader/update is exposed and lockfiles
  are never changed. This satisfies the planned restricted dependency policy,
  not dependency-resolution parity. Optional trusted manifest digest; otherwise
  first HTTPS resolution is TOFU, not publisher/signature authentication.
- **Tests run:** `go test -count=1 -v ./internal/frontend/helm ./internal/cli`
  PASS with actual Helm (no real-renderer skips); `go test -race -count=3
  ./internal/frontend/helm ./internal/cli` PASS; `make check` PASS. Coverage adds
  official Helm package → HTTPS Distribution client → real renderer → CLI,
  local/OCI hash equivalence, consent/no-cache effects, offline reuse after
  server shutdown, private cached apply references, wrong/truncated blobs and
  retry, cache corruption, cancellation mid-fetch, six concurrent compilers,
  conflicting pins/lock cancellation, hostile tar/gzip/limits, metadata/media
  type rejection, HTTPS redirects/token-realm refusal, exact/prerelease/build
  SemVer mapping, locked dependency refusal, and no-follow/nonblocking FIFO
  opens. The offline-capabilities fixture initially failed because its test
  template escaped a quote incorrectly; corrected and reverified green. A later
  no-follow regression exposed that Go Root.OpenFile resolves contained links
  despite O_NOFOLLOW; the helper now explicitly rejects non-regular inputs and
  checks inode identity before/after a nonblocking rooted open. Tests were rerun
  after that correction rather than counting the failed run as a pass.
  Freshly built local CLI example plan PASS with empty isolated XDG workspace.
  `git diff --check` and local Markdown links PASS.
- **Tests NOT run and why:** Public Internet/private registry credential flows
  and hosted CI were not run; HTTPS integration uses a protocol fixture with
  actual packaged chart bytes and Helm, not a public-registry compatibility
  claim. T22 application/KVM gate is a separate next task, not a T21 requirement.
- **Integration evidence:** Linux 7.2.8-200.fc44.x86_64 / amd64,
  Go go1.26.8-X:nodwarf5, `/usr/bin/helm` v4.2.2+gb05881c. Actual official
  package/template commands, HTTP client, cache and CLI exercised without a
  cluster; Apply uses the API test double and makes no VM-execution claim.
- **Known limitations:** Self-contained v2 charts only; one Helm content layer,
  no additional/provenance layers/indexes, dependency resolution, schemas,
  dynamic tpl, non-default namespaces or private-registry credential CLI.
  Template token scanning is conservative, not an OS sandbox. Offline plans
  preview creation against empty observations, not live secret updates.
  Explicit `plan --fetch-chart` may write private cache records (which may
  embed source values), but never desired runtime state or the secret store.
  Cache publication is Linux-only; non-Linux behavior is not a platform claim.
- **Next task:** T22 — Helm gate and compatibility reporting on the real runtime.

## T22 initial Helm hardware check

- **Task:** T22 — Helm gate and compatibility reporting (bounded initial subtask).
- **Status:** IN_PROGRESS; not the full scenario C gate.
- **Dependencies verified:** T19 and T21 completion reports; clean initial tree;
  official Helm v4.2.2, KVM, QEMU/virtiofsd, pasta and existing OCI fixture.
- **Files and contracts changed:** `internal/executor/kvm_helm_test.go`,
  `Makefile` (`test-helm`), compatibility and progress. No production contract
  or dependency change. The official renderer compiles `examples/helm/demo`
  into the shared IR; the real executor boots one microVM and starts its OCI
  container. Default/overridden environment values are read through guest exec.
  Guest kernel boot IDs prove unchanged apply preserves the VM and a template
  update recreates it (stable resource keys alone are insufficient evidence).
  Cleanup is registered before apply, has its own deadline, reports failures,
  and checks that down leaves no sandbox/backend directory.
- **Decisions/ADRs:** None; retains ADR 0005/0007 contracts.
- **Tests run:** Linux/amd64, kernel 7.2.8-200.fc44.x86_64, UID 1000,
  Go go1.26.8-X:nodwarf5, SELinux Permissive:
  - `make test-helm`: PASS, rebuilt guest image, real KVM test (3.47 s).
  - `go test -race -tags kvm -count=2 -v -timeout 300s -run
    '^TestKVMHelmApplication$' ./internal/executor/`: PASS twice, no skips.
  - `go test -count=1 ./internal/frontend/helm ./internal/cli`: PASS,
    including existing rejection/redaction/consent and failure-path tests.
  - `make check`: PASS (format, vet, ordinary unit/race tests, scripts,
    host/agent builds, module audit); `git diff --check`: PASS.
  - Post-test process listing found no QEMU, virtiofsd or grillo-netns process
    (grep returned 1 because there were no matches).
- **Tests NOT run and why:** Full scenario C, multi-consumer config/secret
  updates, PVC persistence, readiness endpoint removal/restoration, Service
  VIP/Ingress datapaths, registry-derived matrix and text/JSON plan counts
  remain separate T22 work. Hosted CI and clean-host verification not run.
- **Integration evidence:** Actual Helm render → shared IR → reconciler →
  QEMU/KVM → guest runc, exec and down, without a Kubernetes control plane or
  privileged host operation. Guest initramfs SHA-256:
  `27462af7ca589331861eaac4bd6ba9bab5be6db78b21b6dc88173f343ec90415`.
- **Known limitations:** The image resolver supplies a preexisting busybox OCI
  rootfs for this test; it does not prove registry pulls/digest resolution. The
  demo has one Deployment and literal env values, not ConfigMap/Secret/PVC,
  Service/Ingress, init containers or sidecars. No complete T22/MVP claim.
- **Next task:** Continue T22 with scenario C and registry-derived compatibility
  reporting; missing runtime datapaths must remain explicit blockers.

## T22 full gate attempt and prerequisite corrections

- **Task:** T22 — Helm gate and compatibility reporting.
- **Status:** BLOCKED, with a reproducible failing acceptance gate, not an
  unfinished smoke-only task. See [full report](../experiments/t22-helm-gate.md).
- **Dependencies verified:** T19/T21 evidence inspected; real Helm v4.2.2,
  KVM/QEMU/virtiofsd, pasta, OCI rootfs fixture and guest artifacts present.
  The full scenario exposed incomplete T12/T16 prerequisites despite their
  previous DONE labels; their current summaries/table are now BLOCKED.
- **Files and contracts changed:** `examples/helm/scenario-c`, full tagged KVM
  gate, executor ephemeral volume scoping/cleanup and regression tests;
  `internal/source/support.go`, frontend registry snapshots,
  `scripts/compatibility` and generated JSON/Markdown data with CI drift tests;
  explicit Kubernetes validation-only diagnostics; pure plan resource/action
  counts and CLI text/JSON reports/fixtures. JSON includes `applicable` and
  structured diagnostics. Capability-blocked structural previews keep failure
  exits, do not store secrets, and cannot provision. No dependency changes.
- **Decisions/ADRs:** No new backend or bypass. The gate uses production
  components, never a test-local Service/Ingress proxy. The registry inventory
  explicitly classifies compilation, not stable runtime/per-field parity.
- **Tests run:** Linux/amd64, UID 1000, kernel 7.2.8-200.fc44.x86_64,
  Go go1.26.8-X:nodwarf5, SELinux Permissive:
  - `make test-helm`: **FAIL**, no skips, Make exit 2. Three real VMs start;
    init/sidecar/config/secret, identical apply, config-only and secret-only
    consumer replacement, PVC retention and explicit cleanup pass. ClusterIP
    DNS returns Pod IPs; Service port 80→named 8080 fails; Ingress has no
    endpoint; readiness filtering cannot pass its positive Service prerequisite
    and restoration times out. Simple demo still passes. No complete gate claim.
  - Earlier attempts failed on shared emptyDir RWO identity (fixed), then on
    overly long test Unix socket paths/partial observation cleanup (fixture
    shortened; attempted owned sandboxes explicitly cleaned). An initial DNS
    check queried both A/AAAA with busybox; it now explicitly requests A so
    unsupported AAAA does not obscure the actual VIP failure.
  - `make check`: PASS (format/vet/unit/race/scripts/build/module audit), including
    registry drift/fixture validation and CLI state/redaction/consent tests.
  - `make test-k8s`: PASS (2.37 s); `make test-f2`: PASS (5.68 s), real KVM,
    no skips after the ephemeral storage fix. Final selected demo/Kubernetes/
    Compose hardware tests with `-race -count=1` also PASS (12.09 s total).
    `go vet -tags kvm ./internal/executor` PASS.
  - CLI scenario tests initially failed on the actual empty capability set and
    existing compatibility exit 1 (rather than the assumed 2). The tests now
    preserve rejection and assert explicit blocked previews, not fake support.
    The first Markdown checker failed on README's existing percent-encoded
    image path; URL-decoded checking then PASS. Final `git diff --check`, all
    changed/new Markdown links, process and owned-workspace checks PASS.
- **Tests NOT run and why:** Full CLI chart execution is rejected by current
  capability validation; no capability bypass was added. End-to-end readiness
  removal/Ingress forwarding cannot be verified without T12 wiring. Hosted CI,
  clean second host, external-registry pulls and long benchmarks/fuzzing not run.
- **Integration evidence:** Actual official Helm → IR → reconciler → QEMU/KVM
  → guest runc; final full gate 31.13 s, rebuilt initramfs SHA-256
  `86daf5760176539fc66a5cf043907dc224b8829feb3ca223e6506ce6e6412c69`.
  This uses a fixed preexisting OCI rootfs, not live pull/digest evidence.
- **Known limitations:** Service/Ingress are component-only; no-op endpoint and
  drain actions must be replaced. CLI capability reporting is incomplete. The
  generated matrix covers registered compiler rules, not exhaustive defaults
  or per-feature hardware coverage. Historical dependent DONE claims need
  reassessment after these prerequisites are repaired.
- **Next task:** Repair T12 production datapaths/readiness/draining, then T16
  accurate capabilities, and rerun the complete T22 gate before proceeding.

## T12 production repair and acceptance — 2026-10-06

- **Status:** DONE. T22 remains BLOCKED on T16, not on the repaired datapaths.
- **Dependencies verified:** T04 IR/planning tests; T11 real namespace bridge via
  the Helm/Compose/Kubernetes KVM gates. Same Linux/amd64 host, UID 1000,
  SELinux Permissive, provisioned QEMU/virtiofsd/pasta/Helm and OCI fixture.
- **Contracts:** namespace-supervised DNS/VIP proxies and restricted Unix relay;
  regular-container startup/readiness/exit gating; endpoint draining; stable
  localhost Ingress and pre-boot TCP port reservation. Public API/CLI inspect
  includes route fallbacks. Guest protocol 1.1 advertises `application-dns`;
  old agents are rejected explicitly for bridged execution.
- **Lifecycle:** cleanup intent is saved before possible network effects, even
  when the first VM creation fails. `ShutdownNetwork` is journaled separately
  from VM replacement. Helper parent-death signals no longer tie application
  lifetime to a Go launching thread/daemon. Missing live-application supervisors
  produce errors rather than a disconnected replacement namespace.
- **Evidence:** `make check` PASS; extended `make test-helm` PASS (scenario C
  16.26 s, demo 2.01 s, no skips), including four DNS aliases, real two-replica
  balancing, headless named SRV, readiness removal/restoration, host TCP
  publication/release and PVC persistence. `make test-f2 test-k8s` PASS (3.35 s,
  1.22 s). `make test-bridged` PASS (5.75 s). Extended Helm, Compose and
  Kubernetes KVM regressions with `-race -count=1` PASS (24.357 s total).
  Component race tests PASS. See the T22 report for artifact hashes.
- **Failures retained:** malformed nftables protocol syntax, missing test
  imports, a BusyBox search-expanded nslookup failure, and Compose regression
  were observed and corrected. Compose now explicitly uses headless DNS,
  preserving undeclared container-port reachability rather than empty VIPs.
- **Limits:** TCP only; fixed host ports require one replica and unprivileged
  ports. TCP streams have bounded lifetime/concurrency. Custom Pod DNS and TLS
  routes are rejected. Forwarding uses the host resolver through pasta; live
  Internet DNS, a second host, hosted CI, and daemon-crash continuity were not
  newly tested. No general StatefulSet/Job or full CLI capability claim.
- **Next:** T16 accurate runtime capability policy, then final T22 CLI/daemon gate.

## T16 and T22 final acceptance — 2026-10-06

- **Status:** DONE for T16 and T22. T15 shared API and T19/T21 gates verified;
  T12 repair is followed by full native CLI/daemon chart evidence.
- **Policy:** `internal/executor/capabilities.go` is an explicit native whitelist,
  shared by CLI preview, executor and daemon. Network-dependent features follow
  executor configuration. Neither empty capabilities nor `FullCapabilities`
  authorizes runtime apply. Implementation-specific restrictions remain blocking
  structured diagnostics before secret persistence and desired-state writes.
- **Real gate:** `make test-helm` now includes the actual daemon/CLI remediation
  test. Final PASS: scenario C 16.23 s, demo 1.97 s; daemon test 19.44 s, CLI
  chart subtest 9.06 s. CLI processes exit while all five regular and two finished
  init containers remain visible; inspect reports real Ingress; a storage guest
  fetches Service port 80; unchanged CLI up succeeds; down removes containers;
  stopped chart stays stopped through actual daemon crash/recovery.
- **Checks:** final `make check`, `git diff --check`, KVM-tagged vet PASS;
  actual daemon gate with `-race -count=1` PASS (20.491 s). No remaining owned
  QEMU/virtiofsd/grillo-netns/pasta processes. No commits were requested.
- **Fixture:** image is assembled by the native builder from the preexisting
  pinned OCI rootfs and cached locally, not a host-container fallback or live
  registry pull. Values contain only synthetic test secrets and never appear
  in CLI output. Existing direct gate proves consumer-only updates/PVC retention.
- **Failures:** first real-daemon attempt exceeded Unix socket path limits and
  exposed a cleanup channel wait bug. Owned leftover VMMs/helpers were stopped
  through their private RPCs. A short private fixture path and closed exit
  channel fixed the harness. Subsequent assertions were corrected to include
  completed init containers and retained stopped-application inventory.
- **Remaining limitation:** production long Unix socket paths still need earlier
  actionable diagnostics; these tests use documented short private paths. No
  second-host, hosted CI, full browser, long fuzz/benchmark or general Job/
  StatefulSet support claim. F3 is not complete until T23 and later product gates.
- **Next task:** T23 web console/secure bridge.

## T23 bridge authentication boundaries — 2026-10-06

- **Task:** T23 — web console and secure bridge (authentication subtask).
- **Status:** IN_PROGRESS; full T23 acceptance remains open.
- **Dependencies verified:** T15 and T22 are DONE with API and real CLI/daemon
  evidence in this tracker; the working tree was clean before this change.
- **Files and contracts changed:** `internal/ui/bridge.go`, bridge/security
  tests and [console usage](../ui.md). Bootstrap exchange is bounded to 4 KiB,
  rejects trailing JSON/unknown fields, expires after five minutes and succeeds
  at most once under concurrency. Listening requires a numeric loopback address;
  supplied mutation Origins must match the HTTP Host and port. Responses disable
  caching and framing. No runtime mutation or secret-reveal surface was added.
- **Decisions/ADRs:** No new dependency or architectural deviation. Missing
  Origin remains allowed for local non-browser clients with the bootstrap token.
- **Tests run:** Linux/amd64: `go test -race ./internal/ui ./internal/cli` PASS;
  `make check` PASS. Regression coverage includes concurrent exchange, expiry,
  malformed/oversized/trailing input, origin mismatch, public-bind refusal and
  canceled loopback serving. Invalid attempts do not consume the valid token.
- **Tests NOT run and why:** Browser smoke and real UI/workload-lifetime gate
  await the full views; KVM gates were not rerun for this handler-only subtask.
  Hosted CI, vulnerability audit and benchmarks were not run.
- **Integration/benchmark evidence:** Local HTTP handler tests only; no new
  browser, hardware or performance claim.
- **Known limitations:** Session lifetime is bridge lifetime; full views,
  browser exec and SSE cursor/gap handling remain pending. Same-user host
  processes are outside this authentication boundary.
- **Next task:** Continue T23 with API-backed views and SSE reconnect/gap handling,
  then documented browser and real workload-lifetime acceptance checks.

## T23 MVP console completion — 2026-10-06

- **Task:** T23 — web console and secure bridge.
- **Status:** DONE (MVP scope; explicit unavailable data is not emulated).
- **Dependencies verified:** T15/T22 DONE; API/daemon and real Helm gate evidence
  precede this work. Prior bridge-authentication edits were preserved.
- **Files and contracts changed:** `internal/observe/view.go` public allowlisted
  DTO; executor projection/tests; API view endpoint/client/tests; daemon wiring;
  UI handlers/assets/security and browser tests; CLI actual-port reporting and
  signal context; dedicated executor exec connections and guest pidfd-based
  exec cancellation with identity/failure tests; `Makefile` browser/KVM UI
  targets; docs and T23 report.
  No config/env/secret contents, secret versions or private guest specs enter
  the projection. Browser exec uses the shared API and explicit replica target.
- **Decisions/ADRs:** No new Go/runtime dependency or architecture deviation.
  Firefox/Node are explicit installed test tools; no downloads or npm packages.
- **Tests run:** `make check` PASS; `go test -race -count=5 ./internal/guest
  ./internal/executor ./internal/ui ./internal/api ./internal/cli ./cmd/grillod` PASS;
  `make test-ui-browser` PASS (real Firefox); `make test-ui` PASS (rebuilt guest,
  freshly built actual CLI bridge/daemon, real KVM chart and browser exec);
  `make test-helm` PASS (full T22 production/CLI/daemon regression gate).
  `node --check` for JS assets/harness and `git diff --check` PASS.
  Initial browser harness expressions failed and were fixed. Initial actual-CLI
  gate failed on SIGINT exit; signal cancellation was added and the full gate
  rerun successfully, not counted as passed before correction. The added real
  cancellation gate subsequently exposed a surviving exec process; dedicated
  connections and guest parent-checked pidfd termination fixed it, with rebuilt
  guest/browser/KVM verification and no unsafe PID-only fallback.
- **Tests NOT run and why:** Hosted CI, other browsers/hosts, long fuzz campaigns,
  benchmarks and vulnerability audit belong to later verification; none claimed.
- **Integration/benchmark evidence:** Linux/amd64 UID 1000, SELinux already
  Permissive; Firefox 157.0/Node 24.18.0; QEMU 10.2.2/virtiofsd 1.14.0.
  Native chart has three microVMs/seven containers. Browser executes a real
  container command with separate output and exit 7; closing browser and actual
  CLI bridge leaves the same live VMM PIDs and successful API exec. A real
  sleeping browser exec is canceled and its container PID is verified gone,
  with follow-up exec succeeding. Fixture
  browser gate separately proves HTML log safety, filtering and SSE gap/resume.
  No test-owned profiles, state directories or processes remained. Artifact
  hashes and exact evidence are in [the T23 report](../experiments/t23-console.md).
- **Known limitations:** Full browser TTY deferred. Original compiler diagnostics
  are not retained; UI points to source planning. Runtime guest log ingestion
  is not implemented by the UI; empty retained spool is explicit, not claimed
  as guest stdout delivery. Guest/container usage, kernel version, PSS/cache and
  CPU percentage are unavailable; config is metadata-only; sessions last for
  bridge lifetime. No TLS/CA, public binding or secret reveal.
- **Next task:** T24 — F3 MVP gate and hardening; F3 is not yet complete.

## T23 PatternFly React follow-up — 2026-10-06

- **Task:** T23 — user-requested console redesign.
- **Status:** DONE (frontend migration; T24/F3 is not complete).
- **Dependencies verified:** Delivered T23/API DTO and T15/T22 real gates;
  npm maintained version/peer/license metadata and explicit user approval.
- **Files and contracts changed:** `web/` React/PatternFly source, pinned npm
  lock/build/client tests/notices; `internal/ui` embedded generated static
  resources, MIME/freshness tests and index; browser harness; Makefile/CI frontend
  prerequisites; plan/README/UI docs/ADR. Two validated explicit daemon CID-base
  flags let the actual KVM gate coexist with the existing review demo; defaults
  are unchanged. API/runtime planning/workload lifetime is unchanged.
- **Decisions/ADRs:** [ADR 0008](../adr/0008-patternfly-react-console.md) records the
  explicit user-requested replacement of the framework-free plan. Formal
  maintainer review pending; no new Go dependencies. No CDN/dev server or runtime
  Node requirement; npm install scripts disabled; upstream notices preserved.
- **Tests run:** `make ui-deps ui-check` PASS (five Node tests, deterministic
  assets); final `make check` PASS; actual Firefox `make test-ui-browser` PASS
  with desktop/390px mobile visibility, local fonts and CSP/runtime-error checks;
  `make test-ui` PASS twice after explicit CID isolation, with freshly built
  CLI/daemon/rebuilt guest and real chart/exec/cancellation/lifetime/recovery;
  `cd web && npm audit --json` PASS (zero known advisories); browser-harness
  `node --check` and `git diff --check` PASS; local Markdown links/fences PASS
  after correcting preexisting `AGENT.md` links to the tracked `AGENTS.md`.
  Initial missing optional-platform
  inspection, actual mobile navigation and CID-collision failures were fixed
  and rerun, not masked as passes. CID validation has positive/negative unit
  tests; no running demo workload was stopped to clear the collision.
- **Tests NOT run and why:** Hosted CI/other browsers/hosts, accessibility audit,
  load/benchmark campaigns, T24/F3 and release packaging are subsequent work.
- **Integration/benchmark evidence:** Firefox 157.0 on Linux/amd64 UID 1000;
  existing SELinux Permissive. Real three-microVM/seven-container chart, real
  output/exit/cancelled guest PID cleanup; unchanged live VMM PIDs after browser
  and bridge closure. Separate fixture validates logs/SSE rendering, not guest
  log ingestion. Raw asset/binary sizes and hashes, failure/retry details are in
  [the follow-up report](../experiments/t23-console.md#patternfly-migration-gate); no benchmark claim.
- **Known limitations:** Original T23 unavailable-data/TTY limits unchanged.
  Explicit CID bases are not a global allocator; concurrent isolated daemons
  need nonconflicting host-wide ranges. Additional locked frontend tooling and
  embedded asset size are deliberate build costs.
- **Next task:** T24 — F3 and hardening, then T27 packaging/build integration.

## T23 — Documentation consolidation after console delivery

- **Status:** DONE (documentation only; T24 remained TODO).
- **Dependencies verified:** T23 delivery records, existing API/CLI/Make targets;
  user approval for documentation cleanup. No runtime task was implemented.
- **Files and contracts changed:** Reader-oriented README/getting-started,
  architecture/testing guides; concise progress with archived history;
  consolidated T23 evidence; current contributor/security/agent guidance.
  Design contracts, task acceptance criteria, ADR status, generated inventories
  and third-party notices remained authoritative and unchanged in meaning.
- **Decisions/ADRs:** Editorial organization only; no architecture change.
- **Tests run:** `python3 /tmp/grillo-check-markdown.py` PASS (44 project Markdown
  files: links/anchors and balanced fences); `git diff --check` PASS; actual
  `bin/grillo --help`/`version` and offline Compose/Helm example plans PASS;
  `make check` PASS on the existing Linux/amd64 development host.
- **Tests NOT run and why:** New KVM/browser/load/security campaigns were not
  required for prose-only changes; dated delivery evidence preserved, not rerun.
- **Integration/benchmark evidence:** Existing [reports](../progress.md#evidence-and-history);
  no new claim.
- **Known limitations:** Product limits remained; cleanup did not complete T24.
- **Next task:** T24 — F3 gate and hardening.

## T24 — Local F3 hardening campaign (2026-10-07)

- **Status:** BLOCKED; no MVP release or T25 authorization.
- **Dependencies verified:** T23 plus fresh native UI/Firefox, Helm, Compose,
  guest, networking, storage and recovery gates.
- **Files and contracts changed:** YAML/DNS/OCI fuzz tests, `make fuzz`/CI smoke,
  lifecycle percentile logging, threat model and [T24 inventory](../experiments/t24-hardening.md).
  Runtime/API contracts and compatibility support states were unchanged.
- **Decisions/ADRs:** No architecture change or ADR acceptance.
- **Tests run:** `make check` twice PASS; vulncheck/npm audit PASS; five-target
  fuzz smoke and 60s/target campaign PASS; fresh focused race PASS; guest,
  storage, executor, F2, Kubernetes, Helm, UI/Firefox, build, bridged/netns gates
  PASS without SKIP; 100 lifecycle cycles/F0 PASS; doctor/diff/Markdown PASS.
  Exact commands/environment/results are preserved in the report.
- **Tests NOT run and why:** Hosted CI and clean external Linux host unavailable;
  ten-minute-per-target fuzz, sustained load, complete memory/network/cold-start
  budgets and redistribution audit remained unverified.
- **Integration/benchmark evidence:** Fedora fc44, SELinux already Permissive;
  100 warm backend cycles and 30 F0 boot/filesystem samples with median/p95.
  No complete application-start/performance guarantee.
- **Known limitations:** At this campaign date, stdout/TTY/detailed usage,
  verifiable installation, artifact enforcement/fresh-key delivery and full
  security/license evidence remained. The subsequent stdout delivery is separate.
- **Next task:** Stay on T24; implement missing MVP contracts and arrange the
  external-host gate, not T25.

## T24 — Bounded guest stdout/stderr ingestion (2026-10-07)

- **Status:** BLOCKED overall; bounded log delivery has real local evidence.
- **Dependencies verified:** T23/prior local T24 campaign; user authorization
  to refine logs/TTY/metrics/artifacts without a second host.
- **Files and contracts changed:** Guest ring/runc stdio/init streaming; additive
  protocol cursor batches; executor/daemon ingestion and redacted diagnostics;
  synchronized spool/gap metadata; API/CLI/Firefox regression and UI loss markers.
  Full limits/commands/failures are in [the report](../experiments/t24-guest-logs.md).
- **Decisions/ADRs:** No new dependency/backend/ADR acceptance.
- **Tests run:** Final `make check`, uncached focused races, real guest/Helm/logs,
  UI/Firefox/native builds/F2/executor/liveness gates PASS without SKIP; Markdown,
  script/diff checks PASS. Initial init-order unit failure was corrected.
- **Tests NOT run and why:** Hosted CI/external host unavailable; no fresh sustained
  load/fuzz or full performance/security/license audit in this bounded delivery.
- **Integration/benchmark evidence:** Fedora fc44, SELinux already Permissive;
  rebuilt guest/CLI/daemon/frontend. No new benchmark claim.
- **Known limitations at delivery:** Lossy ring/polling; incomplete shutdown/host
  retention gaps. TTY, detailed metrics and artifact/fresh-key enforcement were
  open then; the later three-contract delivery supersedes those local blockers.
- **Next task at delivery:** Interactive CLI, metrics/artifacts and remaining T24
  hardening; external-host gate stays open and T25 must not start.

## T24 — Interactive CLI, real metrics and verified boot artifacts

- **Status:** BLOCKED overall; all three local contracts have real delivery evidence.
- **Dependencies verified:** T07/T08/T15/T16/T18/T19/T22/T23, prior T24 log delivery and
  user authorization to implement all three without a second host.
- **Files and contracts changed:** Bounded stdin/resize protocol and Unix upgrade;
  foreground runc PTY/CLI raw restoration, cancellation-aware output; guest/cgroup
  metrics/API/CLI/UI; pinned manifest/private verified snapshots/fresh boot keys;
  sole-reaper network command wait; native terminal/metrics/Firefox/build and
  negative/race regressions. See [report](../experiments/t24-interactive-metrics-artifacts.md).
- **Decisions/ADRs:** [ADR 0009](../adr/0009-verified-boot-and-private-key-overlay.md)
  Proposed, implemented experimentally; no new dependency or ADR acceptance.
- **Tests run:** `make check`, uncached focused races, `make vulncheck`,
  `make test-t07 test-helm test-ui test-ui-browser test-builder-kvm test-f2
  test-executor` PASS without SKIP. Actual host PTY verifies CLI stdin/SIGWINCH,
  mode restoration on exit/SIGINT; real guest cgroups reach Firefox. Unit/fake
  tests separately prove mutation/launch-order/key contracts. Earlier failed
  gates and corrections are preserved in the report. Markdown links/fences,
  shell/JavaScript syntax and diff checks PASS; no QEMU/grillod/pasta leftovers.
- **Tests NOT run and why:** Hosted CI and clean external host unavailable;
  no new sustained load/fuzz/full-budget or redistribution/license campaign.
- **Integration/benchmark evidence:** Existing Fedora fc44/KVM host, SELinux
  already Permissive; rebuilt guest/CLI/daemon/frontend. No new benchmark claim;
  new copy/hash startup costs still need full-budget measurements.
- **Known limitations:** Full browser TTY, PSS/cache/CPU percentages, original
  diagnostics persistence and log shutdown/host-retention gaps remain absent.
  Local manifests are integrity expectations, not publisher authentication or
  signed releases. Clean-host/release installation/quota/load/provenance gates
  remain open; owning-UID/compromised-guest guarantees are not expanded.
- **Next task:** T24 full-budget/quota/load and release provenance/installability
  review; arrange the external-host gate separately. Do not advance to T25.

## D0 — Packaging-ready runtime layout, first bounded preparation

- **Status:** BLOCKED overall; layout/build identity/doctor preparation implemented.
- **Dependencies verified:** T07/T08/T15/T16/T21 and prior T24 verified-boot delivery
  exist; T24 remains BLOCKED. User requested starting the distribution track;
  this does not waive the `T24 → D0` acceptance dependency.
- **Files and contracts changed:** `internal/runtimeassets`, `internal/buildinfo`,
  CLI/daemon/Helm discovery, build metadata injection, doctor JSON/verbose and
  installed-file checks; tests and development setup. See
  [bounded evidence](../experiments/d0-runtime-layout.md).
- **Decisions/ADRs:** Existing pinned Helm executable and ADR 0009 preserved;
  no SDK, new dependency, signature/support claim or ADR acceptance.
- **Tests run:** Focused Go tests PASS; first `make check` failed new-test
  formatting, corrected; second `make check` PASS (frontend, format, vet, tests,
  script tests, races, build and module audit). Moved/read-only host prefix,
  spaces and `/tmp` CWD: version and Helm plan PASS; doctor correctly failed
  absent guest assets. Final focused tests/races PASS. Broad `gofmt -l .` failed
  on ignored upstream toolchain syntax-error fixtures; established formatting
  scope passes. Markdown file links/fences and `git diff --check` PASS.
- **Tests NOT run and why:** No KVM workload/daemon startup or clean-host gate:
  the fixture-free portable guest/inventory was not implemented in this delivery.
  No release, package, upgrade/uninstall, secret-scanning, provenance or benchmark campaign.
- **Integration/benchmark evidence:** Existing Linux/amd64 development host,
  SELinux already Permissive; only host-layout/offline rendering smoke, no
  hardware integration or benchmark claim.
- **Known limitations at delivery:** Guest manifests still used build-time paths;
  ABI validation, helper integrity/ranges, full doctor capability/distro remediation
  and runtime guest without fixtures/embedded key remained open. No packaged release.
- **Next task at delivery:** Continue D0 with portable manifest/ABI and fixture-free
  guest; rebuild and verify moved-prefix real Compose/Helm KVM. T24 external/full-budget
  and release-provenance gates remain separate blockers.
