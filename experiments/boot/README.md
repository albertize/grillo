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
| `guestinit/` | Experiment guest PID 1 (AF_VSOCK server, exec, stream, stop) |
| `guestcmd/` | Deterministic payload used to prove guest execution |
| `run/` | Host harness: Firecracker API over a Unix socket, vsock, exec, stop |
| `build-guest.sh` | Builds the guest kernel and initramfs from pinned downloads |

## Build and run

Prerequisites: the [dependency bootstrap](../../scripts/README.md), an
unprivileged account with `/dev/kvm` access, and `gcc make flex bison bc` plus
`libelf`/OpenSSL headers to build the kernel. The kernel build takes several
minutes the first time; the result is cached under `experiments/artifacts/t01/`.

```sh
go run ./experiments/boot/preflight        # KVM API access only
sh experiments/boot/namespaces.sh          # userns/TAP only
make guest                                 # build kernel + initramfs
go run ./experiments/boot/run -cycles 1    # one boot/exec/stop cycle
make test-kvm                              # kvm-tagged Go tests
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

## Shutdown

The guest kernel config from Firecracker has `CONFIG_VT` but no `SERIO`/`I8042`
driver, so a host-injected `SendCtrlAltDel` has no guest path. With `reboot=k`,
the guest instead calls `reboot(LINUX_REBOOT_CMD_RESTART)`, whose x86 machine
restart path uses the keyboard-controller reset that Firecracker turns into
process exit. The host keeps `SendCtrlAltDel` as a fallback and escalates to
`SIGKILL` on the process group if the VM does not stop within the timeout.

## Safety

- Runs as the calling user; no sudo, no jailer, no host network configuration.
- The kernel and initramfs are built locally from the repository; the kernel
  source/config come from the checksum-verified bootstrap.
- The guest is a trusted, minimal experiment image, not untrusted workload input.

## Limits

No OCI runtime, containers, DNS, storage sharing, or networking beyond the
prerequisite TAP probe. The networking **helper** (e.g. `pasta`/`slirp4netns`) is
detected but its forwarding is not functionally tested here; that is T02/T11.
The protocol and payload binaries are experiment-only. Real evidence is in
[the T01 report](../../docs/experiments/t01-boot-spike.md).
