# 0002 — Explicit dependency bootstrap for the feasibility spike

- Status: Proposed
- Date: 2026-10-03
- Related tasks: T00, T01
- Supersedes: none

## Context

The user requested an installation script. T01 has no VMM or verified guest
artifacts, while ordinary builds must remain offline-capable and cannot install
software, escalate privileges, or change host configuration implicitly.

## Options considered

- Automatically fetch tools in doctor/build: rejected because this hides effects
  and expands the normal runtime trust boundary.
- Install every planned external dependency: rejected because many are still
  provisional and have no selected version or proven backend contract.
- Separate explicit package setup and checksum-pinned user-local downloads:
  chosen for the current scaffold and feasibility experiment.

## Decision

Provide a Bash bootstrap with a no-effects default, a non-root download mode,
and a separate Fedora package mode requiring explicit administrator invocation.
Never invoke sudo inside the script. Preserve package-manager confirmation.
Keep downloaded artifacts private, verify pinned SHA-256 values before any
extraction, preserve upstream licensing, and do not execute downloaded code.

Pin Go 1.26.8, candidate Firecracker 1.17.0, Linux source 6.1.188, and the
Firecracker v1.17.0 x86_64 6.1 guest config. These pins are experiment inputs,
not approval of the backend or a promise that the config boots our guest.
Fedora package versions are distribution-managed rather than reproducibly pinned.

## Evidence

[scripts/README.md](../../scripts/README.md) documents checksum provenance and
operational behavior. Offline installer tests exercise success and failure with
local fixtures. No bootstrap network installation, host package changes, actual
archive extraction, or guest build has been performed in this implementation
session. Release metadata and the small config were inspected over HTTPS.

## Consequences

Setup effects are isolated from normal commands. Maintainers must review pins,
licensing, and updates; checksum provenance is not independent signature
verification. Existing installations fail closed rather than being overwritten.
Bash and GNU host utilities are prerequisites; support is initially Linux/amd64
and Fedora. No new Go module, privileged service, or production runtime adapter
is introduced.

## Validation and follow-up

Maintainer review remains pending. Run the explicitly authorized bootstrap on the
target machine, record tool/artifact identities, and implement the guest before
claiming T01 completion. Later tasks add only the external dependencies they
actually need. T03, not this script, selects the backend.
