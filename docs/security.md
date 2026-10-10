# Experimental runtime threat model

[Security policy](../SECURITY.md) | [Architecture](architecture.md) | [T24 evidence](experiments/t24-hardening.md)

This is an implementation-oriented threat model, not an independent audit or a
production security guarantee. Scope: Linux/amd64 QEMU microvm, virtiofsd, pasta,
Go host runtime/agent, guest runc, Unix API and loopback React console. The
specification's security goals remain requirements where implementation or
verification is incomplete.

## Assets and adversaries

Protect host files not deliberately shared, unrelated applications, private
management operations, secret material, image/artifact integrity, persistent
volumes and availability of the user's workstation.

Inputs and actors include malicious manifests/charts/images, compromised
application containers or guests, hostile web pages, malformed protocol peers,
and slow/disconnected clients. A fully compromised guest kernel controls its
agent and guest secrets; authenticating a channel does not make it trustworthy.
A same-UID host attacker can access that user's files/processes and is not
isolated by this rootless runtime. Host administrators, the host kernel/KVM,
verified tools and their supply chains are trusted, not defended against.

## Trust boundaries and controls

| Boundary | Implemented controls and evidence | Residual risk / required work |
| --- | --- | --- |
| Source/chart → host compiler | Bounded parsing, field registry, explicit downgrade consent, Helm argument arrays/restricted environment; frontend tests | Rendering tools and parsers are trusted; no claim of arbitrary chart compatibility |
| Registry/archive → content/rootfs | Digest verification, atomic CAS/dedup admission recheck, finite shared-layer payload/entry/decompressed-stream limits, bounded diff_id, traversal/link/device rejection; [OCI quota tests/fuzz](experiments/t24-oci-extraction-budgets.md) | Filesystem race and resource exhaustion review remains necessary; fuzz duration is not a proof |
| Container → guest | runc roots/PID namespaces, shared intended network/IPC, no-new-privileges, reduced capabilities and cgroup settings; scenario A | Containers in a Pod share a kernel; guest-container syscall filtering is not asserted by the current OCI config |
| Guest → host | KVM/QEMU microvm, authenticated bounded vsock; QEMU sandbox flags; virtiofsd namespace sandbox and seccomp kill configured | No Firecracker jailer or absolute isolation claim; guest/KVM/devices/helpers remain security-critical |
| Shared filesystem → host data | Explicit bind authorization, read-only exports, owner/lease checks for managed data; scenario G/F0 | Authorized writable binds intentionally expose host data; host-originated file watches require polling |
| Application → other apps/management | Separate rootless namespaces, network policy, loopback publication and private management transport; bridged/netns/F0 negative tests | Not full Kubernetes NetworkPolicy; loopback/LAN restrictions are only those implemented/tested |
| Host caller → local API/state | Private paths/socket, peer UID, one writer, atomic snapshots, identity-checked cleanup; state/API/recovery tests | Same-user compromise remains out of scope; recovery recreates VMs and can interrupt workloads |
| Hostile site/output → browser bridge | One-use bootstrap, HttpOnly/SameSite session, Host/Origin/CSRF checks, closed CORS, CSP, escaped text; handler and Firefox tests | Loopback is not itself authentication; no public bind, TLS, secret reveal or full TTY support |
| Boot inventory → launched bytes | Strict pinned manifest; verified private kernel/initramfs copies before helpers/VMM; fresh private per-boot key overlays; T24 native/unit tests | Local expectations are not publisher authentication; host tools and complete provenance/SBOM remain separate |
| Private data → public surfaces | Separate secret store/public references, allowlisted view DTOs, redacted tool diagnostics; frontend/API/UI tests | Application output deliberately printing a secret cannot be promised redaction |
| Slow/malformed peers → availability | Frame/request/output/session limits, cancellation, bounded workers, guest log ring with explicit gaps and concurrent rotated spool; protocol/API/exec/log tests | [CAS/rootfs aggregate logical quotas and bounded contention/KVM load](experiments/t24-cache-load-license-review.md) now have evidence; no physical/global-host/volume quota or long-duration hostile-guest proof; writable guest tmpfs consumes guest memory |

Controls above describe configured behavior and regression evidence, not an
independent confirmation of every sandbox restriction under attack. Review
`internal/backend/qemu/config.go`, `internal/guest/oci.go`, `internal/guestproto`,
`internal/oci`, `internal/state`, `internal/api` and `internal/ui` when changing
these boundaries.

## Artifact and credential assumptions

Direct development component fixtures retain an image-baked private key. The
daemon ignores legacy `--key-file`: runtime/native-build QEMU boots pin a strict
local manifest, hash kernel/initramfs while copying into private 0600 snapshots
before helpers/VMM, and supply a fresh 32-byte key via a private initramfs overlay.
Idempotent starts retain live credentials; actual restarts rotate them. Private
state/derived images contain those credentials and must never be printed or
redistributed. Snapshots prevent launch-time source mutation; they do not defend
against the owning UID. See [ADR 0009](adr/0009-verified-boot-and-private-key-overlay.md)
and [native/negative evidence](experiments/t24-interactive-metrics-artifacts.md).
Host tools and publisher/source authentication remain separate trust assumptions.

Local checksums detect mismatches against trusted expected content; they do not
establish publisher authenticity by themselves. Guest kernel/runc/BusyBox,
host VMM/helpers and frontend fonts/icons keep their upstream licenses.

## Verification and release blockers

See [T24](experiments/t24-hardening.md) for commands, tested environment,
fuzz scope, benchmark measurements and the §28 blocker inventory. Required
remaining work includes clean external-host validation, a verifiable release
installation path, resource quota/load evidence, full performance accounting
(including new verified-copy costs) and provenance/license review. Guest/cgroup
counters and interactive CLI execution now have local evidence; full browser TTY
and independent attestation of compromised-guest counters are not claimed. Only Fedora on the recorded
development machine has been revalidated; SELinux was already Permissive.
Grillo never changes host policy or invokes sudo to bypass a failed boundary.

Report suspected vulnerabilities using [SECURITY.md](../SECURITY.md), not public
progress logs. Release support/private reporting arrangements require verified
maintainer decisions; this document does not invent a contact or response SLA.
