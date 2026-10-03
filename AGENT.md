# Coding Agent Instructions

These instructions apply to all work in this repository. They are intended for coding agents and humans supervising automated contributions.

## 1. Current state

Grillo is in the design stage. There is no implemented runtime, Go module, build system, or release. Do not invent successful builds, existing APIs, compatibility support, or benchmark results.

Start with:

1. [README.md](README.md) for the project and its philosophy.
2. [Project specification](grillo-project-specification.md) for product requirements.
3. [Implementation plan](IMPLEMENTATION_PLAN.md) for contracts, task dependencies, and acceptance gates.
4. [Progress](docs/progress.md) for actual completion status.
5. [Contributing](CONTRIBUTING.md) and [Security policy](SECURITY.md).

## 2. Authority and scope

Follow explicit instructions from the user and your execution environment. Within repository design documents, the specification defines product goals, the implementation plan defines the delivery approach, and accepted ADRs explain approved changes. If they conflict, surface the conflict instead of silently choosing a convenient interpretation.

Work on one bounded task at a time. Verify all dependencies before claiming a task complete. Missing KVM, networking primitives, guest artifacts, or test infrastructure are blockers—not permission to replace the requested behavior with a mock.

Never edit unrelated user changes, perform destructive cleanup without permission, or fabricate repository ownership, release channels, contact addresses, or project URLs.

## 3. Architectural invariants

- One Kubernetes Pod maps to one hardware-isolated microVM by default.
- Containers in a Pod share the intended guest network context and localhost.
- Compose and Kubernetes frontends compile into one versioned IR.
- Frontends never control VMMs directly.
- No Kubernetes control plane, privileged always-on daemon, or silent host-container fallback.
- Normal host operations run as the user's account. Never add automatic `sudo` escalation.
- CLI and UI use shared runtime logic through the local API.
- Closing the CLI or UI does not own or terminate workload lifetime.
- Unknown or degraded semantics require structured diagnostics; never fake compatibility silently.
- Hardware integration results require real hardware evidence.

## 4. Implementation conventions

Use Go for runtime components, favoring the standard library. Official `golang.org/x/*` modules are allowed where the plan identifies a concrete need. The planned YAML parser is the only initially approved third-party Go dependency. Verify its maintained path/version during T00.

Do not introduce framework dependencies, SDKs, CGO, or custom cryptography without an explicit need and ADR. External VMM, OCI, Helm, and build tools must be pinned, checked, and documented separately from Go dependencies.

Keep interfaces small and defined near their consumers. Use explicit typed data, contexts and deadlines for blocking operations, bounded concurrency, idempotent lifecycle operations, and wrapped errors with actionable context. Keep pure validation/planning separate from effects. Do not create speculative packages or abstraction layers for hypothetical future backends.

Write documentation, comments, help text, diagnostic messages, and commit messages in English. Preserve proper names, upstream excerpts, and required legal notices accurately.

## 5. Security requirements

Treat source manifests, charts, images, archives, guest messages, and workload output as untrusted.

- Never pass untrusted input to a host shell.
- Never expose secret values in public IR, plans, inspection, UI, events, or ordinary diagnostics.
- Do not promise to hide secrets that an application itself deliberately writes to its logs.
- Bound parser input, frames, output buffers, sessions, and worker counts.
- Prevent filesystem traversal, unsafe symlink following, and accidental deletion of bind paths.
- Verify downloaded content and artifacts before use.
- Keep management sockets private and UI loopback-only by default.
- Do not install a CA, alter host configuration, fetch/execute tools, or delete persistent volumes without explicit authorization.
- Do not claim absolute security or protections that the configured VMM/helper does not actually provide.

Report discovered vulnerabilities according to [SECURITY.md](SECURITY.md). Do not add live credentials or confidential exploit details to ordinary progress logs.

## 6. Session workflow

1. Inspect `git status` and relevant files.
2. Select a task from the implementation plan and record its ID and verified dependencies.
3. Identify contracts and security boundaries affected by the change.
4. Add positive, negative, and failure-path tests appropriate to the task.
5. Implement the smallest coherent change that satisfies the acceptance criteria.
6. Run available verification; explain every skipped check and missing prerequisite.
7. Update progress, compatibility status, and ADRs as necessary.
8. Review the diff for unrelated changes, secrets, generated artifacts, and misleading claims.
9. Commit only if requested or authorized by the workflow; use a focused English message.
10. Summarize changed files, actual checks, limitations, blockers, and the next task.

Do not mark `DONE` until all required acceptance evidence exists. Use `BLOCKED` if a required gate cannot be verified. A passing fake-backend test proves the fake contract, not KVM, rootless networking, or filesystem sharing.

## 7. Verification

At the current documentation-only stage, check links, Markdown structure, task consistency, and diffs. Do not run `go test ./...` as though a module already exists.

Once T00 establishes the Go module and tooling, applicable routine checks include:

```sh
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
```

The formatting check must produce no changed-file list. Use the exact build/integration commands introduced by the scaffold rather than inventing them. KVM tests must be clearly separated from ordinary unit tests. Missing `/dev/kvm` means a documented skip/blocker, not a passed hardware gate.

Never report a command as executed unless it was executed, and never omit a failure just because another check passed.

## 8. Progress entry

Record updates in [docs/progress.md](docs/progress.md):

```text
Task: Txx — title
Status: TODO | IN_PROGRESS | BLOCKED | DONE
Dependencies verified:
Files and contracts changed:
Decisions/ADRs:
Tests run (command, environment, result):
Tests NOT run and why:
Integration/benchmark evidence:
Known limitations:
Next task:
```

## 9. Licensing

Original contributions are made under Apache-2.0; follow [CONTRIBUTING.md](CONTRIBUTING.md). Preserve third-party attribution and license terms. Never copy code with incompatible or unknown provenance. Do not add unverified copyright-holder names or strip existing notices.
