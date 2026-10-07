# T24 — Bounded guest stdout/stderr ingestion

Date: 2026-10-07. This bounded change delivers the guest-log producer and
agent → executor → daemon spool → API/CLI/UI path. **T24 remains BLOCKED**;
interactive TTY, detailed metrics, artifact enforcement/hardening, full budgets
and the clean external-host gate are not completed by this change.
The [earlier T24 campaign](t24-hardening.md) is a dated snapshot; its stdout
blocker is superseded here, not silently erased from historical evidence.
Subsequent [interactive CLI, metrics and verified-boot evidence](t24-interactive-metrics-artifacts.md)
supersedes those local implementation blockers; this report retains its original
scope/date, not a current product-blocker inventory.

## Contracts and security boundary

- `logs` capability plus additive `logs`/`logs_result` control messages. Typed
  cursor batches carry container identity, stdout/stderr and raw byte chunks.
  Executors configured with a spool explicitly reject stale agents without the
  capability, with a rebuild instruction.
- A guest-wide ring retains 256 chunks, at most 4096 bytes each (1 MiB payload,
  bounded metadata). Host responses contain at most eight chunks (32 KiB raw).
  Output pipes are drained continuously; host/browser/CLI slowness cannot create
  an unbounded guest file or force the pipe drain to wait for that consumer.
  The ring can evict output rather than block workloads.
- Init output uses the same bounded streaming capture, including failed init
  output. Failure diagnostics report exit state, never interpolate raw output.
  Application stdout/stderr remains attached after runc's detached startup exits.
  Explicit pipe ownership respects the PID 1 reaper: no hidden os/exec copy
  goroutines requiring `Cmd.Wait`. Streaming-copy failures are reported even
  after a successful process exit.
- The executor polls one bounded batch per health tick (about one second),
  validates names against declared containers, and advances the guest cursor
  only after host spool writes succeed. Guest logs do not change readiness.
  Collection errors produce a redacted `guest_logs_unavailable` view diagnostic.
  Start-failure/shutdown draining is best-effort, bounded to 32 batches and a
  two-second context per drain; it does not claim lossless final output.
- Guest eviction becomes a persistent host record with `gap: true`. Within the
  chosen resource, loss markers bypass container filters and UI stream filters.
  Guest and persistent host sequence numbers are independent. New VMs have new
  guest cursors; container restarts preserve the guest ring/sequence.
- The host rotated spool now serializes append/read/close for concurrent sandbox
  producers and API consumers, with unique persistent sequences and explicit
  rejection of writes after close. No new dependency or architecture ADR.

The local API presents bounded text chunks, not necessarily complete lines or
lossless binary bytes (JSON text can replace invalid UTF-8). Human CLI/UI output
adds attribution/formatting. This is workload output, not ordinary diagnostic
redaction: an application deliberately printing a credential can expose it in
its logs. HTML is escaped; there is no raw DOM insertion. Heavy output, VM death,
shutdown races and later host spool rotation can lose records. Only guest-ring
retention loss has the new persistent gap marker; no complete host-retention gap
protocol or crash-safe exactly-once promise is made.

## Files changed

`internal/guestproto`: wire messages, capability, response mapping, typed client
validation. `internal/guest`: bounded ring, init streaming, detached stdio pipes,
reaper contract and tests. `internal/executor`: bounded ingestion/draining,
redacted view diagnostic and cursor tests. `internal/observe`: concurrent spool
and gap metadata/filter tests. Agent/daemon wiring enables the real producer.
CLI/daemon KVM and Firefox harnesses exercise the complete pipeline; React labels
and filtering reflect actual retained output. API contracts, user guides,
compatibility notes, threat model and progress are updated without widening the
Compose/Kubernetes feature registry.

## Verification

Environment: the existing Fedora fc44 development host, Linux/amd64 UID 1000,
KVM, QEMU 10.2.2, virtiofsd 1.14.0, pasta
`0^20261002.gcba3570-1.fc44`, Firefox 157.0, Go
`go1.26.8-X:nodwarf5`, Node 24.18.0/npm 11.16.0. SELinux was already Permissive;
no policy changes/escalation. Guests, CLI/daemon and embedded assets were rebuilt
by the targets under test. No second-host claim.

| Command | Result / scope |
| --- | --- |
| `go test ./internal/guest ./internal/guestproto ./internal/executor ./cmd/grillod` | Initially FAIL: fake-runtime ordering still expected `Run` for init; corrected to bounded `RunStreaming`, preserving init-before-app order |
| `make check` (two final runs) | PASS: frontend tests/assets, fmt/vet, unit/race, script fixtures, builds and module audit |
| `go test -race -count=1 ./internal/guest ./internal/guestproto ./internal/executor ./internal/observe ./cmd/grillod` (final runs) | PASS: fresh race checks, retention/cursor/malformed-batch/sink-failure/concurrent-spool/stream-error tests |
| `make test-t07 test-helm` | PASS after initial implementation: actual init/app stdout/stderr and complete chart/daemon log subtest, no SKIP |
| `make test-t07 test-helm test-ui test-ui-browser test-builder-kvm test-f2 test-executor` | PASS after cursor/gap/filter/drain changes, no SKIP; real init/app logs, CLI/API spool, chart/recovery/readiness, UI lifetime, copy/RUN/multi-stage builds, Compose and liveness |
| `make test-ui test-ui-browser test-builder-kvm` | PASS after final stream-error handling and strengthened native Firefox assertions, no SKIP |
| `go test -race -count=5 -run 'TestRuncStreamingReportsOutputFailure\|TestRuncDetachedLogPipesAndFailureCleanup' ./internal/guest` | PASS after strengthening the success-followed-by-output-error assertion |
| `node --check scripts/ui-browser-smoke.mjs` | PASS |
| `python3 /tmp/grillo-t24/check_markdown.py`, `git diff --check`, `gofmt -l cmd internal guest experiments/boot scripts` | PASS: 47 project Markdown files, clean diff, no unformatted files; no QEMU/grillod/pasta leftovers |

The positive guest gate starts an init that prints `init-log`, an application
that prints distinct stdout/stderr markers then serves HTTP, and a sidecar.
Authenticated cursor reads receive the actual workload output with correct
container/stream attribution; Pod networking/root separation and stop still pass.

The actual daemon gate executes a guest command writing to the running storage
container's own `/proc/1/fd/1` and `/proc/1/fd/2` (not exec capture). It waits for
persisted API records, then reads the same markers via the built CLI. Native
Firefox reads those actual records, verifies stdout/stderr filtering and proves
an HTML marker stays escaped text. This supersedes fixture-only log rendering
evidence, while fixture tests remain useful for deterministic client behavior.

Unit negatives include ring overflow without consumers, reported gaps, chunk
bounds/invalid UTF-8 retention in the guest, immutable snapshot copies,
malformed request/batch/cursor/stream, unknown container rejection, failed spool
writes without acknowledgment, loss marker visibility through filters,
concurrent producers/readers, detached pipe failure cleanup and output-copy
failure after process success. Failed init output is retained only in workload
logs, not the public operation error.

Kernel SHA-256 remained
`550b5916433e758e814039f8324a9f1cdfdd2b3b8e4a1e44cc6836cdc92d5942`.
The final rebuilt guest SHA-256 was
`92b7754559d8a0fb11f1d7a6c2ebee4895afefa073551bf4f7711fde0f728652`.
Archive timestamps can change this hash on subsequent builds.

## Not run / next task

No hosted CI, new external host, long output/load/exhaustion campaign or full
performance accounting was run. The preceding T24 fuzz/audit results remain
dated evidence, not newly rerun by this log delivery. No per-workload fairness,
lossless shutdown, host-retention gap signaling or arbitrary binary-log fidelity
is claimed. Polling/retention limits are deliberate bounded behavior, not a
throughput benchmark.

Next bounded T24 work: interactive CLI stdin/TTY/resize and cancellation, then
actual guest/container metrics and artifact verification/fresh-key delivery.
Keep the clean external-host acceptance gate open; do not advance to T25.
