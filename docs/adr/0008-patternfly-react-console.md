# 0008 — PatternFly React console

- Status: Proposed (implementation explicitly requested by the user; formal maintainer review pending)
- Date: 2026-10-06
- Related tasks: T23, T24, T27
- Supersedes: the framework-free frontend choice in implementation-plan sections 2/11

## Context

The original T23 console passed functional/security/browser/KVM gates but its
single long page was not a satisfactory debugging interface. The user reviewed
it, proposed the library used by OpenShift, and explicitly authorized
**PatternFly React**. This is an approved implementation direction for this
session, not an inferred change to repository ownership or formal ADR acceptance.

The former framework-free delivery plan conflicts with this request. This ADR
records that deliberate change; runtime architecture, Go dependency policy,
private API, bootstrap auth and workload lifetime remain unchanged.

## Options considered

- Continue bespoke HTML/CSS: smallest build chain, but costly to maintain a
  consistent, accessible application console.
- PatternFly CSS with vanilla JS: consistent appearance but manual component,
  state, keyboard and lifecycle work.
- PatternFly React: reusable navigation, tables, cards, forms and state labels;
  adds a locked frontend dependency/build chain but no runtime Node dependency.

## Decision

Implement React **19.3.0**, React DOM **19.3.0**, PatternFly core/table/icons
**6.6.1**, and build-only esbuild **0.28.2**. Version/license/peer metadata was
checked against the npm registry; PatternFly admits React 17/18/19. Use a committed
npm integrity lock, exact direct versions and Node **24.18.0** / npm **11.16.0**
(the installed/tested tools). No router, frontend framework, CDN, chart package,
WebSocket library or Go SDK is introduced.

`web/src` owns the frontend. `make ui-deps` explicitly downloads locked packages
with lifecycle scripts disabled. Routine build/check does not fetch packages;
it fails with instructions if they are absent. `make ui-build` emits ignored
assets under `internal/ui/assets/generated`; Go embeds them in the CLI binary.
`make check` also tests pure client logic, builds/verifies assets, and rejects a
stale source fingerprint. CI explicitly provisions Node and invokes `ui-deps`.
The bridge serves only immutable embedded generated files with correct MIME
and the existing strict CSP, never host paths or source maps. There is no dev
server/HMR requirement and no Node/npm requirement for runtime users.

UI sections are Overview, Workloads, Topology, Networking, Storage,
Configuration, Logs, Events, Exec and Diagnostics. React text interpolation
replaces manual text-node creation without exposing raw HTML. There is no
`dangerouslySetInnerHTML`, inline script, eval, new secret-reveal feature or
runtime orchestration logic in the frontend. Events retain cursor/gap recovery;
log/session/output bounds and explicit unavailable-data labels remain.

## Licensing and supply chain

Original source remains Apache-2.0. React, PatternFly, esbuild and their installed
JS dependencies declare MIT licenses; upstream notices, including nested Popper
notices and tslib copyright, are aggregated into the embedded `licenses.txt`.
Red Hat Text/Display/Mono fonts retain SIL OFL 1.1 notices, copied unchanged from
Google Fonts commit `7085eb89a950e85db5b166b7a58d414544b4140c` and checked against
that pinned source. Font Awesome Free attribution/CC BY 4.0/SIL OFL/MIT notices
are preserved from upstream commit `840c215f894f429b26b8c1402a65da835dc5a450` for
icon shapes carried by PatternFly. No Red Hat/OpenShift branding is used to
imply endorsement. The UI links to the embedded notices.

The npm lock verifies package integrity, not publisher authenticity. `npm audit`
was run explicitly and initially reported zero known vulnerabilities; it is not
an independent security review. Node/esbuild and dependencies execute trusted
repository build logic, never workload/chart scripts. Dependency scripts are
not executed during installation. Native esbuild is supplied by its platform
package in the integrity lock, not a post-install download.

## Evidence and follow-up

Implementation and actual checks are tracked in [progress](../progress.md).
Real Firefox already renders PatternFly pages with local fonts, no CSP
violations, literal HTML output, navigation and working SSE/log filters/exec.
All T23 frontend/Go/browser/KVM gates were rerun successfully after migration,
including actual CLI/daemon/KVM exec, cancellation and unchanged VMM PIDs after
browser/bridge closure; see the [migration evidence](../experiments/t23-console.md#patternfly-migration-gate).
The existing review demo exposed a host-wide guest-CID collision in the first
isolated-daemon gate. Explicit validated workload/build CID-base flags (defaults
unchanged) let the test use separate ranges without stopping review workloads;
this is not a global allocator or silent collision fallback. Hosted
CI and other browsers remain unverified. T24 still owns product-wide hardening.

This choice increases embedded asset size and build prerequisites. Audit/update
frontend pins deliberately; document notices and lock changes together. Do not
weaken CSP for component convenience. Measure bundle/startup cost before adding
larger UI/chart/editor libraries; packaging must include the frontend build.
