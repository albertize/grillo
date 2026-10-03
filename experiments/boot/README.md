# T01 boot spike

This is the **feasibility experiment**, not the Grillo runtime. It proves that a
rootless Firecracker microVM can boot, accept a vsock connection, execute a guest
binary, stream its output, and stop cleanly. It creates no Pod, runs no OCI
runtime, provides no container isolation, and speaks a throwaway protocol that
T06 replaces.

Linux/amd64 only. Run as the normal host user; never with sudo.

## Layout

| Path | Purpose |
|---|---|
| `preflight/` | Go probe that opens `/dev/kvm` and checks the KVM API version |
| `namespaces.sh` | Private user/network-namespace + TAP create/up/delete probe |
| `netns-helper.sh` | Rootless networking-helper probe (pasta): address, route, DNS, egress |
| `guestinit/` | Experiment guest PID 1 (AF_VSOCK server, exec, stream, stop) |
| `guestcmd/` | Deterministic payload used to prove guest execution |
| `spike/` | Reusable boot/session package used by the `run` and `oci` CLIs |
| `run/` | T01 host harness: boot, exec, stop |
| `fetch-oci.sh` | Fetch pinned static runc and build the busybox OCI rootfs + bundles |
| `build-oci-guest.sh` | Build the T02 OCI guest initramfs |
| `oci/` | T02 host harness: runc run/exec/signal and two-container localhost |
| `storage-probe.sh` | Records the shared-filesystem device limitation (scenario G) |
| `qemu/` | T03 QEMU `microvm` experiments: kernel/initramfs builds, storage/network/`f0` probes, guest fixtures, QEMU-backend OCI |
| `build-guest.sh` | Builds the guest kernel and initramfs from pinned downloads |

## Build and run

Prerequisites: the [dependency bootstrap](../../scripts/README.md), an
unprivileged account with `/dev/kvm` access, and `gcc make flex bison bc` plus
`libelf`/OpenSSL headers to build the kernel. The kernel build takes several
minutes the first time; the result is cached under `experiments/artifacts/t01/`.

```sh
go run ./experiments/boot/preflight        # KVM API access only
sh experiments/boot/namespaces.sh          # userns/TAP only
make net-helper                            # rootless helper + egress (pasta)
make guest                                 # build kernel + initramfs
go run ./experiments/boot/run -cycles 1    # one boot/exec/stop cycle
make oci-guest                             # fetch runc/busybox, build OCI initramfs
go run ./experiments/boot/oci              # OCI run/exec/signal + localhost
go run ./experiments/boot/oci -keep        # ... printing the console log
make storage-probe                         # live-bind limitation (exit 3 = BLOCKED)
make qemu-guest                            # QEMU bzImage (VIRTIO_FS) + probes
make qemu-share                            # QEMU + virtiofsd live-bind probe
make test-kvm                              # Firecracker KVM tests (spike + OCI)
make test-qemu                             # QEMU KVM tests (share, net, OCI)
make f0-guest                              # QEMU bzImage + F0 probe initramfs
make test-f0                               # full rootless F0 topology and benchmarks
make bench-t01                             # 30 measured cycles
```

All artifacts stay under the ignored `experiments/artifacts/` directory.

## Experiment protocol (throwaway, replaced by T06)

Newline-delimited, tab-separated, base64 payloads so arbitrary bytes survive line
framing:

```text
guest -> host   GRILLO-T01 <proto> <agent-version>
host  -> guest  PING                          -> PONG
host  -> guest  EXEC\t<path>\t<arg>...        -> OUT\t<b64>, ERR\t<b64>, EXIT\t<code>
host  -> guest  STOP                          -> BYE, then the guest resets
```

`EXEC` runs a path directly: there is no shell and no argument interpolation.
The host rejects tab/newline arguments, limits frames to 64 KiB and combined
output to 4 MiB, and applies a total exec deadline (default 30s) plus the Boot
context's cancellation. A failed exchange closes the connection; it cannot be
reused for another command. This is not the authenticated T06 protocol.

## Shutdown

The guest kernel config from Firecracker has `CONFIG_VT` but no `SERIO`/`I8042`
driver, so a host-injected `SendCtrlAltDel` has no guest path. With `reboot=k`,
the guest instead calls `reboot(LINUX_REBOOT_CMD_RESTART)`, whose x86 machine
restart path uses the keyboard-controller reset that Firecracker turns into
process exit. The host keeps `SendCtrlAltDel` as a fallback and escalates to
`SIGKILL` on the process group if the VM does not stop within the timeout.

## Networking helper

`netns-helper.sh` runs a command in a new user+network namespace through
`pasta --config-net` and verifies a non-loopback address, a default route,
loopback, and external HTTP (and therefore DNS). It refuses root, bounds the run
with a timeout, cleans up its temporary script, and checks that no `pasta`
process is left behind. It prints the distribution restrictions it observes
(`max_user_namespaces`, SELinux mode, `/dev/net/tun`).

Exit codes distinguish outcomes: `0` PASS, `1` FAIL/BLOCKED (helper missing,
unsupported, or namespace not configured), `2` INCONCLUSIVE (namespace
configured but no external egress, e.g. an offline host). Override the target
with `GRILLO_EGRESS_URL`, `GRILLO_EGRESS_TIMEOUT`, and `GRILLO_HELPER_TIMEOUT`.

This proves the helper alone. Wiring guest `virtio-net` through a helper and
verifying DNS/egress/host publishing from inside the VM is T02/T11.

## OCI scenario (T02)

The OCI guest adds a static `runc` and a busybox rootfs obtained from a
content-addressed image (`docker.io/library/busybox:1.37@sha256:bdf5...`,
exported with podman; Grillo implements no registry client). The export uses
an operation-owned container ID and a locked, validated temporary rootfs before
atomic publication. Existing unmarked/partial rootfs caches are rejected: review
and move them aside manually before rebuilding. No preexisting container is
removed by name. `guestinit` also
mounts cgroup v2, devpts, and shm, and brings up loopback, so runc can start
containers and two containers can share localhost.

`go run ./experiments/boot/oci` runs both scenarios and prints a PASS/FAIL line
per check. `runc run` uses `--no-pivot` because the bundle rootfs lives on the
initramfs (ramfs). Detached containers are started with `/dev/null` stdio via a
separate command so they do not hold the management channel open. Live binds are
**not** supported by Firecracker; see the [T02 report](../../docs/experiments/t02-oci-and-storage.md)
and `make storage-probe`.

## QEMU backend (T03)

`experiments/boot/qemu/` evaluates the backend that replaces Firecracker for
product use. It builds a guest bzImage with `CONFIG_VIRTIO_FS` (the Firecracker
config omits it), a storage probe that mounts a virtiofs tag, and a network probe
that uses QEMU user-mode networking with `ip=dhcp`. `make qemu-share` tests
post-mount host updates, guest writes, rename, guest-local inotify, and read-only
enforcement. Host-originated notifications are measured separately: the review
run observed none within 2s and reports DEGRADED (polling required). A storage
PASS does not assert remote event support or completion of F0. `-scenario net`
tests rootless TCP egress and DNS; `make test-qemu` also runs the OCI scenarios
on QEMU via `vhost-vsock`. Results are in the
[T03 report](../../docs/experiments/t03-backend-comparison.md) and ADR 0005.
QEMU/virtiofsd are system packages, not bootstrapped here.

## Full F0 topology (T02/T03)

`make test-f0` runs the complete feasibility experiment. `pasta` creates a user
and network namespace (asserting the mapping is exactly `0 <host-uid> 1`), the
harness builds two bridges and three TAP devices inside it, applies a default-deny
nftables policy, and boots three QEMU `microvm` guests. It then verifies OCI
execution and two-container localhost, two-VM HTTP/DNS/egress, guest→host
publishing on loopback, cross-application and guest→management denial, live
virtiofs binds, a managed ext4 volume with restart persistence, and warm-boot and
filesystem benchmarks. Every VMM is asserted to run with `CapEff=0`,
`NoNewPrivs=1`, and `Seccomp=2`.

`experiments/boot/qemu/f0guest/` is the trusted guest payload, `fsbench/` is the
fixed filesystem benchmark shared with the host baseline, and
`run/dns_fixture.go` is a bounded fixed-answer resolver (not the T12
implementation). The offline tests cover the DNS fixture and the process/relay
failure paths without KVM.

**SELinux.** On the tested Fedora policy, SELinux Enforcing silently kills
processes started by `pasta` inside the user namespace (exit 2, no output). The
harness refuses to run under Enforcing with an actionable message; supply a
policy or set SELinux to Permissive for the experiment. This is a host
prerequisite for `doctor`, not a runtime fallback.

## Safety

- Runs as the calling user; no sudo, no jailer, no host network configuration.
- The kernel and initramfs are built locally from the repository; the kernel
  source/config come from the checksum-verified bootstrap.
- The guest is a trusted, minimal experiment image, not untrusted workload input.

## Limits

No OCI runtime in the *host* runtime sense, no DNS, and no packaged guest
networking: the networking helper is verified standalone (`netns-helper.sh`) and
QEMU user-mode networking/tested rootless, but the production data plane is T11.
Firecracker cannot do live host-directory binds; QEMU + virtiofsd can (T03,
[ADR 0005](../../docs/adr/0005-platform-qemu-virtiofsd.md)). Containers run as
root without user-namespace isolation. The protocol and payload binaries are
experiment-only and are replaced by T06/T07. Real evidence is in the
[T01](../../docs/experiments/t01-boot-spike.md),
[T02](../../docs/experiments/t02-oci-and-storage.md), and
[T03](../../docs/experiments/t03-backend-comparison.md) reports.
