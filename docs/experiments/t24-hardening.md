# T24 — F3 gate and hardening

Date: 2026-10-07. Status: **BLOCKED**, not an MVP release gate pass.
Dependency T23 is verified by [console evidence](t23-console.md) and the fresh
local gates below. This bounded delivery adds repeatable fuzz smoke/campaigns,
phase measurements and the [implementation threat model](../security.md).
No T25 work, release packaging or ADR acceptance is implied.
The subsequent [guest stdout/stderr delivery](t24-guest-logs.md) supersedes this
snapshot's log-producer blocker. The later [interactive CLI/metrics/artifact
delivery](t24-interactive-metrics-artifacts.md) supersedes those implementation
blockers with local evidence; external-host, release and complete-budget gates
remain open. The inventory below is retained as dated historical evidence.

## Environment and artifact identity

Source baseline: `779733c`; measurements include this delivery's uncommitted
changes. Linux/amd64, UID 1000, Fedora fc44 development host, kernel
`7.2.8-200.fc44.x86_64`, Intel Core Ultra 7 155H (22 logical CPUs), about 15 GiB
RAM, NVMe-backed btrfs. VT-x/KVM available. Go `go1.26.8-X:nodwarf5`, Node
24.18.0/npm 11.16.0, QEMU 10.2.2, virtiofsd 1.14.0, pasta
`0^20261002.gcba3570-1.fc44`, Firefox 157.0.
SELinux was already **Permissive**; no host policy or privilege changes.
There were no preexisting QEMU/grillod/pasta workloads during this campaign.

Guest/kernel inputs were local; targets rebuilt agents/initramfs rather than
relying on old binaries. The kernel SHA-256 was
`550b5916433e758e814039f8324a9f1cdfdd2b3b8e4a1e44cc6836cdc92d5942`.
The first application campaign initramfs SHA-256 was
`097e25e5779739351b9a6585506544b2d7e97ef366bd7f169489ee4532e39214`.
The lifecycle gate rebuilt it to
`8196da7b59f527205798b27489d4abc015d2d2131e74cb94143a3631610e2d41`.
Timestamped rebuilds can change the archive digest.
The development manifest includes agent, runc, kernel and initramfs; it is not
a complete release SBOM or proof of launch-time verification.

## Executed verification

| Command | Result and scope |
| --- | --- |
| `make check` (before and after code changes) | PASS: frontend tests/deterministic assets, fmt/vet, unit/race, script negative/ownership fixtures, host/agent builds, module verify/tidy |
| `make vulncheck` | PASS: pinned govulncheck v1.8.0, no vulnerabilities found; advisory snapshot, not an audit |
| `(cd web && npm audit --json)` | PASS: zero known advisories |
| `make fuzz` | PASS: five targets, 5 seconds each, two workers |
| `make fuzz FUZZTIME=60s` | PASS: five targets, 60 seconds each, two workers; scope/limits below |
| `go test -race -count=1 ./internal/frontend/kubernetes ./internal/network ./internal/oci ./internal/guestproto` | PASS: uncached race regression after fuzz additions |
| `make test-t07 test-t10 test-executor test-f2 test-k8s test-helm test-ui test-ui-browser test-builder-kvm test-bridged test-netns` | PASS, no SKIP: fresh real guest/runtime/CLI/Firefox component/application gates |
| `GRILLO_KVM_CYCLES=100 make test-t08` | PASS: 100 cycles, guest zombie check, no surviving tracked VMMs or sandbox directories |
| `make test-f0` | PASS: two-VM rootless topology, egress/publish/negative isolation, live sharing/export boundary, ext4 persistence, warm measurements; no SKIP |
| `./bin/grillo doctor` | PASS exit 0 on this host; read-only readiness, not installer acceptance |
| `python3 /tmp/grillo-t24/check_markdown.py` | PASS: 46 project Markdown files, local links/anchors and balanced fences |
| `git diff --check` | PASS; final process inspection found no QEMU/grillod/pasta leftovers |

Routine checks can use Go's result cache; the focused `-count=1` race run and
hardware gates were fresh. Shell-test ERROR lines are expected rejection fixtures,
not hidden command failures. Firefox's standalone version probe emitted an
unrelated restorecon missing-path warning, then reported 157.0. No automatic
remediation was performed. The initial license inventory script assumed every
module graph entry had a local directory and failed; the corrected inspection
explicitly identifies unmaterialized graph-only modules below.

Hosted CI was **not executed**. Its workflow now runs `make fuzz` after existing
checks/audit. The local smoke does not prove a hosted run.

## A–H mapping (not all requirements satisfied)

| Scenario | Fresh evidence | Remaining scope |
| --- | --- | --- |
| A: Pod | `test-t07`, `test-k8s`, Helm init/sidecar/config/secret subtest: shared localhost, distinct roots/PIDs, init failure blocks apps | Complete stdout/TTY requirements remain open |
| B: Compose | `test-f2`: DNS, managed persistence, idempotency, down/recovery | Fixture is web/worker, not full web/API/Postgres §14 B; no clean external-host checkout evidence |
| C: Helm | `test-helm`: multi-container/two-replica chart, Service/Ingress, config/secret, readiness, PVC, CLI/daemon recovery | Supported subset only |
| D: Updates | Helm consumer-only config/secret replacement; routine planner/reconciler route-only/idempotency/scaling tests | No claim that every D path was newly tested on hardware |
| E: Failures | Routine state/registry/backend/protocol failure tests; KVM init failure, liveness and daemon recovery | Complete sustained fault/load matrix remains open |
| F: Security | Routine adversarial parser/path/auth/redaction tests; Firefox HTML/CSP; F0/bridged/netns negative isolation | Not an independent audit or full malicious-guest campaign |
| G: Binds | `test-t10`, `test-f0`: live host/guest sharing, readonly, export boundary, persistence/rename measurements | Host-originated notifications remain degraded; polling required |
| H: Lifetime | Actual CLI/UI/daemon Helm gate, unchanged workload identity after UI closure, real exec cancellation and daemon restart recovery | Recovery restarts VMs; no uninterrupted adoption guarantee |

## Fuzz scope and reproduction

`make fuzz` selects one Go fuzz target per process, fails on any command failure,
and limits workers to two by default. Use `make fuzz FUZZTIME=10m` for a longer
local campaign; that 10-minute-per-target command has **not** been run here.

| Target | 60-second campaign executions |
| --- | ---: |
| `FuzzReadFrame` | 2,853,510 |
| `FuzzReadMessage` | 2,617,352 |
| `FuzzCompile` (Kubernetes YAML) | 212,720 |
| `FuzzDNSRespond` | 800,142 |
| `FuzzUnpackLayer` (tar/gzip) | 402,224 |

No panic/crash or failing corpus was reported. Execution counters sometimes
stalled during the campaign; exit success alone is not evidence of a per-input
latency/resource bound. No long-duration adversarial exhaustion claim is made.
Targets seed valid and malformed inputs; YAML inputs are capped at 64 KiB, DNS
at 4096 B, compressed/uncompressed layer inputs at 64 KiB with extraction limits
of 64 files/64 KiB written. OCI fuzz checks an outside sentinel and sibling entry
count; that oracle is not a proof of containment against every filesystem race.
The DNS harness has no serving goroutine/external forwarder; YAML has no runtime
effects. Go stores interesting mutations in its local cache, not committed
artifacts. Compose/Helm/API parsing still need additional campaign coverage.

## Measurements

Warm local artifacts; no pulls, image unpack, application containers or readiness
in the backend-cycle measurement. Each phase sample completed; 100/100 cycles,
85.869 seconds total, about 859 ms/cycle including inspections/idempotency checks.
Nearest-rank median/p95 are logged by the existing lifecycle hardware test:

| Backend phase (100 samples) | Median | p95 |
| --- | ---: | ---: |
| Create metadata | 80 µs | 250 µs |
| Start through authenticated agent | 806.620 ms | 813.660 ms |
| Stop and delete | 47.452 ms | 57.535 ms |

This gives a local warm baseline, not a <1s application-start guarantee or a
regression threshold. CPU governor/background load were not controlled.

F0's separate feasibility probe measured 30 warm boots, zero failures:
median 605.220 ms, p95 605.996 ms. Different harness/path: do not directly compare
that to the production-backend Start phase.
For 128 files × 4096 B, 30 warm samples per phase:

| Filesystem phase | Host median / p95 (µs) | virtiofs median / p95 (µs) | Guest ext4 median / p95 (µs) |
| --- | ---: | ---: | ---: |
| chmod | 239 / 593 | 69611 / 73505 | 382 / 447 |
| read | 734 / 1596 | 75390 / 77900 | 1350 / 1917 |
| remove | 394 / 963 | 40594 / 42095 | 1638 / 1797 |
| rename | 663 / 1678 | 100125 / 102220 | 1605 / 1683 |
| write/fsync | 1317 / 3770 | 75473 / 80864 | 48987 / 50858 |

These are fixed feasibility operations, not general throughput benchmarks.
Full cold/warm pull/unpack/container/application timing, idle CPU/RSS/PSS and
guest/container accounting, DNS/proxy throughput/latency and post-cleanup disk
budgets remain **BLOCKED**. No Kubernetes comparative benchmark was run.

## Dependency/license inspection

`go mod verify`, module graph and tidy checks passed. Materialized Go sources
carry YAML's MIT/Apache notices and x/net, x/sys, x/term BSD-style licenses.
x/crypto and x/text appear in the module graph but were not materialized during
this inventory; no complete transitive source-license inspection is claimed.
Frontend builds collect upstream notices for installed locked packages and
committed Red Hat font/Font Awesome notices; optional other-platform packages
are not installed. npm audit covers advisory data, not license obligations.

Fedora RPM metadata was inspected for QEMU (mixed upstream licenses including
GPL/LGPL), virtiofsd (Apache/BSD), passt (GPL/BSD), iproute, nftables and Firefox.
Kernel license inputs exist in the pinned source tree. Guest runc/BusyBox and
any distributed kernel/tool binaries retain source/notice obligations; no
redistribution bundle or complete SBOM/license compliance gate is claimed.

## Specification §28 traceability and blockers

| MVP requirement | Evidence or explicit blocker |
| --- | --- |
| Compose images/env/ports/volumes/healthchecks | Compiler tests, F2, executor liveness and Helm publication gates; experimental subset |
| Helm Deployment/Pod/Service/config/secret/PVC/basic Ingress; one namespace | Fresh scenario C, compiler registry; non-default namespaces rejected |
| One selected VMM, one microVM per Pod, minimal agent | Fresh A/C and 100 cycles; QEMU/virtiofsd |
| OCI pull/cache | Routine OCI contracts and prior T09 live pull; new live-registry gate not rerun |
| Exec/logs/signals/stop/restart | Exec/cancellation/stop/restart verified; **BLOCKED: guest stdout ingestion and full interactive stdin/TTY absent** |
| Addresses/DNS/Service routing | Fresh bridged/netns/Helm/F0 |
| Managed/bind volumes | Fresh F2/C/G; watch degradation explicit |
| Startup/readiness/liveness | Routine threshold/startup tests, fresh readiness endpoint/liveness gates |
| Local reconciler/state/recovery | Fresh executor/F2/daemon recovery plus routine fault fixtures |
| CLI MVP | Real plan/up/down/ps/inspect/exec/UI; **BLOCKED: full shell/TTY, complete resource notation and stdout logs** |
| UI overview/Pod/logs/usage/events/routes | Fresh Firefox/native UI; **BLOCKED: actual guest stdout and detailed guest/container usage unavailable** |
| Verifiable install/artifacts/security/compatibility | Source setup/bootstrap fixtures and generated registry exist; **BLOCKED: clean external-host install, complete artifact enforcement/fresh per-boot key delivery, release provenance/licenses, remaining security/load review** |

No clean Linux host outside this development environment was available to this
session. Reusing this host, changing XDG paths, or passing Ubuntu-hosted unit CI
would not satisfy that real-runtime prerequisite. Fedora fc44 is the only
revalidated distribution; tested Permissive policy does not pass Enforcing.
A verified private reporting channel/support process also needs maintainer input
before security-sensitive release claims (see [policy](../../SECURITY.md)).

Next: remain on **T24**, implement missing MVP runtime/observability contracts,
complete performance/security evidence, and arrange the clean external-host
acceptance run. Do not begin T25 or mark F3 DONE while these blockers remain.
