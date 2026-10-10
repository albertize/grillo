# 0010 — Pod-oriented application console

- Status: Proposed (implementation explicitly requested by the user; formal review pending)
- Related tasks: T23, T24
- Supersedes: none; refines the presentation described in ADR 0008

## Context

The user supplied an OpenShift 4.22 functional UI analysis and requested a close
application-facing experience. Grillo's one-Pod/one-microVM implementation should
not make ordinary navigation a VM management interface. The previous user request
also specified a Services-only topology without connections. The OpenShift analysis
is a design reference, not evidence that Grillo implements OpenShift APIs, RBAC,
operators, controllers or cluster administration.

Specification §20 and plan §11.3 originally emphasize visible sandbox boundaries
and broader topology relationships. This explicit presentation request moves
runtime details to secondary advanced sections and keeps topology Services-only;
it does not change the runtime isolation invariant or accounting semantics.

## Options considered

- Keep a VM-oriented resource dashboard: exposes implementation details but makes
  Pod debugging unnecessarily different from the reference workflow.
- Copy every OpenShift menu and action: misleading without the corresponding APIs.
- Adopt supported resource navigation and Pod detail patterns: familiar workflow
  while retaining explicit limitations and unchanged runtime contracts.

## Decision

Use the third approach. Implement the bounded first slice:

| Area | Grillo pages / behavior |
| --- | --- |
| Home | Overview, Services-only Topology, global daemon Events |
| Workloads | Pods, actual declared workload controllers, ConfigMaps, Secrets |
| Networking | Services, Grillo-published Routes (not OpenShift Route objects) |
| Storage | Volumes; do not manufacture PV/StorageClass resources |
| Observe | Actual Metrics snapshots, Logs, non-TTY Exec, Diagnostics |
| Pod list | Name/status filters, deterministic sorting, exact Pod selection |
| Pod detail | Details, Logs, Terminal, Events, Metrics |
| Runtime details | Collapsed advanced isolation/backend/allocation/accounting |

Application selection remains application selection, not a simulated namespace or
OpenShift project. Original manifest YAML, Kubernetes owner references, nodes,
restart counts and creation timestamps are not supplied by the current public
view. Do not synthesize them or expose private IR/configuration as YAML. No resource
creation, edit, scale, rollout or delete actions are introduced in this slice.

Logs stay locked to the exact Pod resource, optionally its selected container.
Events use exact Pod or Pod/container IDs from the existing retained global stream;
controller/application events are not attributed to the Pod. Empty history is not
proof of health. Missing selected Pods show an explicit unavailable state instead
of silently switching to another replica.

Metrics show container cgroup samples first. Guest environment usage, allocation
and VMM RSS remain separately labelled in advanced accounting and are never summed
or relabelled as container usage. No invented CPU percentages or history charts.

The initial slice kept Terminal unavailable in the browser. This limitation is
superseded by [ADR 0011](0011-browser-terminal-adapter.md) and its Chrome/KVM
interactive-terminal evidence; the paragraph below preserves the original scope.
That slice offered shell-quoted
CLI guidance for an exact running container, not a fake TTY backed by captured
Exec. Real browser TTY requires a separate authenticated, bounded interactive
transport and terminal implementation with cancellation/resize/lifecycle evidence.

## Security and dependencies

No dependencies, host execution, runtime orchestration, secret reveal, private
file access or authentication changes. Existing escaped text rendering, CSP,
output bounds, stream retention and workload lifetime remain intact. No OpenShift
branding or endorsement is implied. The supplied analysis is not copied into the
repository; this record describes original Grillo implementation decisions.

## Evidence and follow-up

See [current progress](../progress.md) for actual checks. Pure presentation tests
cover Pod filters/sorting, exact event scope, empty/invalid selections and command
quoting; the initial browser smoke exercised navigation and Pod tabs when Firefox existed.
The later adapter also supports isolated Chrome verification.
Missing Firefox is a blocker, not verified rendering evidence. Complete browser
verification and real interactive Terminal before claiming the requested workflow
fully delivered. Broader OpenShift-like capabilities remain bounded future tasks,
not implied support from the presence of a UI pattern.
