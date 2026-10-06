# Architecture Decision Records

ADRs explain consequential decisions and preserve the reasoning behind them. Use them for backend selection, rootless networking and storage, protocol or schema changes, major dependencies, licensing/distribution choices, and deviations from the specification.

## Current status

| ADR | Title | Status |
| --- | --- | --- |
| [0001](0001-scaffold-and-dependencies.md) | Scaffold and dependencies | Accepted |
| [0002](0002-explicit-dependency-bootstrap.md) | Explicit dependency bootstrap | Accepted |
| [0003](0003-guest-initiated-shutdown.md) | Guest-initiated shutdown | Accepted |
| [0004](0004-shared-filesystem-backend-comparison.md) | Shared-filesystem backend comparison | Accepted |
| [0005](0005-platform-qemu-virtiofsd.md) | Platform: QEMU `microvm` + virtiofsd | Proposed |
| [0006](0006-native-build-system.md) | Native build system with an optional Podman accelerator | Proposed |
| [0007](0007-controlled-helm-renderer.md) | Controlled official Helm renderer | Proposed |
| [0008](0008-patternfly-react-console.md) | PatternFly React console (user-requested implementation) | Proposed |

The implementation plan still contains **provisional choices**, not evidence that a
backend has passed its feasibility gate. An ADR without required hardware
evidence remains `Proposed`.

## Process

1. Create `NNNN-short-title.md` using the template below and the next available number.
2. Start with status `Proposed` and link the relevant specification sections and implementation tasks.
3. Describe constraints, alternatives, security implications, and the evidence needed.
4. Review with maintainers; move to `Accepted` only after approval and required evidence.
5. If the decision changes, create a new ADR and mark the old one `Superseded` with a link. Preserve historical reasoning.

Allowed statuses: `Proposed`, `Accepted`, `Rejected`, `Superseded`.

## Template

```markdown
# NNNN — Decision title

- Status: Proposed
- Date: YYYY-MM-DD
- Related tasks: Txx
- Supersedes: none

## Context

What problem, constraints, and requirements drive the decision?

## Options considered

List credible alternatives and their trade-offs.

## Decision

Describe the proposed or accepted choice and its scope.

## Evidence

Record experiments, commands, environments, measurements, and test results.
Separate verified facts from assumptions and unresolved questions.

## Consequences

Explain implementation cost, compatibility, migration, security, dependencies,
licensing, and user-visible limitations.

## Validation and follow-up

List acceptance criteria, remaining tasks, and conditions for revisiting the choice.
```

A decision without required hardware evidence may remain proposed; it must not bypass a feasibility gate.
