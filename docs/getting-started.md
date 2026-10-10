# Getting started

[README](../README.md) | [Compatibility](compatibility.md) | [Testing](testing.md)

Grillo currently runs from a source checkout on Linux/amd64. There is no packaged
installer or supported release. Commands below run from the repository root as
your normal user; they do not grant missing host privileges.

## Prepare the host

Build tools:

- Go 1.27.x, exact selected patch 1.27.2 (`.go-version`), Make, Python 3 for
  toolchain policy, Node 24.18.0 / npm 11.16.0. No automatic family upgrade.
- A C compiler for race tests; kernel-build tools include gcc, flex, bison, bc,
  libelf/OpenSSL headers and Perl.
- curl, tar/compression tools, cpio, coreutils and util-linux.

Runtime prerequisites:

- Read/write access to `/dev/kvm` and `/dev/vhost-vsock`.
- Unprivileged user/network namespaces and `/dev/net/tun`.
- `qemu-system-x86_64`, `pasta`, `ip` and `nft` on PATH.
- virtiofsd, normally `/usr/libexec/virtiofsd`.
- Official Helm **v4.2.2**, selected explicitly with `GRILLO_HELM_BINARY`
  for development (installed layouts use `libexec/grillo/helm`).

The recorded Linux host used QEMU 10.2.2 and virtiofsd 1.14.0; this is evidence,
not a promise that arbitrary other versions or distributions work. The tested
Fedora SELinux Enforcing policy kills pasta helpers. Treat that as a blocker;
review host policy separately rather than automatically disabling protection.

The [dependency bootstrap](../scripts/README.md) previews optional Fedora package
provisioning and pinned Go/kernel-source downloads. It is not a complete runtime
installer; its Firecracker download is for historical experiments, not the
selected QEMU backend. Administrator provisioning, if needed, is a separate
explicit operation. Runtime commands never escalate themselves.

## Build binaries and guest artifacts

```sh
make ui-deps
make build
./bin/grillo version --json
```

`ui-deps` downloads integrity-locked frontend dependencies with install scripts
disabled. The compiled CLI embeds JS/CSS/fonts and needs no Node service.
`doctor` reports readiness without modifying the host; a missing prerequisite
must be resolved before continuing.

The current guest build uses the locally prepared development fixture:

```sh
# Explicitly download pinned kernel-source/build inputs if absent.
bash scripts/bootstrap.sh --download

# Build QEMU kernel/probe images, then runc/busybox fixture and real agent image.
make qemu-guest
make oci-guest
make t07-guest
```

The first kernel build can take several minutes. `oci-guest` explicitly fetches
pinned runc and exports a pinned busybox image using rootless Podman when the
fixture is absent. **Podman is development-fixture tooling here**, not the
native workload/build backend; it is not selected as a runtime fallback. This
bootstrap is not yet a self-contained release installation path.

Outputs stay under ignored `experiments/artifacts/`. Development paths are now
explicit overrides, not daemon CWD defaults. From the repository root, configure
absolute paths before starting the daemon:

```sh
export GRILLO_DAEMON_BINARY="$PWD/bin/grillod"
export GRILLO_NETNS_BINARY="$PWD/bin/grillo-netns"
export GRILLO_GUEST_KERNEL="$PWD/experiments/artifacts/qemu/bzImage"
export GRILLO_GUEST_INITRAMFS="$PWD/experiments/artifacts/t07/initramfs-agent.cpio.gz"
export GRILLO_GUEST_MANIFEST="$PWD/experiments/artifacts/t07/manifest.json"
export GRILLO_HELM_BINARY="$(command -v helm)" # provision exact v4.2.2 first
./bin/grillo doctor --verbose
./bin/grillo doctor --json
```

These overrides are inherited by the on-demand daemon. An already-running daemon
keeps its original configuration. Explicit daemon flags take precedence over its
environment. Relative explicit paths are made absolute at invocation. Normal
installed discovery uses `bin/grillo`, `libexec/grillo/{grillod,grillo-netns,helm}`
and `lib/grillo/guest/1.1/{bzImage,initramfs.cpio.gz,manifest.json}` relative to the
resolved executable prefix; an absolute `GRILLO_ASSET_PREFIX` can override that
prefix. No managed helper is selected from CWD or implicitly from PATH.

See [guest inputs and manifest](../guest/README.md) and
[D0's remaining distribution gates](experiments/d0-runtime-layout.md).
Installed assets require [manifest schema 1](../guest/manifest-schema.md), with
exact ABI/platform metadata and manifest-relative paths. The explicit legacy
fixture override above is development-only; incompatible declared metadata still
fails. `doctor` now queries KVM API, transient namespace creation, required QEMU
devices, filesystem-helper options and exact Helm identity without host
configuration changes. It does not prove a guest handshake or the complete
TAP/firewall/MAC datapath, and no general supported dependency range exists.
Its success is not an E2E/support guarantee. Missing installed assets fail
explicitly; known SELinux Enforcing networking restrictions fail explicitly.
Do not redistribute guest artifacts without reviewing their component licenses.

## Experimental runtime-only staging

For a base without T07 roots or reusable boot credentials, use `make runtime-guest`
after explicit pinned-input preparation. `make stage-runtime` copies one coherent
host/guest/helper/example prefix with an explicitly selected Helm binary and SHA.
`make test-installed-runtime` checks actual moved/read-only Compose, Helm and UI
lifetime on KVM with checkout/build tools removed from runtime PATH.
See the [self-contained runtime guide](runtime-layout.md) and
[latest D0 evidence and blockers](experiments/d0-installed-runtime.md).
This is local staging, not a native package installer or release publication.

## Run the Compose example

```sh
./bin/grillo plan examples/f2-compose/compose.yaml
./bin/grillo up examples/f2-compose/compose.yaml
./bin/grillo status f2
./bin/grillo inspect f2
./bin/grillo exec f2 worker -- cat /data/result.html
```

The worker fetches `web` through guest DNS and writes `f2-ok` to a managed volume.
`up` starts the per-user daemon if needed; exiting the command does not stop the
application. The first run may need registry access for the workload image.

```sh
./bin/grillo ui --port 0
./bin/grillo down f2
```

Open the printed URL before its one-use bootstrap expires. `down` stops workloads
but preserves data. Use `./bin/grillo down --volumes f2` only when you intend to
remove its owned managed volumes. Host bind paths and external data are not
cleanup targets.

## Inspect and debug

`status` and `inspect` take an **application ID**, not `service/name` notation.
Exec takes an application and a container name; if replicas make that ambiguous,
use the `sandbox-ID/container` target shown by inspection or the console.

```sh
./bin/grillo ps
./bin/grillo exec f2 worker -- /bin/sh -c 'printf "guest command\n"'
./bin/grillo exec -i f2 worker -- /bin/cat
./bin/grillo shell f2 worker
./bin/grillo metrics f2 --output=json
./bin/grillo logs <resource-ID> --container <container-name> -f
./bin/grillo events -f
```

Commands after exec's `--` run in the guest container, not a host shell. The log
command reads retained container stdout/stderr from the daemon spool. Use the
sandbox resource ID shown by inspection for exact filtering. Records are bounded
text chunks; guest retention gaps are reported explicitly. Output intentionally
printed by an application is not secret-redacted. Ordinary exec captures output
on completion; `exec -i` forwards stdin, `exec -t` also allocates a terminal and
forwards resize, and `shell` invokes guest `/bin/sh` interactively when local
stdin is a terminal. Local terminal modes are restored on exit/interruption;
closing the session cancels exec, not the workload. Minimal/distroless images may
lack a shell. Metrics expose real cumulative counters, not CPU percentages.

Runtime/native-build boots verify kernel/initramfs against `--artifact-manifest`
(default `experiments/artifacts/t07/manifest.json`) into private snapshots and
supply a fresh boot key. Corrupt or missing inputs block launch. `--key-file` is
legacy and ignored by the daemon. Local inventory checks do not authenticate a
publisher or make this a release installation path.

For charts, follow the [Helm examples](../examples/helm/README.md). An applicable
offline plan means implementation compatibility, not proof of host feasibility.

## Troubleshooting

- **Missing device/helper or denied namespace:** run `doctor`; do not substitute
  privileged execution or host containers.
- **Occupied port:** stop the owning application or choose another declared
  unprivileged port; Grillo does not steal a listener.
- **Guest CID already in use:** CIDs are host-wide. Concurrent isolated daemons
  need deliberately separate `grillod -vsock-cid-base` and
  `-build-vsock-cid-base` ranges. These flags are not a global allocator.
- **Degraded or unsupported manifest:** review the structured diagnostic and
  [compatibility matrix](compatibility.md). Consent is code-specific and cannot
  bypass unsupported security features.
- **Expired console URL:** restart only `grillo ui` for a fresh bootstrap;
  workloads remain owned by the daemon.
