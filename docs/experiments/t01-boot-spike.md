# T01 — Rootless microVM boot spike

Date: 2026-10-03. Commit: `386ef9b` (working-tree changes documented below).
Status: **boot spike complete; helper networking still unverified.** See
[the preflight report](t01-preflight.md) for the prerequisite probes.

This is real-hardware evidence from a single workstation, not a benchmark
comparison and not a backend selection. Firecracker remains a candidate; T02/T03
decide the backend after networking and live filesystem sharing are validated.

## Environment

| Item | Value |
|---|---|
| Host | Linux/amd64, `7.2.8-200.fc44.x86_64` |
| CPU | Intel Core Ultra 7 155H (22 logical CPUs), hardware virtualization |
| Memory | 15 GiB |
| Host user | real/effective UID 1000 (rootless; no sudo used) |
| `/dev/kvm` | present; opened read/write, KVM API version 12 |
| Go | `go1.26.8 linux/amd64` |
| VMM | Firecracker `v1.17.0` (pinned download) |
| Guest kernel | Linux `6.1.188` built from the pinned Firecracker 6.1 x86_64 config |

## Artifacts

| Artifact | SHA-256 |
|---|---|
| `experiments/artifacts/t01/vmlinux` (44,210,808 bytes) | `5e533ec165dacf998ecb699f9d3e0a031218002fd29f168ef8666028ed469f3e` |
| `experiments/artifacts/t01/initramfs.cpio.gz` | `04bf1d8ea275e5074ed4ac969f399db3ace8f69da875c5d5ce464a14c6160990` |

The initramfs contains the experiment PID 1 (`guestinit`) and a payload binary
(`guestcmd`). It is rebuilt by `make guest`; its digest changes when those
binaries change.

## Commands and results

| Command | Result |
|---|---|
| `go run ./experiments/boot/preflight` | PASS: UID/EUID 1000, `/dev/kvm` open + KVM API 12, descriptor closed |
| `sh experiments/boot/namespaces.sh` | PASS: private user/net namespace (UID map `0 1000 1`), TAP create/up/delete |
| `make guest` | PASS: kernel + initramfs built |
| `make test-kvm` | PASS: `TestKVMExecBootStop`, `TestKVMExecMissingCommand` |
| `make check` | PASS: fmt, vet, unit/race tests, script tests, builds, module audit |
| `make vulncheck` | PASS: no vulnerabilities found |

`TestKVMExecBootStop` boots a real VM, completes the vsock handshake, runs
`/bin/grillo-testcmd alpha beta`, and asserts stdout (`grillo-testcmd stdout`,
`args=[alpha beta]`), stderr, and exit status 0. `TestKVMExecMissingCommand`
verifies that executing a nonexistent guest binary returns exit status `-1` with
a `start:` diagnostic instead of being silently hidden.

## 30-cycle measurements

Command: `go run ./experiments/boot/run -cycles 30` (warm page cache; no
cold-cache or competing-workload control).

| Phase | min | median | p95 | max |
|---|---|---|---|---|
| boot (process start → valid vsock handshake) | 254 ms | **260 ms** | 265 ms | 271 ms |
| exec (EXEC sent → EXIT received) | 1 ms | 1 ms | 2 ms | 2 ms |
| stop (STOP/BYE → process exit) | 117 ms | 127 ms | 135 ms | 143 ms |
| total (boot + exec + stop) | 377 ms | **391 ms** | 400 ms | 405 ms |

30/30 cycles succeeded. Timings cover VMM process start, guest boot, agent
readiness, one guest command, and shutdown. They do **not** include image pull,
unpack, OCI container start, application readiness, or teardown of storage and
networking, so they are not an F0 budget.

## Cleanup and failure visibility

After the runs: no `firecracker` processes remained (`pgrep -x firecracker`),
no `/tmp/grillo-t01-*` work directories remained, and no Grillo-related mounts
remained. The harness removes each per-cycle directory and escalates to `SIGKILL`
on the process group if a VM does not stop within the deadline. Failures (boot
timeout, malformed protocol, unexpected payload, missing command) abort the run
with a nonzero exit rather than being swallowed.

## Shutdown finding

The pinned Firecracker guest config has `CONFIG_VT` but no `SERIO`/`I8042` driver,
so a host-injected `SendCtrlAltDel` has no guest effect. With the `reboot=k`
kernel argument, a guest-initiated `reboot(LINUX_REBOOT_CMD_RESTART)` resets via
the x86 keyboard-controller path, which Firecracker turns into process exit. This
is recorded as a lifecycle input for T03/T08; the host still escalates to `SIGKILL`
on timeout.

## Unverified / limits

- **Networking helper forwarding not tested.** `pasta` is present but produced no
  `--version` output; `slirp4netns` was absent. Only a private-namespace TAP was
  exercised. No guest egress, DNS, or host publishing was validated. This is
  T02/T11.
- No OCI runtime, containers, storage sharing, or multi-container Pod behavior.
- The vsock protocol, `guestinit`, and `guestcmd` are experiment-only and are
  replaced by the versioned protocol in T06/T07.
- Timings are single-host, warm-cache, 1 vCPU / 256 MiB; not a general claim.
- The kernel is a locally built trusted image; no untrusted guest input was tested.

Next: complete the T01 helper-networking item or proceed to T02 with the backend
still provisional.
