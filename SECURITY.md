# Security Policy

## Current status

Grillo has an experimental Linux/KVM runtime and recorded isolation/security regression tests. There are **no supported runtime releases**, independent security audit or completed product-hardening gate. Implemented controls and test coverage are not a production security guarantee. Do not use this repository as a security boundary for untrusted production workloads.

Security-sensitive design feedback is welcome. A formal release support window and vulnerability-response process must be established before production-grade claims or releases.

## Reporting a vulnerability

Do not disclose exploit instructions, credentials, private paths, or sensitive logs in a public issue or pull request.

A dedicated security email address has **not yet been established**. Do not assume that an address inferred from the project name or a Git commit is an approved reporting channel.

1. If the repository hosting service offers an enabled private vulnerability-reporting feature, use it.
2. Otherwise, contact a repository maintainer privately through an established, verified channel and request a confidential reporting method.
3. If neither is available, open a minimal public request for a private security contact **without technical vulnerability details**. Wait for a verified private channel before sending the report.

Report enough information to reproduce and assess the issue safely:

- Affected commit or release and component.
- Host/guest environment and relevant tool versions.
- Expected versus actual behavior and the trust boundary crossed.
- Minimal reproduction using synthetic data, where possible.
- Preconditions, impact, and any proposed mitigation.

Remove live secrets and unrelated personal information. Coordinate disclosure with maintainers rather than assuming an immediate response or an established response-time SLA.

## Threat model and boundaries

The primary threat is an application workload that is buggy, compromised, or malicious on a developer workstation. The intended microVM boundary reduces direct host-kernel syscall exposure, but does not eliminate vulnerabilities in:

- The host kernel and KVM.
- The VMM and virtual devices.
- The guest kernel, guest agent, and OCI runtime.
- Filesystem sharing, bind mounts, networking helpers, and proxies.
- Manifest/chart parsers, registry clients, archive extraction, and artifact supply chains.
- The local management API and browser-facing console.

Host bind mounts intentionally expose selected host data. A process already running as the same host user can often access that user's files and processes; rootless operation is not a promise of isolation from a fully compromised user account.

## Required implementation principles

- Run host orchestration as the developer's user, with no silent privileged fallback.
- Keep management channels private and separate from workload traffic.
- Bind the UI to loopback by default and still defend against CSRF, hostile origins, and DNS rebinding.
- Protect secret storage, redact public diagnostics, and avoid exposing raw tool output.
- Verify artifacts and image content; prevent archive traversal and unsafe symlink handling.
- Enforce input, memory, disk, connection, session, and concurrency limits.
- Make host access, bind mounts, CA trust, and destructive operations explicit.
- Validate guest-originated data even after channel authentication.
- Fuzz security-sensitive parsers and protocols and test failure/recovery paths.

These principles remain requirements; they do not assert that every control has been implemented or audited. See [architecture](docs/architecture.md), [current status and evidence](docs/progress.md), and [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md), especially sections 6, 7, 11–14 and the unfinished T24 gate. Private reporting and release-readiness requirements below remain unchanged.

## Release readiness

Before distributing a runtime as suitable for security-sensitive use, maintainers should publish the verified threat model, supported versions, a working private reporting channel, artifact provenance, known limitations, and a response process. Avoid claims such as “absolutely secure.”
