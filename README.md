# Grillo

**Local Kubernetes semantics. No Kubernetes required.**

Grillo is a planned rootless-first application runtime that runs local workloads in hardware-isolated microVMs. It accepts Compose projects and Helm/Kubernetes application definitions, translates them into a shared model, and maps each Kubernetes Pod to one microVM.

The goal is a development experience closer to `docker compose up` than to operating a cluster, while preserving the application-facing contracts that matter in production.

> **Project status: initial scaffold.** The repository includes tested command scaffolding and build/CI tooling. There is no executable workload runtime or installable release. Only help, development version output, and an explicitly incomplete doctor command exist; capabilities described below remain intended behavior.

## Why Grillo?

Local development often uses Compose while production uses Kubernetes. Maintaining separate definitions can hide differences in Pod composition, service discovery, configuration, storage, probes, and lifecycle behavior. Running a complete local Kubernetes cluster narrows that gap, but also brings a control plane and its operational overhead onto the workstation.

Grillo explores a different approach:

```text
Local development                         Production

Compose ─┐                                Helm / Kubernetes manifests
         ├─> Shared application model                 │
Helm ────┘              │                            ▼
                        ▼                         Kubernetes
                Local reconciliation                 │
                        │                            ▼
                        ▼                     Production workloads
                 Pod microVMs

              No Kubernetes control plane
```

It is not intended to reproduce a production cluster byte for byte. It aims to make the same application artifacts useful locally while being honest about differences.

## Goals

- **One Pod, one microVM.** Containers within a Pod share a guest and localhost, rather than being split across unrelated virtual machines.
- **Rootless-first operation.** Host orchestration runs as the developer's user, without a privileged always-on daemon.
- **Compose and Helm as first-class inputs.** Both frontends compile into the same versioned intermediate representation (IR) and execution path.
- **Production-shaped application behavior.** Preserve supported image, configuration, secret, mount, DNS, Service, probe, replica, and ingress semantics.
- **Useful local debugging.** Expose status, logs, events, exec sessions, resource usage, mounts, and sandbox boundaries through a CLI and optional web console.
- **Hardware isolation by default.** Make the Pod boundary explicit and reduce direct exposure to the host kernel.
- **Low local overhead.** Measure startup latency, idle memory, filesystem performance, and cleanup from the first prototype.

## Philosophy

### Application intent comes first

Developers should describe an application rather than manage a fleet of VMs. The runtime should interpret that intent, explain incompatibilities, and make the resulting sandbox topology understandable.

### Reuse production artifacts without pretending to be Kubernetes

Grillo should implement a deliberate, tested subset of application-facing semantics. It should not require kube-apiserver, etcd, kubelet, a scheduler, or the Kubernetes controller ecosystem.

Unsupported behavior must be reported before provisioning. A downgraded feature must state what changes and, where required, obtain explicit consent. Accepting YAML is not evidence of compatibility.

### Isolation is a boundary, not a security slogan

The intended default is hardware isolation, not “absolute security.” KVM, the VMM, guest kernel, agent, virtual devices, and filesystem sharing remain security-critical. Host bind mounts deliberately cross part of the boundary and must be visible and explicit.

Rootless also has a precise meaning: ordinary operation should not require root, but access to virtualization devices and kernel namespace features may require one-time host configuration. There must be no silent privileged fallback.

### Small components, explicit dependencies

The implementation is planned in Go, prioritizing its standard library and official `golang.org/x/*` modules. A maintained YAML parser is the only initially planned third-party Go module. CLI, HTTP server, state management, and UI should not acquire frameworks without a concrete need.

This does not mean inventing a hypervisor, image builder, or secure OCI runtime. Specialized external components are preferable where they reduce risk. Their versions, licenses, checksums, prerequisites, and failure modes must be documented.

### Measure before optimizing

Fast startup and low idle overhead are engineering objectives, not current guarantees. Backend, rootless networking, and live filesystem sharing must be validated together before higher-level features depend on them. No fake backend result may substitute for a real KVM integration test.

## Intended developer experience

**Illustrative future commands—not available in this repository yet:**

```sh
# Review compatibility and planned changes without starting workloads.
grillo plan ./charts/store -f values.local.yaml

# Run a Helm chart or Compose application.
grillo up ./charts/store -f values.local.yaml
grillo up compose.yaml

# Inspect and debug the application.
grillo ps
grillo logs deployment/api -f
grillo shell pod/api-0 api
grillo inspect service/postgres

# Open the optional local console.
grillo ui

# Stop the application while preserving managed volumes.
grillo down
```

The intended workflow is iterative: rerun `up` after a change, compute a deterministic plan, and replace only affected sandboxes where practical. Closing the CLI or browser should not stop workloads.

## Architecture

```text
Compose frontend          Helm renderer → Kubernetes frontend
        │                                  │
        └────────────────┬─────────────────┘
                         ▼
              Versioned application IR
                         │
                         ▼
                Planner and reconciler
                   │             │
                   ▼             ▼
              Host services   Sandbox backend
              • local state   • one microVM per Pod
              • DNS/proxy     • minimal guest kernel
              • ingress       • Go guest agent
              • image cache   • guest-side OCI runtime
              • volumes       • app and sidecar containers
                   │             │
                   └──────┬──────┘
                          ▼
                    Local Unix API
                          │
                    CLI / web console
```

- **Frontends** validate source-specific behavior and generate the same IR.
- **The planner** explains changes, incompatibilities, and data impact before execution.
- **The reconciler** maintains desired state on one local host and records why resources change.
- **Host services** manage images, volumes, discovery, routes, and per-user state.
- **The sandbox backend** handles the selected VMM without leaking backend details into frontends.
- **The guest agent** controls containers, probes, signals, and streams without becoming a miniature kubelet.
- **The API** provides shared runtime operations to CLI and UI; neither implements a separate orchestration engine.

## Initial platform and technical direction

| Area | Planned direction |
|---|---|
| Host | Linux/amd64 with KVM; other architectures only after validation |
| Runtime language | Go, standard-library-first |
| Isolation | One hardware-isolated microVM per Pod |
| VMM | Firecracker is a candidate, not a finalized selection |
| Guest | Minimal Linux image, Go PID 1 agent, guest-side runc |
| Networking | Rootless application networking, DNS, Service proxying, localhost publishing |
| State | Per-user atomic JSON snapshots and bounded journal initially |
| Local API | Versioned HTTP over a private Unix socket |
| UI | Optional loopback bridge, embedded framework-free web assets |
| Helm | Official pinned Helm CLI initially; documented SDK trade-off |
| Builds | Existing rootless OCI builder, initially a Podman adapter |

Backend choice depends on proving **rootless networking and real live bind mounts**, not just booting a guest. Firecracker's storage/sharing constraints remain an explicit feasibility question. The [implementation plan](IMPLEMENTATION_PLAN.md) documents alternatives and decision gates.

macOS/Windows hosts, GPU support, snapshots, and alternative VMMs are not part of the first release target. No current performance or platform-support claim is implied by this table.

## Compatibility and scope

The MVP target includes:

- Common Compose image/environment/port/volume/health-check workflows.
- Helm rendering and a tested subset of Pod, Deployment, Service, ConfigMap, Secret, PVC, and basic Ingress behavior.
- Multiple containers per Pod, init containers, logs, exec, signals, probes, and local reconciliation.
- Persistent managed storage, explicit bind mounts, Service discovery, and local routes.
- CLI inspection and an optional web console.

Later milestones extend StatefulSet, Job, update strategies, and explicitly documented Tier 2 behavior. Compatibility is field-level, not just resource-kind-level. See the [current compatibility status](docs/compatibility.md) and [specification](grillo-project-specification.md).

### Non-goals

Grillo is not intended to be:

- A Kubernetes distribution or a complete Kubernetes API implementation.
- A multi-node production orchestrator or a distributed control plane.
- A general-purpose VM manager or Docker-compatible daemon.
- An arbitrary operator/CRD execution environment.
- A replacement for Kubernetes scheduling, admission, RBAC, CNI, CSI, or cloud-provider integrations.
- A guarantee of perfect local/production equivalence or absolute security.

## Roadmap

| Milestone | Focus |
|---|---|
| F0 | Prove a real rootless microVM path, OCI execution, networking, storage, and measurable overhead |
| F1 | Build the native runtime, guest agent, images, lifecycle, API, recovery, and probes |
| F2 | Run a representative multi-service Compose application |
| F3 | Deliver the Helm subset, local console, and tested product MVP |
| F4 | Expand Kubernetes-oriented semantics and compatibility |
| F5 | Optimize and package based on evidence; explore advanced isolation only when justified |

The plan contains **29 tasks, T00–T28**, with dependencies, contracts, tests, and acceptance criteria. No release dates or completed runtime milestones are claimed. Track evidence in [docs/progress.md](docs/progress.md).

## Documentation

| Document | Purpose |
|---|---|
| [Project specification](grillo-project-specification.md) | Product vision, semantics, requirements, and non-goals |
| [Implementation plan](IMPLEMENTATION_PLAN.md) | Detailed engineering tasks and verification gates |
| [Agent instructions](AGENT.md) | Rules and workflow for coding agents |
| [Contributing](CONTRIBUTING.md) | Human and automated contribution guidelines |
| [Architecture decisions](docs/adr/README.md) | Decision process and ADR template |
| [Progress](docs/progress.md) | Current task status and evidence |
| [Compatibility](docs/compatibility.md) | Honest distinction between planned and tested support |
| [Security policy](SECURITY.md) | Security expectations and reporting guidance |
| [Code of conduct](CODE_OF_CONDUCT.md) | Community behavior and moderation principles |
| [Changelog](CHANGELOG.md) | Notable repository and future release changes |

## Contributing

Design reviews, rootless feasibility experiments, adversarial test cases, documentation improvements, and eventually focused Go contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) before starting work. Coding agents should also follow [AGENT.md](AGENT.md).

### Development

Use Go **1.26.8**, Make, and a C compiler for race tests on Linux:

```sh
make check          # formatting, vet, unit/race tests, host/agent builds, module audit
./bin/grillo version
./bin/grillo doctor # exits 1: readiness checks are not implemented
make vulncheck      # explicit download/execution of pinned official audit tool
```

Build outputs stay under `bin/`. The agent is a build scaffold, **not usable as
guest PID 1**. `make test-kvm` currently fails with an explicit blocker; ordinary
tests require no KVM. No workload support is implied by passing these checks.
See the [dependency ADR](docs/adr/0001-scaffold-and-dependencies.md) for pins and
the provisional local module identifier.

## Name

*Grillo* is Italian for “cricket,” evoking the Talking Cricket from Pinocchio: a small companion that advises the puppet. The metaphor fits a runtime that interprets declarative applications and points out incompatibilities. The name is provisional, pending trademark and naming review.

## License

Grillo is licensed under the [Apache License, Version 2.0](LICENSE). See [NOTICE](NOTICE) for project attribution. Third-party components remain subject to their own licenses; Grillo's license does not relicense external tools or guest components.
