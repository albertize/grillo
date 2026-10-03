# T01 — Initial rootless prerequisite evidence

Date: 2026-10-03. Status: **superseded as the T01 headline result**; see the
[boot spike report](t01-boot-spike.md), which adds real boot, exec, stop, and
30-cycle evidence. This report remains the record of the prerequisite probes.
Starting commit: `d2bf8a4` (T00); probes are subsequent working-tree changes.

## Environment

- Linux/amd64, Fedora kernel `7.2.8-200.fc44.x86_64`.
- Host real/effective UID 1000; no sudo or host configuration changes.
- Go `go1.26.8-X:nodwarf5`; iproute2 6.17.0; util-linux unshare 2.41.5.
- `/dev/kvm` and `/dev/net/tun` present; both have world read/write device modes
  on this host. This is an observation, not a recommended permission policy.
- `firecracker`, `qemu-system-x86_64`, and `cloud-hypervisor` not found in PATH.
- `pasta` found, but `pasta --version` produced no version output; its version
  and usability remain unverified. `slirp4netns` not found.
- Installed host kernels exist under `/boot`; none has been validated as a
  suitable, pinned guest kernel. No experiment guest rootfs/init artifact exists.

## Commands and actual results

Run from the repository root; see [probe documentation](../../experiments/boot/README.md).

| Command | Result | What it establishes |
|---|---|---|
| `go run ./experiments/boot/preflight` | PASS, UID/EUID 1000, API 12 | Unprivileged read/write open and KVM API ioctl only |
| `sh experiments/boot/namespaces.sh` | PASS, UID map `0 1000 1` | Private user/net namespace and TAP create/up/delete |
| `make check` | PASS | Formatting, vet, ordinary/race tests, builds, module audit |
| `make vulncheck` | PASS, no vulnerabilities found | Pinned audit tool result for current code |
| `ip link show` after probes | No experiment interface | No TAP/dummy left in host namespace |
| Process listing after probes | No matching experiment processes | No observed leftover probe/VMM processes |

An earlier ad hoc `unshare --user --map-root-user --net` probe also created a
private dummy interface successfully. Its namespace exited and was destroyed.
The repository script improves this by probing TAP specifically, with a timeout.
No helper was left running; no VMM was launched. The API probe closes its device
on both successful and unsuccessful ioctls. Tests verify missing-device and
regular-file ioctl failure paths without KVM.

## Blockers and unperformed gates

No candidate VMM is available in PATH, and no verified guest artifacts exist.
No tools or guest artifacts were downloaded or installed. Provisioning must be
explicitly authorized and must pin/check upstream artifacts before execution.

Consequently **no VM creation/boot, vsock handshake, guest command, log stream,
VM stop, helper forwarding, or 30-cycle measurements were performed**. No
latency, memory, isolation, or backend feasibility claims follow from this
report. T01 is not complete; T02 remains dependent on completing it.

Next: the boot spike is implemented in [the T01 boot report](t01-boot-spike.md).
Helper forwarding (e.g. `pasta`/`slirp4netns`) and real networking remain
unverified and are T01's outstanding item into T02/T11. Firecracker is still only
a candidate; T02 must validate networking and live sharing before T03 selects a
backend.
