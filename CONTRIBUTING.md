# Contributing to Grillo

Thank you for helping shape Grillo. The project currently has design documents and an initial command scaffold, not a working workload runtime. Design reviews, feasibility experiments, test design, and focused documentation improvements are especially useful at this stage.

## Before starting

Read the [README](README.md), [specification](grillo-project-specification.md), [implementation plan](IMPLEMENTATION_PLAN.md), and [progress tracker](docs/progress.md). Coding agents must also follow [AGENT.md](AGENT.md). All participants are expected to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

Use English for documentation, code comments, issue descriptions, and pull requests. Keep technical terms precise and distinguish intended behavior from implemented, tested support.

For a substantial change, discuss the problem and proposed approach with the repository maintainers before implementing it. Use the repository's issue/discussion system where available. Reference a task ID such as T04 and identify prerequisites. Do not infer that a planned task has been assigned or approved merely because it appears in the plan.

## Useful contributions now

- Review the one-Pod/one-microVM architecture and frontend semantics.
- Reproduce rootless KVM, networking, and live filesystem-sharing experiments.
- Identify unsafe assumptions, unsupported Kubernetes fields, or misleading compatibility claims.
- Design small positive and adversarial fixtures with clear expected behavior.
- Review dependency, licensing, packaging, and guest-artifact choices.
- Improve the implementation plan without weakening its acceptance gates.

The Go module and build system cover only the initial scaffold; use `make check` with Go 1.26.8. Do not add empty scaffolding for every planned package or claim that scaffold tests implement a runtime milestone.

## Development workflow

1. Check the existing work and choose a bounded task with satisfied dependencies.
2. Create a focused branch if using a pull-request workflow.
3. Define the behavior and tests before expanding implementation scope.
4. Make the smallest coherent change, preserving unrelated modifications.
5. Run applicable checks and record exact results, including skips and failures.
6. Update relevant documentation, progress, and compatibility entries.
7. Submit a focused pull request explaining motivation, approach, and evidence.

Avoid mixing unrelated refactors, dependency upgrades, and new features. An unavailable hardware prerequisite should be recorded as a blocker rather than bypassed using host containers or privileged setup.

## Go and dependency guidelines

- Prefer the Go standard library and the official `golang.org/x/*` modules identified in the plan.
- Use the approved maintained YAML parser rather than writing a parser from scratch.
- New third-party modules, SDKs, CGO requirements, and external tools need a concrete justification and an ADR when they affect architecture or deployment.
- Keep interfaces narrow, typed, and testable; separate pure compilation/planning from runtime effects.
- Use contexts, timeouts, bounded concurrency, and explicit cleanup ownership.
- Make resource creation and deletion safe to retry.
- Never use untrusted input in a host shell command.
- Avoid silently ignoring errors, fields, failed ownership changes, or unsupported features.

Record versions, licenses, provenance, and security implications of dependencies. Guest Linux, runc, VMMs, and other external components retain their own licenses; distributing artifacts may require additional notices or source obligations.

## Tests and verification

Documentation-only changes should verify local links, task references, Markdown formatting, and diff cleanliness.

After T00 creates the module, routine Go checks include formatting, `go vet ./...`, `go test ./...`, and supported Linux race tests. Follow the repository's actual build targets when they exist. Do not present planned commands as available tooling.

Tests should cover:

- Normal operation and invalid input.
- Unknown or degraded compatibility behavior.
- Cancellation, deadlines, process exits, and recovery.
- Secret redaction and unsafe filesystem/network input.
- Idempotency and concurrency where relevant.

Separate ordinary tests from real KVM integration tests. A fake backend proves a contract but not hardware behavior. A skipped hardware test is not a passed milestone. Benchmark claims require environment information, reproducible commands, sample counts, and results.

## Architecture decisions

Use an [ADR](docs/adr/README.md) for backend selection, state layout changes, new significant dependencies, security-boundary changes, and deviations from the specification. Record alternatives and consequences, not just the selected answer. Do not mark proposed choices accepted without review or evidence required by the plan.

## Pull-request checklist

- [ ] Scope and related task/issue are clear.
- [ ] Dependencies and acceptance criteria have been checked.
- [ ] Tests cover success and relevant failure/security paths.
- [ ] Actual checks, skips, and hardware limitations are reported.
- [ ] No credentials, local runtime state, VM images, or generated binaries are included.
- [ ] Documentation and compatibility claims match delivered behavior.
- [ ] Dependency/license changes and ADRs are included where needed.
- [ ] Unrelated user changes are preserved.
- [ ] Progress is updated without overstating completion.

A useful pull-request description has **Problem**, **Approach**, **Tests**, **Compatibility/security impact**, and **Known limitations** sections.

## Commit messages

Use concise English messages that explain intent. Conventional prefixes such as `docs:`, `feat:`, `fix:`, `test:`, and `refactor:` are encouraged but not a substitute for a meaningful description. Keep commits reviewable and avoid history rewriting on shared branches without agreement.

## Licensing and contribution provenance

Grillo is licensed under the [Apache License, Version 2.0](LICENSE). Unless explicitly stated otherwise, contributions intentionally submitted for inclusion are provided under Apache-2.0, consistent with section 5 of that license. No separate contributor license agreement is currently required.

Only submit work you have the right to contribute. Preserve third-party notices and identify copied/adapted material and its license. AI-assisted contributions remain the submitter's responsibility: review their correctness, security, provenance, and licensing; do not assume generated material is automatically safe to redistribute.

For new original source files, use a language-appropriate comment with:

```text
SPDX-License-Identifier: Apache-2.0
```

This marker does not replace required third-party notices or justify altering a file's existing license. Do not invent copyright ownership or claim all external components are Apache-licensed.

## Security reports

Do not post exploit details or credentials in public issues. Follow [SECURITY.md](SECURITY.md) for the current reporting process.
