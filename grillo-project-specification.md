# Grillo

**Project Specification — A MicroVM-First Local Application Runtime**  
Version 0.1 — 3 October 2026

This specification defines product goals and requirements, not a support inventory.
Its example commands include intended future behavior. For commands available
now, read [getting started](docs/getting-started.md); implementation status and
approved delivery deviations are tracked in the [plan](IMPLEMENTATION_PLAN.md),
[progress](docs/progress.md) and [ADRs](docs/adr/README.md).


## 1. Executive Summary

Grillo is a local application runtime designed to close the gap between container-based development and Kubernetes-based production without running a Kubernetes control plane on the developer workstation.

The core design principle is simple: a Kubernetes Pod is represented by one lightweight microVM sandbox. Containers declared inside that Pod run together inside the same guest, preserving the most important Kubernetes co-location semantics such as shared localhost and a shared sandbox lifecycle. Compose services and Helm/Kubernetes resources are accepted as declarative inputs and compiled into a common internal application model.

Grillo is not a Kubernetes distribution, a replacement for Helm, or a general-purpose virtual machine manager. It is a rootless-first, control-plane-less application runtime that provides container ergonomics, Kubernetes-oriented semantics, strong isolation through hardware virtualization, and a local web console for development and debugging.

The intended developer experience is closer to `docker compose up` than to operating a cluster:

    grillo up ./chart
    grillo up compose.yaml
    grillo ps
    grillo logs api
    grillo shell pod/backend-0 api
    grillo ui

The runtime should make a local application feel like a small single-node platform while avoiding kube-apiserver, etcd, scheduler, kubelet, controllers, and other control-plane services. Grillo only implements the semantics needed to execute and debug an application locally.


## 2. Product Vision

Grillo should make the development environment structurally resemble the production deployment environment without requiring developers to operate a real Kubernetes cluster.

The project is built around four ideas:

1. Declarative applications are the primary unit of work. Developers normally run an application composed of several services rather than individual containers.
2. The Pod, not the individual container, is the natural isolation boundary for Kubernetes-oriented workloads.
3. Hardware-backed isolation should be the default rather than an opt-in runtime mode.
4. Kubernetes deployment artifacts should be reusable locally even when Kubernetes itself is absent.

A successful Grillo workflow should allow the same application to move through environments such as:

    Local development:
    Helm / Compose -> Grillo -> Pod microVMs

    Production:
    Helm -> Kubernetes -> runc / Kata / another CRI runtime

The local environment is therefore not a byte-for-byte replica of production infrastructure. Instead, it preserves the application-facing contracts that matter most: image references, Pod composition, Service DNS names, configuration, secrets, mounts, probes, ports, replica topology, and ingress routes.


## 3. Naming

The working project name is Grillo.

"Grillo" is the Italian word for cricket and evokes the Talking Cricket from Pinocchio: a small voice that accompanies and advises a puppet. The name matches the product metaphor well: applications are declarative "puppets", while Grillo interprets their desired state, points out incompatibilities, and guides them into a runnable local form.

The name should be treated as provisional until a formal trademark, package-name, domain-name, and repository-name review is completed.

Suggested taglines include:

- Local Kubernetes semantics. No Kubernetes required.
- Compose ergonomics. Pod isolation. Production-shaped development.
- A small conscience for your containers.


## 4. Goals

Grillo has the following primary goals.

G1 — Rootless-first operation
The normal developer workflow must not require a privileged daemon. Host-side orchestration should execute as the calling user. Access to hardware virtualization may require host configuration, such as permission to use `/dev/kvm`, but Grillo itself should not require root for normal operation.

G2 — One microVM sandbox per Pod
For Kubernetes-oriented workloads, each Pod maps to one microVM sandbox by default. Multiple containers in a Pod share that guest and can communicate over localhost as they would in Kubernetes.

G3 — Compose and Helm as first-class frontends
Both `compose.yaml` and Helm charts should be accepted directly. They should compile into the same internal representation instead of being implemented as separate execution paths.

G4 — No Kubernetes control plane
The normal runtime must not require kube-apiserver, etcd, scheduler, kubelet, controller-manager, CRI, or a Kubernetes distribution.

G5 — Production-shaped local networking
Applications should receive stable service names and predictable local ingress routes. Kubernetes Service DNS conventions should be emulated sufficiently for normal application behavior.

G6 — Excellent local debugging
Logs, events, probes, resource usage, networking, mounts, configuration, and sandbox boundaries should be easy to inspect through both the CLI and a local web console.

G7 — Strong isolation by default
The project should use hardware-assisted virtualization to reduce the blast radius of compromised application workloads. "Absolutely secure" is not a valid product claim; the goal is a stronger and more explicit security boundary than ordinary same-kernel containers.

G8 — Fast startup and low idle overhead
The runtime should remain useful on developer laptops. Startup latency and memory overhead must be treated as product-level constraints, not as afterthoughts.


## 5. Non-Goals

The first versions of Grillo explicitly do not aim to:

- implement the full Kubernetes API;
- provide a multi-node production orchestrator;
- replace Kubernetes scheduling, admission control, RBAC, or cluster lifecycle management;
- execute arbitrary CRDs and operators;
- reproduce every kube-proxy, CNI, CSI, or cloud-provider behavior;
- act as a general-purpose VM manager;
- promise perfect behavioral parity with a real Kubernetes cluster;
- provide a privileged remote management service by default;
- become a Docker-compatible daemon implementation.

When an input resource cannot be represented faithfully, Grillo should report the incompatibility explicitly rather than silently pretending to support it.


## 6. Core Concepts

Application
A logical collection of workloads, services, configuration, volumes, and routes loaded from one Compose project or one rendered Helm release.

Sandbox
The hardware-isolated execution unit. For Kubernetes input, one sandbox normally corresponds to one Pod. The initial implementation should map one sandbox to one microVM.

Container
An OCI process environment executed inside a sandbox. Containers within the same sandbox share the guest kernel and selected Pod-level namespaces.

Service
A stable local name and endpoint abstraction that resolves to one or more sandbox/container endpoints.

Route
A developer-facing HTTP/TCP entry point, usually derived from an Ingress or an explicit local exposure rule.

Release
A concrete instantiation of a chart or application definition with resolved values, environment overlays, generated names, and runtime state.

Runtime State
The minimum local metadata required to reconcile desired state with actual sandboxes. Runtime state is local metadata, not a distributed cluster database.


## 7. High-Level Architecture

The system is divided into declarative frontends, a common application intermediate representation, a reconciler, host services, and sandbox backends.

    +----------------------+       +----------------------+
    |  compose.yaml        |       |  Helm chart          |
    +----------+-----------+       +----------+-----------+
               |                              |
               v                              v
        Compose frontend                Helm renderer
               |                              |
               +---------------+--------------+
                               v
                    +---------------------+
                    | Application IR      |
                    |---------------------|
                    | Workloads           |
                    | Sandboxes           |
                    | Containers          |
                    | Services            |
                    | Routes              |
                    | Volumes             |
                    | Config / Secrets    |
                    | Probes / Resources  |
                    +----------+----------+
                               |
                               v
                    +---------------------+
                    | Local Reconciler    |
                    +---+--------+--------+
                        |        |
          +-------------+        +-----------------+
          v                                        v
    +------------+                            +------------+
    | Host Plane |                            | Sandboxes  |
    |------------|                            |------------|
    | DNS        |                            | microVM A  |
    | ingress    |                            | microVM B  |
    | network    |                            | microVM C  |
    | volumes    |                            +------------+
    | state      |
    +------------+

The CLI and web UI consume the same local API and event stream. Neither should contain business logic that is absent from the core runtime.


## 8. Input Model: Compose

Compose is a first-class developer-facing format because it already expresses the most common local application topology: services, images, builds, environment variables, ports, volumes, health checks, networks, and dependencies.

For an initial mapping, each Compose service should map to one sandbox containing one primary container. This keeps the mapping simple and predictable:

    services.frontend -> sandbox frontend -> container frontend
    services.backend  -> sandbox backend  -> container backend
    services.db       -> sandbox db       -> container db

A later extension may allow explicitly grouping several Compose services into one sandbox, but this should not be implicit because it changes the isolation model.

Supported Compose concepts should include at minimum:

- image;
- build context, delegated to an OCI image builder;
- command and entrypoint;
- environment and env_file;
- ports/expose;
- volumes;
- healthcheck;
- depends_on as an ordering hint, not as a guarantee of application readiness;
- resource limits where representable;
- restart policy;
- named networks, mapped to Grillo logical networks.

Unsupported Compose features must produce a structured compatibility warning or error.


## 9. Input Model: Helm and Kubernetes

Helm support is central to Grillo's production-parity objective. Grillo should render charts locally using Helm-compatible libraries and then translate supported Kubernetes objects into the internal application model.

The user should be able to run:

    grillo up ./charts/myapp
    grillo up ./charts/myapp -f values.yaml -f values.local.yaml
    grillo up oci://registry.example.com/charts/myapp --version 2.4.1

The initial compatibility target should focus on application-facing resources rather than cluster administration resources.

Tier 1 — execute faithfully where practical:

- Pod;
- Deployment;
- StatefulSet;
- Job;
- ConfigMap;
- Secret;
- Service;
- Ingress;
- PersistentVolumeClaim;
- startupProbe, readinessProbe, livenessProbe;
- container resource requests and limits;
- initContainers;
- sidecars expressed as regular Pod containers.

Tier 2 — validate, partially emulate, or degrade with explicit warnings:

- DaemonSet, interpreted as one local replica unless the user asks for a different simulation;
- ServiceAccount as metadata unless local credential behavior is explicitly configured;
- PodDisruptionBudget as validation-only metadata;
- NetworkPolicy through a reduced local policy model where feasible.

Tier 3 — unsupported in the initial product:

- arbitrary CRDs;
- Operators;
- admission webhooks;
- cluster-scoped RBAC semantics;
- cloud LoadBalancer integrations;
- CSI/CNI plugins;
- autoscaling controllers;
- multi-node scheduling semantics.

Grillo should expose a `grillo doctor` or `grillo plan` command that renders an application and reports exactly which resources are supported, ignored, downgraded, or rejected before anything is started.


## 10. Application Intermediate Representation

Both Compose and Kubernetes inputs must compile into a stable internal representation (IR). The IR is the product's architectural center: it prevents frontends from becoming tightly coupled to execution backends.

A conceptual IR might contain:

    Application {
      name
      namespace
      workloads[]
      services[]
      routes[]
      volumes[]
      configs[]
      secrets[]
    }

    Workload {
      kind
      replicas
      sandboxTemplate
      restartPolicy
      updatePolicy
    }

    SandboxTemplate {
      containers[]
      initContainers[]
      cpu
      memory
      kernelProfile
      mounts[]
      networkAttachments[]
      probes[]
      securityProfile
    }

The IR should be versioned. Frontends should perform syntactic and source-format-specific validation, while the IR validator should enforce Grillo's runtime invariants.

A stable IR also makes future frontends possible, such as a native Grillo manifest, Nomad-like input, or generated application plans, without changing the microVM lifecycle layer.


## 11. Sandbox and MicroVM Model

The default Kubernetes mapping is:

    1 Pod = 1 Grillo sandbox = 1 microVM

A Pod containing several containers therefore becomes one guest:

    +----------------------- microVM -----------------------+
    |                                                      |
    |   init containers -> app + sidecar + metrics         |
    |                     \____ shared localhost ____/      |
    |                                                      |
    +------------------------------------------------------+

This design preserves the conceptual Pod boundary and avoids the semantic mismatch of placing each Pod container into an unrelated VM.

The microVM backend should be abstracted behind a narrow interface. Candidate implementations may include libkrun, Firecracker, crosvm, or another KVM-based VMM. The project should initially optimize for one backend rather than prematurely guaranteeing backend portability.

The host runtime is responsible for:

- creating the sandbox configuration;
- allocating CPU and memory;
- presenting an OCI-derived root filesystem;
- attaching application volumes;
- attaching one or more virtual network interfaces;
- booting the guest kernel;
- establishing a host/guest control channel, preferably over vsock or an equivalent mechanism;
- starting a small guest agent or init process;
- executing container processes inside the guest;
- streaming stdout/stderr and events;
- delivering signals;
- reporting resource usage;
- orderly shutdown and forced termination.

The guest image should be minimal and purpose-built. It should contain only the kernel, init/agent requirements, and facilities needed to execute application containers.


## 12. Guest Agent

A small guest agent provides the bridge between Grillo's host-side runtime and processes inside the microVM.

Required responsibilities include:

- container process creation and termination;
- OCI bundle/application rootfs setup;
- stdout/stderr streaming;
- exec sessions;
- signal forwarding;
- probe execution;
- mount setup delegated by the host;
- status and process metadata;
- graceful sandbox shutdown.

The guest agent should not evolve into a miniature kubelet. Its API should be intentionally narrow and tied to Grillo's IR rather than to the entire Kubernetes API.

The control protocol should be versioned and authenticated at the channel level where practical. Host-controlled vsock addressing is preferred over exposing a TCP management endpoint inside each guest.


## 13. Rootless and Host Security Model

Grillo should be rootless-first, but "rootless" must be described precisely.

The orchestration process, image management, state store, UI, ingress proxy, DNS service, and sandbox lifecycle should normally run as the developer's user account. Hardware virtualization still depends on host kernel functionality such as KVM and therefore is not literally "all userspace".

Normal installation should require only the host configuration necessary to let the user access the virtualization device and required networking primitives.

Security principles:

- bind management interfaces to loopback by default;
- minimize the trusted computing base on the host;
- keep the guest agent small;
- use least-privilege file and device access;
- avoid a privileged always-on daemon;
- separate application network traffic from management channels;
- never expose secret contents casually through logs or the web UI;
- make host filesystem sharing explicit;
- use immutable or verified guest components where practical;
- support sandbox-specific security profiles;
- treat the VMM, KVM interface, virtio devices, guest kernel, filesystem sharing, and guest agent as part of the attack surface.

Product language should say "hardware-isolated by default" or "strong isolation by default", never "absolutely secure".


## 14. Networking

Networking must provide enough Kubernetes-like behavior to let normal applications run unchanged while remaining small enough for a local runtime.

Each sandbox receives an address on a Grillo-managed application network. Sandboxes belonging to the same application can reach each other according to the selected network policy.

Service discovery should support stable names such as:

    postgres
    postgres.default
    postgres.default.svc
    postgres.default.svc.cluster.local

The exact compatibility mode may be configurable, but chart workloads should not require code changes merely because Kubernetes is absent.

For a Service with multiple eligible replicas, Grillo can implement local load balancing through a small host proxy or DNS-based endpoint selection. Exact kube-proxy implementation details are not a goal; preserving the application-visible contract is.

The networking subsystem should support:

- Pod/sandbox IPs;
- Service names;
- port publishing to localhost;
- application-level networks;
- ingress routing;
- optional local TLS;
- DNS records generated from supported Services;
- clear inspection through `grillo network` and the web UI.


## 15. Ingress and Developer Routes

Ingress resources should become developer-friendly local routes rather than requiring a full ingress-controller installation.

For example, an application containing:

    app.local -> frontend:3000
    api.app.local -> backend:8080

may be served by an embedded reverse proxy managed by Grillo.

The runtime should print clickable routes after startup and expose them in the UI. Where the host platform permits, Grillo may provide an opt-in mechanism for managing local hostnames. A fallback based on localhost ports must always remain available.

TLS should support a local development certificate authority as an optional feature. Grillo must never silently install or trust a host certificate authority without explicit user action.


## 16. Storage and Volumes

Grillo needs a storage model that is predictable for local development while matching Kubernetes and Compose intent where possible.

Volume classes should include:

1. Ephemeral sandbox storage — destroyed with the sandbox.
2. Named application volumes — persistent across application restarts.
3. Bind mounts — explicit host paths made available to a sandbox.
4. Kubernetes PVC-compatible local volumes — persistent local storage mapped from supported PersistentVolumeClaim objects.

The runtime should clearly distinguish between a host bind mount and a managed volume because their security properties differ substantially.

A volume attached to a Pod must be presented consistently to all containers in that Pod. The selected filesystem-sharing mechanism should be benchmarked carefully because file sharing can become both a performance bottleneck and a security boundary.

Snapshot support may later enable fast cloning, warm-start environments, or test fixtures, but it should not be required for the MVP.


## 17. Configuration and Secrets

ConfigMaps, Compose environment variables, mounted configuration files, and Secrets should all compile into explicit IR objects.

Secret values must be masked in normal CLI output and the web console. Revealing a secret should be an explicit action. Secrets should not be written into ordinary diagnostic logs.

For local use, the first implementation may store secrets in the local state directory with operating-system file permissions. Future versions may integrate with host keychains or external secret providers.

The design should distinguish between source configuration and rendered runtime configuration so that `grillo plan` can show structural changes without dumping sensitive values.


## 18. Probes, Lifecycle, and Reconciliation

Grillo needs a small reconciler because declarative applications describe desired state. The reconciler should remain intentionally local and narrow.

Responsibilities include:

- ensuring the requested number of workload replicas exist;
- restarting sandboxes according to restart policy;
- running startup, readiness, and liveness probes;
- maintaining Service endpoint membership based on readiness;
- applying controlled updates when the desired application definition changes;
- recording concise local events for debugging.

This does not require a distributed scheduler or a Kubernetes-style controller ecosystem. The only scheduling target is the local host.

A useful local update model is:

    grillo up ./chart
    # edit values or source
    grillo up ./chart

The second invocation calculates a plan and changes only affected sandboxes where practical.

The UI and CLI should display the reason for every transition: image pull, guest boot, probe failure, restart, configuration change, route update, or user action.


## 19. CLI Experience

The CLI should feel closer to Compose and Podman than to cluster administration.

Core commands:

    grillo up <compose-file|chart|manifest>
    grillo down [application]
    grillo plan <source>
    grillo ps [application]
    grillo status [resource]
    grillo logs <resource> [-f]
    grillo shell <pod> [container]
    grillo exec <pod> [container] -- <command>
    grillo restart <resource>
    grillo inspect <resource>
    grillo events [application]
    grillo ui
    grillo doctor
    grillo image ...
    grillo volume ...
    grillo network ...

Kubernetes resource notation should be accepted where useful:

    grillo logs deployment/backend
    grillo shell pod/backend-0 api
    grillo inspect service/postgres

The runtime should favor clear application concepts over exposing implementation details. Advanced commands may expose sandbox IDs, VMM details, guest kernel versions, and low-level diagnostics when requested.


## 20. Web Console

The optional web console is a first-class debugging surface but not a control plane.

It is started explicitly:

    grillo ui

or together with an application:

    grillo up ./chart --ui

The default bind address must be loopback only, for example `127.0.0.1:9090`.

Primary views:

Application Overview
- application/release status;
- workload list;
- readiness summary;
- local routes;
- aggregate resource usage.

Topology
- workloads and Services;
- visible traffic relationships where they can be inferred;
- clear microVM/sandbox boundaries;
- ingress routes and exposed ports.

Pod/Sandbox Detail
- containers;
- guest kernel and sandbox backend;
- CPU and memory allocation/usage;
- sandbox IP;
- probe status;
- recent events;
- mounts;
- logs;
- shell/exec controls.

Configuration
- ConfigMaps and non-sensitive resolved configuration;
- Secret metadata with values hidden by default;
- rendered Helm values where safe.

Diagnostics
- unsupported resource warnings;
- failed image pulls;
- guest boot failures;
- networking conflicts;
- probe history;
- resource pressure.

The UI should consume the same local API as the CLI. Closing the UI must not stop workloads.

If the user explicitly exposes the UI beyond loopback, authentication and transport security should be required or the operation should produce a prominent warning and demand an explicit insecure override.


## 21. Local API

The core runtime should expose a local API used by the CLI and UI. This API is not intended to become a public cluster API in the Kubernetes sense.

Preferred transports are a Unix domain socket on Linux and equivalent local IPC on other platforms. A loopback HTTP endpoint may be exposed by the UI process as a translation layer if convenient.

The API should provide:

- application lifecycle operations;
- declarative plan/apply operations;
- resource inspection;
- log streaming;
- exec/shell sessions;
- event streaming;
- metrics snapshots;
- image/volume/network inspection;
- compatibility reports.

The API should be versioned from the beginning. Internal state schema and external API versioning should be independent so one can evolve without forcing the other to change at the same cadence.


## 22. State Management

Grillo should maintain only the state required to reconcile a local application and recover after the CLI process exits.

A small embedded database such as SQLite is a suitable default for metadata, while large artifacts such as OCI layers, volumes, and snapshots should live in dedicated content stores.

State categories:

- installed/rendered application definitions;
- desired workload replica counts;
- sandbox identity and lifecycle metadata;
- service endpoint mappings;
- local route configuration;
- image/content references;
- volume metadata;
- recent events;
- compatibility diagnostics.

State should be per-user by default. Multiple users on the same workstation should not share a privileged global runtime unless explicitly configured.


## 23. OCI Images and Builds

Grillo should consume standard OCI images and registries. It should avoid inventing a new image format.

Image responsibilities include:

- pull and credential handling;
- content-addressed layer storage;
- unpacking or presenting layers to the sandbox backend;
- architecture/platform selection;
- image metadata inspection;
- garbage collection.

For Compose `build:` and local development workflows, Grillo should provide its own build system so that building works on a host without Podman or Docker. The native builder parses a supported Dockerfile subset, assembles the root filesystem, and executes `RUN` steps inside a sandboxed Guest microVM, publishing a verified OCI image into the content-addressed store. An external builder (rootless Podman) may be offered as an explicit opt-in accelerator, but it must never be required and must never be selected automatically. The integration should remain modular behind one builder contract.

A long-term optimization may cache sandbox-ready root filesystem representations so repeated guest creation does not repeatedly perform expensive extraction work.


## 24. Performance Objectives

Performance targets should be defined early because the product only makes sense if it remains comfortable on a developer workstation.

Initial engineering objectives, to be validated by prototypes, should include:

- idle host-side runtime overhead measured in tens of megabytes rather than hundreds;
- no permanent Kubernetes control-plane processes;
- sub-second creation target for a warm simple sandbox where the selected backend and host permit it;
- application startup dominated by image availability and application boot rather than orchestration overhead;
- aggressive sharing of immutable OCI content between sandboxes;
- bounded guest memory overhead;
- observable memory accounting that separates workload memory from sandbox/VMM overhead;
- fast teardown with deterministic cleanup of networking and mounts.

These are engineering goals, not promises. Real targets should be established after benchmarking the chosen VMM, guest kernel, filesystem sharing, and networking stack.


## 25. Observability

Grillo should make debugging local distributed applications easier than debugging them in a real cluster.

Every resource should expose:

- current status;
- desired status;
- recent state transitions;
- associated sandbox/container IDs;
- CPU and memory usage;
- health probe results;
- logs;
- network identity;
- mounts;
- source mapping back to Compose or Kubernetes/Helm objects.

A structured event stream is especially important. Instead of forcing users to infer what happened from several logs, Grillo should record concise events such as:

    10:22:14 deployment/backend desired replicas changed 1 -> 2
    10:22:14 pod/backend-1 sandbox allocated
    10:22:14 pod/backend-1 microVM booted in 87 ms
    10:22:15 pod/backend-1 container api started
    10:22:16 pod/backend-1 readiness probe passed
    10:22:16 service/backend endpoint added 10.42.0.12:8080

Metrics export in Prometheus format may be added later, but local visibility is the initial priority.


## 26. Compatibility and Honesty

A central product principle is "never fake compatibility silently."

For every loaded application Grillo should be able to produce a compatibility report:

    SUPPORTED     Deployment/backend
    SUPPORTED     Service/backend
    SUPPORTED     ConfigMap/backend-config
    DEGRADED      DaemonSet/node-agent -> one local replica
    VALIDATE_ONLY PodDisruptionBudget/backend
    UNSUPPORTED   CustomResource/foo.example.io

The report should explain the practical consequence of every downgrade. This is more useful to developers than claiming Kubernetes compatibility while ignoring semantics.

The project should publish and version a compatibility matrix for supported Compose and Kubernetes features.


## 27. Suggested Security Threat Model

The primary threat scenario is an application container that is buggy, compromised, or actively malicious while running on a developer workstation.

Grillo should aim to prevent that workload from trivially reaching the host kernel or unrelated application sandboxes. The microVM boundary reduces the direct host-kernel syscall surface compared with conventional containers, but the following remain trusted or security-critical components:

- host kernel and KVM;
- VMM/microVM backend;
- virtual devices;
- filesystem sharing mechanism;
- guest kernel;
- guest agent;
- host networking/proxy components;
- Grillo core runtime;
- image supply chain.

Host bind mounts intentionally punch through some isolation and must be displayed prominently in security inspection views.

The project should maintain a written threat model, fuzz parsers and guest/host protocols, minimize unsafe code where practical, support reproducible guest artifacts, and define a vulnerability response process before claiming production-grade security.


## 28. MVP Scope

The first meaningful prototype should prove the architecture rather than maximize feature count.

MVP inputs:
- Compose services with image, environment, ports, volumes, and health checks;
- Helm rendering with Deployment, Pod, Service, ConfigMap, Secret, PVC, and basic Ingress;
- one local namespace model.

MVP runtime:
- one microVM per Pod/sandbox;
- one selected Linux/KVM VMM backend;
- minimal guest kernel and agent;
- OCI image pull/cache;
- exec, logs, signals, stop/restart;
- sandbox IP allocation;
- local DNS and basic Service routing;
- managed volumes and explicit bind mounts;
- startup/readiness/liveness probes;
- simple local reconciler.

MVP developer surfaces:
- `up`, `down`, `plan`, `ps`, `logs`, `shell`, `inspect`, `doctor`;
- local web UI with application overview, Pod detail, logs, resource usage, events, and routes.

Explicitly postpone:
- CRDs/operators;
- GPU support;
- Windows guests;
- live migration;
- snapshots as a user-facing primitive;
- distributed/multi-host execution;
- full NetworkPolicy semantics;
- Docker API compatibility.


## 29. Proposed Delivery Phases

Phase 0 — Feasibility
Build the smallest host-to-microVM path: boot one guest, start one OCI workload, stream logs, execute a command, stop cleanly. Measure startup and memory overhead before building higher-level features.

Phase 1 — Native sandbox runtime
Implement image management, sandbox lifecycle, guest agent, networking, volumes, logs, exec, resource accounting, and a minimal state store. At this phase, input may be a temporary native Grillo manifest.

Phase 2 — Compose frontend
Support common Compose projects. The goal is to replace `docker compose up` for a representative multi-service developer application.

Phase 3 — Helm/Kubernetes frontend
Render charts and support the Tier 1 Kubernetes subset. Add `plan` and compatibility diagnostics so developers can understand the local interpretation before startup.

Phase 4 — Web console
Add the local topology view, logs, events, metrics, routes, sandbox boundaries, configuration inspection, and shell/exec.

Phase 5 — Production-parity hardening
Expand Kubernetes semantics, improve filesystem/network performance, add security hardening, reproducible guest artifacts, compatibility testing, and integrations with CI workflows.

Phase 6 — Advanced isolation and acceleration
Evaluate snapshots, confidential-computing backends, alternative VMMs, GPU/device passthrough, and warm sandbox pools only when real workloads justify them.


## 30. Example User Journey

A developer clones a service repository containing the same chart used by CI and production:

    charts/store/
      Chart.yaml
      values.yaml
      templates/

They create a local overlay:

    values.local.yaml

Then run:

    grillo plan ./charts/store -f values.local.yaml

Grillo renders the chart and prints:

    3 workloads
    4 Pod sandboxes
    4 microVMs
    6 containers
    3 Services
    2 routes
    2 managed volumes
    0 unsupported resources

The developer starts the application:

    grillo up ./charts/store -f values.local.yaml --ui

Grillo starts the required sandboxes and prints:

    web:       https://store.local
    api:       https://api.store.local
    console:   http://127.0.0.1:9090

A backend readiness probe fails. The web console shows the failure, the affected Pod, recent events, logs, and the Service endpoint remaining unready. The developer opens a shell directly into the `api` container inside that Pod sandbox, fixes the configuration, reruns `grillo up`, and only the affected sandbox is replaced.

No Kubernetes cluster was created at any point, but the developer exercised the same chart structure, Pod grouping, configuration, Service naming, probes, persistent storage declarations, and ingress intent used in production.


## 31. Example CLI

Example commands and expected intent:

    # Start a Compose application
    grillo up compose.yaml

    # Start a Helm chart
    grillo up ./chart -f values.dev.yaml

    # Show how the chart will be interpreted
    grillo plan ./chart -f values.dev.yaml

    # List workloads and sandboxes
    grillo ps

    # Follow logs using Kubernetes-style notation
    grillo logs deployment/api -f

    # Open a shell in a specific Pod container
    grillo shell pod/api-0 api

    # Inspect the VM boundary and guest information
    grillo inspect sandbox/api-0

    # Start the local web console
    grillo ui

    # Stop the app but preserve named volumes
    grillo down

    # Remove the app including managed volumes
    grillo down --volumes


## 32. Open Technical Questions

The following questions should be answered by prototypes rather than by design preference alone:

- Which VMM gives the best combination of startup latency, memory overhead, security model, embeddability, and maintainability for the first Linux implementation?
- Should each sandbox use a dedicated minimal root filesystem plus container layers, or should container root filesystems be presented through a different sharing mechanism?
- What is the best host/guest filesystem strategy for source-code bind mounts with acceptable macOS/Linux development performance?
- How much Kubernetes DNS behavior is needed for real-world charts?
- Should Service load balancing happen through a host proxy, guest routing, or DNS endpoint selection?
- How should image builds integrate: BuildKit, Buildah, another backend, or an abstract builder interface?
- How should rootless networking be implemented while keeping Pod IPs and local routing understandable?
- What is the minimum guest agent surface necessary for multi-container Pods?
- Can multiple application networks be represented without privileged host networking?
- Which security profiles should be available by default for bind mounts, host access, and device passthrough?
- How should chart hooks be interpreted when no Kubernetes API server exists?
- How should StatefulSet identity and stable storage be represented across local restarts?


## 33. Success Criteria

The project should be considered successful when a developer can take a representative production Helm chart for a multi-service application and run it locally with the following experience:

- no Kubernetes installation;
- no privileged container daemon;
- one hardware-isolated microVM per Pod;
- startup fast enough for iterative development;
- service names and ingress routes that require no application code changes;
- working volumes, configuration, secrets, and probes;
- clear compatibility warnings for unsupported Kubernetes behavior;
- a useful CLI and local web console;
- materially lower idle overhead and operational complexity than running a real local cluster;
- enough production-shape parity that Compose-only local manifests become optional rather than mandatory.

The strongest product signal would be teams deleting a separate development-only orchestration definition because the production Helm chart became pleasant enough to run directly on a laptop.


## 34. One-Sentence Definition

Grillo is a rootless-first, control-plane-less local application runtime that runs Compose or Helm-defined workloads with Kubernetes-oriented semantics and one hardware-isolated microVM per Pod.
