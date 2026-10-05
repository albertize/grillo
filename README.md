# Grillo

**Local Kubernetes semantics. No Kubernetes required.**

![A cricket whispering to an expressive wooden ship’s wheel, illustrated in sepia.](media/Whispering%20Cricket%20and%20Ship%E2%80%99s%20Wheel.png)

Grillo is a rootless-first application runtime that runs local workloads in hardware-isolated microVMs. It accepts Compose projects, Kubernetes manifests and local/OCI Helm charts, translates them into a shared model, and maps each Kubernetes Pod to one microVM.

The goal is a development experience closer to `docker compose up` than to operating a cluster, while preserving the application-facing contracts that matter in production.

> **Project status: pre-release, with a working runtime.** On Linux/KVM the native runtime pulls OCI images, boots QEMU microVMs, runs real containers and init containers, shares a Pod's containers over localhost, serves Service DNS, attaches managed and bind volumes, provides exec/logs/events/probes, reconciles desired state with crash recovery, and exposes a local Unix-socket API and CLI. Compose projects and a Kubernetes MVP subset compile into the shared IR; the F0 and F2 gates and a multi-container Kubernetes scenario pass on real hardware. Local and exact-version OCI Helm charts render with Helm v4.2.2 and are accepted by `plan`/`up` (T21 complete); the Helm runtime acceptance gate (T22) and the full web console (T23) remain pending, there is no packaged release or supported version, and behavior not covered by [docs/progress.md](docs/progress.md) and [docs/compatibility.md](docs/compatibility.md) remains intended, not delivered.

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

The implementation is in Go, prioritizing its standard library and official `golang.org/x/*` modules. The only third-party Go module outside that set is the maintained YAML parser. CLI, HTTP server, state management, and UI should not acquire frameworks without a concrete need.

This does not mean inventing a hypervisor, image builder, or secure OCI runtime. Specialized external components are preferable where they reduce risk. Their versions, licenses, checksums, prerequisites, and failure modes must be documented.

### Measure before optimizing

Fast startup and low idle overhead are engineering objectives, not current guarantees. Backend, rootless networking, and live filesystem sharing were validated together on real hardware before higher-level features were built on them (F0). No fake backend result may substitute for a real KVM integration test.

## Developer experience

These commands work today for Compose projects, Kubernetes manifests, and the supported Helm chart subset:

```sh
# Review compatibility and planned changes without starting workloads.
grillo plan compose.yaml
grillo plan deployment.yaml
grillo plan examples/helm/demo --release demo

# Run a Compose project or a Kubernetes manifest.
grillo up compose.yaml
grillo up deployment.yaml

# Inspect and debug the application.
grillo ps
grillo status <application>
grillo logs deployment/api -f
grillo exec pod/api-0 api -- <command>
grillo shell pod/api-0 api
grillo inspect service/postgres
grillo events -f

# Build and inspect images without an external container engine.
grillo build -t example:local .
grillo image ls

# Open the optional local console.
grillo ui

# Stop the application while preserving managed volumes.
grillo down <application>
```

The workflow is iterative: rerun `up` after a change, compute a deterministic plan, and replace only affected sandboxes where practical. Closing the CLI or browser does not stop workloads.

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

## Platform and technical direction

| Area | Direction |
|---|---|
| Host | Linux/amd64 with KVM; other architectures only after validation |
| Runtime language | Go, standard-library-first |
| Isolation | One hardware-isolated microVM per Pod |
| VMM | QEMU `microvm` + virtiofsd (ADR 0005; the working backend) |
| Guest | Minimal Linux image, Go PID 1 agent, guest-side runc |
| Networking | Rootless application networking, Service DNS; Service proxy and Ingress datapaths planned |
| State | Per-user atomic JSON snapshots and bounded journal initially |
| Local API | Versioned HTTP over a private Unix socket |
| UI | Optional loopback bridge, embedded framework-free web assets |
| Helm | Official pinned Helm CLI initially; documented SDK trade-off |
| Builds | Native builder by default (Dockerfile subset executed in a guest); rootless Podman is opt-in |

Backend choice depended on proving **rootless networking and real live bind mounts**, not just booting a guest. The F0 gate now passes on QEMU `microvm` + virtiofsd: rootless two-VM networking, DNS, egress, loopback publishing, enforced isolation, live virtiofs binds, and a persistent ext4 volume were all measured on real hardware (`make test-f0`). Firecracker was rejected for the product because it exposes no shared-filesystem device. Two limitations are recorded honestly: host-originated virtiofs notifications are degraded (polling required), and SELinux Enforcing silently breaks the `pasta` helper on the tested Fedora policy. See [ADR 0005](docs/adr/0005-platform-qemu-virtiofsd.md) and the [T03 report](docs/experiments/t03-backend-comparison.md). The [implementation plan](IMPLEMENTATION_PLAN.md) documents alternatives and decision gates.

macOS/Windows hosts, GPU support, snapshots, and alternative VMMs are not part of the first release target. No current performance or platform-support claim is implied by this table. The runtime built on this backend runs the F2 Compose application and the multi-container Kubernetes scenario described in [docs/progress.md](docs/progress.md).

## Compatibility and scope

Implemented and exercised on the real runtime:

- Compose image/environment/port/volume/health-check workflows (F2 gate).
- A Kubernetes MVP subset: Pod, Deployment, Service, ConfigMap, Secret, PVC, and basic Ingress compilation.
- Multiple containers per Pod, init containers, logs, exec, probes, and local reconciliation.
- Persistent managed storage, explicit bind mounts, and Service discovery through in-guest DNS that resolves Service names to sandbox addresses.

Local and exact-version OCI Helm chart rendering/CLI input are implemented with Helm v4.2.2 (T21). OCI fetch requires explicit `--fetch-chart`, and cached charts render offline with verified digests. Dependencies remain rejected; the Helm runtime acceptance gate (T22) is pending. See the [chart example and OCI usage](examples/helm/README.md).

Not implemented yet: Helm dependency resolution; the full web console; StatefulSet and Job; rolling updates; the Service VIP/load-balancing and Ingress reverse-proxy datapaths (only their compilation and DNS exist); and the remaining Tier 2 behavior. Compatibility is field-level, not just resource-kind-level. See the [current compatibility status](docs/compatibility.md) and [specification](grillo-project-specification.md).

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
| F0 | Rootless microVM path, OCI execution, networking, storage, and measurable overhead — **gate passed** on QEMU `microvm` + virtiofsd (T01–T03) |
| F1 | Native runtime: guest agent, images, lifecycle, API, recovery, probes — **components implemented and exercised on real KVM** (T04–T16), with a review-remediation pass tracked in [remediation.md](docs/remediation.md) |
| F2 | Representative multi-service Compose application — **gate passed** (T19) |
| F3 | Helm subset, local console, and tested product MVP — **in progress**: Kubernetes MVP compiler done (T20); local/OCI Helm renderer and CLI done (T21), Helm runtime gate pending (T22); full console pending (T23) |
| F4 | Expand Kubernetes-oriented semantics and compatibility |
| F5 | Optimize and package based on evidence; explore advanced isolation only when justified |

The plan contains **29 tasks, T00–T28**, with dependencies, contracts, tests, and acceptance criteria. There is no packaged release or supported version, and F3–F5 remain open. Track evidence in [docs/progress.md](docs/progress.md).

## Documentation

| Document | Purpose |
|---|---|
| [Project specification](grillo-project-specification.md) | Product vision, semantics, requirements, and non-goals |
| [Implementation plan](IMPLEMENTATION_PLAN.md) | Detailed engineering tasks and verification gates |
| [Agent instructions](AGENT.md) | Rules and workflow for coding agents |
| [Contributing](CONTRIBUTING.md) | Human and automated contribution guidelines |
| [Architecture decisions](docs/adr/README.md) | Decision process and ADR template |
| [Progress](docs/progress.md) | Current task status and evidence |
| [Remediation](docs/remediation.md) | Runtime review findings R1–R8, status and evidence |
| [Compatibility](docs/compatibility.md) | Honest distinction between planned and tested support |
| [Security policy](SECURITY.md) | Security expectations and reporting guidance |
| [Code of conduct](CODE_OF_CONDUCT.md) | Community behavior and moderation principles |
| [Changelog](CHANGELOG.md) | Notable repository and future release changes |

## Contributing

Design reviews, rootless feasibility experiments, adversarial test cases, documentation improvements, and focused Go contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) before starting work. Coding agents should also follow [AGENT.md](AGENT.md).

### Development

Use Go **1.26.8**, Make, and a C compiler for race tests on Linux.
The [dependency bootstrap](scripts/README.md) provides an opt-in Fedora package
setup and checksum-pinned Linux/amd64 Go, Firecracker, and guest-kernel source
downloads. Preview it safely with `bash scripts/bootstrap.sh`.

Routine checks:

```sh
make check           # formatting, vet, unit/race tests, host/agent builds, module audit
make test-executor   # real KVM: boot, apply, exec, teardown
make test-f2         # real KVM: multi-service Compose application
make test-k8s        # real KVM: multi-container Kubernetes Pod
./bin/grillo version
./bin/grillo doctor  # read-only host readiness check
make vulncheck       # explicit download/execution of pinned official audit tool
```

Build outputs stay under `bin/`. The guest agent (`cmd/grillo-agent`) is the
real guest PID 1; build the guest image with `make t07-guest`. `make test-executor`,
`make test-f2`, and `make test-k8s` are real KVM/QEMU integration gates and SKIP
(never pass) when `/dev/kvm`, `pasta`, or the guest artifacts are missing;
ordinary tests require no KVM. See the
[dependency ADR](docs/adr/0001-scaffold-and-dependencies.md) for pins and the
module identifier `github.com/albertize/grillo`.

## Name

*Grillo* is Italian for “cricket,” evoking the Talking Cricket from Pinocchio: a small companion that advises the puppet. The metaphor fits a runtime that interprets declarative applications and points out incompatibilities. The name is provisional, pending trademark and naming review.

## License

Grillo is licensed under the [Apache License, Version 2.0](LICENSE). See [NOTICE](NOTICE) for project attribution. Third-party components remain subject to their own licenses; Grillo's license does not relicense external tools or guest components.
