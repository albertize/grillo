# Contributing to Grillo

Grillo has a working experimental runtime, not a supported release. Contributions
should preserve real application behavior and make unsupported semantics clear.
Read the [README](README.md), [compatibility](docs/compatibility.md) and
[current progress](docs/progress.md); implementation tasks and acceptance gates
remain in the [plan](IMPLEMENTATION_PLAN.md). Coding agents follow
[AGENTS.md](AGENTS.md). All participants follow the [Code of Conduct](CODE_OF_CONDUCT.md).

Use English for documentation, comments, issues and commits. Keep changes focused
and preserve unrelated work. Discuss significant architectural or dependency
changes before implementing them; a backlog entry alone is not authorization.

## Build and test

Use Go 1.27.x at the exact selected patch in `.go-version` (currently 1.27.2),
Make, Python 3 for fail-closed toolchain checks, Node 24.18.0/npm 11.16.0 and a C
compiler for Linux race tests. `GOTOOLCHAIN=local` prevents implicit downloads;
unknown vendor/development identities and `GOVERSION` overrides are rejected:

```sh
make ui-deps
make check
```

Frontend installation is an explicit integrity-locked step with lifecycle scripts
disabled. Node/npm are not runtime requirements of the binary. See
[testing](docs/testing.md) for focused checks, Firefox and real KVM gates, and
[getting started](docs/getting-started.md) for host/guest preparation.

Test the failure mode the change can affect:

- Pure compilation/planning: valid/invalid input, unknown/degraded semantics,
  deterministic output, source diagnostics and secret separation.
- Runtime/state: cancellation, deadlines, crash recovery, idempotency, ownership
  and concurrent operations.
- Parsers/storage/networking: adversarial inputs, bounds, traversal/symlinks,
  permissions, unsafe auth/redirects and failed cleanup.
- CLI/UI/API: real parsing/contracts, authentication, output safety and lifetime.

A fake proves its contract, not KVM/rootless networking/live sharing. Missing
prerequisites are skips/blockers, never successful hardware evidence. Rebuild
artifacts actually under test. Performance claims require reproducible commands,
environment, sample counts and results. Do not weaken a gate to hide a regression.

For prose-only changes, check links/anchors, Markdown structure, actual command
syntax, task/ADR consistency and diff cleanliness. Preserve historical evidence
while keeping current guides free of superseded status claims.

## Implementation conventions

Prefer the Go standard library and approved official `golang.org/x/*` modules;
use the maintained approved YAML parser. New significant dependencies, CGO,
SDKs, external tools or security-boundary changes need explicit justification
and an [ADR](docs/adr/README.md). The React/PatternFly build is separately pinned
under `web/`; it does not change Go dependency policy.

Keep interfaces small and typed, blocking operations contextual/bounded, and pure
planning separate from effects. Host commands use argument arrays, never
untrusted input in a host shell. Cleanup requires operation ownership and safe
process/filesystem identity. Unsupported behavior fails visibly rather than
falling back to privileged execution or host containers.

Record versions, licenses, provenance and security implications. External VMM,
kernel, runc, fonts/icons and build tools retain their own licensing obligations.

## Submitting a change

1. Inspect existing changes and select a bounded task with verified dependencies.
2. Define the behavior/contracts and positive, negative and failure-path tests.
3. Implement the smallest coherent change; avoid unrelated refactors/upgrades.
4. Run applicable checks and record actual results, including failures and skips.
5. Update current documentation, compatibility and progress; link detailed evidence.
6. Submit motivation, approach, checks/environment, security/compatibility impact
   and known limits. Use a focused branch if following a pull-request workflow.

Before submission, check that there are no credentials, private runtime state,
VM images, generated binaries or unrelated changes. Do not infer repository
ownership, release channels, contacts or URLs. ADR approval is separate from
implementation evidence; do not mark proposed decisions accepted without review.

Use concise English commit messages describing intent. `docs:`, `feat:`, `fix:`,
`test:` and `refactor:` are useful prefixes, not substitutes for an explanation.
Commit only when authorized; avoid shared-history rewriting without agreement.

## Licensing and contribution provenance

Grillo is licensed under the [Apache License, Version 2.0](LICENSE). Unless explicitly stated otherwise, contributions intentionally submitted for inclusion are provided under Apache-2.0, consistent with section 5 of that license. No separate contributor license agreement is currently required.

Only submit work you have the right to contribute. Preserve third-party notices and identify copied/adapted material and its license. AI-assisted contributions remain the submitter's responsibility: review their correctness, security, provenance, and licensing; do not assume generated material is automatically safe to redistribute.

For new original source files, use a language-appropriate comment with:

```text
SPDX-License-Identifier: Apache-2.0
```

This marker does not replace required third-party notices or justify altering a file's existing license. Do not invent copyright ownership or claim all external components are Apache-licensed.

## Security reports

Do not post exploit details or credentials in public issues. Follow
[SECURITY.md](SECURITY.md) for the verified private-reporting process.
