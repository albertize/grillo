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

## Navigation

A PatternFly masthead and responsive sidebar replace the original long page.
Choose an application in the toolbar. Overview shows status cards, routes,
activity and per-VM resource accounting; dedicated pages contain Workloads,
Topology, Networking, Storage, Configuration, Logs, Events, Exec and Diagnostics.
Workload rows open sandbox detail; its Exec action selects the exact replica.
Configuration uses ConfigMap/Secret tabs. Errors, loading and empty states are
explicit and styled consistently. Third-party notices are linked in the sidebar.

## Views and controls

- Application overview: desired replicas versus observed ready microVMs, source
  kind/path and snapshot time.
- SVG topology: actual sandbox boundaries, Services and routes. Selector edges
  are explicitly inferred from declarations, not measured traffic.
- Workloads and sandbox detail: containers/init containers, image references,
  mounts, declared probes, actual container state and observed probe results,
  private IP and QEMU backend. Exec selection uses `sandbox-ID/container` so
  replicas cannot be silently confused.
- Resource usage: allocated vCPU/guest memory budget and timestamped VMM RSS and
  cumulative CPU time sampled from `/proc`. No misleading aggregate sum.
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
requires Origin and JSON content type. Local non-browser bootstrap clients may
omit Origin but still need the token. CSP forbids external scripts and framing,
CORS is closed, responses disable caching, and all workload strings are rendered
using escaped JSX text, never `dangerouslySetInnerHTML`. Scripts, CSS, fonts and
notices are served only from the immutable embedded build, with no CDN. React
and PatternFly run under the same CSP without unsafe-inline/unsafe-eval. No
secret-reveal, source-file-read or arbitrary host-file API is exposed. SSE is bounded to eight concurrent streams (five-minute reconnect
window); exec to four concurrent requests. Bootstrap/exec bodies are capped at
4 KiB/32 KiB. Closing the bridge cancels its requests, not daemon lifetime.

## Reproducible browser smoke

Test prerequisites: installed Firefox with WebDriver BiDi and Node.js with
built-in WebSocket (tested Firefox 157.0 and Node 24.18.0). After the explicit
frontend `ui-deps` step, tests fetch/install no packages or tools. The harness
uses Node built-ins, not an npm browser package/driver, and no CDN.
Firefox uses a unique temporary profile and loopback-only remote-control port.

```sh
make ui-deps         # explicit build dependency fetch; no install scripts
make check           # frontend build/verification/tests + Go checks
make test-ui-browser  # real Firefox with deterministic UI data fixtures
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
[PatternFly revalidation](experiments/t23-console.md#patternfly-migration-gate) for actual checks.

Manual smoke steps:

1. Start a supported workload, run `grillo ui --port 0`, open the URL once.
2. Select an application and compare replica counts, VM boundaries and routes
   with CLI inspection. Check unavailable metrics are labelled, not zeros.
3. Confirm config/Secret panes contain metadata only. Use text containing HTML
   in retained log/exec output and verify it remains text, not DOM markup.
4. Filter logs by resource/container/stream; toggle polling. Disconnect/reconnect
   the event connection and confirm cursor resume; after a retention gap, confirm
   snapshot refresh. The deterministic browser gate exercises the latter.
5. Select a running container, run a JSON argument array, check stdout/stderr and
   exit code, and cancel a long request. Check CLI shell guidance and TTY limits.
6. Close the browser and interrupt the bridge. Verify workload status and exec
   still succeed, with unchanged VMM PIDs.

## Explicit MVP limits

Full browser TTY is deferred; CLI stdin/TTY/resize is implemented. Actual guest
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
