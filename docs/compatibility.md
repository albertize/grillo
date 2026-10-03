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

## How to read a diagnostic

Every diagnostic carries a `code`, `severity`, `compatibility` state, a source
span, and a `consequence`. `plan` and `up` print them; a `Degraded` diagnostic
becomes an error unless its code is passed to `--allow-degraded=CODE`.
