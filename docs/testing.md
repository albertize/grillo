# Testing and development

[README](../README.md) | [Contributing](../CONTRIBUTING.md) | [Progress](progress.md)

Use the repository's actual Make targets. A unit/fake-backend test proves its
contract, not hardware execution. A skipped hardware check is not a passed gate.

## Routine checks

Go 1.27.2 (`.go-version`), Make, Node 24.18.0/npm 11.16.0 and a C compiler for Linux race tests:

```sh
make ui-deps          # explicit npm integrity-lock installation, scripts disabled
make check
```

`check` builds/tests/verifies frontend assets, checks Go formatting, runs vet,
unit/race and script tests, builds host/agent binaries, verifies modules and
checks tidy drift. It does not download frontend packages automatically. Build
outputs and generated assets are ignored; a source checkout needs frontend
preparation before compiling packages that embed the console.

For focused work:

```sh
make ui-check
make fmt
make vet
make test
make race
make test-scripts
```

Go tests under their ordinary tags need no KVM. See [frontend development](../web/README.md)
for pure client tests/build checks and [bootstrap tests](../scripts/README.md)
for installer fixture behavior.

## Real browser and hardware gates

Prepare the host/guest as described in [getting started](getting-started.md).
Browser tests use installed Firefox with WebDriver BiDi and Node's built-in
WebSocket; they do not install a driver or download a browser.

| Target | Evidence |
| --- | --- |
| `make test-ui-browser` | Real Firefox with deterministic API fixtures: UI, CSP, HTML safety, filters, SSE and desktop/mobile navigation; no hardware claim |
| `make test-ui` | Actual CLI bridge/daemon, rebuilt guest, native Helm chart, guest stdout/stderr via API/CLI/Firefox with safe HTML/filtering, real exec/cancellation and unchanged VMM identity after UI closure |
| `make test-executor` | Real apply, container exec, teardown and liveness restart |
| `make test-f2` | Compose DNS, managed storage, idempotent apply/down and recovery |
| `make test-k8s` | Compiled multi-container Kubernetes Pod |
| `make test-helm` | Direct-runtime scenario C plus actual CLI/daemon lifecycle, routing, readiness, storage and recovery |
| `make test-builder-kvm` | Native Dockerfile RUN/multi-stage work inside build guests |
| `make test-f0` | Feasibility topology, isolation, live binds, storage and recorded measurements |

Component gates include `test-t07`, `test-t08`, `test-t10`, `test-bridged` and
`test-netns`; inspect [Makefile](../Makefile) for prerequisites and commands.
`test-builder` explicitly tests the opt-in Podman backend. `test-netreg` pulls a
pinned image from a real registry and requires network access.

Do not run every hardware gate blindly: they create real VMs/helpers and consume
memory/devices. Rebuild the guest and the binaries actually under test. Use
operation-owned resources, never clean up another runtime's matching fixture.
Concurrent isolated daemons need nonconflicting host-wide guest CID ranges;
`test-ui` and the daemon remediation gate set explicit test ranges, not a global
reservation scheme. A real collision or host-policy failure must remain visible.

Inspect the test log for SKIP as well as the exit code. Missing KVM, artifacts,
helpers, browser or network may produce a documented skip/blocker, not evidence
that the behavior works. Passing browser markers do not override failed process
exit, timeout or cleanup.

## Audits and documentation

```sh
make vulncheck             # explicit execution/download of pinned Go audit tool
(cd web && npm audit)       # explicit current npm advisory check
```

Neither replaces a security review, publisher-authenticity verification or the
unfinished T24 product gate. Run bounded parser/protocol smoke separately:

```sh
make fuzz                    # five targets, 5 seconds each, two workers
make fuzz FUZZTIME=60s        # modest local campaign
make fuzz FUZZTIME=10m        # opt-in longer campaign; not yet recorded as run
```

The CI workflow includes the smoke, not KVM. Targets cover guest framing/messages,
Kubernetes YAML, DNS responses and tar/gzip extraction. Harness input/output
bounds do not prove complete parser resource safety. Failures must remain visible;
retain failing Go corpora as regression fixtures after reviewing sensitive data.
See [current CLI/metrics/boot-artifact evidence](experiments/t24-interactive-metrics-artifacts.md),
[T24 campaign results and blockers](experiments/t24-hardening.md) and the
[threat model](security.md). Longer fuzz/load/benchmark campaigns and clean-host
verification remain required by the [delivery plan](../IMPLEMENTATION_PLAN.md).

For documentation changes, check relative links and anchors, balanced fences,
examples against actual CLI help/source, task/ADR status consistency, and
`git diff --check`. Generated compatibility inventories must remain unchanged
unless regenerated from validators with `go run ./scripts/compatibility -output docs`.

## T24 bounded local campaign and acceptance preparation

With already prepared artifacts and an explicitly trusted Helm path/digest:

```sh
make test-t24-admission
make test-cache-load test-license-inventory
make bench-t24-network
make test-t24-resources HELM_BINARY=/absolute/path/to/helm HELM_SHA256=reviewed_sha256
```

Resource collection runs 30 installed-runtime tests, each with a fresh private
cache and a warm-cache restart, readiness confirmed by actual HTTP. It samples
owned-process RSS, CPU tick deltas at idle, and logical/allocated test-directory
bytes. RSS peaks are sampled, not instantaneous maxima or PSS. Fresh caches may
pull from the registry: this is **not** page-cache-cold or guaranteed offline.
The target stops at the first failed iteration (including cleanup) and requires
all 30 samples; never aggregate a failed batch into a passed budget gate.
Registry rate limits are blockers, not permission to substitute a mock.

Explicit live provisioning (`make prepare-t24-fixture T24_FIXTURE_OUT=/new/output`)
creates a digest-verified graph for repeatability. Pass `T24_SEED_CAS=/fixture/cas`
to the resource target to transfer only verified content into each new private
cache. Phases then say `seeded-cas-*`: never label these fresh online pulls.
`make test-t24-resource-budget T24_SEED_CAS=/fixture/cas` retains a unique complete
30-repeat receipt and checks candidate local readiness/RSS/disk/idle-tick bounds.
These are not portable product SLOs or full guest-load/peak measurements.

`make test-t24-volume-quota` rebuilds the guest and requires real KVM plus reviewed
mke2fs 1.47.3; it proves ext4 ENOSPC/retry/persistence through the executor.
`make test-t24-volume-load` runs 30 block-storage race repetitions, including a
child-process lock/cancel case. Optional formatter unit tests can SKIP on hosts
without that pin; the hardware target instead fails on absent prerequisites.
See [ADR 0014](adr/0014-fixed-size-block-volume-quotas.md) for opt-in scope:
legacy/bind data and native RUN workspaces are not bounded by this proof.
Native host-write budget regressions run in `go test ./internal/build ./internal/oci` and `make check`; [evidence](experiments/t24-native-host-write-budgets.md)
covers admission before writes, multi-stage/overwrite/import accounting,
cancellation, rooted paths, staging limits and failed publication. These tests
do not replace `make test-builder-kvm` or prove a quota on guest RUN writes.
`go test ./internal/guestproto ./internal/guest ./internal/build` covers the
[agent export prerequisite](experiments/t24-agent-owned-build-export.md), including
actual authenticated in-memory framing, non-reading-peer cancellation, failed
RUN cleanup and private bounded snapshot extraction. The opt-in integration adds
software coverage; rebuilt guest/KVM execution is checked separately below.

`make test-t24-build-workspace` checks accessible KVM before rebuilding the guest,
then runs the opt-in native block test. It fails on missing hardware/artifacts or
formatter rather than skipping. The real gate checks ext4 ENOSPC/retry,
COPY/RUN coherence, metadata, owned cleanup, RUN cancellation, owned VMM SIGKILL
and excessive output. Faults must preserve the prior image reference and allow
a fresh successful build afterward.
`make test-t24-build-workspace-load` first rebuilds/runs that gate, then performs
ten sequential repetitions with the race detector. This does not establish
persistent-volume crash repair, daemon-crash recovery or whole-host saturation.
To evaluate an explicitly reviewed runtime guest instead of the legacy fixture,
set `GRILLO_TEST_BUILD_GUEST_SELECTION` to its selection file. The test verifies
the portable manifest and uses boot snapshots with fresh keys; invalid explicit
selection fails without falling back. The Make target still rebuilds the legacy
fixture, but its hash is not evidence for the selected portable guest; retain the
selected guest's inventory/hashes separately. See the
[candidate report](experiments/t24-external-candidates.md), including a retained
combined-load timeout and the separate isolated pass.
The [reaper investigation](experiments/t24-reaper-timeout.md) fixes a confirmed
lost exit delivery and records subsequent old/new-guest load runs, including
separate DNS bind failures. Failed workspace gates now retain private phase
metadata (64 KiB total) and the first 64 KiB of serial/helper logs per owned guest
before cleanup, with at most 32 unique guest handles. Boot keys and sandbox state
are excluded; retained serial logs remain untrusted private diagnostic material.
`go test -tags kvm -run '^$' ./internal/build` compiles it without running it.
[Hardware evidence](experiments/t24-block-workspace-kvm.md) and the
[fault/load continuation](experiments/t24-workspace-faults-advisory-review.md)
record actual passes, earlier failures and remaining gates.
The read-only `scripts/source-inventory.py` (Python 3.11+) hashes deliberately
provisioned sources/crates, not legal clearance or a fetch/build step.

Network benchmarks exercise actual loopback UDP DNS and TCP publication with
response validation, two parallel workers and 30 repetitions. They do not prove
guest-network throughput or chart/application scalability. See the
[initial campaign](experiments/t24-autonomous-local-campaign.md),
[continuation evidence](experiments/t24-pinned-cache-block-quota-sources.md) and
[acceptance checklist](t24-acceptance.md) for scope and remaining gates.

## Recording evidence

Record command, environment, result, skips/blockers and artifact versions/hashes.
Keep current status in [progress](progress.md), reproducible acceptance reports in
`docs/experiments/`, and historical implementation notes in
[the implementation log](history/implementation-log.md). Completed runtime review
findings are in [the review archive](history/runtime-review.md).

Retain failures that explain a correction or invalidate a gate; do not turn a
successful retry into a claim the first attempt passed. Benchmark comparisons
need matched hardware, artifacts, cache/load conditions and sample counts. A test
duration or raw bundle size alone is not a performance benchmark. Never store
live secrets, bootstrap URLs, private logs or generated VM artifacts in reports.
