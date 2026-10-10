# Compatibility matrix

This file summarizes what each frontend maps faithfully, degrades, or rejects.
The validator registries now generate [Markdown](compatibility-registry.md)
and [JSON](compatibility-registry.json) inventories; ordinary tests reject drift.
Regenerate with `go run ./scripts/compatibility -output docs`. Those inventories
classify **compilation**, not stable runtime behavior or an exhaustive field
inventory. Fixture links identify regression suites, not per-field hardware proof.
This narrative records runtime evidence and restrictions separately. Until real
runtime integration proves a behavior, treat it as experimental.

This is current subset behavior, not a supported release promise. See
[getting started](getting-started.md) for prerequisites and
[progress](progress.md) for dated runtime evidence. Compiler acceptance alone
does not imply host feasibility.

The [D0 installed-runtime gate](experiments/d0-installed-runtime.md) validates
standalone Compose/Helm examples and embedded UI lifetime from a moved/read-only
prefix on the recorded Fedora development host. It does not establish a clean
or supported host, SELinux Enforcing networking, release installation or general
dependency ranges. `doctor` reports the experimental support boundary explicitly.
The user also reports second-host Fedora 44 Compose/Helm HTTP and UI functionality
under Permissive; see [host evidence/support boundaries](host-support.md) for the
partial scope, missing checks and Enforcing blocker. No distribution support
promise follows from these functional observations.

## Compose (F2 subset)

| Concept | State | Notes |
| --- | --- | --- |
| `image`, `command`, `entrypoint`, `environment`, `env_file` | Supported | `.env` then host environment; `environment` overrides `env_file` |
| `${VAR}` interpolation | Supported | default/required/alternative forms; `$$` escape |
| `ports`, `expose` | Supported | long/short syntax; TCP |
| `volumes` (named, ephemeral, bind) | Supported | project-relative binds resolve against the Compose file |
| `healthcheck` | Runtime semantics differ | compiler accepts it as a liveness probe; runtime restart-on-unhealthy is not faithful Compose health behavior; `NONE` disables |
| `depends_on` | Degraded | startup ordering only, never readiness |
| `restart`, resource limits, `deploy.replicas` | Supported | |
| named `networks` | Supported (one topology) | multiple distinct topologies are rejected (`compose.multi_network`) |
| `build` | Supported via the native builder | not yet run automatically from `up` |
| `secrets`, `configs`, `profiles`, `extends`, `devices`, `privileged`, `network_mode: host` | Rejected | structured diagnostics |

## Kubernetes (MVP)

| Kind / field | State | Notes |
| --- | --- | --- |
| `Pod`, `Deployment` | Supported | multi-container, init containers, sidecars |
| `Deployment.spec.strategy` | Degraded | `RollingUpdate` (the default) is not implemented; Grillo uses Recreate and requires `--allow-degraded=kubernetes.rollout_strategy` |
| `Service` (ClusterIP, headless) | Runtime and CLI-proven | Normal VIPs, ready headless Pod addresses/SRV, named targetPort and replica balancing pass real KVM; TCP only |
| `Service` NodePort/LoadBalancer/ExternalName | Rejected | never silently mapped to ClusterIP |
| `Ingress` (`Exact`, `Prefix`) | Runtime and CLI-proven | Published localhost fallback and real Service routing; segment-aware paths; `ImplementationSpecific` and TLS are rejected |
| `ConfigMap` (`data`, `binaryData`) | Supported | env and `envFrom` references |
| `Secret` (`data`, `stringData`, `Opaque`) | Supported | values never enter the public IR or plan diff |
| `PersistentVolumeClaim` | Supported | `ReadWriteOnce`, Filesystem only |
| probes (`exec`, `httpGet`, `tcpSocket`), resources, security context | Supported | |
| `DaemonSet` | Degraded | one local replica, with consent |
| `ServiceAccount`, `PodDisruptionBudget` | Validate only | metadata only; no credential or eviction behavior |
| `StatefulSet`, `Job`, `CronJob` | Rejected | scheduled for F4 |
| `NetworkPolicy` | Rejected | reduced policy model not implemented |
| CRDs, Operators, webhooks, cluster RBAC | Rejected | Tier 3 |
| `hostNetwork`, `hostPID`, `hostIPC`, `privileged`, `hostPath` | Rejected | host access is outside the model |
| `configMap`/`secret`/`projected`/`downwardAPI` volumes | Rejected | use env references; projected volumes are not implemented |
| `nodeSelector`, affinity, tolerations | Rejected | single-node scheduling only |

## Helm

Local charts render with the explicitly provisioned
Helm v4.2.2 executable and delegates manifests to the Kubernetes compiler.
CLI `plan`/`up` accept chart directories, `Chart.yaml`, or exact-version OCI
charts, an optional `--release` (default: chart basename), `--namespace default`,
ordered repeated `-f` values files, and code-specific `--allow-degraded` consent. Values paths are
relative to the invoking working directory; repeated `-f` requires a positional
chart. See the [example](../examples/helm/README.md).

`plan` never stores secrets; offline plans use an `unresolved-offline` version
marker rather than content-derived fingerprints and do not describe live
secret-update diffs. `up` validates before persisting random-versioned private
references. Dated [renderer](experiments/t21-helm-renderer.md) and
[runtime/CLI chart](experiments/t22-helm-gate.md) reports distinguish compiler/API
fixtures from real workload evidence. The chart gate uses a preexisting/local
OCI fixture, not a live registry pull.

CLI, executor and daemon share an explicit implementation whitelist. Routes and
host ports require bridged configuration; privileged, StatefulSet/Job and unknown
features remain unavailable. No `FullCapabilities` bypass is used. A
structurally valid but capability-blocked offline plan can print resource/action
counts while keeping a nonzero exit, text `BLOCKED` and JSON `applicable: false`.
JSON preserves structured support diagnostics and consequences; frontend or
structural/security errors do not become actionable plans. Unsupported `up` remains blocked before daemon startup and secret persistence.
Offline applicability is implementation compatibility, not host feasibility.
The bounded subset is not arbitrary chart compatibility or completion of the
product hardening gate. The [console report](experiments/t23-console.md) separates
browser fixtures from native CLI/daemon/KVM evidence; UI delivery does not expand
workload semantics. T24 also delivers [interactive CLI stdin/TTY/resize, real
guest/cgroup counters and verified boot snapshots](experiments/t24-interactive-metrics-artifacts.md),
without expanding source-manifest compatibility. Browser full TTY remains absent;
T24/F3 remains blocked. T24 now separately verifies [bounded guest stdout/stderr
ingestion](experiments/t24-guest-logs.md); it does not complete the F3 gate.

OCI charts require exact SemVer via `--version`. Missing cache entries require
explicit `--fetch-chart`; cached inputs never fetch or refresh automatically.
An optional `--chart-digest sha256:...` supplies a trusted manifest pin; without
it, first HTTPS resolution is trust-on-first-use. Private bounded records verify
manifest/config/chart hashes, sizes, metadata and safe archives before publication
and on every reuse. Corruption and concurrent pin conflicts block, not repair.
Fetch consent permits cache writes even for `plan`, but not runtime state or
secret-store writes. Default transport refuses HTTP, including redirect and
bearer-token downgrades, and does not inherit proxy credentials.

The subset rejects dependencies/subcharts (including locked dependencies),
additional OCI layers/indexes, provenance signatures, CRDs, values schemas,
hooks, lookup, DNS templates, dynamic `tpl`, and known nondeterministic template
functions. Lockfiles are never rewritten; no tool or dependency installation is
performed. Private-registry credentials are not exposed through the CLI.
See [ADR 0007](adr/0007-controlled-helm-renderer.md) for limits and the
[T21 acceptance report](experiments/t21-helm-renderer.md) for evidence.

## How to read a diagnostic

Every diagnostic carries a `code`, `severity`, `compatibility` state, a source
span, and a `consequence`. `plan` and `up` print them; a `Degraded` diagnostic
becomes an error unless its code is passed to `--allow-degraded=CODE`.
