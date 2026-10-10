# T23 — running full-stack Compose review environment

Status: **actual local build/HTTP/network gates PASS; left running for user review**.
This is not a distribution/default-policy/security acceptance gate; T24/D0 remain
BLOCKED. User explicitly requested a live Compose environment with UI, Nginx and
Go backend; no commit/push requested for this iteration.

## Stack and boundaries

[Example source](../../examples/ui-stack/README.md):

- `ui`: static HTML/CSS/JS served by BusyBox httpd.
- `backend`: trusted local standard-library Go source, built CGO-free Linux/amd64
  and assembled into a scratch image by Grillo's native copy-only builder.
- `nginx`: gateway serving `/` from UI, `/api/` and `/healthz` from Go.
- Only Nginx's guest port 8080 is published at host **127.0.0.1:18088**. No host
  listener is published for the private backend/UI; no host-container fallback.
- Separate rebuilt Grillo management console is loopback-only and points at the
  same dedicated private XDG daemon, not preexisting user state. Workloads remain
  running when CLI/console closes. Review-owned workload/build CID bases 45000/
  46000 selected deliberately; not a global allocator or guarantee of availability.

Remote base inputs: BusyBox 1.37 pinned as in existing distribution example;
Nginx `1.28.0-alpine` index `sha256:30f1c0d78e0ad60901648be663a710bdadf19e4c10ac6782c235200619158284`,
selected Linux/amd64 manifest `sha256:09ab424a8c788f8d0fe3a64429f6d19dfa526885c8609b748d0943a75dcb9f8c`.
Registry manifest bytes checked against returned digest before pinning; no claim
of publisher authenticity/advisory clearance/source/license completion. Base
content is fetched/executed inside the workload, never as host installation scripts.
No dependency, CA install, security-policy change or sudo escalation introduced.

## Ownership and actual observations

Unique application: `ui-stack-HkWRTQjf`.
Private session: `/tmp/grillo-stack.HkWRTQjf/`.
Owned build/session artifacts: `experiments/artifacts/ui-stacks/review.0NYvRFK4/`.
Its `session.env` is mode 0600, directories 0700, contains explicit helper/guest/XDG
paths and unique local tags (no credential). Sources remain tracked example files;
compiled executable, plan/snapshots/logs/PIDs are ignored local artifacts. Temporary
state is intentionally live, not cleaned while the requested review is running.

Observed sandbox IDs/IPs:

| Service | Actual microVM ID | Private IP | Allocation |
|---|---|---|---|
| Go backend | ui-stack-HkWRTQjf-backend-0 | 10.77.0.2 | 1 vCPU / 512 MiB guest budget |
| Nginx | ui-stack-HkWRTQjf-nginx-0 | 10.77.0.3 | 1 vCPU / 512 MiB guest budget |
| Web UI | ui-stack-HkWRTQjf-ui-0 | 10.77.0.4 | 1 vCPU / 512 MiB guest budget |

All three observed containers running and sandboxes ready after HTTP verification.
This is **allocation**, not measured aggregate host usage. Runtime sources/kernel/
helper paths are the prepared runtime-only store and current checkout binaries;
same recorded Fedora fc44/Linux 7.2.9/UID 1000/already-Permissive environment.
Backend actually reports Go `go1.26.8-X:nodwarf5` and hostname `(none)`; no invented
hostname. Console HTTP shell was probed successfully on an ephemeral loopback port.

## Failed attempts and fixes

1. Three native image builds PASS; first up **FAIL**:
   `executor: container "backend" has no command`. Image ENTRYPOINT metadata is
   not resolved into an executor command here. Added explicit Compose commands;
   no stub or silent compatibility claim.
2. Next apply succeeds but gateway HTTP **FAIL** with empty reply. Actual Nginx
   stderr: `host not found in upstream "ui:8080"`. Backend/UI running, gateway
   container stopped; no success inferred from apply alone. `depends_on` is only
   ordering; headless DNS is not guaranteed immediately at launch.
3. Added bounded **guest-side** UI/backend HTTP readiness retries before exec'ing
   Nginx. Re-applied successfully, gateway/HTTP then verified. Manifest preserves
   existing startup-ordering warning, loopback-address warning, and bounds failure
   instead of infinite retry/faked readiness. Retries use workload shell, never a
   shell interpreting untrusted manifest input on the host.
4. Inspection still reports `published_endpoints: null` despite working HTTP.
   Known presentation issue remains; private Service snapshots also do not list
   `expose`-only ports. Evidence does not claim those DTO fields are repaired.

## Executed verification

- `go test ./examples/ui-stack/backend` PASS; final full `make check` PASS,
  including new standard-library backend unit/race checks and existing UI build/
  fingerprint checks. No package provisioning was needed.
- Real `grillo build -t ... <context>` for all three unique image references PASS;
  no `--podman` or host-container process.
- Actual `grillo plan`, repeated `grillo up`, inspect/status and Unix-API snapshot
  checks. Final three live VM observations PASS; failed steps retained above.
- GET `/`, `/app.js`, `/style.css`, `/healthz` on gateway **200**; correct HTML/JS/
  CSS/health markers, same-origin CSP header verified. GET `/api/status` **200**
  identifies real Go server. POST `/api/notes` **201**, subsequent GET returns added
  note. Backend test covers malformed/unknown/empty/oversized JSON, content-type,
  trailing object and 128-note admission failure paths. Note values not logged.
- Actual guest exec in Nginx fetches `http://backend:8080/api/status`; exec in UI
  fetches `http://backend:8080/healthz`: **PASS**, DNS/private cross-VM HTTP direction
  actually exercised (not inferred from host GET alone).
- Dedicated management UI bridge HTTP/static shell **200**. Bootstrap credential
  deliberately not printed or copied into tracked docs/ordinary diagnostics; a
  mode-0600 private one-use URL handoff file is separate from error/daemon logs.
  Bootstrap has the usual five-minute lifetime; open privately, then remove that
  owned handoff file once used. Do not share its content. Application notes carry
  no authentication and are only suitable for this trusted local demo.

Browser interaction for the new sample web form was not automated in this run;
HTTP/assets/real POST/readback were verified, not a guessed browser rendering PASS.
Existing Grillo console's desktop/mobile/CSP/XSS/exec/lifetime gates remain as in
[UI revision evidence](t23-console-redesign.md); this three-service review did not
rerun their entire fixture/KVM harness. Down/post-down refusal and daemon cleanup
were **NOT** executed because the user requested the live environment remain up.
No long-duration/load/security/release/other-host gate or source/notice clearance.

## User controls

From the checkout, source the private `session.env` before CLI operations so the
correct isolated daemon/image store/application is addressed. Application URL is
`http://127.0.0.1:18088/`. Private console handoff file is inside the session; if
expired, start another explicit `bin/grillo ui --port 0` using the same environment
and its printed URL. Do not delete sockets or terminate unverified PIDs.

When finished: `grillo down "$GRILLO_STACK_NAME"` in this environment, probe the
old gateway for refusal, then deliberately stop only the recorded owned UI/daemon
PIDs after verifying their `/proc/PID/exe`. No persistent volume/bind cleanup is
needed. Retain build/cache artifacts unless explicitly pruning unused owned data.

Next: user review of the real three-service console, table/topology UX and scoped
logs; keep inspection/DNS startup limitations separate from acceptance claims.
