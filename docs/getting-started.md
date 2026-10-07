# Getting started

[README](../README.md) | [Compatibility](compatibility.md) | [Testing](testing.md)

Grillo currently runs from a source checkout on Linux/amd64. There is no packaged
installer or supported release. Commands below run from the repository root as
your normal user; they do not grant missing host privileges.

## Prepare the host

Build tools:

- Go 1.26.8, Make, Node 24.18.0 / npm 11.16.0.
- A C compiler for race tests; kernel-build tools include gcc, flex, bison, bc,
  libelf/OpenSSL headers and Perl.
- curl, tar/compression tools, cpio, coreutils and util-linux.

Runtime prerequisites:

- Read/write access to `/dev/kvm` and `/dev/vhost-vsock`.
- Unprivileged user/network namespaces and `/dev/net/tun`.
- `qemu-system-x86_64`, `pasta`, `ip` and `nft` on PATH.
- virtiofsd, normally `/usr/libexec/virtiofsd`.
- Helm **v4.2.2** on PATH when using charts.

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
./bin/grillo doctor
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

Outputs stay under ignored `experiments/artifacts/`. The default daemon expects
`experiments/artifacts/qemu/bzImage` and the initramfs/manifest in
`experiments/artifacts/t07/`. Keep the working directory at the repository root
when using those defaults. See [guest inputs and manifest](../guest/README.md).
Do not redistribute guest artifacts without reviewing their component licenses.

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
