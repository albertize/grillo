# T24 — Interactive CLI, guest metrics and verified boot artifacts

Date: 2026-10-07. **T24 remains BLOCKED overall.** This delivery completes three
locally verifiable contracts authorized by the user; it does not advance T25,
provide a release installer, or replace clean external-host acceptance.
Dependencies: T07/T08/T15/T16/T18/T19/T22/T23 and the prior
[guest log delivery](t24-guest-logs.md). Source baseline `779733c`, with preserved
uncommitted changes from this and the earlier local campaigns.

## Environment

Same Linux/amd64 Fedora fc44 development host as the
[T24 campaign](t24-hardening.md): UID 1000, KVM/vhost-vsock available, QEMU 10.2.2,
virtiofsd 1.14.0, pasta `0^20261002.gcba3570-1.fc44`, Firefox 157.0,
Go `go1.26.8-X:nodwarf5`, Node 24.18.0/npm 11.16.0.
SELinux was already Permissive. No sudo, device permission changes, policy
changes or host configuration modifications were used.

The final multi-gate run rebuilt the guest/frontend and test-owned CLI/daemon.
Its base kernel SHA-256 was
`550b5916433e758e814039f8324a9f1cdfdd2b3b8e4a1e44cc6836cdc92d5942`;
base initramfs SHA-256 after that run was
`f05f99be45cb4de334214af8b46957db130b317490cef82bb867ffcd0d0b249d`.
Timestamped rebuilds change the initramfs hash. Production boot overlays contain
fresh credentials and therefore differ from the base artifact; never publish
those images or their credentials.

## Delivered contracts

### Interactive CLI

`exec -i` forwards stdin without a terminal; `exec -t` allocates a guest terminal
and also forwards stdin. `shell` uses an interactive terminal when local stdin
is a terminal. Ordinary exec remains noninteractive. The actual CLI puts local
stdin into raw mode only for terminal sessions, restores it on return/error or
interrupt, forwards SIGWINCH, propagates exit codes and cancels on disconnect.
Closed output pipes are errors, not SIGPIPE exits bypassing restoration;
non-reading terminal/pipe output is cancellation-aware.

The private Unix API upgrades `POST /v1/exec-attach` to bounded guest-protocol
frames. It is not exposed by the browser bridge. Sessions have a one-hour server
limit, five-second frame writes, at most 16 active API attachments, and 16-frame
input queues. Guest capabilities `stdin`, `tty`, `resize` are negotiated;
missing support fails explicitly. Invalid frames/queue overflow fail the session.
TTY streams merge stderr; non-TTY streams retain separation. EOF uses an empty
stdin frame; canonical TTY EOF uses EOT and is not a universal raw-mode EOF.

Guest runc runs in foreground on an operation-owned controlling PTY and forwards
the terminal to the exec process. Resize reaches the inner terminal through
runc's SIGWINCH handling. Cancellation uses the runc PID file and a parent-checked
pidfd, rather than killing a reused numeric PID or the container's main process.
Raw arbitrary injected `io.Reader`/`io.Writer` implementations must provide their
own cancellation behavior; production CLI stdin/terminal/pipe descriptors are
handled explicitly. Regular-file kernel I/O is not an interruptible disk deadline.

### Real metrics

The additive `metrics` capability returns guest `/proc/meminfo` total/available
bytes and `/proc/stat` busy/idle USER_HZ ticks. Busy counts user, nice, system,
IRQ and softirq; idle includes iowait. Steal is excluded from guest work;
guest/guest_nice are not double-counted. These are cumulative counters, not CPU
percentages or guessed seconds.

Container memory and cumulative CPU microseconds come from cgroup v2
`memory.current` and `cpu.stat usage_usec`, locating the group through the
runtime-reported PID. Exited init containers or absent/unsupported files yield
nullable unavailable values, not fabricated zero. Parsers have input/overflow
bounds; the host checks sample shape and declared container names.

Queued protocol calls retain their own deadline; closing a client interrupts
active transport I/O instead of waiting for the serialized request lock. A
regression covers queued timeout and close while an earlier request is active.

The shared application view, `grillo metrics <application> [--output=json]` and
console carry collection timestamps/provenance. VMM RSS, guest allocation, guest
memory and cgroup usage remain distinct and are not summed. Compromised guests
can falsify guest-reported data; this is not host attestation. PSS, cache usage,
CPU percentages and guest kernel-version discovery remain unavailable.

### Verified artifacts and fresh keys

The daemon's runtime and native-build QEMU backends require the configured
artifact manifest (default `experiments/artifacts/t07/manifest.json`). Inventory
parsing is strict, at most 1 MiB/16 entries, with duplicate/name/digest validation.
Kernel/initramfs expectations are pinned at backend open, not silently re-trusted
if a file/manifest changes later.

Every actual boot verifies configured paths, size and SHA-256 while copying into
operation-private 0600 snapshots, rejecting nonregular/final-symlink files. These
snapshots precede helper/VMM launch and avoid verifying one file then launching a
changed source. Source size is limited to 2 GiB per boot artifact. Verification
failure prevents launch; it is never a compatibility fallback.

QEMU generates 32 random bytes for every new boot, stores them only in private
state and appends a second gzip/newc archive overriding `/etc/grillo/key` in the
private initramfs. Idempotent Start retains a live VM's address/key; a restart
rotates the key. Consumers obtain the actual persisted boot identity, including
when Create reuses a handle. Keys are not arguments, logs or public DTO fields.
The legacy `--key-file` daemon flag is accepted but ignored; direct component
fixtures retain a private baked key. Fixture images/keys are 0600, and fixture
archive/manifest publication is atomic per file.

This is local integrity verification against an operator-trusted inventory, not
publisher authentication, a signed release or a complete SBOM. Only boot
kernel/initramfs files are independently rehashed at launch; agent/runc content
is covered by the verified base initramfs. Host VMM/helpers and upstream
provenance/license review remain separate gates. See [ADR 0009](../adr/0009-verified-boot-and-private-key-overlay.md).

## Executed verification

| Command | Result / scope |
| --- | --- |
| `make check` | PASS, including formatting, vet, all Go unit/race tests and frontend checks |
| `go test -race -count=1 ./internal/api ./internal/cli ./internal/guestproto ./internal/guest ./internal/backend/qemu ./internal/executor ./internal/build ./internal/observe` | PASS, uncached focused campaign |
| `make test-t07 test-helm test-ui test-ui-browser test-builder-kvm test-f2 test-executor` | PASS, no SKIP; actual guest/Helm/CLI/daemon/Firefox/native builds/Compose/liveness gates |
| `make vulncheck` | PASS, Go/npm audits |

Native evidence includes guest stdin and exit 7, separated/merged stderr,
42×101 guest resize, actual CLI/API stdin, and real host PTY SIGWINCH to 47×111.
Host terminal modes are compared before/after normal exit and SIGINT. API abort
waits for an observed exec PID and checks it no longer exists. Actual `/proc` and
cgroup samples reach API/CLI and Firefox, including source/units.

Unit/fake-process evidence separately covers malformed protocol/API input,
missing counters, overflow/traversal, blocked output cancellation, strict
inventory, source digest mutation despite a rewritten manifest, immutable
snapshots, overlay structure and key rotation/idempotency. Fake launch tests
prove launch-order/key contracts; native gates prove the real boot/auth paths.

Logs are local, untracked under `/tmp/grillo-t24/`, including
`three-check-complete.log`, `three-kvm-complete.log`, `three-race.log` and
`three-vuln.log`; these are not hosted CI evidence.

## Failures retained and corrections

- An initial focused unit run failed the old test expecting implicit raw mode
  for ordinary exec. The expectation now requires explicit `-t`; non-TTY exec
  has a regression asserting it does not alter terminal mode. The first full
  `go test ./...` also failed stale embedded-asset validation after frontend
  edits; the supported targets rebuilt assets before the passing full checks.
- A first KVM TTY implementation used runc console-socket without detached mode;
  real runc rejected it. It was replaced by the foreground controlling PTY path,
  then rebuilt/retested on KVM.
- Native CLI metrics JSON initially printed valid data but returned status 1
  through an unconditional error helper. Corrected with text/JSON exit tests.
- A Helm re-create gate failed with `waitid: no child processes` in BusyBox network
  setup. PID 1's reaper and os/exec were competing for the same child. Network
  commands now use the reaper as sole wait owner with bounded context; a
  100-command regression, failure/cancellation tests and fresh Helm gate pass.
  The subsequent CID/volume failures in that run were not treated as passes.
- The first real host-PTY test encountered `/dev/pts/ptmx` mode 000. The helper
  supports the existing permitted legacy `/dev/ptmx` device instead; no chmod or
  privilege escalation. Native terminal tests then passed.

## Remaining gates

The subsequent [verified lifecycle/dependency campaign](t24-closure-campaign.md)
adds 100-cycle warm verified-copy/overlay, post-auth RSS/PSS and logical-byte
measurements plus current advisory fixes. It does not close the full budget,
external-host, load/quota, provenance or release gates below.

T24/F3 remains open for clean external-host reproduction, a verifiable release
installation/delivery path, hosted CI evidence, sustained adversarial/quota/load
work, full startup/memory/disk/resource budgets with the new boot-copy cost,
and redistribution/provenance/license review. Earlier benchmark numbers are
historical, not updated production-copy measurements. Browser full TTY remains
outside this delivery. Host retention/shutdown log gaps remain as documented in
the log report. T25 is not started.
