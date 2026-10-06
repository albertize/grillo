# T23 — Console acceptance

Status: **DONE for MVP console delivery**, including the PatternFly migration,
2026-10-06. This does not complete T24/F3 hardening or extend workload
compatibility. Dependencies T15/T22 have recorded real daemon/KVM evidence.

This is dated acceptance evidence. For current use and limits, read the
[console guide](../ui.md); for dependency/build rationale, read
[ADR 0008](../adr/0008-patternfly-react-console.md).

## Contracts verified

The console uses `api.Client`, not frontend/VMM adapters. Protected
`GET /v1/applications/{id}/view` projects allowlisted declarations and actual
observations. Config/env values, secret versions, probe commands/headers and
private guest paths are omitted from dedicated public DTO fields. Missing data
is explicitly unavailable; application-generated output is not promised secret
redaction.

Views cover overview, inferred topology, workloads/sandbox/probes, VMM samples,
mounts/volumes/routes, configuration metadata, diagnostics, retained logs, SSE
and non-TTY exec. The current React/PatternFly frontend replaces the original
framework-free console without changing the runtime/API boundary.

Handler tests cover auth/CSRF/Host/Origin/CSP, public-bind rejection, bootstrap
expiry/replay, strict bounded bodies, stream/exec concurrency, deadlines and
cancellation. CLI port zero reports the actual port; signal cancellation closes
the bridge without owning workload lifetime.

## Environment

Linux/amd64 UID 1000, kernel `7.2.8-200.fc44.x86_64`, Go
`go1.26.8-X:nodwarf5`, QEMU 10.2.2, `/usr/libexec/virtiofsd` 1.14.0, pasta
`0^20260728.gf8df3f1-2.fc44.x86_64`, Firefox 157.0, Node 24.18.0/npm 11.16.0.
SELinux was already **Permissive**; no policy change or privilege escalation.
Browser tests used installed Firefox and private profiles, not downloaded drivers.
The bare virtiofsd version probe failed outside PATH; its configured absolute
path succeeded.

## Original MVP gate

| Check | Result and scope |
| --- | --- |
| `make check` | PASS: format, vet, unit/race, scripts, builds, module checks |
| `go test -race -count=5 ./internal/guest ./internal/executor ./internal/ui ./internal/api ./internal/cli ./cmd/grillod` | PASS: identity, PID-file and cancellation regressions |
| `make test-ui-browser` | PASS: real Firefox/API fixtures; fragment removal, HttpOnly session, safe metadata/HTML logs, filters, exec, SSE gap/reconnect |
| `make test-ui` | PASS: actual CLI/daemon and real three-VM/seven-container Helm chart; metrics, container output/exit, cancellation and lifetime |
| `make test-helm` | PASS: T22 direct-runtime and CLI/daemon regression gates |
| JS syntax and diff checks | PASS |

The real browser executes a container command with separate stdout/stderr and
exit 7, cancels a sleeping exec, verifies its recorded guest PID is gone, and
executes again successfully. Browser closure and CLI SIGINT leave unchanged live
VMM PIDs and successful API exec. The surrounding chart gate exercises reapply,
down, persistence and daemon recovery. Literal retained-log rendering/filtering
is separately fixture-backed, not guest stdout-ingestion evidence.

Initial harness expressions failed and were corrected. The actual CLI gate
initially failed on SIGINT exit; context cancellation fixed it. The stronger
cancellation check then exposed a surviving guest exec process. Dedicated
authenticated exec connections plus guest parent-checked pidfd termination fixed
it, with rebuilt guest/full-gate verification. There is no unsafe PID-only kill
fallback; HTTP abort or a success marker alone is not cleanup evidence.

## PatternFly migration gate

React/React DOM 19.3.0, PatternFly core/table/icons 6.6.1 and build-only esbuild
0.28.2 are integrity-locked. Installed lifecycle scripts are disabled. Local
fonts and notices are embedded; there is no CDN, raw HTML insertion or CSP
relaxation. UTF-8 retained text is capped at 256 KiB, events also at 200 records.

| Check | Result and scope |
| --- | --- |
| `make ui-deps ui-check` | PASS: 22 platform-appropriate installed packages, five Node tests, deterministic assets |
| `make check` | PASS after final migration source changes |
| `make test-ui-browser` | PASS: actual Firefox, local fonts/CSP/runtime-error checks, all views, safe output/metadata, filters/SSE/exec and visible 390px mobile navigation |
| `make test-ui` | PASS twice after explicit CID isolation: freshly built CLI/daemon/rebuilt guest, real exec/cancelled-PID cleanup, lifetime and recovery |
| `cd web && npm audit --json` | PASS: zero known advisories at execution; not a security review or publisher-authenticity proof |
| Harness syntax, Markdown links/fences, `git diff --check` | PASS after corrections |

An initial build failed while inspecting uninstalled optional packages for other
platforms; ENOENT is ignored only for optional lock entries. A visibility test
exposed the unmanaged PatternFly Page's missing resize observation: mobile
navigation remained offscreen. Its documented resize callback and inert closed
sidebar fixed it. Programmatically clicking hidden links was not counted as
mobile evidence.

The first migration KVM run failed with guest CID 20 already in use by the review
demo. That workload was not stopped. Explicit validated workload/build CID-base
flags (defaults unchanged: 20/200) isolate the gate's ranges. Collisions still
fail; this is not a global allocator. Subsequent full gates passed while the
existing three demo VMMs remained alive. The refreshed review bridge served
byte-identical current JS/CSS/notices under strict CSP; the application remained
HTTP 200. Incidental session notes are retained in the
[delivery archive](../history/t23-patternfly-notes.md).

## Artifact identity and measurement scope

The kernel SHA-256 was
`550b5916433e758e814039f8324a9f1cdfdd2b3b8e4a1e44cc6836cdc92d5942`.

| Tested phase | Rebuilt guest SHA-256 |
| --- | --- |
| Original MVP | `7cc2ffe173f8e0d47522e2aac52cc26b13f785e81f2577a3d4fe229964905323` |
| Final PatternFly gate | `89c3f43ea235ceaee5150de99fb24f9d0c99c184182170ecdfe5edbe56754e3d` |

Later timestamped archive rebuilds can change the initramfs digest. No test-owned
profiles/state/processes remained in the recorded original post-test inspection.

Raw migration output sizes: JS 565,652 B, CSS 1,790,834 B, 15 assets totalling
2,680,695 B; CLI 14,877,065 B. Latest migration gate duration was 35.585s, with a
5.30s browser subtest. These are build/test observations, not startup/network or
performance benchmarks.

## Not verified and remaining limits

Hosted CI, other browsers/hosts, accessibility audit, long fuzz/load/performance
campaigns, full T24 hardening and T27 packaging were not completed here. The
frontend migration preserves the [MVP data/TTY limits](../ui.md#explicit-mvp-limits).
Original compiler line diagnostics, guest stdout ingestion and detailed usage
remain unavailable. TLS/CA, public exposure and secret reveal are outside scope.
Next task: **T24**.
