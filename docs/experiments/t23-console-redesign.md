# T23 — first console visual and topology revision

Status: **local implementation/browser/runtime gates PASS** for this bounded
iteration. User visual review remains next; T24/D0 remain BLOCKED overall.

## Direction and dependencies

The user supplied three console references: compact dark masthead/sidebar,
white dashboard panels on a neutral background, selectable topology and a
right-hand resource inspector. Use those as structural/interactivity references,
not copied code, image assets or Red Hat/OpenShift branding. Grillo keeps petrol
`#102b35`, teal `#087f78` and mint `#7de0bf` identity. Existing status colors retain
meaning. No additional npm/Go/framework/chart dependency or ADR is introduced;
[ADR 0008](../adr/0008-patternfly-react-console.md) still defines the build chain.

Verified dependencies: T15 public snapshot/session/exec APIs and T22 chart gate;
T23 React/PatternFly, local fonts, existing browser/security/lifetime harness.
Pure presentation and diagram computation remain separate from runtime effects.

## Delivered

- Inline compact masthead, petrol sidebar, active mint accent, breadcrumb/context
  toolbar and compact, squared panels/actions, including mobile shell.
- Three-column overview at wide desktop sizes, two/one-column reflow: source and
  resource inventory, desired/observed readiness, guest memory, published routes,
  global daemon activity, diagnostics and runtime boundary.
- Guest memory rings use **one VM's** reported `total - available` / total, with
  time/source. Missing, inconsistent, non-finite or invalid samples show unavailable;
  never synthetic zero/CPU/host disk/network capacity. At most four overview rings;
  detailed samples remain in resource accounting, separate from allocation/RSS.
- Topology shows Routes → Services → **workload groups**, not traffic edges or a
  false VM boundary around an entire workload. Inspector identifies individual
  observed microVMs and their exact running-container Exec targets.
- Keyboard/click resource selection, Resources/Details tabs, close/focus return,
  bounded name filter, zoom-out/in presets and fit reset. Mobile stacks the
  inspector below an internally scrollable diagram without document overflow.
- Nodes cap at 100 per kind; visible cap notice. Edges use declared backend/selector
  relationships only, omit dangling targets, deduplicate workload selectors and
  disappear when either endpoint is filtered. Selection keys survive list reorder;
  application change remounts diagram state. The diagram does not provision/control
  workloads or invent unavailable target-port/traffic data.
- Inspector's **Daemon logs** opens global logs; it is not mislabelled as a scoped
  stream. Current API only exposes declared Service TCP port numbers, not complete
  port → targetPort mappings; those are not fabricated to match the reference.

Files: `web/src/{app.jsx,style.css,presentation.mjs,presentation.test.mjs,topology.jsx}`,
`web/package.json` test discovery, `scripts/ui-browser-smoke.mjs`, frontend/UI guides,
progress/history. No API, authentication, guest, frontend compiler or VMM changes.

## Executed checks and preserved correction

```sh
make ui-check
make check
GRILLO_UI_SCREENSHOT=/tmp/grillo-console-redesign.png make test-ui-browser
make test-ui
```

- All PASS; 10 Node unit tests (five existing client tests, five new presentation
  tests). Deterministic asset/fingerprint verification, Go vet/unit/race/build/audit
  included in full checks; dependencies were already provisioned, no npm install.
- Initial Firefox test **FAIL**: new BiDi Enter action sent a literal escaped
  string rather than a single private-use key code point. Fixed harness encoding,
  reran successfully; no CSP relaxation or production auth change.
- Screenshots initially revealed default-blue PatternFly action/tab tokens and
  stacked mobile masthead. Corrected semantic tokens/inline masthead and reran.
- Final fixture Firefox gate **PASS without SKIP, 9.07s**: desktop/mobile navigation,
  native keyboard selection, both inspector tabs, filter/zoom, close/focus return,
  mobile inspector/no document overflow, palette assertions, local fonts, no CSP
  or runtime errors, metadata-only values, literal HTML logs/exec, SSE gap/cursor.
- Final CLI/daemon/native KVM gate **PASS without SKIP, 39.11s**: rebuilt tested
  assets/binaries/guest, chart plan/up/inspect/down, real logs/filters/metrics,
  browser topology interactions, exec/cancellation, unchanged VMM lifetime after
  UI/browser closure and owned cleanup. Earlier run also passed (38.29s).
- Same recorded local Fedora fc44/Linux 7.2.9/UID 1000/QEMU 10.2.2, SELinux
  already Permissive and unchanged. Real KVM result is independent of fixture
  rendering; neither establishes default-policy/clean-host support.

Local screenshots (ignored/untracked, synthetic fixture data, no bootstrap token):
`/tmp/grillo-console-redesign.png`, `-topology.png`, `-mobile.png`,
`-topology-mobile.png`. They were inspected, not committed. Screenshots alone are
not accessibility/security evidence; browser assertions are separately recorded.
No remaining owned QEMU/daemon/netns/pasta processes were listed after the gates.

## Limits and next review

No full browser TTY, layout editing/drag/free pan/fullscreen, traffic telemetry,
host capacity, original compiler line maps, retained usage history or added mutation
API. Other resource pages retain existing functionality with the shared new shell/
panel theme; they are not all redesigned into new master/detail workflows yet.
No new browser-family/hosted CI/external-host UI gate, complete accessibility audit
or cold-render/performance campaign. Test durations are suite times, not paint time.
Experimental transfer payloads previously produced are **not rebuilt** by this
iteration; use the freshly built CLI with the explicit checkout helper/guest
[review overrides](../ui.md#reviewing-a-development-checkout) to review. Existing UI
bridge still serves old embedded assets until deliberately closed/restarted;
workload lifetime is independent. Do not publish bootstrap URLs/screenshots with
credentials. No commit/push or host-policy change performed in this iteration.

Follow-up correction: the user tried the originally suggested plain
`bin/grillo ui --port 0` and received missing checkout `libexec/grillo/grillod`.
That suggestion omitted development overrides; `make build` produces `bin/grillod`
but default discovery intentionally requires an installed prefix. Corrected the
review guide to select explicit daemon/netns/runtime-only guest/Helm paths. No
silent sibling-binary fallback, host state cleanup or automatic daemon replacement.
This is a documentation correction; original browser/KVM PASS does not prove the
previously incomplete manual invocation. No user-context UI/daemon started to
validate it. Executed corrected overrides with `bin/grillo doctor --verbose`:
helper paths, guest integrity/ABI, KVM API, transient namespaces, QEMU and Helm
probes PASS; expected first-run runtime-directory and experimental-support WARNs.
`go test ./internal/runtimeassets ./internal/cli` PASS (cached); `git diff --check`
PASS. These read-only/contract checks are not a manual UI startup gate; full browser/
KVM gates were not rerun for the documentation-only correction.

Next: user review of dashboard/topology proportions and density, then workload
master/detail, scoped logs navigation and more resource-table interaction. Preserve
unknown-data, secret and lifecycle boundaries while adding presentation features.
