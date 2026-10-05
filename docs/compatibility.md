# Compatibility matrix

This file summarizes what each frontend maps faithfully, degrades, or rejects.
It is maintained alongside the in-code registries
(`internal/frontend/compose/support.go`, `internal/frontend/kubernetes/support.go`).
Until real runtime integration proves a behavior, treat it as experimental.

## Compose (F2 subset)

| Concept | State | Notes |
| --- | --- | --- |
| `image`, `command`, `entrypoint`, `environment`, `env_file` | Supported | `.env` then host environment; `environment` overrides `env_file` |
| `${VAR}` interpolation | Supported | default/required/alternative forms; `$$` escape |
| `ports`, `expose` | Supported | long/short syntax; TCP |
| `volumes` (named, ephemeral, bind) | Supported | project-relative binds resolve against the Compose file |
| `healthcheck` | Supported | mapped to a liveness probe; `NONE` disables |
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
| `Service` (ClusterIP, headless) | Supported | TCP; readiness endpoints |
| `Service` NodePort/LoadBalancer/ExternalName | Rejected | never silently mapped to ClusterIP |
| `Ingress` (`Exact`, `Prefix`) | Supported | `ImplementationSpecific` is rejected |
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

## Helm (T21 rendering/CLI subset complete)

`internal/frontend/helm` renders local charts with the explicitly provisioned
Helm v4.2.2 executable and delegates manifests to the Kubernetes compiler.
CLI `plan`/`up` accept chart directories, `Chart.yaml`, or exact-version OCI
charts, an optional `--release` (default: chart basename), `--namespace default`,
ordered repeated `-f` values files, and code-specific `--allow-degraded` consent. Values paths are
relative to the invoking working directory; repeated `-f` requires a positional
chart. See the [example](../examples/helm/README.md).

Real-renderer/CLI tests verify ordered values, stable creation plans, secret
separation, redacted failures and rejection before daemon startup. `plan` never
stores secrets; offline plans use an `unresolved-offline` version marker rather
than content-derived fingerprints and do not describe live secret-update diffs.
`up` persists random-versioned private references only after validation. Apply
wiring is tested against the API test double; **T22's real Helm workload gate
has not been run**, so this is not an end-to-end Helm runtime support claim.

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
