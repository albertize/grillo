# Experimental runtime prefix and first run

This is a local D0 development payload, **not a supported or redistribution-cleared
release**. Linux/amd64 and working KVM are required. No package installers,
supported host matrix or publisher authentication are provided yet.

The Grillo LICENSE/NOTICE cover original Grillo contributions, not the bundled
Linux kernel, runc, BusyBox/glibc or Helm. Component licenses/notices,
corresponding-source obligations and release provenance must be reviewed before
publishing this payload. Do not redistribute it as an official release.

## Install layout

All paths below are relative to the extracted/staged prefix:

```text
bin/grillo
libexec/grillo/{grillod,grillo-netns,helm}
lib/grillo/guest/1.1/{bzImage,initramfs.cpio.gz,manifest.json,runtime-build.json}
share/grillo/examples/{hello-compose,hello-helm}/
share/doc/grillo/{LICENSE,NOTICE,VERSION.json,runtime-layout.md,host-support.md}
share/doc/grillo/third-party/{frontend-licenses.txt,*LICENSE.txt,*OFL.txt}
```

Agent/runc and the minimal guest network utility are inside the initramfs, never
host PATH executables. There are no application roots, demo fixtures or boot keys
inside the reusable image. Each boot uses a private verified copy with a new
credential overlay. Ordinary commands must run as your unprivileged account.

`VERSION.json` records the exact copied binary/file hashes and actual Go module/
toolchain metadata extracted from CLI, daemon, netns helper and Helm bytes. The
frontend/font notices are copied verbatim and inventoried; `notice_coverage` stays
explicitly partial pending complete Go/guest/Helm notice/source review. Missing,
empty, oversized, symlink or special-file notice inputs fail staging. No producer
signature or source-rebuild proof is inferred from embedded Go metadata.
See [host evidence/support boundaries](host-support.md).

The prefix can be moved and made read-only. No working directory, source checkout,
Go, Node, npm, Podman, external Helm or kernel build is required to run the staged
examples. Host QEMU, virtiofsd, pasta, ip/nft and kernel devices are still required;
a tarball/prefix does not provision them or change host access/security policy.

## Check and run

Add only the prefix's `bin` to your shell PATH, or invoke its absolute `bin/grillo`:

```sh
grillo version --json
grillo doctor --verbose
grillo doctor --json
```

`doctor` queries file integrity/ABI, helpers, required QEMU devices, KVM API and
transient unprivileged user/network namespace creation. It does not create a VM,
configure host interfaces/firewall, change sysctls/groups, install tools or weaken
MAC policy. The namespace probe child immediately exits without persistent state.
Checks execute only fixed read-only arguments on provisioned host helpers; those
helpers remain trusted host dependencies, not a tool-execution sandbox.

A missing device differs from denied access. Follow each `Why`, `Check`, `Fix`
and local `Docs` field. Existing unsafe runtime directories/log links are refused,
not automatically chmodded. SELinux Enforcing remains an explicit networking
blocker on the recorded Fedora policy. Other MAC/TAP/firewall/guest operations
still require actual E2E evidence; read-only checks are not a support guarantee.
The locally evidenced QEMU is 10.2.2; no general compatibility range is advertised.

Run the bundled Compose example, replacing `/path/to/prefix` explicitly:

```sh
grillo up /path/to/prefix/share/grillo/examples/hello-compose/compose.yaml
curl --fail http://127.0.0.1:18080/
grillo inspect hello-compose
grillo ps
grillo down hello-compose
```

Expected response: `grillo-compose-ok`. To avoid an occupied port, set
`GRILLO_DEMO_PORT` to an explicitly chosen unprivileged port before `up`.
The example uses a digest-pinned public BusyBox OCI workload image. A first pull
requires Internet access unless that image is already in your private OCI cache.
It is not bundled in the prefix; do not claim offline first run.

For Helm, the renderer is already in `libexec/grillo/helm`; no Kubernetes or
separate renderer preparation is needed:

```sh
grillo plan /path/to/prefix/share/grillo/examples/hello-helm --release hello-helm
grillo up /path/to/prefix/share/grillo/examples/hello-helm --release hello-helm
grillo inspect hello-helm
# Probe the actual loopback HTTP endpoint returned by inspect.
grillo down hello-helm
```

Expected HTTP response: `grillo-helm-ok`. The chart exercises one Pod/microVM,
TCP Service and basic Ingress. This is a subset, not general Kubernetes parity.
CLI exit does not own workload lifetime. `grillo ui --port 0` starts the optional
local console; keep its one-use bootstrap URL private.

`up` identifies a successful application and points to `inspect`. Inspection
reports actual Ingress URLs and declared loopback TCP publications; it does not
invent HTTP semantics for arbitrary container ports.

## Overrides, persistence and upgrades

Explicit developer overrides precede prefix discovery. Daemon flags precede
its environment. Managed helpers never fall back to CWD/PATH lookalikes:

- `GRILLO_ASSET_PREFIX`: absolute application prefix.
- `GRILLO_DAEMON_BINARY`, `GRILLO_NETNS_BINARY`, `GRILLO_HELM_BINARY`: explicit helper paths.
- `GRILLO_GUEST_KERNEL`, `GRILLO_GUEST_INITRAMFS`, `GRILLO_GUEST_MANIFEST`: guest overrides.
- `GRILLO_VIRTIOFSD_BINARY`: explicit distro filesystem helper.

Only explicit developer manifest overrides permit legacy unversioned inventories;
invalid declared ABI/platform/schema never falls back. A daemon already running
keeps its selected files/settings. Host-wide vsock CID ranges must not overlap
between independent daemons; single-user defaults are not a CID allocator.

User state lives in the normal private XDG runtime/state/data/cache directories,
not this prefix. Moving/removing prefix files does not silently delete volumes,
images, secrets or desired state. `down` preserves owned persistent data; removal
requires a separate deliberate operation. Do not delete bind paths.

Guest build-store directories are immutable content-identity entries. A new guest
build publishes a new directory; it never overwrites a historical image. Boot
snapshots are private per-operation copies. Local staging likewise refuses to
replace an existing prefix. Keep old prefixes while their daemon/workloads are
active; do not retarget a running daemon implicitly. Native package upgrades,
artifact-retention GC and uninstall orchestration are not implemented here.

## Cache limits

CAS and runtime rootfs caches have finite aggregate logical-byte/entry policies:
16 GiB CAS, 32 GiB rootfs, 200,000 entries per family. Explicit daemon flags
`--oci-cache-bytes`, `--rootfs-cache-bytes`, `--cache-entries` adjust them.
Cooperating writers serialize across processes; quota exhaustion fails without
silently evicting active roots or user data. Explicit image pruning can release
CAS space. Rootfs-aware GC is not implemented; do not manually delete active roots.

These limits are not physical disk or whole-XDG quotas: chart/build workspaces,
guest stores and volumes remain separate. Existing cached reads are not blocked
or retroactively audited. Source/license review still blocks redistribution;
SHA expectations and a clean local RPM verification are not publisher attestation.

## Developer preparation (source checkout only)

Provision pinned inputs explicitly using the existing development guide. These
steps are **build-time**, not target-host installation requirements:

```sh
make runtime-guest
# Explicitly select a trusted exact renderer and its verified/local-development pin.
make stage-runtime STAGE_PREFIX=/absolute/new-prefix \
  HELM_BINARY=/absolute/trusted/helm HELM_SHA256=<expected-64-hex-digest>
make test-installed-runtime \
  HELM_BINARY=/absolute/trusted/helm HELM_SHA256=<expected-64-hex-digest>
```

For a transferable local archive instead of retaining a stage, set the same
explicit trusted Helm variables once in your shell, then run:

```sh
export HELM_BINARY=/absolute/trusted/helm
export HELM_SHA256=<expected-64-hex-digest>
make payload
```

Each invocation rebuilds the runtime and prints a fresh uniquely named directory
under `experiments/artifacts/payloads/` containing `grillo-test.tar.gz` and
`grillo-test.tar.gz.sha256`. Override `PAYLOAD_ROOT` to choose another parent.
Previous payloads are never overwritten; failed current packaging is cleaned.
The archive has one top-level `grillo/` directory. Verify the checksum on the
target before extraction; checksums are not signatures or redistribution clearance.
No installer, dependency download or transfer is performed. Update into a new
prefix and deliberately stop the old daemon before using the rebuilt binaries.

No build target silently downloads host/guest tools. Native helper and guest
versions/commit are injected together by `make build`; development builds remain
`dev`. The staged VERSION inventory records file digests and explicitly pending
redistribution status; it is not a signed release attestation.
