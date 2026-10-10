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
make test-ui-browser  # real Firefox rendering/security/navigation/SSE fixtures
make test-ui          # actual CLI bridge + daemon + real KVM workload gate
```

The build writes only `internal/ui/assets/generated/`, which is ignored and
embedded in Go binaries after building. Generated assets/node_modules are not
committed. There is no dev server, HMR, runtime Node requirement or CDN; the
existing Go bridge serves the embedded files. All build/test entrypoints must
prepare assets before Go compilation. UI tests reject missing/stale fingerprints;
`npm run check` recomputes and compares all expected output files.

`src/presentation.mjs` contains tested guest-memory/readiness presentation and a
bounded, declaration-only topology model. `src/topology.jsx` renders selectable
resources, keyboard interactions, filter/zoom and a responsive detail inspector.
`src/client.mjs` contains pure bounded filtering, cursor and validation helpers,
covered by Node's built-in tests. `src/app.jsx` owns presentation and browser
request/session lifecycle, not planning/reconciliation. All workload data is
rendered through escaped JSX text. No `dangerouslySetInnerHTML` or eval. Actual
API limits and private/public data contracts remain in Go.

## Design

The first user-requested visual revision follows the supplied console screenshots
for structure, not upstream branding: petrol masthead/sidebar, teal actions, mint
accents, compact white panels, three-column overview and topology + resource
inspector. See [design and gate evidence](../docs/experiments/t23-console-redesign.md).

Use PatternFly primitives for page/navigation, cards, tables, forms, labels,
alerts, tabs and empty/loading states. Use meaningful resource state, never
invent usage or readiness. Keep missing guest/container metrics and original
compiler-diagnostic/log-ingestion limits explicit. Exec stays non-TTY and selects
`sandbox-ID/container`; changing views cancels the current browser request.

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
