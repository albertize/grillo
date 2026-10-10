# Grillo console frontend

React 19.3.0, PatternFly React 6.6.1, esbuild 0.28.2. See
[ADR 0008](../docs/adr/0008-patternfly-react-console.md) for the user-requested
replacement of the framework-free T23 console.

## Build and check

Use Node **24.18.0** / npm **11.16.0** (Node 24.x required). Dependency fetch is
separate and explicit; installed packages are verified by the committed npm
integrity lock, and lifecycle/post-install scripts are disabled.

```sh
make ui-deps          # explicit npm ci, no install scripts or tool auto-installer
make ui-check         # build, node:test, deterministic output verification
make check            # also frontend checks, then normal Go checks/builds
make test-ui-browser  # isolated Chrome/Firefox rendering/security/SSE/Terminal fixtures
make test-ui          # actual CLI bridge + daemon + real KVM workload gate
```

The build writes only `internal/ui/assets/generated/`, which is ignored and
embedded in Go binaries after building. Generated assets/node_modules are not
committed. There is no dev server, HMR, runtime Node requirement or CDN; the
existing Go bridge serves the embedded files. All build/test entrypoints must
prepare assets before Go compilation. UI tests reject missing/stale fingerprints;
`npm run check` recomputes and compares all expected output files.

`src/presentation.mjs` contains tested guest-memory/readiness presentation and a
bounded Services-only topology model, with no connections. `src/topology.jsx`
renders selectable Services, keyboard interactions, filter/zoom and an instance
information inspector. Workloads → Pods has tested name/status filtering and
sorting, a list/detail workflow and Details/Logs/Terminal/Events/Metrics tabs.
Logs and events are scoped to the exact Pod. `terminal.jsx` connects the selected
container's real stdin/PTY/resize using the authenticated bridge and
`@xterm/xterm` 5.5.0 / `@xterm/addon-fit` 0.10.0 (MIT, exact npm integrity lock).
`terminal.mjs` scopes style-only CSP nonce authorization, bounds UTF-8 input and
waits for parser writes with cancellation. Input is integrated into xterm, not a
separate form. ANSI colors/cursor editing and alternate screens are supported;
OSC title/link/clipboard and window effects are disabled. See
[ADR 0013](../docs/adr/0013-xterm-browser-terminal.md) and
[real vi/PTY evidence](../docs/experiments/t23-xterm-terminal.md).
`src/client.mjs` contains pure bounded filtering, cursor and validation helpers,
covered by Node's built-in tests. `src/app.jsx` owns presentation and browser
request/session lifecycle, not planning/reconciliation. All workload data is
rendered through escaped JSX text or xterm's parser. No `dangerouslySetInnerHTML` or eval. Actual
API limits and private/public data contracts remain in Go.

## Design

The first user-requested visual revision follows the supplied console screenshots
for structure, not upstream branding: petrol masthead/sidebar, teal actions, mint
accents, compact white panels, three-column overview and topology + resource
inspector. See [design and gate evidence](../docs/experiments/t23-console-redesign.md).
The later user-requested OpenShift-like resource navigation and Pod-oriented
presentation are recorded in [ADR 0010](../docs/adr/0010-pod-oriented-console.md).
VM details are secondary advanced information, not the primary navigation model.
Do not manufacture unsupported cluster APIs, YAML or resource actions.

Use PatternFly primitives for page/navigation, cards, tables, forms, labels,
alerts, tabs and empty/loading states. Use meaningful resource state, never
invent usage or readiness. Keep missing guest/container metrics and original
compiler-diagnostic/log-ingestion limits explicit. Exec stays non-TTY and selects
`Pod-ID/container`; changing views cancels the current browser request/shell.
Pod Terminal uses a dedicated real TTY rather than captured Exec. Browser tests
use only Node built-ins and a private test-owned Chrome/Firefox profile; no existing
browser instance/profile is reused. See [Chrome/KVM evidence](../docs/experiments/t24-browser-terminal-compose-dependencies.md).

## Branding and themes

`build.mjs` copies an explicit icon whitelist from `../media/g-icon/` into the
embedded generated subtree: 32/64px masthead images, SVG/ICO favicon, Apple touch
icon and 192/512px manifest icons. Their bytes and the HTML entrypoint participate
in the source fingerprint checked by Go tests. Other media files are not served.

The small `theme-init.mjs` entrypoint and `ThemePicker` use `theme.mjs` to apply
PatternFly's `pf-v6-theme-dark` root class. `theme.css` covers custom surfaces and
SVG; `style.css` keeps the existing light palette. System follows the browser's
live color scheme; explicit modes persist under the non-secret `grillo.theme`
local-storage key, with in-memory fallback for denied storage. No inline code,
external resources or new dependencies are required. A webmanifest supplies icon
metadata, not offline support. See [tests and limitations](../docs/experiments/t23-console-branding-theme.md).

## Notices

MIT package notices (including nested bundled notices) plus font/icon attribution
are embedded at `/assets/generated/licenses.txt` and linked in the sidebar.
The upstream font licenses in `licenses/` are copied verbatim; no copyright
owner names or legal terms are invented. Their provenance is recorded in the
ADR. The lock is the package/version inventory; installed dependency inspection
uses `npm ls --all`. Native esbuild dependencies for other architectures may be
listed as optional/uninstalled, not failed runtime prerequisites.

Use `npm audit` explicitly for a current registry-advisory check. A passing audit
does not establish publisher authenticity or replace T24 security review.
