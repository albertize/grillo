# Runtime architecture

[README](../README.md) | [API](../api/local-api.md) | [Decisions](adr/README.md)

This guide describes the implemented runtime, not every feature in the
[product specification](../grillo-project-specification.md). Current support and
its exceptions live in [compatibility](compatibility.md).

## One execution path

```text
Compose ──────────────────────┐
Kubernetes manifests ─────────┼─> versioned IR -> planner -> reconciler
Helm -> Kubernetes compiler ──┘                               |
                                                  native executor
                                                   /           \
                                            host services    QEMU microVMs
                                                   \           /
                                                    private API
                                                     CLI / UI
```

Compose and Kubernetes frontends compile into `internal/model`'s
`grillo.dev/v1alpha1` IR. The controlled Helm renderer invokes the explicitly
provisioned official CLI, then uses the Kubernetes compiler. Compilation,
validation and offline planning do not control VMMs.

`internal/plan` computes changes; `internal/reconcile` records intent and applies
bounded, retryable operations. `internal/executor` connects those operations to
sandbox, storage, image and network components. `cmd/grillod` wires the concrete
runtime and recovery into `internal/api`. There is no Kubernetes control plane.

## Pod and guest boundary

One Kubernetes Pod maps to one QEMU `microvm`; Compose services map to separate
workloads/sandboxes. The Go PID 1 agent starts containers through guest-side
runc. Containers share the intended guest network/IPC context and localhost,
while using separate roots and PID namespaces. Init containers complete before
regular containers start.

The guest protocol uses authenticated, bounded vsock exchanges. Authentication
does not make guest messages trusted. See the [protocol](../api/guest-protocol.md)
and [guest build](../guest/README.md) for actual contracts and artifact handling.

Immutable OCI content is cached separately from writable container roots. A
container's writable overlay lives in guest tmpfs; large writes consume guest
memory, and per-container disk quotas are not implemented. Declared volumes
provide persistence. Host binds are explicit sharing, not part of the private
container root.

## Host networking and storage

Each application has a pasta-backed user/network namespace supervised by
`internal/netns`, with a bridge and per-sandbox TAP devices. DNS and TCP Service
proxies run in that namespace; readiness controls eligible endpoints. Host
publishing uses loopback listeners and basic Ingress fallback routes. Applications
are isolated even when they reuse logical names/addresses.

The host TCP publication primitive admits **128 simultaneous connections per
listener**, immediately closing excess peers without a worker queue. Its
connections have a **five-minute total read/write lifetime**; applications using
long-lived TCP sessions must reconnect. Both copy directions preserve half-close
responses and finish before shutdown returns. These are operational limits, not
unlimited TCP-session compatibility or guest-network benchmark claims.

virtiofsd exposes declared filesystem shares. Managed/PVC storage has owner-bound
leases and explicit retention; repeated down is safe and does not delete bind
paths. Unsupported multi-VM writable sharing is rejected. Host-originated
virtiofs notifications are degraded, so file watchers need polling.

`grillod --volume-image-bytes N` opts new non-bind volumes into fixed raw ext4
images mounted in the guest through virtio-blk. Private, interprocess admission
bounds total image geometry and image count; actual filesystem ENOSPC bounds
guest data writes. Default 0 retains directory mode. No host mount/sudo,
automatic resize/migration or bind quota is implied. The reviewed mke2fs 1.47.3
formatter is only required in this mode; usable payload is smaller than geometry.
[ADR 0014](adr/0014-fixed-size-block-volume-quotas.md) records crash/repair and
compatibility limits. Default native RUN workspaces remain writable virtiofs.
The opt-in block path below is software-integrated but lacks real hardware
acceptance; CAS/rootfs logical quotas alone are not guest-write enforcement.

Native builds now share a per-build host-write budget across context/stage
copies, base-image extraction and compressed layer staging: 4 GiB cumulative
writes and 100,000 conservative path-entry charges by default. Reservations run
before effects, including overwrites. Context copies and snapshot/layer reads
use rooted filesystem access with bounded scans and cancellation checks.
[ADR 0015](adr/0015-native-host-write-budgets.md) records the host-only scope;
default guest RUN writes and host-wide admission remain outside that policy.

The optional guest `build-export` capability generates full build snapshots in
agent code, with bounded tar streams and authenticated completion receipts.
RUN/export are serialized; failed RUN cleanup blocks subsequent build operations.
The host receiver validates and extracts into private, budgeted staging.
[ADR 0016](adr/0016-agent-owned-build-export.md) records the original primitive.

`grillod --build-workspace-bytes N` selects a fresh raw ext4 disk per native RUN,
with read-only seed input, agent population and verified snapshot adoption after
guest cleanup. Numeric ownership/root metadata and special modes are kept in
private build staging without host chown. Image geometry/archive staging are
budgeted; RUN output is capped at 4 MiB. Offline builds use a backend without the
build-network launcher. Native builds serialize per daemon; retained/interrupted
workspaces block further admission and require explicit review. This does not
reserve physical host free space or coordinate unrelated daemons.
[ADR 0017](adr/0017-opt-in-block-build-workspaces.md) records the opt-in scope,
missing KVM/crash gates and default legacy behavior.

Explicit digest OCI references can reuse only a verified cached index/manifest
and config/layer graph, including blob digest/size checks. Mutable tags still
contact the registry; local mutable labels cannot replace a requested digest.
Config/local metadata is limited to 8 MiB. Seeded installed tests clearly
separate cache reuse from fresh online registry measurements.

The selected backend is QEMU/virtiofsd ([ADR 0005](adr/0005-platform-qemu-virtiofsd.md));
Firecracker was evaluated but cannot provide the required live shared-filesystem
device. Neither host permissions nor security policy are changed automatically.

## Lifetime and recovery

The CLI starts `grillod` on demand. A per-user state lock enforces one writer for
a layout; the private Unix socket checks peer UID. The daemon, not the CLI or
browser, owns workload lifetime.

Desired state and operation progress use atomic JSON snapshots and a bounded
journal under XDG directories. Secret values use separate private storage; public
IR/plans/views use references or allowlisted metadata.

After daemon failure, recovery cleans up identity-verified orphan resources and
recreates desired sandboxes. It does **not** adopt running VMs or guarantee zero
downtime. Unverified process identity blocks cleanup; a PID alone is insufficient.
Stopped applications remain stopped, and retained volumes survive recovery.

Guest CIDs are host-wide. Separate XDG state does not allocate an independent CID
namespace; concurrent isolated daemons must coordinate explicit ranges. Current
CID-base flags do not provide a global allocator.

## CLI and browser

The CLI and loopback UI bridge use the same Unix API client. The bridge serves
embedded React/PatternFly assets and exposes a bounded authenticated projection
of API operations. It does not read arbitrary host files or serialize private
guest specifications. Closing it cancels its requests, not applications.

The [console guide](ui.md) explains bootstrap/session handling, CSP, metadata-only
configuration, inferred topology and non-TTY exec. The UI cannot manufacture
original compiler diagnostics or unavailable usage. Actual guest `/proc` and
container cgroup counters now carry collection time/provenance; they are not
host attestation. Interactive CLI exec uses a dedicated authenticated channel;
captured browser Exec remains non-TTY. Pod Terminal adapts that same private Unix
attach client to authenticated output/input/resize/close HTTP requests, with bounded
sessions and xterm-compatible rendering ([contract](../api/local-api.md#browser-terminal-adapter-loopback-bridge-only)).
[ADR 0013](adr/0013-xterm-browser-terminal.md) records the locked emulator,
style-only CSP nonce and disabled workload-driven host effects.
Production boot inputs use verified private snapshots
and fresh credential overlays ([ADR 0009](adr/0009-verified-boot-and-private-key-overlay.md)). Container stdout/stderr
is drained into a bounded guest-wide ring and polled into the daemon spool;
retention gaps are explicit and do not block workloads. See the
[log delivery evidence](experiments/t24-guest-logs.md) for limits.

## Where to look

| Area | Source |
| --- | --- |
| IR and diagnostics | `internal/model`, `internal/source` |
| Input compilation | `internal/frontend` |
| Planning and reconciliation | `internal/plan`, `internal/reconcile` |
| Runtime effects and wiring | `internal/executor`, `cmd/grillod` |
| VMM and process identity | `internal/backend/qemu`, `internal/sandbox`, `internal/platform/linux` |
| Guest control | `internal/guest`, `internal/guestproto`, `cmd/grillo-agent` |
| Networking and storage | `internal/netns`, `internal/network`, `internal/storage` |
| Images and native builds | `internal/oci`, `internal/image`, `internal/dockerfile`, `internal/build` |
| State and secrets | `internal/state`, `internal/secrets` |
| API, CLI and console | `internal/api`, `internal/cli`, `internal/ui`, `web` |

Significant decisions remain in [ADRs](adr/README.md); their review status is not
changed by this guide. The [security policy](../SECURITY.md) defines the threat
model and reporting process. Hardware isolation reduces direct host-kernel
exposure, not all risk from VMMs, helpers, parsers or authorized host sharing.
