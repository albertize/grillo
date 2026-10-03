# Grillo — Implementation Plan

**Status:** planning document, not an implemented runtime.

**Normative source:** [Project specification](grillo-project-specification.md), v0.1.

**Audience:** coding agents, including LLMs with limited context and planning capacity.

**Language:** Go for the CLI, runtime, guest agent, and UI server; framework-free HTML/CSS/JavaScript for the browser.

## 1. How to use this document

Implement **one task at a time**, following the dependencies in section 15. Do not generate the entire project in one session. Every task has deliverables and verifiable acceptance criteria. An interface backed by a fake does not complete a task requiring real hardware.

At the beginning of each session:

1. Read [AGENT.md](AGENT.md), this plan, the relevant specification sections, and [docs/progress.md](docs/progress.md).
2. Check `git status`; never overwrite unrelated user changes.
3. Select the first unfinished task whose dependencies are satisfied.
4. Read existing code and contracts before editing them.
5. Write tests for the required behavior, implement it, and run applicable checks.
6. Update progress, decisions, and limitations. Keep completed tasks in focused commits when committing is requested; report untested behavior explicitly.

If a prerequisite is unavailable, mark the task **BLOCKED**, document the missing evidence, and work only on independent tasks. Never replace microVM execution with host processes or host containers, simulate KVM, or report a real integration test as passed because a fake passed.

### 1.1 Decision hierarchy

- The specification defines the product; this plan makes implementation choices and milestones explicit.
- A milestone limitation does not automatically become a permanent product limitation.
- Deviations require an architecture decision record (ADR) under `docs/adr/`, an explanation of user impact, and tests.
- Do not add dependencies or alternative backends to bypass a problem without assessing their cost.
- No Kubernetes API server, etcd, privileged container daemon, or distributed scheduler.

## 2. Scope, milestones, and initial decisions

### 2.1 Platform

Initial release: **Linux, amd64, KVM**, one user, and the Kubernetes namespace `default` within each application. Add arm64 only after verifying the VMM, kernel, and images. macOS and Windows require an outer VM and are outside the initial release. Offline commands such as `plan` may be portable; runtime commands must fail clearly on unsupported hosts.

Rootless means host processes run with the developer's UID, normal workflows do not invoke `sudo`, and no privileged daemon is required. Access to `/dev/kvm` and user/network namespaces may require one-time host configuration. If a distribution disallows these primitives, `doctor` must explain the restriction; never silently fall back to privileged execution.

### 2.2 Separate milestones

| Milestone | Required outcome | Insufficient evidence |
|---|---|---|
| F0: feasibility | Real microVM, OCI workload, rootless networking, logs, exec, stop, measurements | Booting only the kernel |
| F1: native runtime | Multi-container guest, images, storage, recovery, API, probes | Guest processes without OCI isolation |
| F2: Compose | Web/API/database example, DNS, persistence, differential updates | YAML translation without execution |
| F3: product MVP | Compose and Helm MVP subset, UI, security checks, E2E tests | Claiming support for the entire Kubernetes Tier 1 |
| F4: extended parity | StatefulSet, Job, remaining targeted Tier 1 behavior, explicit Tier 2 handling | Ignoring unknown Kubernetes fields |
| F5: optimization | Benchmark-backed improvements and hardening; additional backends if justified | Premature snapshots or warm pools |

### 2.3 Fixed and provisional decisions

**Fixed:** shared versioned IR; one Pod per microVM; local Unix-socket API; per-user state; secrets separated from public views; no frontend-to-VMM coupling; offline `plan` without runtime mutations; deterministic diffs; optional UI that does not own workload lifetime.

**Candidate backend:** Firecracker as an external process, using its HTTP API over a Unix socket and a Grillo-controlled Linux kernel and guest image. This is a candidate, not proven feasibility: it needs TAP networking and does not provide virtio-fs as a general bind-mount solution. **T01–T03 must validate networking and storage together before confirming the backend.** If these constraints prevent a rootless product, compare QEMU `microvm` or a backend with suitable filesystem sharing. Confirm one backend through an ADR and adjust the adapter rather than developing three implementations.

**Guest:** Go PID 1 agent and an external `runc` binary inside the guest for namespaces, capabilities, cgroups, and OCI process isolation. Guest-side runc does not imply host Docker/containerd. Do not build a security-sensitive OCI runtime from scratch.

**State:** atomic JSON files and a bounded journal initially, without SQLite. The specification identifies SQLite as a suitable option, not a requirement. A single writer—the user service—makes this choice practical. Reconsider SQLite only when measurements or transaction complexity justify it.

**Helm:** the official, version-pinned Helm executable, invoked through `os/exec` without a shell. This is an **explicit deviation** from the specification's preference for Helm-compatible libraries: it preserves the official renderer while avoiding the SDK's large transitive dependency graph. Record the deviation in an ADR. If a self-contained executable becomes mandatory, use the official SDK and accept its dependencies; do not recreate Helm templating.

## 3. Dependency and tooling policy

### 3.1 Go modules

Default to the standard library: `net/http`, `encoding/json`, `os/exec`, `archive/tar`, `compress/gzip`, `crypto/*`, `log/slog`, `embed`, `testing`, `flag`, and `html/template`. Select a supported stable Go release in T00 and pin it in `go.mod` and CI. Reproducible builds must not use floating `latest` versions.

Additional modules are allowed only where needed:

| Module | Purpose | Constraint |
|---|---|---|
| `golang.org/x/sys/unix` | vsock, namespaces, ioctl, peer credentials, Linux primitives | Official Go module; confined to Linux adapters |
| `golang.org/x/term` | Raw terminal mode and restoration for CLI sessions | Official Go module; keep out of core packages |
| `golang.org/x/net/dns/dnsmessage` | DNS encoding and decoding | Official Go module; server and policy remain Grillo code |
| `go.yaml.in/yaml/v3` | Compose/Kubernetes YAML, nodes, source locations | Only planned third-party module; verify maintained module path and version in T00 |

`golang.org/x/*` modules are official Go modules but **not** part of the standard library. Do not initially add Cobra, Gin, GORM, client-go, compose-go, or the Helm SDK. Do not write a homemade YAML parser. Unsupported OCI compression such as zstd should initially produce a structured error; add a library only with an ADR and tests. Commit `go.sum` and inspect `go list -m all`, licenses, and vulnerabilities.

### 3.2 External, non-Go dependencies

Reducing Go modules does not eliminate system dependencies. Publish versions, checksums, licenses, and installation instructions for:

- The selected VMM, guest kernel, guest runc, and rootfs construction tools.
- Any selected rootless networking helper, such as slirp4netns or pasta, and required namespace/filesystem tools.
- Helm for charts and rootless Podman as the first optional image builder.
- Benchmark and integration tools, separately from runtime requirements.

Never silently download and execute tools. `doctor` identifies missing versions; any future `setup` operation requires consent. Verify guest artifacts before boot.

## 4. Architecture and intended layout

```text
cmd/grillo/                  CLI and API client
cmd/grillod/                 foreground user service; started on demand
cmd/grillo-agent/            guest PID 1 and container control
internal/model/              versioned IR and pure validation
internal/source/             source detection, source maps, diagnostics
internal/frontend/compose/   parsing, interpolation, compilation
internal/frontend/kube/      Kubernetes objects to IR
internal/frontend/helm/      Helm invocation to manifests to kube frontend
internal/plan/               normalization, hashing, diff, action DAG
internal/core/               shared use cases and operation authorization
internal/reconcile/          desired/observed state, retries, recovery
internal/state/              JSON store, locks, migrations, journal
internal/secrets/            private data and public projections
internal/oci/                registry, CAS, unpacking, image config, GC
internal/build/              external builder adapter
internal/sandbox/            VMM-independent contracts
internal/backend/firecracker/ provisional name; confirm through ADR
internal/guestproto/          host/guest protocol and framing
internal/guest/              PID 1, runc adapter, probes, mounts, streams
internal/network/            IPAM, rootless transport, DNS, Service proxy
internal/storage/            volumes, attachments, locking, sharing
internal/observe/            events, log spool, metrics
internal/api/                Unix /v1 server, DTOs, client, streams
internal/ui/                 loopback bridge and embedded assets
internal/platform/linux/     syscalls, process identity, namespaces, vsock
web/                         static HTML/CSS/JavaScript
api/                         API and streaming specifications
guest/                       kernel config, image build, artifact manifest
scripts/                     build/check scripts, not business logic
examples/                    equivalent native, Compose, and Helm examples
integration/                 real tests with integration/kvm build tags
internal/testutil/            fake clock/backend/registry and fixtures
docs/                        ADRs, security, compatibility, progress
```

Use one Go module. Do not create empty packages or speculative frameworks. The final module path depends on the repository URL; initially use a consistent local identifier and document its replacement rather than inventing a remote organization.

Dependency direction: frontend → model; plan → model; reconcile → state/sandbox/network/storage contracts; adapters → contracts; API → core; CLI/UI → API. `model` must not import process, HTTP, or frontend packages. Core tests must not need KVM.

### 4.1 User-service lifetime

`grillo up` checks the socket, starts `grillod` if needed, and waits for a bounded handshake. The service acquires an exclusive kernel lock: two concurrent CLI processes must not start two runtimes. Use Linux `flock`, not merely a PID file. `grillod` remains alive after the CLI/UI exits. A systemd user unit is optional, not mandatory.

The daemon supervises its helpers. For the first release, after a daemon crash, recover desired state and volumes, identify and clean up verified orphan processes, and recreate sandboxes. **Do not promise uninterrupted recovery.** A PID alone never authorizes termination: verify boot ID, process start time, and executable/process identity. If identity is ambiguous, block the resource and request intervention.

## 5. Data contracts to establish before adapters

### 5.1 Application IR: `grillo.dev/v1alpha1`

Use JSON serialization and explicit Go types, not `map[string]any` in the core. YAML belongs at the edges. Define at least:

| Type | Fields and invariants |
|---|---|
| `Application` | apiVersion, identity, source, workloads, services, routes, volumes, configs, secretRefs, networks |
| `Identity` | Persistent ID, name, namespace; key `(user, application, namespace, name)` |
| `Workload` | ID, kind, replicas, template, restartPolicy, updatePolicy, optional completionPolicy |
| `SandboxTemplate` | initContainers, containers, guest resources, volumes, networks, DNS, securityProfile, hostname |
| `Container` | Name, image reference and resolved digest, entrypoint/args preserving absent versus empty, env/envRefs, mounts, ports, resources, probes, workingDir, user |
| `Resources` | CPU millicores, memory bytes, separate requests/limits; no floating-point quantities |
| `Service` | Name, label selector, named ports, protocol, named/numeric targetPort, headless, publishNotReadyAddresses |
| `Route` | Hostname, path, pathType, Service/port, optional TLSRef, actual local endpoint |
| `Volume` | Ephemeral/managed/bind/PVC kind, source, readOnly, accessMode, capacity, subPath if supported |
| `Config` | Text/binary entries with source maps; explicit file and environment projections |
| `SecretRef` | Opaque identifier and version; never a value in public IR |
| `Probe` | exec/HTTP/TCP, delay, timeout, period, failure/successThreshold |
| `Diagnostic` | Stable code, severity, support status, source path/line, resource/field, consequence, remediation |

Compatibility states: `SUPPORTED`, `DEGRADED`, `VALIDATE_ONLY`, `UNSUPPORTED`. Severity and compatibility state are separate. An object with an unsupported dangerous field is not globally supported. JSON reports preserve field-level diagnostics.

Normalization sorts semantically unordered collections while preserving argument, init-container, and override order. Hash templates using canonical typed JSON; exclude timestamps, status, and source locations. Include image digests, config/secret reference versions, mounts, and security policy. Secret changes use a random version token updated only when the value changes, not a public hash that enables guessing low-entropy secrets.

`Validate(app)` checks references, duplicate names, ports, selectors, dependency cycles, quantities, networks, volumes, unsupported capabilities, and aggregate host requests. It has no side effects. Guest allocation includes a documented OS/VMM budget in addition to container limits; never confuse a container memory limit with total VM RAM.

### 5.2 Indicative internal APIs

Finalize signatures in T04; preserve these boundaries rather than necessarily these names:

```go
type Frontend interface {
    Compile(context.Context, Source, CompileOptions) (CompileResult, error)
}
type Backend interface {
    Capabilities(context.Context) (Capabilities, error)
    Create(context.Context, SandboxSpec, OperationID) (SandboxHandle, error)
    Start(context.Context, SandboxHandle) error
    Inspect(context.Context, SandboxHandle) (SandboxObservation, error)
    Stop(context.Context, SandboxHandle, time.Duration) error
    Delete(context.Context, SandboxHandle) error
}
type GuestClient interface {
    StartContainer(context.Context, ContainerSpec) (ContainerID, error)
    Exec(context.Context, ExecRequest) (ExecSession, error)
    Signal(context.Context, ContainerID, Signal) error
    Status(context.Context) (GuestStatus, error)
    Shutdown(context.Context, time.Duration) error
}
```

Blocking operations accept contexts and timeouts. `Create` is idempotent by operation key. Stopping/deleting an absent resource succeeds. Streams have `Close` and bounded backpressure. Validation consumes `Capabilities`: a theoretically possible feature is not an available feature.

### 5.3 Persistent state

Follow XDG paths, with home-directory fallbacks and no unprotected shared directories:

```text
$XDG_RUNTIME_DIR/grillo/      socket, lock, process metadata (0700)
$XDG_STATE_HOME/grillo/       desired state, observations, operations, events
$XDG_DATA_HOME/grillo/        volumes, guest artifacts, secret store
$XDG_CACHE_HOME/grillo/       OCI blob CAS and reproducible derived rootfs
```

If `XDG_RUNTIME_DIR` is missing, use a private per-user directory and verify ownership and symlinks. Socket mode `0600`, private directories `0700`, sensitive files `0600`, restrictive umask. State and API versions are independent.

Minimum transaction: write desired snapshot and pending operation in the same application document, create a temporary file in the same directory, `fsync(file)`, rename, and `fsync(parent)`. Only then perform external effects. Atomically update observations/progress; recovery replays idempotent operations. Reserve IPs, ports, and volumes with owner and operation ID before use. Recovery reconciles reservations against applications rather than assuming nonexistent multi-file transactions.

The event journal is append-only NDJSON with persistent sequence IDs and rotation. A truncated last record is recoverable; corruption in the middle must not be ignored. Reject future state versions and back up before migration. Events are not the sole source of desired state.

## 6. Critical path: microVM, guest, network, filesystem

### 6.1 Mandatory backend experiment

Prototype a genuinely rootless topology:

1. A host user supervisor starts a helper in a dedicated user/network namespace.
2. Create TAP/bridge devices using capabilities **inside the user namespace**, not the host's initial namespace.
3. Start VMMs through that helper; expose Unix APIs and vsock through verified private paths/relays.
4. Use a userspace helper for egress and port forwarding; sandbox-to-sandbox traffic crosses the virtual segment.
5. Run DNS and Service proxies in the application network namespace, reachable by guests. Host-to-helper communication uses Unix sockets without privileged `setns`.
6. Isolate applications with default-deny behavior between them. Verify isolation using negative tests; different subnets alone do not prove it.

**This is a hypothesis to validate**, not a universally working recipe. Measure userns support, TAP/bridge creation, VMM/jailer restrictions, KVM access, egress, overlapping addresses, ingress routing, and cleanup. Do not quietly require a root-only jailer. Restrict guest access to host services; Internet egress does not imply unrestricted loopback/LAN access. Host-access policy must be explicit and tested.

The T03 ADR must include reproducible commands, process tree/UIDs, kernel/VMM/helper versions, active security features, risks, and results. If the candidate fails, investigate another backend before coupling frontends/UI to it. A test using a manually root-configured TAP does not pass F0.

### 6.2 Root filesystems and volumes: expose the hard part

Separate:

- Immutable guest base containing the agent and runc.
- Cached immutable OCI-derived container roots with per-container writable layers.
- Persistent application volumes and ephemeral volumes.
- Explicitly authorized host bind mounts.

With Firecracker, prototype virtio-block ext4 disks built **without privileged host mounts**, using verified filesystem tools such as e2fsprogs. Guest-side mounts require guest privileges, not host privileges. Never attach the same writable filesystem to multiple VMs as if that provided shared storage.

Firecracker alone does not solve live bind mounts. Compare authenticated userspace filesystem sharing with correct UID mapping and semantics, or a backend with suitable virtio-fs/9p support. Copying files into the guest is acceptable for immutable configuration; **it is not a live development bind mount**. Before passing MVP, measure and document host↔guest reads/writes, rename, supported chmod behavior, and file-watch behavior.

The chosen design must cover persistent managed volumes, read-only and read-write bind mounts, sharing between containers in one Pod, and rejection of unsupported multi-Pod sharing. Never pass through the home directory or `/` for convenience. Handle canonical paths, symlinks, races, attachment locks, and crash cleanup.

### 6.3 Guest agent

PID 1 startup mounts `/proc`, `/sys`, `/dev`, runtime tmpfs, and cgroup v2; configures networking/DNS; starts control transport; reaps **all** children; handles signals and shutdown. Runc creates OCI bundles and namespaces: shared Pod network namespace, shared IPC where specified, separate container filesystems and PID namespaces unless explicitly configured otherwise. Containers must not see agent files or another container's secrets without declared mounts.

Init containers run sequentially and must succeed before application containers start. Regular sidecars are concurrent application containers. Native sidecar init containers with special restart behavior remain unsupported until specifically implemented. Non-root users must work. Document and test default capabilities, seccomp, no-new-privileges, and cgroups. Reject privileged/hostPID/hostNetwork/Kubernetes hostPath requests until an explicit safe policy exists.

### 6.4 Host/guest protocol

Use vsock or the selected equivalent; never expose management on the application network. Handshake includes major/minor version, sandbox ID, capabilities, nonce, and mutual authentication using a random per-boot key delivered through a private guest channel, not command-line arguments or logs. A compromised guest may read its own secrets: authentication does not make it trusted.

Keep the format small and documented: bounded length-prefixed JSON control messages; binary stream frames with session ID, type (`stdin/stdout/stderr/resize/exit/error`), and bounded length. Initial limits: 1 MiB control messages, 64 KiB data frames; validate lengths before allocation. Requests/responses carry IDs, deadlines, typed errors, and cancellation. Bound connections/sessions, use heartbeats, and reconnect without duplicate starts. Handle short reads/writes, unknown frames, overflow, non-reading peers, and disconnections.

Logs must not block workloads when CLI/UI clients stop consuming. Use a host spool with quotas and rotation; signal data loss. Non-TTY exec separates stdout/stderr. TTY sessions combine them, support resize, and restore the host terminal even after errors or SIGINT.

## 7. OCI images, authentication, and builds

### 7.1 Pull and cache

Implement an explicit OCI Distribution API subset with `net/http`:

1. Normalize reference, registry, repository, tag, and digest; absent tag means `latest`, with a reproducibility warning.
2. Support HTTPS and bearer challenges; bound timeouts, redirects, and sizes; never forward Authorization across hosts.
3. Support Docker schema2/OCI manifests and indexes; select `linux/amd64`; reject schema1 and missing platforms.
4. Verify SHA-256 and size for manifests/config/layers before CAS commit.
5. Download to temporary files, deduplicate concurrent requests, and rename only after verification.
6. Initially support uncompressed tar and gzip layers; reject unimplemented media types.
7. Apply layers in order with correct whiteouts and opaque-directory behavior, permissions and UID/GID handling; prevent escapes through absolute paths, `..`, links, device nodes, or races.
8. Verify `diff_ids`, when present, against uncompressed content; do not confuse them with blob digests.
9. Merge image and runtime configuration: entrypoint/cmd/env/user/workdir/stop signal, preserving explicit overrides.

Unpacking is security-critical: fuzzing and adversarial fixtures are mandatory. If arbitrary UIDs cannot be represented rootlessly, retain metadata for guest-side materialization instead of ignoring failed chown operations; validate this strategy in T09. Partial cache entries must never look valid.

Credentials: read a documented Docker auth-config subset or an explicitly selected credential helper. Base64 `auth` is not encryption. Never expose credentials in events, process arguments, or the UI API. Insecure HTTP registries require an explicit allowlist. Never execute arbitrary helpers/plugins supplied by an untrusted project.

Default `plan` does not pull images and reports unresolved digests. `up` resolves tags and freezes digests in desired state; define `--pull=missing|always|never`. The final diff reports changed resolved digests. An offline plan cannot promise the identity of a mutable tag.

Use locked mark-and-sweep GC with pins for desired images, in-flight operations, rootfs, and existing sandboxes. Destructive maintenance defaults to dry-run. Image GC never deletes volumes.

### 7.2 Builds

First adapter: rootless `podman build`, followed by OCI export and cache import. Verify namespace availability and export format; do not assume a Docker daemon. Keep the adapter narrow so BuildKit can be added later. Use argument arrays, explicit working directories, deadlines, process-group cancellation, and bounded output. `plan` describes builds without running them. Dockerfiles are untrusted code: execute builds only through `up` or an explicit build command. Keep build secrets out of printed arguments; reject unsupported secret mechanisms.

## 8. Networking and discovery

### 8.1 IPAM and Services

Each sandbox receives a private IP stable for its lifetime and a persistent owner-bound lease. Release it only after verified teardown. Provide configurable subnets and conflict checks. A Pod IP may not be directly reachable from the host namespace; `inspect` must explain this and show published endpoints.

Implement ClusterIP using local VIPs and TCP proxies in the application network namespace. Readiness filters endpoints; balance connections round-robin with timeouts and draining. Allocate VIPs separately from sandbox addresses, inside the user namespace only. Do not claim UDP support with a TCP-only proxy. Headless Services return ready Pod addresses. Initially support A records; support IPv6/AAAA only when functional end to end.

Serve guest DNS on port 53 **inside the private namespace**. Support `postgres`, `postgres.default`, `postgres.default.svc`, `postgres.default.svc.cluster.local`, trailing-dot FQDNs, and SRV records for named ports when declared supported. Configure coherent search domains and ndots. Resolve short names through guest search lists and avoid cross-application collisions. Support UDP and TCP fallback, documented short TTLs, and distinct NXDOMAIN/SERVFAIL responses. Bound external forwarding and prevent loops; never expose an open host resolver.

Compose named networks restrict peers to shared allowed networks. If the backend cannot isolate multi-network topologies, reject them rather than silently flattening them. Kubernetes NetworkPolicy remains unsupported in MVP; any future reduced model is `DEGRADED` with precisely enumerated rules.

### 8.2 Port publishing and Ingress

Bind to host `127.0.0.1` and unprivileged ports by default. For ports below 1024, return a clear remapping suggestion rather than invoking sudo. Reserve ports and diagnose collisions before starting workloads; a second `up` must not steal a port.

Support basic `networking.k8s.io/v1` Ingress: host, Service/port backend, `Exact`, and segment-aware `Prefix` paths—not plain string-prefix matching. Diagnose `ImplementationSpecific` paths and controller-specific annotations. Use `httputil.ReverseProxy`, appropriate limits/timeouts, and handling of untrusted forwarded headers. Application ingress must never expose the management API.

Always provide a localhost-port fallback. Never modify `/etc/hosts` automatically. Local TLS is optional later; trusting a development CA requires an explicit consent-based command. Protect certificates and keys. UI and application routes use separate origins.

## 9. Frontends and compatibility

### 9.1 Shared behavior

Bound file sizes, document counts, nesting/aliases, and Helm output; honor cancellation. Preserve source paths/lines when available. Detect `Chart.yaml`, Compose, or Kubernetes `apiVersion/kind`; ambiguous input requires `--format`, not silent guessing.

Unknown fields: harmless metadata/annotations may be `VALIDATE_ONLY`; execution, security, networking, and data semantics require explicit feature-registry entries. Never unmarshal and silently discard them. Blocking unsupported features prevent apply. Downgrades require code-specific consent, such as `--allow-degraded=CODE`; do not add a blanket bypass for security rejections.

Support multi-document manifests and Kubernetes `List`. Reject duplicate resources. Initially block non-default namespaces and cross-namespace references. Applications remain isolated even when they use the same logical namespace.

### 9.2 Compose

F2 subset:

- One service maps to one workload with one container per replica; no implicit grouping.
- `image`, `build`, `entrypoint`, and `command`; distinguish arrays, strings, null, and empty values according to Compose. Never indiscriminately wrap strings in `/bin/sh -c`; test quoting and command semantics.
- `${VAR}` interpolation, default/required forms, and `$$`; document shell/`.env` precedence. Resolve `env_file` relative to the Compose file. `environment` overrides `env_file`; distinguish absent from empty variables. No shell expansion.
- Long/short `ports` syntax, metadata-only `expose`; parse IPv4/IPv6 and protocols while rejecting unsupported transports.
- Named, ephemeral, and project-relative bind volumes; read-only long syntax. External volumes must already exist rather than being implicitly created.
- `healthcheck` `CMD`, `CMD-SHELL`, `NONE`, interval/timeout/retries/start_period. Map Compose health to local status/probes without inventing automatic restart-on-unhealthy behavior. Explain any Grillo-specific endpoint gating.
- `depends_on` is startup ordering, not readiness. Emulate healthy/completed conditions only when implemented; otherwise produce an accepted downgrade or error, never a false guarantee.
- Restart policies no/always/on-failure/unless-stopped, retries, persistent manual stop, representable resource limits, and explicit replica support when implemented.
- Real named networks, aliases, and conflict checks.

Profiles, include/extends, advanced Compose secrets, devices, privileged execution, host networking, and cluster-only deploy options remain outside the first subset with diagnostics. Build support may follow the prebuilt-image example, but is required before claiming the frontend covers specification section 8.

### 9.3 Helm and Kubernetes MVP

Helm supports local charts, ordered repeated `-f` files, stable release identity, `--namespace default`, and documented fixed Kubernetes/API capabilities. Run `helm template`, not `install`; no cluster is required. OCI charts require exact versions and verified cache/digests. Download dependencies only explicitly, honoring lockfiles. Diagnose hooks, `lookup`, test hooks, CRDs, and cluster access rather than pretending a cluster release was executed. Offline `lookup` can return empty: detect usage and disclose the limitation; rendered YAML alone is insufficient evidence.

Never print raw Helm stderr or output, which may include secrets or values. Limit plugin/downloader behavior and inherited environment. Values may be sensitive without having names such as `password`; UI displays only explicitly non-sensitive allowlisted data, not a field-name blacklist.

MVP translators:

- Pod: multiple containers, init containers, env/envFrom, a fieldRef subset (`metadata.name/namespace`, `status.podIP` after allocation), command/args, probes, config/secret/emptyDir/PVC, supported security context.
- Deployment: coherent selector/template, replicas, initially explicit `Recreate` behavior. Unimplemented RollingUpdate is not faithful support. The Kubernetes default is RollingUpdate even when strategy is omitted; disclose the downgrade and require consent until implemented.
- ConfigMap/Secret: data/binaryData, stringData precedence, optional/keyRef behavior, file modes. Service-account-token Secrets remain unsupported without actual credential behavior.
- Service: TCP ClusterIP, selector, numeric/named targetPort, readiness endpoints, headless at the declared milestone. Do not silently map NodePort/LoadBalancer/ExternalName to ClusterIP.
- PVC: local storage, declared binding/retention, requests/accessModes/storageClass; no RWX claim without real support. RWO does not mean one container; distinguish Pods and VMs and reject unsupported multi-VM requests.
- Basic Ingress as described in section 8.

F4 adds StatefulSet identity/per-ordinal storage, Job completion/parallelism/backoff/deadline, RollingUpdate, and other feasible Tier 1 fields. Tier 2: DaemonSet as one local replica only with consent, ServiceAccount as metadata without tokens, PDB as validation only, and reduced NetworkPolicy only after testing. Tier 3 remains rejected with explanations.

### 9.4 Compatibility matrix

Maintain `docs/compatibility.md` and machine-readable data from the same registry used by validators; test for divergence. Each entry records format/version, kind/field, milestone, support state, defaults, consequences, and positive/negative fixtures. Until real runtime integration proves behavior, mark it experimental rather than stable supported functionality.

## 10. Reconciliation, updates, and recovery

### 10.1 State machines

Sandbox: `Pending → Preparing → Booting → Initializing → Running → Stopping → Stopped`; phases can fail, followed by explicit cleanup. Readiness is separate from Running. Container: `Waiting/Running/Terminated`, exit code, attempt, reason, timestamp. Application status aggregates desired/observed state and `Progressing/Ready/Degraded/Failed` conditions without hiding partial failures.

Use one reconciliation loop per application and a bounded worker pool across applications. Triggers include desired changes, process exits, guest/probe events, and periodic jittered resync. Inject clocks in tests. Permanent errors wait for input changes; transient errors use capped exponential backoff and jitter.

Distinguish container restart policy from VM restart policy. Liveness restarts the affected container when possible; guest death recreates the sandbox. Readiness removes endpoints but does not restart. Startup probes gate readiness/liveness until successful. Respect deadlines and thresholds. Execute probes in the correct guest/container network context, not host loopback. Timed-out exec probes must kill their child processes. Job completion must not become an endless restart loop.

### 10.2 Plan and apply

Plans contain a typed action DAG (`PullImage`, `PrepareVolume`, `CreateSandbox`, `UpdateEndpoints`, `Drain`, `StopSandbox`, `DeleteSandbox`, and others), reasons, data impact, and diagnostics. Offline `plan` does not start the daemon, create volumes, pull, or build. Local rendering may use isolated temporary files; remote chart/cache access requires explicit opt-in. Describe unavailable dependencies honestly.

Apply revalidates hashes and the current revision. Concurrent updates produce conflicts rather than silent last-writer-wins behavior. Allocate an operation ID before effects; replay idempotently. Transfer secret material only over the protected private API, never in public plans.

Initial workload updates use Recreate: drain endpoints → stop → create replacement → wait for readiness → add endpoints. Only changed templates are replaced; route-only changes do not reboot VMs. Scale-down preserves stable ordinals and retained volumes. If replacement fails, desired state remains new and status reports failure; automatic rollback requires separate design/tests.

For MVP, config/secret changes recreate consuming sandboxes. Explain that live projected-volume updates are not emulated. Avoid leaking sensitive paths or values into human-readable reasons.

### 10.3 Stop and crash handling

`down` persists desired stopped state to prevent restarts, removes routes/endpoints, sends TERM, waits for grace, escalates to KILL, stops guest/VMM, and releases networking/ports/ephemeral storage. Retain volumes and inspectable definitions according to documented policy. `down --volumes` explicitly deletes only owned managed/PVC volumes, never bind paths or external volumes; block deletion of referenced volumes. Repeated down is safe after partial cleanup.

Test crashes before and after every persistence/effect boundary. Never interpret permission errors as absence. If committing state fails because the disk is full, preserve the previous readable snapshot and stop new mutations.

## 11. API, CLI, and UI

### 11.1 Local API `/v1`

HTTP over Unix sockets with restrictive permissions and Linux peer-UID validation. Public DTOs are separate from private structures. Use `{code,message,resource,retryable,details}` errors without raw stderr/secrets, plus correlation/operation IDs. Bound request bodies, timeouts, and stream counts. Long mutations return `202` and an operation ID with explicit query/cancel behavior.

Define endpoints in `api/local-api.md` before implementing clients:

| Method/path | Purpose |
|---|---|
| `GET /v1/version`, `GET /v1/health` | Handshake and service health |
| `POST /v1/plans` | Shared planning library with controlled private IR input |
| `POST /v1/applications`, `PUT /v1/applications/{id}` | Create/update apply with expected revision |
| `POST /v1/applications/{id}/down` | Stop, with explicit volume-removal option |
| `GET /v1/applications`, `GET /v1/applications/{id}` | List and public status |
| `GET /v1/resources/{id}` | Redacted inspection |
| `POST /v1/resources/{id}/restart` | Tracked restart |
| `GET /v1/logs`, `GET /v1/events` | History, filtering, following, cursors |
| `POST /v1/exec-sessions` | Exec in an explicitly selected container |
| `GET /v1/metrics` | Timestamped snapshots and provenance |
| `GET /v1/images`, `GET /v1/volumes`, `GET /v1/networks` | Inventory and usage |
| `GET /v1/operations/{id}` | Progress and operation errors |

Use SSE events with sequence IDs and `Last-Event-ID`. Expired retention produces a gap event and resync. Log NDJSON/SSE records identify resource/container/stream/time/sequence and obey limits. CLI exec uses a documented framed stream over a dedicated Unix socket or HTTP upgrade; do not hand-roll WebSocket. Browser exec may use POST stdin/resize plus SSE output with session tokens and sequence numbers. Full browser TTY is optional initially; real non-TTY exec belongs in MVP. Expired/disconnected sessions must not leak processes indefinitely.

UI requests for arbitrary host paths or bind mounts use the same core policies as CLI requests. The API must not become an unrestricted host file server.

### 11.2 CLI

Use standard `flag` with subcommand dispatch and a documented parser for flags before/after positionals, repeated `-f`, and exec `--`. Do not rely on default `flag` stopping at the first positional when examples require another behavior. Provide readable output and `--output=json`; no color on non-TTY output.

Commands: up/down/plan/ps/status/logs/shell/exec/restart/inspect/events/ui/doctor; image/volume/network list/inspect/prune according to capability. Resolve `pod/name`, `deployment/name`, `service/name`, and `sandbox/name`. Ambiguity requires an application/container selection, not an arbitrary choice.

`up` waits for operation acceptance; `--wait` waits for readiness with a deadline. Define/document defaults and test help/examples. Failures print an operation ID. `shell` uses an available shell and explains distroless-image limitations; never silently install a shell. Propagate exec exit status. Common CLI codes: 0 success, 1 runtime failure, 2 input/compatibility failure, 3 missing prerequisites, 4 timeout. JSON output includes the cause.

### 11.3 Secure UI without frontend dependencies

Embed assets, use fetch and SSE EventSource. Views cover overview, workloads/Pods, VM boundaries, logs/events, metrics, mounts/routes, diagnostics, and config/secret metadata. Generate SVG topology from actual data, marking inferred relationships. Render logs using text nodes, never HTML. No CDN dependencies.

Bind the bridge to `127.0.0.1:9090` or an explicitly reported available port. Generate a short-lived local bootstrap token, pass it via URL fragment/browser bootstrap, and exchange it for an HttpOnly SameSite cookie. Never log tokens. Check Host/Origin, protect mutations against CSRF, enforce CSP, and keep CORS closed: localhost alone does not prevent hostile websites or DNS rebinding. Public binding is disabled in MVP; future exposure requires TLS/auth or a deliberate insecure override as described in the specification. Initially omit UI secret reveal; explicit CLI reveal must audit the action without logging its value.

Closing the browser or bridge does not stop the daemon/applications. Missing metrics are `unavailable`, not invented zeros. Separate VMM RSS, guest memory budget, container usage, and cache without double-counting shared memory.

## 12. Security and constraints

Maintain [SECURITY.md](SECURITY.md) and an implementation threat model under `docs/security.md`. Treat workloads, images, charts, guests, and events as untrusted. Include VMM/KVM, agent, network helpers, parsers, and filesystem sharing in the trusted computing base. Bind mounts weaken isolation intentionally. Distinguish buggy applications from compromised guest kernels and same-user host attackers; a same-user runtime cannot fully isolate itself from the latter.

Mandatory pre-MVP checklist:

- Never use host `exec.Command("sh", "-c", input)`; shell semantics belong only in explicitly declared container execution.
- Protect secrets in files and transport; redact public API/UI/events/plans/errors. Do not promise to redact arbitrary workload stdout that intentionally prints credentials.
- Bound parsers, secure tar extraction and paths, fuzz DNS/framing, handle symlinks deliberately.
- Version and verify host/guest dependencies; maintain licenses and a minimal SBOM.
- Verify VMM/guest privileges, capabilities, and seccomp. Do not imply Firecracker-jailer protection if the jailer is unused.
- No external management bind; explicit host-network access policy; negative cross-app and guest-to-management tests.
- Quotas for disk/logs/sessions/connections; malformed guest traffic must not exhaust daemon memory.
- Helm/Compose sources must not execute host scripts; downloaded artifacts are not executable without verification.
- Document vulnerability response before claiming production-grade security.

## 13. Testing and performance budgets

### 13.1 Test layers

1. **Unit:** IR, hashes/diffs, state/recovery, quantities, source maps, frontends, IPAM, fake-clock probes, routing, redacted DTOs.
2. **Contract:** fake and real backend behavior, guest protocol, HTTP/Unix API, fake Helm/builder executables, `httptest` registries including TLS/auth/redirects.
3. **Non-KVM integration:** frontend-to-plan, concurrent API/state, writer crashes, DNS/proxy where available, process cancellation.
4. **KVM integration:** real guest/containers and user namespaces, persistent storage, rootless networking; `kvm` build tag and a dedicated runner.
5. **E2E:** equivalent Compose/Helm apps, CLI/UI, fault injection, recovery, security regressions.
6. **Fuzz:** YAML limits, tar paths/whiteouts/links, DNS, guest framing, API decoding; version regression corpora.

Ordinary CI: no formatting diff, `go vet ./...`, `go test ./...`, Linux `go test -race ./...`, host/agent builds, module audit. Pin `govulncheck` as a separate tool. KVM CI is distinct. Missing `/dev/kvm` means a justified SKIP, never a passed hardware requirement. MVP release requires a report from a real KVM host.

### 13.2 Reproducible benchmarks

Record CPU/RAM/disk/OS/kernel/Go/VMM/guest/helper versions, hardware virtualization, cold/warm caches, replica counts, commit, and artifact digests. Use at least 30 samples for simple startup and report median, p95, and failures. Separate pull, unpack, create, boot, agent-ready, container-start, application-ready, and teardown timings.

Measure host and per-VMM RSS/PSS, idle guest memory/CPU, DNS/proxy throughput and latency, bind-mount small-file/write/rename/watch behavior, cache hits, and post-cleanup disk usage. Cluster comparisons require equivalent apps/hardware; they are experiments, not slogans.

Initial nonbinding objectives: host runtime overhead in tens of MiB, warm simple sandbox creation toward <1 second where feasible, bounded cleanup. T03 establishes measured budgets and justified regression thresholds, such as +20% p95 against a noise-controlled baseline. Do not modify tests to hide regressions or turn unmeasured aspirations into absolute promises.

## 14. Examples and acceptance scenarios

Use small fixtures, digest-pinned images, and dynamically allocated host ports in tests.

**A. Multi-container Pod:** API on localhost, sidecar calling it, expected shared network/IPC, separate roots, init container writing a shared file. Verify one VM and distinct container IDs.

**B. Compose web/API/Postgres:** DNS names, persistent database, env_file, healthcheck, localhost port, restart policy; separate optional build fixture. Down/up preserves a database row; explicit down --volumes removes owned data.

**C. Equivalent Helm chart:** Two-replica API Deployment, Service/Ingress, ConfigMap/Secret/PVC, init and sidecar. Plan counts are correct and update downgrades explicit. Failed readiness removes endpoints; recovery restores them.

**D. Updates:** API-only config changes replace only consumers; route-only changes reboot nothing; identical apply performs no disruptive action; scaling 2→1 preserves volumes.

**E. Failures:** Registry 401/500, invalid digests, disk full, denied KVM access, unresponsive guest, killed VMM, daemon SIGKILL, occupied port, dead helper, truncated state. Produce useful errors without unrecoverable VMs/leases/locks.

**F. Security:** Tar traversal/symlinks, oversized YAML/frames, malformed DNS, forbidden guest access to management/other apps/host, HTML logs, CSRF, hostile Host header. No secret values in plan/inspect/events/UI/server logs.

**G. Bind persistence:** Host writes visible to guest and vice versa, read-only writes denied, rename/watch measured; initial copies never count as live sharing.

**H. Process lifetime:** CLI/UI exit leaves workloads running; daemon crash follows documented recovery with possible restarts; repeated down remains safe.

## 15. Executable backlog and definitions of done

Each task produces the required code/tests, consistent documentation, and verification notes. Split tasks that exceed a reasonable session into subtasks preserving the same contracts; do not mark a partially delivered task complete.

### T00 — Repository scaffold and conventions

**Dependencies:** none. **Files:** `go.mod`, minimal commands, `Makefile`, `.gitignore`, `docs/progress.md`, dependency ADR, CI configuration.

- Select Go/module identity, pin allowed dependencies; provide CLI version and doctor skeleton without claiming unperformed checks.
- Add fmt/vet/test/race/build/test-kvm targets; outputs under `bin/`, no committed binaries.
- Maintain task/status/evidence/blocker progress and error/context conventions.

**Done when:** a clean checkout passes basic tests/build without KVM; dependency tree reviewed; no empty speculative packages.

### T01 — Rootless VMM and minimal guest spike

**Dependencies:** T00. **Files:** `experiments/boot/`, guest scripts, `docs/experiments/` report.

- Build a minimal guest, boot KVM as the user, handshake over vsock, execute a guest command, stream logs, stop.
- Test userns/TAP/helpers without root; record distribution restrictions and cleanup.
- Verify host UIDs and perform 30 boot/stop cycles.

**Done when:** reproducible commands and real results exist, no orphan processes remain, failures are visible. This is not yet the OCI runtime.

### T02 — OCI, filesystem, and application-network spike

**Dependencies:** T01.

- Run an OCI image through guest runc, exec and signal it; run two containers in one guest communicating over localhost.
- Connect two VMs, verify DNS/egress/host publishing and management isolation.
- Test persistent managed storage and live read/write/read-only binds; measure filesystem overhead.
- Use a preexisting OCI fixture to separate runtime feasibility from registry implementation.

**Done when:** minimal A/G scenarios pass on real hardware without privileged host mounts or copy-based bind substitutes. Otherwise BLOCKED pending backend comparison.

### T03 — F0 gate and platform ADR

**Dependencies:** T02.

- Confirm VMM/network/sharing/rootfs/supervision with diagrams, versions, and commands.
- Record risks, doctor prerequisites, measured memory/time budgets, and support matrix.
- Record Helm CLI/state JSON deviations; choose alternatives if evidence invalidates them.

**Done when:** evidence supports the decision. Backend/storage-dependent tasks must not start before this gate passes; pure IR/frontend work may proceed independently.

### T04 — IR, diagnostics, and capabilities

**Dependencies:** T00. **Files:** `internal/model`, `internal/source`, JSON fixtures.

- Implement section 5 types, validation, normalization/hashes, support registry, quantity parsing.
- Provide an experimental native JSON manifest for runtime tests, not a new stable public format.
- Add secret references and public redaction immediately.

**Done when:** deterministic goldens, unknown-version/reference/cycle/overflow tests pass; model imports no VMM/frontend code.

### T05 — State, secrets, and recovery primitives

**Dependencies:** T04. **Files:** `internal/state`, `internal/secrets`.

- Implement writer lock, XDG layout, atomic snapshots, pending operations, bounded journal, schema migration.
- Inject failures at each commit step; validate owners, permissions, and symlinks.
- Separate secrets and DTOs; collect obsolete secret versions only when unreferenced.

**Done when:** crash/disk-full scenarios recover, two writers are rejected, future versions fail, permissions are correct, and secret fixtures do not leak publicly.

### T06 — Guest protocol and testable client

**Dependencies:** T03, T04. **Files:** `internal/guestproto`, `api/guest-protocol.md`.

- Implement handshake/version/auth, lifecycle/exec/status/probe messages, framing, cancellation.
- Separate codecs from transport; provide fake `net.Conn` and the chosen vsock/relay adapter.

**Done when:** roundtrips, incompatibility, partial I/O, overflow, backpressure, timeout, and failed-auth tests pass; fuzzing finds no panic or unbounded allocation.

### T07 — Guest PID 1 and OCI runtime

**Dependencies:** T06. **Files:** `cmd/grillo-agent`, `internal/guest`, `guest/`.

- Build reproducible guest artifacts, mounts/cgroups/reaping, runc bundles, init/app containers, user/capability handling.
- Add logs, TTY/non-TTY exec, resize, signals, exit status, shutdown.
- Enforce supported resource limits and namespaces.

**Done when:** real A/start/exec/stop scenarios pass; init failure blocks apps; sidecar localhost works; no zombie children; artifact manifest records versions/checksums.

### T08 — Backend lifecycle and process supervision

**Dependencies:** T05, T07.

- Implement the selected adapter's devices/disks/network/config, boot/handshake, inspect, grace/kill/delete.
- Add idempotent operation IDs, robust process identity, VMM API deadlines, reverse-order cleanup.
- Diagnose missing host capabilities.

**Done when:** guest/VMM/supervisor failures are handled; repeated create/stop/delete never duplicate resources; recycled PIDs are never killed; 100 cycles show no evident leaks.

### T09 — OCI registry and CAS

**Dependencies:** T04 and T03 for rootfs materialization.

- Implement references/auth/manifests/indexes/blobs, atomic caching, safe unpacking, metadata merge.
- Add opt-in credential helpers, pull policies, digest pinning, HTTP limits.

**Done when:** schema2/OCI/gzip/whiteout/user/entrypoint fixtures pass; unsafe auth/redirects and corrupt digests/sizes/diffIDs fail; adversarial tar cannot escape; concurrent pulls deduplicate.

### T10 — Storage manager

**Dependencies:** T05, T08, T09.

- Implement validated storage backend, volume IDs/owners/refcounts/leases, ephemeral/managed/PVC/bind behavior.
- Attach volumes across Pod containers; project readonly config/secrets with correct permissions; validate capacity/access modes.
- Add safe retention/deletion and explicit UID mapping.

**Done when:** G passes; database survives restart; readonly is enforced; attachment failures recover without permanent locks; down --volumes never touches bind/external data.

### T11 — Production rootless networking and IPAM

**Dependencies:** T05, T08.

- Replace spike shortcuts with a supervised helper and controlled protocol.
- Add application namespaces, guest/VIP addresses, egress policy, guest DNS setup, publishing, logical-network capabilities.
- Persist leases and clean up safely.

**Done when:** same-named services in different apps remain isolated; management is unreachable from guests; egress/publishing work without root; helper failure is visible; crash recovery retains consistent leases.

### T12 — DNS, Service proxy, and Ingress

**Dependencies:** T11, T04.

- Implement UDP/TCP DNS, search names, forwarding, TCP Service VIPs, ready endpoints, named target ports.
- Add Exact/Prefix ingress, localhost fallback, port/hostname conflict handling.

**Done when:** all four specified service names resolve, connections balance across replicas, unready endpoints are excluded, timeout/path-segment tests pass, UDP Service limitations are explicit.

### T13 — Observability and probes

**Dependencies:** T05, T07, T12.

- Add sequenced events, bounded log spool/follow, resource snapshots, source mapping.
- Implement exec/HTTP/TCP startup/readiness/liveness probes, thresholds, fake-clock tests, nonoverlapping scheduling.
- Observe guest/container exits without excessive polling.

**Done when:** startup gates other probes, readiness does not restart, liveness restarts only the intended container, slow consumers do not block apps, missing metrics are explicit, timed-out probes leave no processes.

### T14 — Planner, reconciler, and updates

**Dependencies:** T08–T13.

- Implement action DAG/diff, desired/observed state, bounded workers, classified retries, operation recovery.
- Add Recreate, scaling, VM/container restart policies, config updates, draining.
- Execute native manifests for F1; implement idempotent down and crash recovery.

**Done when:** core A/D/E/H scenarios pass; identical and route-only applies reboot nothing; persistence/effect fault injection and race checks pass. F1 requires real networking and volumes.

### T15 — Local API and daemon lifetime

**Dependencies:** T14.

- Implement Unix API/version/health, peer UID, limits, error DTOs, asynchronous operations, streams/cursors.
- Add singleton autostart, startup recovery, and distinct daemon-shutdown/application-down semantics.
- Publish endpoint/session contracts and client/server tests.

**Done when:** concurrent CLIs share one daemon; malformed requests do not crash it; unauthorized access fails; disconnected clients do not cancel accepted apply unless explicitly requested; session leak tests pass.

### T16 — Native-runtime CLI

**Dependencies:** T15.

- Implement command parsing/output/resource resolution, offline shared-core plan, lifecycle/inspection commands.
- Add logs/events/exec/shell/restart and real doctor checks; test arguments and exit codes.

**Done when:** specification section 19 examples work with native manifests; `--` and repeated flags parse correctly; terminal restoration works; CLI exit does not own VM lifetime; doctor never changes the system or requires root.

### T17 — Compose parser and compiler

**Dependencies:** T04; final integration requires T16.

- Implement YAML source maps, interpolation, env precedence, volumes/ports/healthchecks/restart/networks.
- Register field-level support, unknown-field diagnostics, dependency DAG.
- Add IR/plan goldens and selected optional comparisons with `docker compose config` as a test tool, not a runtime dependency.

**Done when:** every supported feature has positive/negative fixtures; injection and secret leakage tests pass; absent/empty behavior is verified; frontend does not call the backend directly.

### T18 — Builder and image/volume/network tooling

**Dependencies:** T09, T10, T11, T17.

- Add rootless Podman build/export/import and cancellation; inventory/inspect/prune and CAS pins.
- Delegate build context and dockerignore interpretation to the builder.

**Done when:** a real local build executes successfully; plan never builds; GC preserves active data; missing tools produce actionable errors.

### T19 — F2 gate: Compose application

**Dependencies:** T12–T18.

- Run complete B with real supported named-network behavior and up/update/down/recovery tests.
- Document reproducible prerequisites and commands.

**Done when:** clean example checkout runs without root, preserves data, and cleans up correctly; attach real E2E evidence; no Docker daemon dependency.

### T20 — Kubernetes MVP compiler

**Dependencies:** T04; T14 for semantic verification.

- Implement multi-document/List and Pod/Deployment/Service/Ingress/ConfigMap/Secret/PVC subset with security/resources/probes.
- Define selectors, target ports, init/sidecars, config refs/optional behavior, namespace/resource validation.
- Add registry and unknown-field tests.

**Done when:** goldens and privileged/hostNetwork/Tier3 rejection tests pass, secrets never enter diffs, default RollingUpdate is not represented as faithful Recreate, multi-container runtime tests pass.

### T21 — Helm rendering and OCI charts

**Dependencies:** T20.

- Add controlled Helm versions, ordered values, offline capabilities, release identity, local/OCI versioned charts.
- Enforce output/time/cancellation limits; diagnose hooks/CRDs/lookup and restrict plugins/dependencies.

**Done when:** equivalent chart/manifests yield equivalent IR; repeated inputs are stable except explicitly diagnosed nondeterministic Helm functions; errors are redacted; no cluster access is required; remote fetching is explicit.

### T22 — Helm gate and compatibility reporting

**Dependencies:** T19, T21.

- Run C; add degraded/validation-only/unsupported fixtures and text/JSON plan counts.
- Publish registry-derived matrix with fixture references and semantic notes.

**Done when:** a realistic subset chart runs without kube-apiserver; unsupported input blocks provisioning; config changes replace only consumers; PVC data persists.

### T23 — Web console and secure bridge

**Dependencies:** T15, T22.

- Embed section 11 views, SSE reconnect/gap handling, log filters, routes, source diagnostics.
- Add local bootstrap authentication, CSRF/Origin/Host/CSP checks; route mutations through the core API.
- Support real browser non-TTY exec and CLI shell guidance; track full browser TTY separately if incomplete.

**Done when:** handler tests and documented browser smoke tests pass; secrets are absent from DOM/responses; HTML logs do not execute; UI closure leaves workloads running; public binding is rejected in MVP.

### T24 — F3 MVP gate and hardening

**Dependencies:** T23.

- Run A–H, CI fuzz smoke and documented longer campaigns, race/vulnerability/license checks.
- Complete benchmarks, verifiable installation, artifact manifest, threat model, and compatibility matrix.
- Test on at least one clean Linux host outside the development environment; document tested distributions.

**Done when:** every specification section 28 requirement has evidence or an explicit blocker. Do not label a release MVP if live binds/rootless operation/probes/Helm/UI are missing. Known limitations do not erase gate requirements.

### T25 — StatefulSet and Job

**Dependencies:** T24.

- Implement stable ordinals/hostnames, headless DNS, volumeClaimTemplates retention, documented updates, OrderedReady or explicit limitation.
- Add Job parallelism/completions/backoff/deadlines and persistent terminal outcomes; crash retries must not duplicate completion accounting.

**Done when:** scale-down/up reuses correct PVCs; completed Jobs stay completed after daemon restart; fake-clock and E2E failure/backoff tests pass. Do not claim every field of a kind is supported.

### T26 — Extended Tier 1/Tier 2 parity and TLS

**Dependencies:** T25.

- Implement RollingUpdate with maxSurge/maxUnavailable, capacity preflight, readiness/drain, documented rollback policy.
- Add consent-based local DaemonSet, metadata-only ServiceAccount, PDB validation, reduced network policy only when proven.
- Add opt-in local CA/TLS and further DNS/resource/security/storage semantics through the matrix.

**Done when:** updates respect ready-replica policy, CA trust is never automatic, downgrades require explicit acceptance, and F4 has feature-by-feature evidence.

### T27 — Measured optimization and packaging

**Dependencies:** T24; T26 before claiming complete F4.

- Profile host/guest with pprof; optimize rootfs/cache/startup against measured baselines.
- Produce reproducible releases for verified architectures, checksums/SBOM/licenses, user install/uninstall, application CI workflows.
- Reconsider SQLite or new dependencies only for measured needs.

**Done when:** before/after reports and regression checks exist; uninstall preserves data unless explicitly requested otherwise; every added dependency is justified.

### T28 — Future research, not a prerequisite

**Dependencies:** stable F3 and measured use cases.

Snapshots, warm pools, alternate VMMs, macOS outer VMs, confidential computing, GPU, and multi-host execution require separate RFCs. Establish a threat model and measurable benefit before implementation. Do not preallocate unused APIs for them.

### 15.1 Recommended single-agent order

`T00 → T01 → T02 → T03 → T04 → T05 → T06 → T07 → T08 → T09 → T10 → T11 → T12 → T13 → T14 → T15 → T16 → T17 → T18 → T19 → T20 → T21 → T22 → T23 → T24 → T25 → T26 → T27`.

Pure portions of T04/T17/T20 can progress offline while hardware is unavailable, but cannot complete runtime gates. Multiple agents must agree on versioned contracts before concurrently editing dependent components.

## 16. Specification traceability

| Specification | Implementation and evidence |
|---|---|
| §§1–7, 10: architecture and IR | T03–T05, T14–T16; A/H |
| §8: Compose | T17–T19; B |
| §9: Helm/Kubernetes | T20–T22, T25–T26; C |
| §§11–13: microVM/agent/rootless | T01–T08, T24; A/E/F |
| §§14–15: networking/ingress | T11–T12, T26; B/C/F |
| §§16–17: storage/config/secrets | T05, T09–T10, T20; B/F/G |
| §18: lifecycle/probes | T13–T14, T25–T26; C/D/E |
| §§19–21: CLI/UI/API | T15–T16, T23; H and contract tests |
| §§22–23: state/images/builds | T05, T09, T18; E and fault injection |
| §§24–25: performance/observability | T03, T13, T24, T27; benchmarks |
| §§26–27: compatibility/security | T04, T17, T20–T24; F and matrix |
| §§28–31: MVP/phases/user journey | F0–F4, T19/T22/T24; A–H |
| §32: open questions | T01–T03, T25; evidence-backed ADRs |
| §§33–34: success | T24/T26 and T27 comparative measurements |

## 17. LLM delivery template

Update `docs/progress.md` with an entry like:

```text
Task: Txx — title
Status: TODO | IN_PROGRESS | BLOCKED | DONE
Dependencies verified:
Files and contracts changed:
Decisions/ADRs:
Tests run (command, environment, result):
Tests NOT run and why:
Integration/benchmark evidence:
Known limitations:
Next task:
```

Suggested implementer prompt:

> Read AGENT.md, IMPLEMENTATION_PLAN.md, docs/progress.md, and the specification sections relevant to Txx. Implement only Txx while respecting contracts and dependencies. Do not introduce unapproved third-party libraries, host-container fallbacks, or simulated functionality. Write positive and negative tests, run applicable checks, and update progress and compatibility documentation. If a hardware or semantic gate cannot be verified, mark it BLOCKED with evidence instead of claiming completion. Summarize changed files, checks, and the next step.

**Final rule:** judge the project by contracts it actually preserves and limitations it clearly reports, not by the number of resource kinds its parser accepts.
