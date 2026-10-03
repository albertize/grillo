# T01 — Rootless microVM boot spike

Date: 2026-10-03. Commit: `386ef9b` (working-tree changes documented below).
Status: **complete.** Boot, exec, streaming, stop, rootless namespace/TAP, and
rootless helper connectivity all verified on real hardware. T02 remains
unstarted. See [the preflight report](t01-preflight.md) for the prerequisite
probes.

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
| `sh experiments/boot/netns-helper.sh` | PASS: pasta provided address, default route, DNS, and HTTP egress rootless |
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

## Rootless networking helper

`netns-helper.sh` runs a command in a new user+network namespace via
`pasta --config-net` and verifies a non-loopback IPv4 address, a default route,
loopback, and an HTTP fetch by name (exercising DNS and TCP egress). Result on
this host: PASS. The namespace received `192.168.1.4/24`, a default route via
`192.168.1.1`, and `http://example.com/` returned HTTP 200.

Observed distribution restrictions: host UID 1000; `max_user_namespaces=62055`;
the Debian-style `unprivileged_userns_clone` knob is absent; SELinux is
`Enforcing` (pasta still worked); `/dev/net/tun` is present. Cleanup: no `pasta`
process remained and the host interface count was unchanged.

Failure paths were exercised: an unroutable target yields INCONCLUSIVE (exit 2),
an unsupported helper yields BLOCKED (exit 1), and a missing helper would be
BLOCKED. This validates the helper in isolation; connecting the VM's `virtio-net`
to a helper and testing in-guest DNS/egress/host publishing is T02/T11.

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

- **Application networking not yet wired to the VM.** The helper is proven
  standalone; guest `virtio-net`, DNS, egress, and host publishing are T02/T11.
- `slirp4netns` was absent and its probe path is not implemented; only `pasta`
  is exercised. Adding it is future work, not a claim of support.
- No OCI runtime, containers, or storage sharing.
- The vsock protocol, `guestinit`, and `guestcmd` are experiment-only and are
  replaced by the versioned protocol in T06/T07.
- Timings are single-host, warm-cache, 1 vCPU / 256 MiB; not a general claim.
- The kernel is a locally built trusted image; no untrusted guest input was tested.

Next: T02 — run an OCI image through guest runc, connect two VMs, and validate
disks and live bind mounts, keeping the backend provisional until T03.
