# Runtime review remediation

Scope: eight findings from the runtime code review. This is a corrective workstream for T07–T10, T14–T16, T19 and T20, not a new feature milestone.

Status vocabulary: TODO, IN_PROGRESS, BLOCKED, DONE. Implementation and hardware evidence are tracked separately. No hardware-dependent item is DONE on the strength of fake-backend tests alone.

| ID | Finding | Implementation | Verification |
|---|---|---|---|
| R1 | Isolate writable container roots from the immutable image cache | DONE | Unit + real KVM |
| R2 | Resolve per-container security policy and enforce or reject it | DONE | Unit + real KVM |
| R3 | Wire workload networking in the actual daemon | DONE | Real KVM daemon |
| R4 | Persist desired/observed state and recover after daemon restart | DONE | Unit + SIGKILL restart |
| R5 | Serialize desired-state publication with application mutations | DONE | Deterministic concurrency test |
| R6 | Correct millicore-to-CFS quota conversion | DONE | Boundary unit + in-guest cgroup |
| R7 | Resolve Pod PVC aliases to one claim identity | DONE | Compiler regression |
| R8 | Select exec targets deterministically and reject ambiguity | DONE | Protocol unit |

## Constraints and acceptance

- Rootless host operation; no implicit downloads, privilege escalation, or host-container fallback.
- Recovery may restart sandboxes, as specified in implementation plan §4.1. Unverified process identities must block cleanup rather than authorize termination.
- Persist intent before effects; keep Secret values outside application state.
- Unsupported security semantics fail closed, including native API input.
- Preserve unrelated files and changes; do not commit without authorization.
- Rebuild guest artifacts before hardware tests. Missing hardware/tools/artifacts are blockers, not passes.

## What changed

- **R1** `internal/executor`: image shares are now `ReadOnly`, container roots are opened with `PrivateRoot`, rootfs cache keys are versioned `immutable-v1-*`; `internal/guest/rootfs_linux.go` builds a per-container overlayfs (private upper layer in guest tmpfs) and fails closed when overlayfs is unavailable.
- **R2** `internal/model` (per-container `SecurityProfile`), `internal/frontend/kubernetes` (copy-on-write effective policy, `runAsUser`/`runAsGroup` validation, `readOnlyRootFilesystem` honored for both true and false), `internal/executor/security.go` (rejects privileged/seccomp/capability/subPath, resolves user and read-only root), `internal/guest/oci.go` (root `readonly`).
- **R3** `cmd/grillod/main.go` enables `EnableNetwork`, the netns supervisor, QEMU/pasta paths and `RuntimeDir`, and installs the `Launch` callback before any effect.
- **R4** `internal/reconcile/persistent.go` (atomic `applications.json` intent + observed progress under the state writer lock), `cmd/grillod/recovery.go` (replays durable intent, honors stopped/removeVolumes), `internal/backend/qemu/recovery_linux.go` (QMP `SO_PEERCRED` host PID, identity-verified stop, deletes only verified sandboxes), startup recovery before the API serves.
- **R5** `core.mutationMu` serializes `SetDesired`, persistence and effects as one operation; the reconciler still holds its per-application lock.
- **R6** `internal/executor/executor.go` uses a 1 s CFS period, `quota = millicores * period / 1000`, and rejects negative/overflowing limits.
- **R7** `internal/frontend/kubernetes/compile.go` resolves Pod volume aliases to the claim name, propagates `readOnly`, rejects duplicate volume names and `subPath`.
- **R8** `internal/executor/executor.go` records container names per sandbox, requires a unique target, and accepts `sandbox-ID/container` to disambiguate; unknown/cross-application targets fail.

## Verification evidence

- `make check` (fmt, vet, unit, scripts, race, build, module audit): PASS.
- `make test-executor`: PASS on real KVM/QEMU.
- `make test-f2`: PASS on real KVM (Compose web/worker, DNS, volume, down/up).
- `make test-k8s`: PASS on real KVM (init + app + sidecar).
- `TestKVMDaemonRemediation` (`go test -tags kvm ./cmd/grillod`, real `grillod` + `grillo-netns` process and API): PASS. It proves, on one application, that a native build is pulled, cross-VM Service DNS/fetch works, a private write inside one sandbox is not visible in a sibling or in the shared image cache, `runAsUser` is enforced as UID 1000, a read-only root rejects writes, the in-guest `cpu.max` is `100000 1000000` (100m), and that after `SIGKILL` of the daemon the desired state is recreated while a stopped application stays stopped.
- Unit regressions: `internal/executor/remediation_test.go`, `internal/frontend/kubernetes/remediation_test.go`, `internal/guest/rootfs_linux_test.go`, `internal/reconcile/persistent_test.go`, `internal/backend/qemu/recovery_linux_test.go`, `cmd/grillod/core_test.go`.

## Commands and environment

- Host: Linux/amd64, KVM and vhost-vsock present; `qemu-system-x86_64`, `pasta`, `ip`, `nft` on `PATH`; guest initramfs rebuilt before each KVM target.
- KVM tests skip (not pass) when `/dev/kvm`, `/dev/vhost-vsock`, `pasta` or the guest artifacts are missing. Hosted CI has not run.

## Known limitations and follow-ups

- Overlay upper layers live in guest tmpfs; a very large writable set is bounded by guest RAM, and there is no per-container disk quota yet.
- Container capability overrides and custom seccomp profiles are rejected, not emulated; `runAsNonRoot` without a numeric UID is rejected rather than guessed.
- Recovery restarts sandboxes; it does not adopt running VMs, and it does not promise zero downtime.
- The mutation lock is global per daemon: correct, but it serializes independent applications until per-application serialization is added.
- `subPath`, `ReadWriteMany`, and multi-VM PVC sharing remain unsupported and are rejected.
