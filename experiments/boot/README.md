# T01 boot spike — prerequisite probes

These probes are **not a boot test**, a production doctor, or evidence for F0.
They currently target Linux/amd64 and must run as the normal host user.

From the repository root:

```sh
go run ./experiments/boot/preflight
sh experiments/boot/namespaces.sh
```

The Go probe opens `/dev/kvm` read/write, checks KVM API version 12, and closes
the descriptor. It creates no VM. Unit tests exercise missing-device and invalid
ioctl failures without accessing hardware.

The shell probe runs under a ten-second timeout, creates a user/network
namespace, maps only the caller to namespace UID 0, then creates, brings up,
and deletes a TAP. Namespace UID 0 is not host root. All network changes are
inside that namespace; exit destroys it even if a command fails. It starts no
persistent helper and performs no host network configuration. Required host
tools are `timeout`, `unshare`, and `ip` with tuntap support.

A nonzero exit is a failed or unavailable prerequisite, not permission to use
sudo. Success proves only these narrow operations. It does not prove VM boot,
network forwarding, management-channel isolation, or real live bind mounts.

## Remaining work

1. Provision the pinned candidate VMM and guest kernel source with the opt-in
   [dependency bootstrap](../../scripts/README.md). It records upstream sources,
   checksum pins, and licenses but does not build or validate a bootable guest.
2. Build an experiment-only guest init with a bounded vsock handshake and fixed
   guest command; do not use the current `grillo-agent` placeholder as PID 1.
3. Exercise guest command output, stop, error handling, and cleanup under the
   caller's host UID, then record 30 boot/stop cycles and environment metadata.
4. Test the selected rootless networking helper; executable presence is not
   proof that it works. Keep backend selection provisional until T03.

Results and blockers: [T01 report](../../docs/experiments/t01-preflight.md).
`make test-kvm` remains blocked until real guest integration tests exist.
