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
