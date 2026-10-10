# D0 — Portable inventory and guest ABI validation

## Scope

Second bounded D0 preparation following [runtime layout](d0-runtime-layout.md).
T24 and D0 remain BLOCKED overall. This delivery implements portable boot
expectations and validates metadata; it does not produce a distributable guest,
package or supported host. The contract is
[guest manifest schema 1](../../guest/manifest-schema.md).

The subsequent [runtime-only guest/installed lifecycle report](d0-installed-runtime.md)
adds fixture/key scanning and actual relocated Compose/Helm/UI evidence. Remaining
items in this report describe this earlier delivery, not regression of later gates.

## Changed contracts

- Versioned manifest with exact host/guest ABI and Linux/amd64 metadata,
  canonical manifest-relative kernel/initramfs paths, SHA-256 and bounded sizes.
- `guest/manifest -portable` writes deterministic, atomic boot inventories from
  existing staged files. No executable/download, opaque-image audit or secret scan.
- Installed daemon workload/native-build backends require versioned metadata.
  Explicit developer manifest overrides preserve legacy fixtures. Invalid declared
  ABI/schema/platform never falls back to legacy.
- `doctor` uses the same loader and relative-path anchor; installed legacy assets
  fail. Hash reads are bounded and regular-file-only. Observed symlinks/special
  files are rejected; portable reads use filesystem-root confinement.
- QEMU retains anchored artifact objects and verifies them into private snapshots
  before launch. Existing fresh-key overlay/authentication and teardown remain.
- Strict JSON rejects unknown/duplicate fields, trailing data, oversize and deep
  nesting. No new dependency or ADR acceptance.

## Environment and inputs

Existing development host: Linux `7.2.8-200.fc44.x86_64`, x86-64,
QEMU `10.2.2` (`qemu-10.2.2-1.fc44`), Go `1.26.8-X:nodwarf5`.
SELinux was already **Permissive**; this delivery did not change policy or exercise
pasta networking. It does not resolve Fedora's Enforcing blocker.

`make test-d0-manifest` rebuilt the T07 agent/image before the hardware run:

| Development input | SHA-256 |
| --- | --- |
| Existing QEMU kernel | `550b5916433e758e814039f8324a9f1cdfdd2b3b8e4a1e44cc6836cdc92d5942` |
| First rebuilt T07 initramfs | `5fe6fad084264bad25eb953dcd535e519894399dfd8d25061f032dabc0315071` |
| Final rebuilt T07 initramfs (after strict JSON regressions) | `14d3eb58b7834990184746ed387c72b568faa65a8caea2130c3d50ac0a981c46` |

The T07 image still contains fixture roots and a legacy key. It is **not a release
artifact**. The test's ancestor temporary directory is private; its base
bytes are copied into private verified images and a new key overlaid for each boot.

## Actual checks

- Focused ordinary Go tests for `internal/guest`, `internal/backend/qemu`,
  `internal/cli`, `guest/manifest` and `cmd/grillod` passed; focused races for guest,
  backend and CLI passed.
- Positive tests: deterministic sorted output, moved/read-only directory, spaces,
  arbitrary CWD, shared doctor/backend loading, legacy developer compatibility.
- Negative/failure tests: future/missing schema, ABI/platform mismatch, missing/
  duplicate artifacts, duplicate JSON fields, noncanonical/absolute/traversal/
  drive/NUL paths, absent/outside sources, symlink substitutions, FIFO/special
  inputs, size/digest mutation, missing manifest and unanchored in-memory inventory.
  Fake backend lifecycle proves only its contract, not KVM.
- `make test-d0-manifest` passed **without SKIP**. The new dedicated test copied
  the rebuilt fixture kernel/image into an operation-owned directory, generated
  schema 1, moved it to a prefix containing spaces, made the guest directory
  read-only and changed CWD. The real backend completed **two authenticated KVM
  boots** with distinct fresh keys, running/guest-alive inspection, stop and delete.
  Recorded VMM identities were no longer alive and backend resource directories
  were empty. No workload/Service/pasta/Compose/Helm was executed by this gate.
- `make check` passed frontend/static/unit/script/race/build/module checks and
  passed again after the duplicate-field/nesting changes. `make test-d0-manifest`
  likewise rebuilt the image and passed again without SKIP. Both actual input
  digests are retained above; no reproducible guest-image claim is made.
- CLI smoke: copied freshly built host binaries, existing pinned Helm, kernel and
  rebuilt fixture image into a temporary prefix; generated the portable manifest
  with the actual `go run ./guest/manifest -portable` command; moved the prefix,
  removed write permissions and ran `doctor --json` from `/tmp`, with all asset
  overrides unset. Exit was 0; daemon/netns/Helm and guest inventory checks were
  OK. This is an installed-layout/inventory smoke, not clean-host readiness proof.

## Remaining gates and next task

Define and build a separate fixture-free runtime image without a reusable
credential, including minimal required guest networking utilities. Review/scanning
must cover opaque image contents, not merely the inventory's two entries. Then
rebuild host/guest files and verify the actual installed daemon/CLI Compose + Helm
lifecycle from moved/read-only files, without the source checkout as a runtime
asset dependency. Document guest retention/upgrade behavior before distribution.

Helper digests/provenance, publisher authentication, component licenses/source
redistribution, complete doctor features/distro remediation, clean external-host
and T24 budgets/quota/load/CI gates remain open. No new performance or supported-
distribution claim; no package/upgrade/uninstall or full onboarding test was run.
