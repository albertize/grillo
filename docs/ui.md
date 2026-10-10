# Local web console

The T23 MVP console uses embedded **React 19.3.0 / PatternFly React 6.6.1**
assets and the same Unix-socket API client as the CLI. The user-requested
framework change is recorded in [ADR 0008](adr/0008-patternfly-react-console.md).
Node/npm are needed to build it, not to run the console. See
[getting started](getting-started.md) for preparation. Run `grillo ui` and open
its printed URL. The daemon must
already be available. The default bind is `127.0.0.1:9090`; `--port 0` selects
and reports an available port. Closing the browser or interrupting `grillo ui`
does **not** stop workloads. See the [T23 evidence](experiments/t23-console.md).

## Reviewing a development checkout

`make build` produces sibling binaries under `bin/`, not the installed layout
under `libexec/grillo/`. With no healthy existing daemon, plain `bin/grillo ui`
therefore cannot auto-start `libexec/grillo/grillod`. Use explicit development
helpers and the prepared runtime-only guest selection (run `make runtime-guest`
first if it is missing; required inputs must already be provisioned):

```sh
# Run from the repository root, with Bash.
IFS= read -r guest < experiments/artifacts/runtime-selected.txt
GRILLO_DAEMON_BINARY="$PWD/bin/grillod" \
GRILLO_NETNS_BINARY="$PWD/bin/grillo-netns" \
GRILLO_GUEST_KERNEL="$guest/bzImage" \
GRILLO_GUEST_INITRAMFS="$guest/initramfs.cpio.gz" \
GRILLO_GUEST_MANIFEST="$guest/manifest.json" \
GRILLO_HELM_BINARY=/usr/bin/helm \
  ./bin/grillo ui --port 0
```

The Helm path is this recorded development host's provisioned helper, not a
universal host path. Existing healthy daemons are reused, not replaced by these
overrides. An installed/staged prefix needs no checkout overrides. A running UI
bridge keeps its old embedded assets; deliberately close/restart that bridge to
review rebuilt UI. Do not share its bootstrap URL or stop workloads just to
refresh presentation.

## Navigation

A PatternFly masthead and responsive sidebar replace the original long page.
Choose an application in the toolbar. The petrol/teal/mint shell uses compact
navigation, an application context toolbar and theme-aware panels over a neutral canvas.
Navigation follows the supported application-facing patterns in the user's
OpenShift 4.22 reference; see [ADR 0010](adr/0010-pod-oriented-console.md).
The sidebar groups Home (Overview, Topology, Events), Workloads (Pods, Workload
controllers, ConfigMaps, Secrets), Networking (Services, Routes), Storage (Volumes)
and Observe (Metrics, Logs, Exec, Diagnostics). Application selection is not a
simulated OpenShift project/namespace selector. Unsupported cluster management,
operators, RBAC and resource mutations are not presented as working features.
Overview has application inventory, Pod readiness, Pod environment memory samples,
published routes and recent global daemon activity. Pod/container metrics have a
dedicated page. Missing samples are unavailable, not 0%; no host capacity or CPU
percentages are invented. Errors, loading and empty states are explicit.
Third-party notices remain linked in the sidebar.

## Branding and color theme

The masthead uses the supplied leaf-G icon from `media/g-icon/`, including a
high-density variant. The embedded build also includes the SVG/ICO favicon,
Apple touch icon and 192/512px browser/home-screen icons in a local webmanifest.
No external image requests, service worker or offline/PWA guarantee are added.
Only the explicitly selected icons are embedded; the media directory is not served.

Choose **System / Light / Dark** in the masthead, also on mobile. System is the
default and follows `prefers-color-scheme` live. Explicit modes override browser
changes. The non-secret `grillo.theme` preference persists in local storage and
updates other same-origin tabs; if storage is blocked, the current tab can still
change theme without persistence. Different bridge origins have separate preferences.
PatternFly's root dark theme covers controls; custom cards, topology/SVG and text
have matching dark colors. The small local theme entrypoint applies preferences
before the main React bundle; no inline script/style or CSP relaxation is used.
See [browser evidence](experiments/t23-console-branding-theme.md).

## Views and controls

- Application overview: desired replicas versus observed ready Pods, source
  kind/path and snapshot time.
- Topology: a selectable **Services-only overview**, without connections,
  route nodes, workload nodes or VM nodes. Instance/Pod information and published
  routes appear in the selected Service inspector. Instances are associated using
  declared selectors, not measured traffic. Filtering matches Service names before
  the 100-Service display cap; zoom/fit and keyboard selection remain available.
  Closing the inspector returns focus. On mobile it stacks below the canvas.
  Inspector Logs and Terminal open the exact Pod's corresponding detail tab.
- Workloads → Pods: name/status filters and deterministic name/status/workload
  sorting. Open a Pod by name, Logs or Terminal; the detail page replaces the list
  and has a return-to-Pods breadcrumb. Tabs are Details, Logs, Terminal, Events
  and Metrics. Missing selected Pods are explicit, never replaced by another
  replica. Details show actual containers, images, mounts and declared/observed
  probes. Backend/isolation/allocation are in collapsed Runtime details (advanced).
  Node, restart count, creation time, Kubernetes owner references and manifest YAML
  are unavailable; no synthetic fields or private configuration are substituted.
  Container actions select its logs, interactive Terminal or exact non-TTY Exec target.
- Pod logs lock the resource filter to the Pod, with container/stream selection,
  optional follow and retention-gap resync. Pod Events filter exact Pod and
  Pod/container IDs from the retained daemon stream; global/controller events are
  not assigned to the Pod. Empty retained history does not prove health.
- Terminal selects an exact running container and connects a real guest `/bin/sh`
  with stdin/PTY and resize. Connect is explicit; Ctrl+C interrupts foreground work,
  Ctrl+D sends EOF, paste is limited to 8 KiB. Disconnect or leaving the tab cancels
  the shell, not the Pod. xterm.js provides direct keyboard/IME input, ANSI colors,
  cursor editing, selection and alternate-screen applications, with 1000 scrollback
  lines and measured rows/columns. The guest shell uses `TERM=xterm-256color`.
  Shell-less images fail explicitly; no shell is installed. See
  [real vi/PTY evidence](experiments/t23-xterm-terminal.md).
- Metrics: actual container cgroup memory and cumulative CPU samples first.
  Guest environment usage, allocated vCPU/memory budget and timestamped VMM RSS
  are separately labelled in collapsed Runtime accounting (advanced). They are
  not container usage and are not summed. History charts are unavailable.
- Volumes and routes: storage class/access/retention-relevant metadata, explicit
  bind warnings, actual loopback HTTP fallback links. Host bind source paths
  are not exposed by this projection.
- Configuration/Secrets: names and config entry counts only. No config or env
  values, rendered Helm values, secret versions or reveal control.
- Diagnostics: safe runtime availability diagnostics and source provenance.
  Original field-level compiler diagnostics/line maps are not retained in runtime
  state; the console explicitly directs users to `grillo plan <source>` rather
  than inventing them.
- Logs: retained daemon spool records, exact resource/container filters,
  stdout/stderr filtering, explicit polling, cursor-based pagination. Empty
  results do not prove silence; check diagnostics for collection failures.
  Guest retention gaps bypass container/stream filters so loss stays visible.
  Visible text retains at most 256 KiB.
- Events: global daemon stream with resource-prefix filtering, automatic
  EventSource reconnect, forwarded `Last-Event-ID`, duplicate suppression and
  snapshot/log resync on `events.gap`. Stored events are bounded to 200 records.
- Non-TTY exec: explicit JSON argument array, separate stdout/stderr and exit
  code, cancellation of the HTTP request, 30-second deadline and 1 MiB total
  output limit (truncation is reported). Exec owns a dedicated authenticated
  guest connection; cancellation terminates the runc-reported exec process
  using a parent-checked pidfd without interrupting the shared probe/status
  channel. Arguments are never evaluated by a host shell. Output is captured on
  completion. CLI `grillo shell <application-ID> <container>` invokes guest
  `/bin/sh` with real stdin/TTY/resize when local stdin is a terminal. Browser
  exec remains non-TTY; minimal images may lack a shell.

The console can faithfully show intentionally printed secret values in workload
logs/exec output; it cannot promise to hide an application's deliberate output.

## Authentication and boundaries

The printed fragment contains a bootstrap credential: do not share it. It
expires five minutes after bridge creation and is exchanged at most once,
including concurrent requests. The browser removes it before rendering and
receives an HttpOnly SameSite=Strict cookie. Restart `grillo ui` for a new token
if it expires or a new browser session is needed. Sessions last for the bridge
lifetime; explicit logout/session expiration is not implemented.

Public binds are rejected before opening the socket. Host must be loopback;
supplied mutation Origins must exactly match the HTTP Host and port. Exec also
requires Origin and JSON content type. Terminal creation/input/close requires the
same cookie, exact Origin and JSON content type; session IDs never replace auth. Local non-browser bootstrap clients may
omit Origin but still need the token. CSP forbids external scripts and framing,
CORS is closed, responses disable caching, and all workload strings are rendered
using escaped JSX text or xterm's terminal parser, never `dangerouslySetInnerHTML`.
The terminal's dynamic renderer gets a fresh style-only CSP nonce through a
scoped document adapter; scripts receive no nonce, and arbitrary inline
styles remain forbidden. OSC title/link/clipboard and window effects are disabled.
See [ADR 0013](adr/0013-xterm-browser-terminal.md). Scripts, CSS, fonts and
notices are served only from the immutable embedded build, with no CDN. React
and PatternFly run under the same CSP without unsafe-inline/unsafe-eval. No
secret-reveal, source-file-read or arbitrary host-file API is exposed. SSE is bounded to eight concurrent streams (five-minute reconnect
window); captured exec and Terminal share four concurrent slots. Bootstrap/exec/Terminal
bodies are capped at 4 KiB/32 KiB/32 KiB. Each Terminal has a 16-frame input queue,
8 KiB chunks, five-second write deadlines and a 30-minute / 16 MiB output limit;
interrupted output is not a successful exit. See the
[browser-terminal contract](../api/local-api.md#browser-terminal-adapter-loopback-bridge-only).
Closing the bridge cancels its requests/shells, not daemon lifetime.

## Reproducible browser smoke

Test prerequisites: installed Google Chrome (CDP) or Firefox (WebDriver BiDi),
and Node.js with built-in WebSocket. The current run used Chrome 155.0.8059.39 /
Node 24.18.0; Firefox was unavailable (older Firefox evidence remains historical).
After the explicit frontend `ui-deps` step, tests fetch/install no packages or tools.
The harness uses Node built-ins, not an npm browser package/driver or CDN.
Each browser is a separate process with a unique temporary profile and loopback
remote-control port; an existing Chrome/Firefox instance is not reused or stopped.

```sh
make ui-deps         # explicit build dependency fetch; no install scripts
make check           # frontend build/verification/tests + Go checks
make test-ui-browser  # isolated Chrome/Firefox with deterministic UI fixtures
make test-ui          # rebuilt guest + actual CLI bridge/daemon, real KVM chart
```

Missing browser/hardware prerequisites yield a documented SKIP, never a passed
gate. `test-ui-browser` proves rendering/XSS/SSE behavior; only `test-ui` proves
actual runtime exec, metrics and workload lifetime. Test-owned profiles, state,
volumes, processes and sockets are cleaned up; existing browser profiles and
workloads are not used. Concurrent isolated test daemons must choose
nonconflicting host-wide guest CID ranges (`grillod -vsock-cid-base …
-build-vsock-cid-base …`); the gate sets separate explicit ranges rather than
stopping existing workloads. This is not a global allocator.

See [original acceptance](experiments/t23-console.md) and
[PatternFly revalidation](experiments/t23-console.md#patternfly-migration-gate) and
[visual/topology iteration](experiments/t23-console-redesign.md) for historical checks.
Current [Chrome/PTY/resize/signal/lifetime evidence](experiments/t24-browser-terminal-compose-dependencies.md)
is separate from the fixture-only browser test.

Manual smoke steps:

1. Start a supported workload, run `grillo ui --port 0`, open the URL once.
2. Select an application and compare replica counts, VM boundaries and routes
   with CLI inspection. Open Observe → Metrics; unavailable samples are labelled,
   not zeros. Workloads → Pods opens resource tabs; runtime details stay advanced.
3. Confirm config/Secret panes contain metadata only. Use text containing HTML
   in retained log/exec output and verify it remains text, not DOM markup.
4. Filter logs by resource/container/stream; toggle polling. Disconnect/reconnect
   the event connection and confirm cursor resume; after a retention gap, confirm
   snapshot refresh. The deterministic browser gate exercises the latter.
5. Select a running container, run a JSON argument array, check stdout/stderr and
   exit code, and cancel a long request. Open a Pod Terminal, connect, type a command,
   interrupt foreground work with Ctrl+C, resize, then disconnect. The automated KVM
   gate verifies a real PTY with `test -t`/`stty` and checks shell/child cleanup.
6. Close the browser and interrupt the bridge. Verify workload status and exec
   still succeed, with unchanged VMM PIDs.

## Explicit MVP limits

Interactive browser stdin/TTY/resize uses a bounded xterm-compatible emulator.
No replay/reconnect, complete OpenShift parity or exhaustive terminal/Unicode/IME
and accessibility coverage is claimed. CLI stdin/TTY/resize remains available.
Actual guest
`/proc` memory/CPU ticks and container cgroup memory/cumulative CPU microseconds
are displayed with collection times and sources, separately from VMM RSS and
guest allocation. Missing values are unavailable, never zeroed; guest reports
are not host attestation. Guest kernel version, PSS, cache usage and VMM CPU
percentage remain unavailable rather than invented. Guest stdout/stderr collection is implemented by the
agent/executor/daemon, not the UI; the console consumes that bounded spool.
Event production and compiler-diagnostic persistence still depend on the daemon;
the UI reads existing API surfaces and labels missing data. TLS, dev CA management,
public exposure and secret reveal are not part of this console. Same-user host
processes are outside its authentication boundary.
