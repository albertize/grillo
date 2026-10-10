# D0 — Runtime layout preparation

## Scope and status

First bounded part of [D0](../distribution-onboarding-spec.md#d0--packaging-ready-runtime-layout-p0),
not a completed distribution gate. T24 is still BLOCKED. The user authorized
starting the distribution specification; only preparation independent of T24's
external-host and release gates is delivered here.

Subsequent [portable inventory](d0-portable-manifest.md) and
[runtime-only guest/installed Compose+Helm+UI](d0-installed-runtime.md) deliveries
resolve the local guest/lifecycle prerequisites listed below. This historical
report's T24/release blockers are not waived.

## Contracts delivered

- One prefix-relative layout for CLI, daemon, network helper, pinned Helm helper
  and expected guest ABI directory `1.1`. Launcher symlinks resolve to the actual
  binary before deriving the prefix. Prefixes containing spaces are supported.
- Explicit absolute prefix/environment and daemon flag overrides; missing
  overrides do not silently fall back to another binary. No implicit CWD/PATH
  search for Grillo helpers or Helm. Distro-managed QEMU/network utilities still
  use existing host-tool selection.
- On-demand daemon startup resolves the installed daemon only when no daemon
  answers. Workload lifetime remains independent of CLI/browser lifetime.
- Shared CLI/daemon build version, commit and build-time injection via
  `make build VERSION=... COMMIT=... BUILD_TIME=...`. Defaults identify development
  builds, not a supported release. `version --json` reports expected guest ABI,
  not proof of artifact compatibility or an asset digest.
- `doctor --json` and `--verbose`; TUN/ip/nft, root-user, helper and guest-inventory
  checks added. Denied device access is a failure. Checks do not reconfigure the
  host or create assets. Remediation is conservative, not yet distro-specific.
- Daemon startup requires the network helper rather than silently disabling it.

Source-development setup is in [getting started](../getting-started.md). Existing
per-boot key overlays and private verified snapshots are unchanged; see
[ADR 0009](../adr/0009-verified-boot-and-private-key-overlay.md).

## Actual verification

Environment: existing Linux/amd64 development host, SELinux already Permissive,
KVM device present. No security policy changed.

- `go test ./internal/runtimeassets ./internal/cli ./internal/frontend/helm ./cmd/grillod`
  passed. Tests cover moved prefixes, symlink launchers, spaces, explicit overrides,
  absent/non-executable helpers, no PATH fallback, command flags/JSON, guest
  tampering and inventory/path mismatch. These are unit contracts, not KVM gates.
- First `make check` failed formatting for the new distribution test file;
  corrected with gofmt. Second `make check` passed frontend tests/assets, format,
  vet, Go tests, script regressions, races, builds and module audit.
- Final focused tests and races for runtimeassets/CLI/Helm passed after normalizing
  explicit Helm overrides. A broad `gofmt -l .` additionally traversed ignored
  downloaded Go toolchain sources and failed on upstream intentional syntax-error
  fixtures; no downloaded file was modified. Repository formatting is checked
  through the established `make fmt` scope, not those external test corpora.
- Final `make check` passed again, including rebuilt binaries and the guest ABI /
  protocol drift regression; `git diff --check` and local Markdown links/fences
  passed.
- Copied freshly built host binaries and the existing Helm executable into a
  temporary staged tree, moved it to a prefix containing spaces, removed write
  permissions, and invoked from `/tmp`: `version --json` reported development
  identity; `doctor --json` reported installed helper paths and correctly failed
  for the absent guest manifest. This expected negative smoke is not a ready host.
- From that prefix and `/tmp`, `plan <absolute examples/helm/demo> --release demo`
  passed with the prefix's Helm helper (one workload/sandbox/container).

## Open acceptance gates

No Compose/Helm workload boot, daemon autostart KVM, fixture-free runtime guest,
secret-scanning, clean external host, package/upgrade/uninstall or signed release
check was run in this delivery. A copied developer tree is not a release payload.

The existing guest manifest stores build-time paths: prefix movement of guest
artifacts was **not solved in this first delivery**. The subsequent
[portable-manifest delivery](d0-portable-manifest.md) implements versioned relative
paths, ABI metadata validation and a dedicated real boot gate. Helper digests/provenance and complete
capability-specific/distro remediation remain open. Doctor does not prove QEMU
features, exact renderer version, real namespace usability or SELinux/AppArmor
compatibility; Helm checks its exact version when rendering.

The development initramfs still contains test roots and a legacy fixture key;
it must not be distributed. Define a separate auditable runtime build without
fixtures or embedded credentials, then rebuild and verify real KVM Compose/Helm
from moved/read-only assets. Preserve live workloads during future version
retention/upgrade work. No D0 DONE or supported-distribution claim is made.
