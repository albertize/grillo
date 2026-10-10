# Grillo Distribution and First-Run Experience

**Status:** Proposed specification  
**Audience:** Maintainers, contributors, release engineers  
**Target:** Post-T24 onboarding and first public experimental release  
**Initial platform:** Linux/x86-64 (amd64), hardware-assisted virtualization (KVM)  
**License of this document:** Apache-2.0, consistent with the Grillo repository  
**Suggested repository path:** `docs/distribution-and-onboarding.md`

> **Product promise:** Install Grillo, validate the host, and run a Compose application or Helm chart without compiling Grillo, building a guest kernel, assembling an initramfs, or running a Kubernetes cluster.

## 1. Executive decision

Grillo SHALL publish **one versioned release payload** through three channels:

1. **Portable `.tar.gz` release**, the reference artifact and first deliverable.
2. **`.deb` package** for verified Debian-family hosts.
3. **`.rpm` package** for verified RPM-family hosts.

The DEB and RPM MUST be built from exactly the same staged application and guest artifacts as the tarball. They MUST NOT maintain separate runtime implementations. Native packages are the preferred end-user experience because the package manager can install supported host dependencies and manage updates. The tarball remains essential for reproducibility, CI, troubleshooting, and users who cannot use the native package.

**Do not ship a package merely because it builds.** A distribution is supported only after a clean-host end-to-end test passes under its standard security policy. In particular, the currently documented Fedora SELinux Enforcing/pasta failure is a blocker to claiming Fedora support, not a reason to disable SELinux automatically.

### 1.1 Key design choice: bundle Grillo, not a virtualization stack

- Distribute the Grillo CLI, per-user daemon, network helper, guest kernel, guest runtime image, integrity metadata, and required license/notice files.
- Use distribution-managed **QEMU**, **virtiofsd**, **pasta**, `ip`, `nft`, and other host utilities within a tested compatibility range; do not freeze private QEMU copies in the initial release.
- Make **Helm usable out of the box** for users installing the standard package. The preferred first-release approach is to package the project's exact tested Helm renderer as a controlled helper (subject to license review); alternatively, embed it using the Helm SDK after proving identical behavior. Relying on a separately installed, arbitrary `helm` from `PATH` is a development workaround, not the intended packaged UX.
- Ship only a minimal **runtime guest**. Do not ship busybox/application roots that exist solely for test fixtures.
- Continue to use **rootless runtime processes**. Package-manager installation may require administrator authorization, but ordinary `grillo` commands MUST NOT invoke `sudo` or silently alter host privileges.

## 2. Goals and measurable outcomes

| ID | Requirement | Verification |
| --- | --- | --- |
| DX-01 | On a supported clean host, a developer can install Grillo without Go, Node, npm, Podman, or a source checkout. | Clean-host install test. |
| DX-02 | No user must compile a Linux kernel, `runc`, or guest agent. | Offline installation and guest boot with shipped artifacts. |
| DX-03 | A single native-package install brings in declared, supported host runtime dependencies. | Fresh VM/host install using only the configured distro repositories. |
| DX-04 | `grillo doctor` reports actionable readiness failures, without changing the host. | Negative-path and read-only tests. |
| DX-05 | `grillo up` works from an arbitrary working directory (not only the Git checkout). | Run from `$HOME`, `/tmp`, and paths containing spaces. |
| DX-06 | The included Compose demo boots at least one real microVM and exposes an HTTP endpoint on loopback. | Hardware-backed E2E. |
| DX-07 | The included Helm demo renders without Kubernetes or a separately prepared Helm toolchain. | Offline rendered-chart + real KVM E2E, with images cached. |
| DX-08 | Closing the CLI or browser does not terminate managed applications. | Lifetime/recovery E2E. |
| DX-09 | Uninstalling the package never silently deletes user volumes, images, secrets, or desired state. | Install/uninstall/upgrade tests. |
| DX-10 | Every distributed binary and guest artifact has provenance, digest, license review, and compatibility metadata. | Release pipeline attestation and manifest verification. |
| DX-11 | Public documentation states tested host configurations and known limitations rather than promising full Kubernetes or absolute security. | Release-documentation gate. |

**Onboarding target (to be measured, not advertised until proven):** On a preconfigured, KVM-capable supported Linux workstation with Internet access, complete native installation, `doctor`, and first demo response in **10 minutes or less**, excluding unusually slow package downloads. Once installed, the user should need only three conceptual steps: install, check, run. Record cold-cache and warm-cache timings separately.

## 3. Non-goals

The initial distribution effort SHALL NOT introduce:

- A Kubernetes API server, cluster, scheduler, etcd, or privileged global Grillo daemon.
- Rootful workload fallback when KVM, namespaces, SELinux policy, or another dependency fails.
- Automatic host reconfiguration (KVM group changes, `chmod 666 /dev/kvm`, SELinux disablement, kernel command-line changes, firewall rules, user-service enablement, or CA trust modifications).
- macOS/Windows runtime distributions, arm64 packages, GPU support, or alternate VMM backends without independent hardware gates.
- An APT/DNF repository service, system-wide auto-update agent, or install-by-curl one-liner as a precondition for the first release.
- New Kubernetes kinds or unrelated product features. The target is packaging and operability of the implemented runtime.

## 4. Current repository baseline and release blockers

This section describes the inspected repository snapshot (`grillo-main.zip` provided for review), **not implementation already completed by this specification**.

| Existing behavior | Packaging consequence / required change |
| --- | --- |
| `Makefile` produces `grillo`, `grillod`, `grillo-netns`, and `grillo-agent`. | Classify host binaries separately from guest-only binaries. Build them once per release. |
| `grillod` currently defaults to `experiments/artifacts/qemu/bzImage`, `experiments/artifacts/t07/initramfs-agent.cpio.gz`, `experiments/artifacts/t07/key`, and `bin/grillo-netns`. | Replace development paths with stable, versioned installed-asset discovery; preserve explicit developer overrides. |
| Guest build script embeds a generated handshake key in a reusable initramfs. | **Security release blocker:** remove reusable embedded secrets. Generate a fresh credential for each VM boot and deliver it over a private channel with a documented threat model. |
| Guest build includes busybox test rootfs fixtures. | Produce a separately defined, fixture-free release guest image and keep fixtures in test artifacts. |
| QEMU/virtiofsd/pasta and devices are host prerequisites; `doctor` only checks some of these today. | Add packaging-aware checks for all critical prereqs and suggest tested, distro-specific remediation; avoid false OK. |
| Host tests show Fedora SELinux Enforcing blocks pasta helpers. | Fedora package may be experimental/unavailable until fixed and verified under Enforcing. Never auto-switch to Permissive. |
| Helm rendering requires an exact Helm version from `PATH`. | Provide a pinned, trusted renderer in the release and make version discovery deterministic. |
| The CLI supports daemon startup on demand and the UI is local-only. | Preserve this lifecycle; packages MUST NOT require an always-on system-level service. |
| Version constants are hardcoded in both binaries. | Inject matching semantic versions, commit, guest ABI, and build metadata in release builds. |
| The repository tracks an existing T24 MVP/hardening gate and later T27 packaging work. | Execute this spec as a **post-T24, pre-launch distribution track**; defer nonessential T27 optimization rather than blocking onboarding. |

**Release gate:** A successful compile, package lint, or checksum comparison cannot waive a failing security or real-KVM blocker.

## 5. Supported host policy

### 5.1 Support levels

- **Supported:** Tested clean installation and complete Compose + Helm E2E on a named distribution version, architecture, kernel policy, and dependency set.
- **Experimental:** Packages or tarballs are published and likely functional, but a known environment-specific limitation remains or coverage is incomplete. Not a supported recommendation.
- **Unsupported:** Runtime is not tested or critical prerequisites are unavailable.

### 5.2 Initial matrix (targets, not existing guarantees)

| Host | Packaging | Initial status | Promotion gate |
| --- | --- | --- | --- |
| Ubuntu 24.04 LTS amd64 | Tarball + DEB | Candidate for first supported target | Clean-host KVM, rootless networking, virtiofsd, Compose, Helm and UI E2E. |
| Fedora (version under active CI) x86-64 | Tarball + RPM | **Blocked/experimental until SELinux issue resolved** | Same E2E with SELinux **Enforcing**, without broad security workarounds. |
| Debian stable amd64 | Tarball + DEB | Future candidate | Dedicated clean-host suite; do not extrapolate from Ubuntu. |
| Other RPM families | Tarball, RPM only after validation | Unsupported initially | Dedicated install/runtime validation for each declared distro. |
| arm64, macOS, Windows, WSL | None | Unsupported initially | Separate architecture/host design and test plan. |

The actual **first supported distribution** SHALL be whichever candidate passes its clean-host gates first. The table is a plan, not a promise that the current kernel/device configuration passes on Ubuntu.

### 5.3 Environment requirements

The installer/preflight MUST distinguish:

1. **Hardware:** CPU virtualization support, usable `/dev/kvm`, and suitable `/dev/vhost-vsock` / `/dev/net/tun` devices.
2. **Kernel and policy:** required virtualization, unprivileged user/network namespace and filesystem features; effective user permissions; SELinux/AppArmor denials where detectable.
3. **Userspace helpers:** QEMU with required `microvm` devices, compatible virtiofsd, pasta, `ip`, `nft`, and other helpers actually exercised by the release.
4. **Guest assets:** valid manifest and checksums, supported host/guest protocol and architecture, guest kernel/runtime files.
5. **Optional features:** Helm renderer and supported chart features; rootless native builder prerequisites if different from baseline workloads.

A missing permission is not necessarily a missing package. Error messages must explain the difference.

## 6. Release payload and filesystem contract

### 6.1 One staged payload

The release pipeline SHALL assemble an immutable staging tree before producing `.tar.gz`, `.deb`, or `.rpm`:

```text
stage/
├── bin/grillo
├── libexec/grillo/grillod
├── libexec/grillo/grillo-netns
├── libexec/grillo/helm               # if chosen renderer packaging strategy
├── lib/grillo/guest/<guest-abi>/
│   ├── bzImage
│   ├── initramfs.cpio.gz
│   └── manifest.json
├── share/grillo/examples/
│   ├── hello-compose/compose.yaml
│   └── hello-helm/...               # local chart + compatible runtime images
└── share/doc/grillo/
    ├── LICENSE
    ├── NOTICE
    ├── THIRD_PARTY_LICENSES...
    └── VERSION.json
```

Here paths are **relative to the payload layout**, not necessarily package filesystem roots. Native installations map `bin` to `/usr/bin`, `libexec` to `/usr/libexec`, `lib` to `/usr/lib`, and shared files to `/usr/share`; distro-specific conventions may differ, but published logical paths must be tested. Tarballs preserve an equivalent **prefix-relative** layout.

- `grillo-agent` and guest `runc` belong **inside the guest image**, not on the host's normal `PATH`.
- An immutable, versioned guest-ABI directory prevents opaque overwrites during upgrades; retain or clean historical artifacts only under explicit lifecycle rules.
- Do not ship test keys, private signing keys, fixture root filesystems, cached registries, or experiment outputs.
- File and directory permissions are explicit; the installed executable tree is not user-writable in native packages.
- Each artifact MUST have SHA-256, size, build provenance, source version and component-license data.

### 6.2 Runtime asset discovery

**Required refactor:** `grillo` and `grillod` must resolve the installed daemon, netns helper, Helm renderer and guest assets without depending on the invocation CWD.

Resolution rules:

1. Explicit flag or environment override is allowed for developers (validated, never silently ignored).
2. Otherwise use a trusted install manifest/location embedded or discovered relative to the resolved binary/package prefix; do not blindly prefer a same-named executable from the current working directory.
3. Validate manifest, protocol/guest ABI and digest before starting a microVM.
4. Fail with an actionable error when assets are incomplete or incompatible; never switch automatically to host containers or unverified downloads.

Proposed stable contract (name subject to CLI review):

```sh
grillo version --json         # host version, commit, guest ABI, asset digest
grillo doctor --json          # machine-readable preflight, read-only
grillo doctor --verbose       # human-readable fixes / feature-specific checks
```

`doctor --json` and `--verbose` are **proposed additions**, not existing commands in the repository snapshot.

### 6.3 Guest signing and credentials

A shipped initramfs is a public distribution artifact. It MUST NOT contain a reusable authentication credential.

The chosen per-boot handshake design MUST specify: key generation, transfer (private virtio/vsock-compatible channel), peer authentication, memory lifetime, crash cleanup, logging exclusions, and replay protection. Guest artifact signatures/digests authenticate release content, **not** per-boot sessions. An implementation review and KVM integration test are release prerequisites.

## 7. Packaging channels

### 7.1 Portable tarball (P0)

Example **proposed** file name:

`grillo_<version>_linux_amd64.tar.gz`

Contents: staged payload plus a manifest and readme for direct extraction. User-friendly install instructions must cover `/usr/local` and home-prefix usage without requiring a root-run installer. The tarball does not magically install host libraries or adjust access to kernel devices; `doctor` must explain missing prerequisites.

**Acceptance:** Extract to an arbitrary prefix; from another CWD run `grillo version`, `grillo doctor`, a real Compose demo, and a Helm demo after prerequisites are available. A read-only prefix should work. No build tooling required on the target.

### 7.2 DEB (P1)

- One first-party package named `grillo` should provide CLI, helper executables, compatible guest artifacts, managed Helm renderer and examples.
- Use distro-native `Depends` for known, required packages. Package names MUST be resolved and tested against the actual target distro/version (do not assume Fedora and Ubuntu package names are equivalent).
- The installation may require `sudo apt install ./grillo_..._amd64.deb`; **the runtime itself remains rootless**.
- Maintainer scripts MUST NOT configure `/dev/kvm`, user/group membership, namespaces, firewall policy, SELinux/AppArmor, CA trust, or auto-enable a service.
- Uninstall (`remove`) leaves per-user state intact; even `purge` does not delete arbitrary users' data. Document a separate, explicit cleanup command after workloads stop.
- Package dependencies are not a substitute for hardware readiness checks.

### 7.3 RPM (P1)

- Single `grillo` RPM for each validated RPM-based distribution, using the same versioned staged payload and license manifest.
- Use tested RPM dependencies and virtual provides only where the exact runtime behavior is verified.
- The package MUST run under normal SELinux policy; **do not require users to disable SELinux** or invoke permissive mode as an installation step.
- RPM scriptlets MUST NOT silently change kernel-device permissions, install policy exceptions, enable a privileged service, stop workloads, or remove user data.
- Package signatures should be provided when publishing an official RPM channel; package repository metadata signing becomes mandatory if an official DNF repository is introduced later.

### 7.4 What not to ship first

Avoid an install-time kernel build, hidden `curl | sh` downloads, a bespoke system service, a Snap/Flatpak runtime bundle with untested device mediation, or cross-distro QEMU vendoring. Add Nix/Homebrew/other channels only after measuring demand and isolating platform-specific differences.

## 8. First-run user journey

The intended supported-host flow should read like this (commands are **target UX**, not a claim that installers already exist):

```sh
# Debian / Ubuntu, once official assets exist:
sudo apt install ./grillo_<version>_amd64.deb

# Fedora, only after the SELinux Enforcing KVM gate passes:
sudo dnf install ./grillo-<version>-1.x86_64.rpm

# Same workflow regardless of package format:
grillo version
grillo doctor
grillo up /usr/share/grillo/examples/hello-compose/compose.yaml
grillo ps
grillo ui --port 0
```

Helm should also work with the bundled example, without Kubernetes or separate Helm installation:

```sh
grillo plan /usr/share/grillo/examples/hello-helm --release hello
grillo up /usr/share/grillo/examples/hello-helm --release hello
```

**Required usability details:**

- Native package postinstall message: concise and copyable `grillo doctor` + `grillo up` instructions, no promotional boilerplate.
- If a machine cannot satisfy KVM or a hard security requirement, `doctor` must **fail clearly**; do not recommend rootful execution as fallback.
- Resolve helper paths and asset paths independently of CWD.
- The first successful `up` prints application/sandbox state and the reachable loopback URL, or points to a single command that does.
- `grillo ui --port 0` prints a local bootstrap URL; it remains loopback-only. Avoid logging or persisting its one-time credential.
- Binaries and guest artifacts are installed before use; the first boot may pull workload images from an OCI registry only when the application explicitly requires them.
- Provide a clearly named `doctor` error for each unsupported SELinux/AppArmor, namespace, KVM, TUN or vsock condition. Suggested fixes must be admin-reviewable and optional.
- Provide a **small local example** whose runtime behavior is useful to verify networking, guest execution, and cleanup. Keep demo assets minimal; any registry pull should be declared and digest-pinned where feasible.

### 8.1 Error-message contract

Every required readiness failure should include:

```text
FAIL  <capability>  <observed failure>
Why:  <what Grillo needs it for>
Check: <read-only command or log location>
Fix:  <distro-specific, explicit next step>
Docs: <stable documentation URL or local file>
```

Do not give generic advice such as "run as root". Where a distro policy prevents operation, say the distribution is currently unsupported and link an open compatibility issue.

## 9. Installation, upgrades, and uninstallation

### 9.1 Installation

- System package installation and runtime startup are distinct trust operations.
- Do not modify `~/.bashrc`, `.profile`, group membership, or per-user application data.
- No mandatory systemd service. Optional user-systemd integration can be introduced after testing lifetime/session behavior.
- Installation is safe even on machines without KVM; `doctor` reports that workloads cannot run.

### 9.2 Upgrade

- All included host helpers and guest artifacts declare version compatibility independently of SemVer.
- Prefer atomic package-managed replacement and immutable guest-version paths.
- Running workloads are not killed by install/upgrade scripts. Detect active old runtimes and offer an explicit, non-destructive upgrade procedure.
- The new CLI shall either negotiate a compatible daemon API or fail with an explicit "restart/upgrade daemon" explanation.
- Before any state migration, back up durable metadata and reject unknown future schemas; never rewrite volumes or secrets as part of package scripts.
- A failed upgrade must not silently switch to an older guest image or launch unverified binaries.

### 9.3 Uninstall

- Package removal deletes package-owned binaries, guest files and examples, but never user-owned volumes, OCI caches, application state, or secrets without explicit user action.
- If Grillo is running, removal should avoid destructive stop behavior; document how to stop workloads and the per-user daemon before uninstall, including consequences of package files being removed.
- Provide an explicit, interactive **user data removal procedure** distinct from package removal; default to preservation. An eventual `grillo reset --all` MUST require deliberate confirmation and identify exact paths first.

## 10. Release engineering and supply chain

### 10.1 Single source of truth

Build a release from an immutable signed/versioned git tag and locked sources. Produce the CLI, daemon, netns helper, guest agent, guest kernel/runtime image, and tested Helm renderer **once** in an isolated, auditable release pipeline. From the same staged artifacts produce all packaging formats. Never rebuild or patch executables independently inside a DEB or RPM job.

Release artifact set:

```text
grillo_<version>_linux_amd64.tar.gz
grillo_<version>_linux_amd64.tar.gz.sha256
grillo_<version>_amd64.deb
grillo-<version>-1.x86_64.rpm
SHA256SUMS
SHA256SUMS.sig / attestation metadata
sbom.spdx.json (or CycloneDX equivalent)
provenance.json / build attestation
THIRD_PARTY_LICENSES and corresponding-source information
release-notes.md
```

The release shall contain a machine-readable component manifest with at least:

```json
{
  "grillo_version": "<release-version>",
  "commit": "<commit-sha>",
  "platform": "linux/amd64",
  "guest_abi": "<guest-protocol-version>",
  "guest_kernel": "<pinned-kernel-release>",
  "renderer": "helm/<tested-version>",
  "components": [
    {"name": "grillo", "sha256": "<digest>"},
    {"name": "guest-kernel", "sha256": "<digest>"},
    {"name": "guest-initramfs", "sha256": "<digest>"}
  ]
}
```

This JSON is **illustrative schema**, not production metadata. Define and version the real schema before release.

### 10.2 Integrity and signatures

- Publish checksums for all downloadable assets; verify digests in CI and in asset-loading code where applicable.
- Produce provenance attestations and an SBOM covering both host and guest components, including embedded frontend dependencies and the Helm renderer.
- Use signed release artifacts/attestations with a documented verification command; never teach users to ignore signature failures.
- No secret values, signing private keys, development handshake keys, or credentials in package payloads or public provenance metadata.
- Preserve third-party licenses/attributions. **Linux guest kernel GPL obligations and corresponding-source distribution require specific review** before shipping its binaries; Apache-2.0 licensing of Grillo does not relicense bundled components.
- Record guest kernel configuration, input source digests, runc source/build version, build toolchain and full recipe so the guest can be rebuilt independently.

### 10.3 Versioning

- Keep public package version, CLI version, daemon version, API schema, guest protocol and guest image compatibility explicit.
- Mark prereleases `v0.1.0-alpha.N` (or similar), with clear experimental status.
- A tag does not imply support for all Linux distributions; release notes must link to an exact tested-host matrix.
- Build-from-source and package builds must report the same commit/identity for the same release; development builds should not masquerade as a final version.

## 11. CI and test gates

### 11.1 Required jobs

| Job | Purpose | Hard pass criterion |
| --- | --- | --- |
| `unit-and-static` | Existing Go, frontend, format, race and audit gates. | Green; no silent test skips. |
| `release-build` | Deterministic host + guest artifact staging. | Manifest, hashes, reproducibility comparison where feasible. |
| `tarball-smoke` | Extract in non-default, read-only location. | `version`/`doctor`/helper resolution work without repo checkout. |
| `deb-install-smoke` | Fresh supported DEB-family host. | Native dependencies resolve, normal user can run `doctor`. |
| `rpm-install-smoke` | Fresh Fedora host with SELinux Enforcing. | Policy remains Enforcing; no security-setting changes. |
| `kvm-compose-e2e` | Package-installed runtime, actual QEMU/KVM. | Pod boot, OCI start, DNS/HTTP, status, down, cleanup. |
| `kvm-helm-e2e` | Package-installed runtime, exact Helm renderer. | Chart render, apply, service reachability, down/recovery. |
| `upgrade-uninstall` | Lifecycle and persistence. | No data loss, no forced stops, version/ABI errors actionable. |
| `supply-chain` | Attestation, component licenses, SBOM, hashes. | All artifacts accounted for and verified. |

The hardware tests MUST execute on known KVM-capable hosts with trustworthy kernel/namespace access. **A skip for missing `/dev/kvm` must not count as a green release gate**. Generic hosted CI can perform static/package checks but does not substitute for actual KVM evidence.

### 11.2 Clean-host manual acceptance script

A developer other than the author should record:

1. OS/version/kernel, CPU architecture, default MAC policy, KVM and namespace settings.
2. Exact release asset and cryptographic verification performed.
3. Package installation command and full dependency-resolution outcome.
4. Output of `grillo doctor` before remediation and after permitted explicit remediation.
5. Compose up, real HTTP probe, `ps`, UI open, down, restart/recovery, stale-process/volume checks.
6. Helm plan/up and degraded/unsupported-manifest diagnosis.
7. Package upgrade, package removal and confirmation that user data is retained.
8. Measured installation time, time-to-first-successful-response, and relevant resource usage.

Publish sanitized evidence in `docs/experiments/` or a release-test report; do not publish user paths, secrets, session tokens, or private host configuration.

### 11.3 Security regression tests

Test at minimum: package/archive path traversal and symlink attacks, executable/helper hijacking from CWD, guest manifest tampering, missing device access, mismatched ABI, mutable runtime directory ownership, accidental root execution, stale handshake/session replay, unsafe host bind mounts, API ownership enforcement, and reinstall/upgrade over a running daemon.

## 12. Documentation and discovery

Minimum public documentation:

```text
README.md                         # 20-second value proposition and install route
docs/install.md                  # distro matrix, package instructions
docs/first-run.md                # copyable Compose / Helm walkthrough
docs/troubleshooting.md          # doctor codes, KVM, SELinux, namespace, VSock
docs/distribution-and-onboarding.md   # this specification
docs/security-model.md           # threats and limitations (or link to equivalent)
CHANGELOG.md
SECURITY.md
```

README install section should contain only two prominent actions: **Install Grillo** and **Run the first application**, with a link to `doctor` remediation. Separate contributor-specific kernel builds and Go/Node requirements into a **Develop from source** section so users do not mistake them for installation prerequisites.

Do not use a tutorial that requires `sudo grillo up`, disabling SELinux, installing Kubernetes, or building a guest kernel. Provide tested instructions for Fedora only once the Enforcing blocker is solved.

## 13. Proposed implementation plan (after T24)

Each work package has an independent demo and merge gate; preserve the current feature freeze.

### D0 — Packaging-ready runtime layout [P0]

**Work:** Rework helper/guest path resolution, version injection, guest ABI metadata, read-only installed assets, and `doctor` capability model. Remove repo-CWD assumptions and mixed host/guest binary paths. Define runtime guest vs test-fixture guest; implement per-boot credential handling before distributing reusable guest images.

**Done when:** A local staged prefix, moved to a different directory, runs Compose + Helm with hardware-backed KVM and no source checkout/build tools. Secret-scanning confirms no reusable authentication key in guest distribution files.

### D1 — Portable tarball and verified guest [P0]

**Work:** One release staging target, pinned runtime guest, Helm packaging strategy, artifact manifest, checksums, third-party licenses/source compliance, tarball, extraction smoke and clean-host test.

**Done when:** A second developer can install from tarball onto a verified host and run both included demos, without contacting the project maintainer.

### D2 — DEB / Ubuntu clean-host onboarding [P1]

**Work:** Dependency mappings, maintainer scripts, versioned prefix, postinstall hints, upgrade/uninstall semantics, package CI and Ubuntu KVM E2E.

**Done when:** Published DEB passes the complete supported-host matrix without modifying host security settings. If Ubuntu fails the guest-device/backend requirements, keep support experimental and record the blocker.

### D3 — RPM / Fedora Enforcing onboarding [P1]

**Work:** Rootless pasta vs SELinux policy resolution with narrowly scoped evidence, RPM dependency mappings, package CI, Fedora KVM E2E, and a policy-respecting documentation path.

**Done when:** Fedora passes the exact same first-run demo under SELinux Enforcing. No fallback to Permissive, rootful workloads, or blanket policy exceptions.

### D4 — First external-user release [P1]

**Work:** Release signing/attestations, checksums, SBOM, stable docs, upgrade/uninstall tests, support matrix, issue templates for `doctor` output, external tester feedback and metrics. Publish a clearly experimental tagged release only after all claimed-host gates pass.

**Done when:** At least one independent developer installs from a release artifact on a clean supported Linux host, runs a Compose app and Helm chart, opens the UI, shuts down, and uninstalls without data loss or author assistance.

**Dependency order:** `T24 → D0 → D1 → (D2 || D3 when host blockers allow) → D4`. D2 and D3 can proceed independently. Existing T27 performance profiling/optimization can follow once users can install the runtime; release-critical packaging work is pulled forward into this track.

## 14. Ship / no-ship checklist

### Must be true for the first release advertised as installable

- [ ] A real KVM E2E completes using **installed** files (not experiment paths).
- [ ] Guest image is fixture-free, reproducible enough to audit, and contains no shared embedded boot credential.
- [ ] Manifest/ABI mismatch and missing helpers fail closed with actionable diagnostics.
- [ ] Standard Compose and Helm examples run on the supported clean host.
- [ ] Runtime runs unprivileged; no automatic host security degradation.
- [ ] Install, upgrade, remove and user-data preservation have been tested.
- [ ] All shipped artifacts have digests, provenance/verification instructions and license review.
- [ ] A signed or otherwise appropriately authenticated release distribution path is documented.
- [ ] Package documentation accurately identifies **supported**, **experimental** and **unsupported** hosts.
- [ ] An independent tester completes onboarding without a source checkout.

### Must not be claimed without evidence

- “Works on Linux” without a narrow supported matrix.
- “Secure” or “production-hardened” merely because it uses a microVM.
- “Completely rootless installation” when a system package manager needs administrator rights.
- “Fully Kubernetes-compatible” because Helm templates render.
- “Offline first run” when an example requires an unshipped OCI image from the network.

## 15. Decisions to settle before D0 is accepted

1. **Bundled Helm:** Ship the exact tested renderer helper in standard releases or adopt the SDK? Choose based on size, supply chain, and behavioral compatibility, not on preference alone.
2. **First supported distro:** Which clean-host environment actually passes KVM + networking + security policy? Evidence decides, not the maintainer's main workstation.
3. **Guest credential delivery:** Which documented private device/handshake path replaces baked-in keys? This is release-blocking.
4. **Immutable guest artifact retention:** How do multiple installed package/guest versions coexist safely during upgrades without touching live workloads?
5. **Guest redistribution:** Are source, config, runc, third-party runtime files and license notices complete for every bundled component?
6. **Package monolith vs split:** Keep one installable `grillo` initially; consider `grillo-guest` or `grillo-examples` only if measured package size or distro compliance makes splitting useful.

---

### Recommendation to maintainers

**Build the portable tarball first, but design the staging layout for DEB and RPM from day one.** Treat the shipped guest image, secure per-boot credentials, and clean-host rootless KVM execution as the actual release work. Once that is proven, native packages should be straightforward wrappers around a tested product rather than a way of hiding an unfinished install process.
