# Dependency bootstrap

`bootstrap.sh` provisions **Go/kernel-source inputs and historical Firecracker experiment dependencies**.
It is not a Grillo runtime installer. Linux/x86_64 downloads and Fedora build
packages are supported initially; there is no automatic sudo, VM boot, persistent
service, network change, device-permission change, or shell-profile edit.

Run from the repository root:

```sh
# Safe default: show the plan only.
bash scripts/bootstrap.sh

# Optional host package installation: explicitly grant administrator privileges.
# Review the script first. dnf still asks for confirmation.
sudo bash scripts/bootstrap.sh --system-packages

# Run as your normal user. Downloads, verifies, and extracts pinned artifacts.
bash scripts/bootstrap.sh --download

# Activate the installed Go and Firecracker in this Bash session only.
source experiments/artifacts/dependencies-v1/env.sh

go version
firecracker --version  # historical experiment binary, not the runtime backend
make ui-deps          # requires separately installed Node/npm
make check
```

The administrator command is optional if the packages already exist. Unlike
runtime operations, host package provisioning can require root; the script never
escalates itself. Do not run `--download` as root. There is no combined mode that
silently changes the host and installs downloaded executables in one invocation.

## Scope and provenance

| Artifact | Pin | Source / purpose |
|---|---|---|
| Go | 1.26.8, linux-amd64 | go.dev; CLI/agent/kernel-init development |
| Firecracker | 1.17.0, x86_64 | firecracker-microvm GitHub releases; candidate VMM, not selected backend |
| Linux source | 6.1.188 | cdn.kernel.org; guest kernel build input, not a built kernel |
| Guest kernel config | Firecracker v1.17.0, x86_64 6.1 config | upstream repository; starting point requiring review/adaptation |
| Fedora build packages | Distribution-managed | gcc, make, flex, bison, bc, libelf/OpenSSL headers, Perl, cpio, curl, tar/compression, coreutils, util-linux, iproute |

SHA-256 pins are embedded in `bootstrap.sh`. Their provenance was checked against:

- [Go release metadata](https://go.dev/dl/?mode=json&include=all).
- [Firecracker v1.17.0 release metadata](https://api.github.com/repos/firecracker-microvm/firecracker/releases/tags/v1.17.0).
- [Kernel checksum listing](https://cdn.kernel.org/pub/linux/kernel/v6.x/sha256sums.asc).
- The config downloaded from the versioned upstream HTTPS URL in the script.

This is HTTPS/upstream-metadata-based checksum verification, **not independently
verified signatures or a complete supply-chain audit**. Pin updates require
review. Fedora packages use configured repositories and their package-signature
policy; their versions are not frozen by this script. No repository is added and
no whole-system upgrade is requested. Record actual installed versions in each
experiment report.

Upstream licenses/notices remain in the extracted trees. Go is BSD-3-Clause;
Firecracker is Apache-2.0 with applicable bundled notices; Linux includes GPL-2.0
and per-file licensing terms. Do not redistribute built guests without reviewing
component obligations.

## Safety and behavior

- Default and `--dry-run` print the plan without creating files or using the network.
- Transfers use HTTPS only, connection/total timeouts, bounded retries, and a
  256 MiB per-download size cap (requires curl with `--max-filesize` support).
- Every artifact must match its checksum before any extraction begins.
- Downloads are never executed by the installer.
- A private staging directory is cleaned on failure/interruption; the completed
  tree is published only after successful extraction. Keep sufficient disk space
  for both archives and expanded sources; later kernel builds need additional space.
- Existing installations are refused, not overwritten or automatically removed.
  Reuse a trusted installation by sourcing `env.sh`. To reinstall, review and move
  the old directory aside yourself. The script does not verify an existing tree.
- Symlink install directories are rejected. The same-user account is a trust
  boundary; this is not protection against a compromised host account.
- All artifacts live below ignored `experiments/artifacts/`; nothing is installed
  in `/usr/local`, and no generated artifact belongs in git. A generated module
  boundary keeps Go's `./...` and `mod tidy` traversal out of downloaded sources;
  formatting checks cover project source directories only.

## Outside this bootstrap's scope

The working runtime uses QEMU/virtiofsd, pasta, a Go PID 1 agent and guest runc;
this script does not install the complete runtime toolchain or a bootable guest.
Prepare those prerequisites and build the kernel/initramfs using
[getting started](../docs/getting-started.md) and [guest inputs](../guest/README.md).
The current OCI development-fixture export uses explicitly provisioned Podman;
the native builder itself does not require it. Helm and Node/npm are also
separate explicit prerequisites. No prebuilt distribution rootfs or alternative
product backend is installed here. The pinned Go audit tool is invoked only by
explicit `make vulncheck`.

## Verification

```sh
make test-scripts
```

Offline tests cover argument handling, checksum success/mismatch, transfer failure,
fixture extraction, activation with shell-special paths, existing/symlink target
refusal, and staging cleanup. The installer fixture tests run as a non-root user;
root environments test root refusal and explicitly skip those fixtures. Tests
never download artifacts, install packages, execute the fixture programs, or
prove hardware integration. `make check` includes these tests.

## Experimental transfer payload

`make payload` rebuilds and stages the local runtime and archives one `grillo/`
prefix, printing the new archive/checksum paths. Set trusted `HELM_BINARY` and
`HELM_SHA256` explicitly (shell exports allow repeated `make payload` calls).
`PAYLOAD_ROOT` defaults to ignored `experiments/artifacts/payloads`; every invocation
uses a unique private directory, without overwriting previous output. No transfer,
installation, signature, source-compliance clearance or dependency download occurs.
See [runtime layout](../docs/runtime-layout.md#developer-preparation-source-checkout-only).

`payload.sh` is invoked from the repository root. Offline `payload_test.sh` tests
real archive/checksum/extraction behavior around a fake stage tool, two successive
outputs, space-containing paths, pin argument rejection, symlink-root refusal and
stage/archive failure cleanup preserving prior payloads/unrelated files. These are
included in `make test-scripts`/`make check`; fake staging is not real artifact or
hardware evidence. SIGKILL/power loss can leave an incomplete private output
folder; only transfer the pair from a successfully completed invocation.

## Compatibility registry generation

Run from the repository root:

```sh
go run ./scripts/compatibility -output docs
go test ./scripts/compatibility
```

This writes the intended version-controlled JSON/Markdown compiler registry
inventories. It performs no rendering, downloads or runtime operations. Ordinary
CI rejects divergence from validator maps, missing fixture links and
nondeterministic output. These inventories are not per-field runtime evidence;
see [compatibility](../docs/compatibility.md) for actual gates and limitations.
