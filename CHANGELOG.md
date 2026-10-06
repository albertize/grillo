# Changelog

User-visible changes are recorded here. There are no runtime releases yet;
[progress](docs/progress.md) tracks task completion and dated acceptance evidence.

## Unreleased

### Added

- Experimental Linux/amd64 QEMU/KVM runtime with a Go guest agent, guest-side
  runc, multi-container Pods, init containers, probes and persistent storage.
- Rootless application networking, Service DNS/TCP routing and loopback Ingress
  fallbacks; private Unix API, CLI and desired-state recovery.
- Compose and Kubernetes subset compilers, controlled local/exact-version OCI
  Helm rendering and explicit compatibility diagnostics.
- OCI image/cache tooling and a native guest-based Dockerfile build path, with
  Podman available only by explicit opt-in.
- Embedded React/PatternFly console with metadata-only inspection, retained-log
  and event views, VMM metrics and bounded non-TTY exec.
- Reproducible unit, browser and real KVM acceptance gates with dated reports.
- Product specification, delivery plan, ADRs and Apache-2.0 contribution policy.

### Changed

- Documentation organized into practical setup, architecture, console,
  compatibility and testing guides; historical implementation/review notes
  separated from current status.

### Known limits

- No packaged release, supported runtime version or completed product-hardening
  gate. See [compatibility](docs/compatibility.md), [console limits](docs/ui.md#explicit-mvp-limits)
  and [security policy](SECURITY.md); acceptance of a field is not universal
  Kubernetes compatibility or production security assurance.
