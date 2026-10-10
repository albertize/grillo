# Experimental host evidence and support boundaries

**No supported runtime release, distribution range or security-response SLA exists.**
These records identify tested scopes, not a support contract. Linux/amd64 and real
KVM are current runtime prerequisites; other runtime platforms are not claimed.

## Evidence matrix

| Environment | Evidence | Status / limitation |
|---|---|---|
| Local Fedora fc44, Linux 7.2.9, QEMU 10.2.2, SELinux already Permissive | Installed Compose/Helm/UI, private verified boots, bounded cache/load campaigns | Experimental local PASS within each report's scope; not clean-host or Enforcing support |
| User's second Fedora 44 Workstation, Linux 7.2.9-200.fc44, SELinux Permissive | Compose up/down/up and HTTP, Helm Ingress HTTP/down, user confirms UI functionality | Experimental external functional PASS; exact helper/binary hashes, full cleanup, clean-environment and lifetime checks incomplete |
| Recorded Fedora SELinux Enforcing policy | pasta exits blocked in the recorded experiments | BLOCKED for rootless networking; never recommend disabling MAC as an acceptance shortcut |
| Ubuntu, Debian or other Linux distributions | No dedicated external acceptance evidence recorded | UNVERIFIED, not supported by inference from Fedora |
| macOS/Windows, arm64 or GPU runtime | No independent hardware/backend gate | Outside the currently verified runtime target |

Detailed reports are in the source checkout under `docs/experiments/`:
`d0-installed-runtime.md`, `t24-cache-load-license-review.md`, and
`d0-second-host-fedora44.md`. They are not included in the runtime prefix.
A successful doctor probe or named helper version does not prove workload
networking, filesystem semantics, isolation or standard-policy compatibility.

## Before running

Use a normal user account and provision host QEMU, virtiofsd, pasta, ip/nft and
accessible KVM/tun/vhost-vsock devices deliberately. The experimental prefix
contains Grillo helpers, guest assets and an exact managed Helm renderer; no
runtime Go/Node/npm/Podman/source checkout is required for its packaged examples.
First workload pull needs Internet unless that image is already cached.

```sh
grillo version --json
grillo doctor --verbose
grillo doctor --json
uname -rs
```

Record OS/version, architecture, helper versions and actual MAC policy through
read-only host commands. Do not publish full environment dumps, raw private state,
user paths, registry credentials, workload secrets or UI bootstrap URLs. Do not
change SELinux/AppArmor, sysctls, device/group permissions or host firewall merely
to manufacture a PASS. Explicit remediation needs the owner's authorization and
must remain separately recorded from baseline/default-policy evidence.

## Artifact and licensing boundaries

`share/doc/grillo/VERSION.json` identifies copied files by SHA-256/size and now
extracts module/toolchain metadata from each copied Go executable, including Helm.
This metadata can be forged by the producer; it does **not** authenticate a
publisher. Dev commit/guest inventory alone does not identify dirty host patches.
Check the archive checksum before extracting a private experimental transfer, and
retain its exact checksum when reporting results. No signature/trust root exists
for these local archives.

The payload includes frontend/font notices under `share/doc/grillo/third-party`.
`notice_coverage` remains explicitly partial. Original Grillo LICENSE/NOTICE do
not license all bundled bytes; kernel/runc/BusyBox/glibc/Helm and Go dependencies
retain separate obligations. Corresponding-source and complete dependency notice
review, SBOM/attestation and publisher/release decisions remain blocking work.
Do not publish the payload as an official redistribution-cleared release.

## Diagnostics and operational limits

- `down` stops workload resources while retaining application registration and
  managed persistent data. Current `ps` lists registered names without state;
  use `inspect` and probe the previous endpoint to distinguish stopped workloads.
- Compose endpoint inspection currently has a reported null-field defect despite
  a reachable published port. A configured route without an endpoint after Helm
  down is not evidence that the old listener is still active: probe it.
- Closing the CLI/browser does not own workload lifetime. Confirm this on the
  target host, rather than infer it from UI launch output.
- Quotas cover CAS/runtime rootfs logical bytes and entries, not whole-XDG/
  physical storage/volumes. No active-rootfs GC or package-manager upgrade/
  uninstall orchestration exists. Do not manually delete active roots or bind data.
- Upgrade a private test into a new prefix. An existing daemon keeps old code;
  stop owned workloads and deliberately terminate only the verified old daemon
  before using the replacement. No blanket pkill or automatic privilege changes.

## Reporting and release prerequisites

For potential vulnerabilities, use an enabled private reporting feature on the
verified repository host, or contact a maintainer privately through an established,
verified channel. If neither exists, request a private contact without technical
details and wait before sending the report. The source checkout's `SECURITY.md`
contains the full policy; no security email/maintainer identity is invented here. Ordinary
sanitized reports should include exact payload/binary checksums, versions, minimal
commands, exit codes, observed responses, hardware/MAC prerequisites, failed or
skipped checks, and owned-resource cleanup results. Never turn SKIP into PASS.

Before claiming distribution support: complete clean-host/default-policy E2E,
remaining T24 budgets/security/fault gates, source/notices/provenance, hosted CI
and verifiable delivery. Maintainers must establish release channels, reporting
contact, supported-version policy and response process before promising support.
Delivery status and requirements are in the source checkout's `docs/progress.md`
and `docs/distribution-onboarding-spec.md`; this staged guide does not claim those
source-only documents are available in the runtime prefix.
