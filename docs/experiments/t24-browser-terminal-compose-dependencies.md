# Browser Pod Terminal and Compose startup dependency gates

## Scope and environment

User-requested continuation of T23/T24: use a second Chrome instance, implement
actual browser Terminal and reduce Compose `depends_on` differences. This is a
bounded runtime/UI change, not full OpenShift/Compose parity or T24/D0 closure.
See [Terminal decision](../adr/0011-browser-terminal-adapter.md),
[dependency decision](../adr/0012-compose-startup-dependency-gates.md),
[UI guide](../ui.md) and [compatibility](../compatibility.md).

Actual local environment: Linux/amd64, kernel `7.2.9-200.fc44.x86_64`, user UID
1000, Go `1.27.2`, Node `24.18.0`, Google Chrome `155.0.8059.39`, QEMU `10.2.2`,
virtiofsd `1.14.0`, SELinux Permissive. `/dev/kvm` and `/dev/vhost-vsock` were
accessible. Firefox was absent. No policy changes, sudo, downloads, installs,
existing-browser profile reuse or unrelated workload cleanup were performed.

## Commands and results

```sh
env -u GRILLO_HELM_BINARY PATH=/usr/local/go/bin:$PATH make check
PATH=/usr/local/go/bin:$PATH make test-ui-browser
PATH=/usr/local/go/bin:$PATH make test-ui
PATH=/usr/local/go/bin:$PATH make test-compose-dependencies
node --check scripts/ui-browser-smoke.mjs
git diff --check
```

The routine check clears the inherited Helm override because the missing-helper
regression deliberately tests PATH selection. Prior inherited-Helm failure is
preserved in [history](../history/implementation-log.md); this is not a product
fallback. Build targets prepare embedded assets before Go compilation. Every KVM
target rebuilds the guest before execution, rather than trusting a stale guest.

- `make check`: PASS after registry regeneration and correcting failures below;
  frontend pure tests/deterministic asset verification, fmt/vet/unit/script/race,
  host/agent builds and module audit. This is not an advisory scan.
- `make test-ui-browser`: Chrome PASS with a private temporary profile and
  loopback CDP port; Firefox SKIP, not PASS. Real desktop/mobile rendering,
  navigation, Pod tabs/filtering, keyboard topology, fonts, no runtime/CSP errors,
  metadata-only responses, literal HTML logs/terminal output, SSE resumption/gaps,
  captured Exec and browser terminal stdin against a deterministic fake core.
  The fixture proves transport/rendering, **not a guest PTY**.
- `make test-ui`: PASS against the actual CLI bridge, Unix daemon and real KVM
  chart. Latest signal-enabled run completed in 36.16 seconds (one gate, not a
  benchmark). The browser selects an exact Pod/container, opens `/bin/sh`, writes
  stdin, verifies guest `test -t 0` and `stty size`, changes viewport and verifies
  guest columns changed. Ctrl+C interrupts a foreground child; the shell resumes.
  Disconnect cancels the shell; a separate captured guest Exec confirms both
  recorded shell/child PIDs no longer exist. Existing daemon-gate checks verify
  metrics, guest logs, captured-exec cancellation, preserved Pod/VMM lifetime and
  test-owned cleanup. Latest rebuilt initramfs SHA-256:
  `e0da291ba7139c58f647f10e1134daf0f27044ca3073d72f25e0cbe183ac6c36`.
- `make test-compose-dependencies`: PASS, 4.34 seconds (one gate, not a benchmark).
  The real Compose consumer is alphabetically earlier than its dependency, so
  alphabetical planning cannot accidentally pass. It remains absent through failed
  guest readiness periods; the unhealthy container PID does not change. A real
  guest Exec creates the probe file; only after observed health does the consumer
  appear. Dependency PID remains unchanged. Cleanup uses a fresh context and only
  the test's application/resources. Guest hash for this run:
  `02a18357499551d38a1d7d3f54ecfcb7b844215229e39572197a6a6aca98c8de`.
- Pure tests cover dependency references/cycles/zero replicas, malformed/unsupported
  conditions, no-healthcheck healthy gates, all-replica and application isolation,
  cancellation/deadlines, readiness-not-liveness mapping, disabled/string health
  commands and rejected unsupported timing. Terminal tests cover exact origin/auth,
  strict bodies, input/resize, explicit exit, disconnect cancellation, slot/queue/
  aggregate-output limits, sanitized failures and bounded VT/keyboard behavior.

## Failed checks and corrections

1. Initial Chrome smoke failed because the preload observer was not installed:
   `window.__cspErrors` was null. Enable the CDP Page domain before adding preload;
   rerun passed. Missing observers are not treated as zero violations.
2. Initial dependency parser regression accepted a disabled healthcheck for
   `service_healthy`: shared dependency validation now runs during compilation,
   rejects missing health/reference/cycle cases, and remains enforced by the model.
3. Initial real dependency gate failed waiting for a virtiofsd socket: the long
   test-name/application suffix exceeded the Unix socket path budget. Give the
   test-owned backend a unique short temporary prefix, without touching preexisting
   resources. Rebuilt-guest rerun passed; the failure is not counted as hardware
   success.
4. Routine `make check` first failed compiler-registry/document consistency after
   the semantic change. Regenerate with
   `go run ./scripts/compatibility -output docs`; rerun passed.
5. A direct focused Go test after editing frontend sources failed
   `TestFrontendAssetsMatchSources` (stale embedded assets). Rebuild assets via the
   prescribed Make targets before rerunning; do not weaken fingerprint validation.

## Boundaries and remaining work

Terminal uses fixed guest `/bin/sh`, stdin/PTY and resize over the existing private
attach client; it never executes a host shell. Cookie + exact Origin + JSON are
required for create/input/close. Four shared terminal/Exec slots, 16 queued frames,
8 KiB chunks, five-second writes, 30-minute / 16 MiB aggregate output limit. Text
rendering ignores active OSC/DCS commands; bounded VT support is not full-screen
or complete wide-character emulation. No reconnect/replay, tool installation,
secret reveal or promises to redact deliberate workload stdout.

Compose gates support `service_started` and `service_healthy`, startup only, all
replicas, with cancellation and a two-minute maximum wait. Unknown/malformed or
unsupported semantics block apply. `service_completed_successfully`, optional
`required: false` and update-driven `restart: true` remain rejected. Do not use a
stopped state/default zero exit code as proof of successful primary completion.
Health status does not restart containers, but local endpoint filtering and warm-up
initial delay remain explicit `compose.degraded` behavior. Whole-second timing,
no `start_interval`, no inherited image healthcheck. Later dependency failures do
not continuously stop/restart consumers.

No extended advisory/fuzz campaign, external/clean-host gate, full terminal emulator,
long-idle/throughput benchmark or full Compose lifecycle closure was run here.
SELinux Enforcing/rootless-host support limits and other T24/D0 blockers remain.
