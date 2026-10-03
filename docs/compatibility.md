# Compatibility Status

## No implemented runtime support yet

Grillo is in the design stage. **Nothing in the table below is currently a supported executable feature.** The specification defines targets; the implementation plan defines how they must be proven.

Do not treat parser acceptance, a fake backend, or an illustrative command as proof of runtime compatibility.

## Planned scope

| Area | Initial target | Later or explicitly limited behavior | Current status |
|---|---|---|---|
| Host | Linux/amd64, KVM, rootless operation | Other architectures after validation; macOS/Windows require separate design | Not implemented |
| Compose | Images, environment, ports, volumes, health checks, restart and ordering semantics | Advanced fields rejected or diagnosed until implemented | Not implemented |
| Builds | Existing rootless OCI builder adapter | No custom Dockerfile build engine | Not implemented |
| Helm | Official local rendering, values files, versioned OCI charts | Cluster-dependent behavior, hooks, and lookup require explicit handling | Not implemented |
| Kubernetes workloads | Pod and Deployment subset, init and regular sidecar containers | StatefulSet, Job, RollingUpdate in extended milestones | Not implemented |
| Kubernetes configuration | ConfigMap and Secret projections/references | No implicit service-account credentials | Not implemented |
| Services and ingress | TCP Services, local DNS, basic HTTP Ingress | No silent LoadBalancer, UDP, IPv6, or controller-annotation emulation | Not implemented |
| Storage | Managed, ephemeral, PVC-compatible local volumes, explicit live binds | Access modes and cross-VM sharing limited by verified backend capability | Not implemented |
| Lifecycle | Probes, logs, exec, signals, reconciliation and recovery | No continuity guarantee across daemon crash in initial design | Not implemented |
| Tier 2 | Explicit reduced/validation-only behavior where implemented | Consent-based DaemonSet mapping; reduced policies require tests | Not implemented |
| Tier 3 | Clear rejection | Arbitrary CRDs/operators, cloud integrations, full cluster semantics are out of initial scope | Not implemented |

## Reporting model

The implementation will report field-level compatibility using:

- **SUPPORTED:** implemented and tested within documented limits.
- **DEGRADED:** mapped to a different behavior with stated consequences and required consent.
- **VALIDATE_ONLY:** checked or retained as metadata without executing its original controller semantics.
- **UNSUPPORTED:** cannot be executed by the selected capability set; blocking cases prevent apply.

These classifications are distinct from diagnostic severity and from an experimental/stable maturity label. Unknown fields affecting execution, security, networking, or data must not be silently discarded. A resource containing unsupported fields is not automatically supported just because its kind is recognized.

## Implementation requirement

T04 introduces the feature registry. As frontends arrive, generate this matrix and its machine-readable counterpart from the registry used by validators, or verify their equivalence with tests. Each entry must include:

- Source format and version.
- Resource kind and field.
- Support classification and milestone.
- Defaults and user-visible consequences.
- Positive and negative fixtures.
- Real runtime evidence where relevant.

Until that machinery exists, this page is a manually maintained **planning overview**, not an automatically verified compatibility matrix. See [IMPLEMENTATION_PLAN.md](../IMPLEMENTATION_PLAN.md), section 9, for detailed target semantics.
