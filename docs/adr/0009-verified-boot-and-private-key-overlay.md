# 0009 — Verified boot snapshots and private per-boot key overlay

- Status: Proposed
- Date: 2026-10-07
- Related tasks: T07, T08, T18b, T24; specification §6.4 and §28
- Supersedes: none

## Context

Development component fixtures used a reusable image-baked handshake key.
Creating an artifact inventory did not enforce production launch verification.
T24 requires verifiable boot inputs and fresh private credentials without host
privilege escalation, command-line secrets, a custom protocol, or new dependencies.

## Options considered

- Rebuild a complete guest image for every VM: simple but duplicates packaging
  work and trusted content; requires broader builders and cache handling.
- A new virtio/fw_cfg credential device: plausible, but adds device/configuration
  plumbing and another independently tested guest bootstrap path.
- A private verified base image plus a Linux initramfs key overlay: uses existing
  boot transport and standard gzip/newc extraction, with no extra host tools.

## Decision

For the experimental daemon's QEMU runtime/build paths, pin a strict local
artifact inventory at backend open. Verify kernel/initramfs bytes into private
0600 per-operation snapshots before starting helpers or the VMM. Never execute
original mutable source paths after verification.

Generate a fresh 32-byte key for every actual boot; append a second gzip/newc
archive that replaces `/etc/grillo/key` with a root-owned 0600 entry. Persist the
credential only in private backend state. A live idempotent Start keeps its
identity; consumers query the actual persisted CID/port/key after Start rather
than assume a new Create candidate was used. No credential goes into process
arguments, public DTOs or diagnostics. Direct component fixtures remain separate.

This is an implemented proposal, not a declaration of maintainer ADR acceptance.

## Evidence

[The T24 report](../experiments/t24-interactive-metrics-artifacts.md) records real
KVM runtime/native-build handshakes and daemon gates using the overlay, plus
negative mutation, immutable snapshot, archive, canceled-copy and fake-process
restart/idempotency tests. These do not establish publisher authenticity or a
clean external-host acceptance result.

## Consequences

- Adds private disk copies and digest work at boot; new full-budget measurements
  are required. Base-image cache mutation is an explicit boot failure.
- Local expected inventories are operator-trusted, not signed releases. Boot
  kernel/initramfs are independently verified; embedded content is transitively
  covered. Host VMM/helper provenance remains separate.
- Private derived images contain credentials. Same-UID host compromise and a
  compromised guest remain outside credential confidentiality guarantees.
- Linux multi-archive unpacking is required; this is not portable runtime-host
  support. Default manifest absence/corruption blocks production launch.
- No Go dependency, executable download or upstream license change is introduced.

## Validation and follow-up

Review/acceptance, clean-host reproduction, complete resource/performance budgets,
release inventory authentication/provenance and license/SBOM work remain open.
Revisit credential delivery if supported kernels/backends cannot safely unpack
multiple archives, or measured copy costs require a different verified design.
