# Guest protocol

Status: version 1.1 (implemented in `internal/guestproto`).

This document is the contract between the Grillo host runtime and the guest
agent. It follows the specification's host/guest control channel (§6.4): a
versioned, authenticated, bounded protocol carried over AF_VSOCK (vhost-vsock)
or an equivalent private relay. Management is never exposed on the application
network.

The framing codec is independent of the transport. Any `io.Reader`/`io.Writer`
pair works, so the same codec is used with an AF_VSOCK socket, a relay, and an
in-memory pipe in tests.

## Transport

- Byte stream, ordered and reliable.
- The host dials the guest's vsock port; the guest never dials outward for
  control.
- One connection carries one handshake followed by any number of requests.
- The host client serializes requests on a connection (one in flight). The guest
  server handles requests concurrently and cancels them on request.

## Framing

All integers are unsigned big-endian. Every frame is:

```
+--------------+----------+-----------------------+
| length u32   | kind u8  | body (length-1 bytes) |
+--------------+----------+-----------------------+
```

`length` counts the kind byte plus the body. It is validated **before** any
allocation.

| Kind | Value | Body |
| --- | --- | --- |
| control | 1 | UTF-8 JSON control message |
| data | 2 | `session u32` + `stream u8` + payload |
| ping | 3 | empty |
| pong | 4 | empty |

Limits:

- control body: **1 MiB** (`MaxControlBytes`)
- data payload: **64 KiB** (`MaxDataBytes`)

A frame that declares a length above its kind's bound, an unknown kind, or a
data body shorter than five bytes is rejected. Writers never buffer a frame
beyond these bounds, so a non-reading peer applies backpressure through the
socket instead of unbounded memory growth.

### Stream kinds

| Stream | Value | Payload |
| --- | --- | --- |
| stdin | 1 | input bytes |
| stdout | 2 | output bytes |
| stderr | 3 | error bytes |
| resize | 4 | `rows u16` + `cols u16` |
| exit | 5 | `code i32` (empty means 0) |

## Control messages

A control body is a JSON object:

```json
{
  "id": "req-7",
  "type": "status",
  "replyTo": "req-7",
  "deadline": "2026-10-03T15:04:05Z",
  "error": {"code": "busy", "message": "starting", "retryable": true},
  "payload": { }
}
```

- `id` correlates a request with its response. The response repeats the
  request's `id`.
- `replyTo` references the request a message answers or cancels.
- `deadline` is the absolute time after which the guest should abandon the
  request. The client always sets it from its context.
- `error` is present on an `error` reply or a failed response.
- `payload` is message-specific and typed below.

Message types:

| Type | Direction | Purpose |
| --- | --- | --- |
| `hello` | host → guest | start the handshake |
| `hello_ack` | guest → host | version, capabilities, nonce |
| `auth` | both | key proof |
| `error` | both | typed failure |
| `ping` / `pong` | both | heartbeat |
| `cancel` | host → guest | cancel a pending request by `replyTo` |
| `start` / `started` | host → guest → host | start the workload |
| `stop` / `stopped` | host → guest → host | stop the workload |
| `status` / `status_result` | host → guest → host | guest/container state |
| `probe` / `probe_result` | host → guest → host | health probe |
| `exec` / `exec_result` | host → guest → host | run a command with streams |
| `run` / `run_result` | host → guest → host | run a build step in a mounted share |
| `export` / `export_result` | host → guest → host | agent-owned bounded full build snapshot |
| `logs` / `logs_result` | host → guest → host | bounded retained container stdout/stderr batch |
| `metrics` / `metrics_result` | host → guest → host | nullable guest/cgroup counters |

`ping`, `pong`, `hello`, `hello_ack`, and `auth` may also use the ping/pong
frame kinds for heartbeats; a control `ping` is answered with a control `pong`.

## Handshake and authentication

The per-boot key is 32 random bytes delivered to the guest through a private
channel (for example the kernel command line is **not** acceptable; use a
virtio device or the boot configuration). It is never written to logs or
process arguments.

```
host                                   guest
 |  hello{version, sandbox, clientNonce} |
 |-------------------------------------->|
 |  hello_ack{version, sandbox,          |
 |            serverNonce, caps}         |
 |<--------------------------------------|
 |  auth{mac = HMAC(key, client side)}   |
 |-------------------------------------->|
 |  auth{mac = HMAC(key, server side)}   |
 |<--------------------------------------|
```

- `clientNonce` and `serverNonce` are 32 random bytes, base64 on the wire.
- The MAC is `HMAC-SHA256(key, "grillo-guest-handshake\0" || role || "\0" ||
  sandbox || "\0" || clientNonce || "\0" || serverNonce)` where `role` is
  `client` or `server`. The role is included so the two proofs cannot be
  reflected.
- Both sides compare in constant time.
- Sandbox context is MAC-bound; an explicitly configured nonempty ID must match.
  The current daemon uses unique per-boot credentials and assigned vsock addresses,
  not pre-boot nonempty sandbox labels.

Version negotiation: the host offers a version; the guest accepts if the major
versions match and replies with the lower minor version. A different major is a
`version_mismatch` error. The negotiated version is fixed for the connection.

Capabilities the guest may advertise: `exec`, `tty`, `resize`, `stdin`,
`probe`, `files`, `application-dns`, `logs`, `metrics`, `build-export`, `block-build`. A client must not send a message the guest did not advertise.
The additive `DNSConfig.server` IPv4 field selects the private application
namespace resolver instead of a guest-local static resolver. Bridged executors
require `application-dns` and reject old agents with a rebuild instruction.

### Build snapshot export

The additive `build-export` capability uses protocol 1.1 framing and authentication.
Clients reject old agents without it; no silent export fallback is available.
`export` carries `session`, `share`, `maxBytes` and `maxEntries`. The share must
be an agent-mounted tag in a started build sandbox. Byte limits are mandatory
(1 KiB–8 GiB of complete uncompressed tar), as are entry limits (1–100,000).
Only stdout frames for that session carry archive bytes; other stream/session
frames fail. `export_result` carries `bytes`, `entries` and lowercase hex `sha256`
for the complete tar, including trailer. The host validates counts/checksum and
then independently validates and extracts the archive into private staging.

The agent serializes RUN/export. Failed RUN cleanup overrides success and blocks
further build operations until sandbox recreation. Export has a five-minute
guest ceiling plus the request deadline; host calls use the existing 30-second
default unless their context has a deadline. Cancellation interrupts socket
writes by closing the connection, and failed transfers cannot reuse it. No
hard deadline for a stalled local filesystem writer is claimed. Snapshots reject
special files and reserved `.wh.*` names; they preserve numeric owners and opaque
symlink metadata. Export uses agent Go code, never a workload executable.

The opt-in block RUN path now uses this primitive. `export_result.root` optionally
reports numeric `uid`, `gid` and `mode`; block builds require it. Mode is bounded
to 07777. `block-build` advertises `SandboxSpec.buildSeed`: source/target tags,
archive `bytes`, `entries` and `sha256`. The source must be read-only virtiofs,
the target writable ext4 on `/dev/vda`, with disjoint target paths. The agent
verifies `root.tar` from the source share and populates only a fresh target before
reporting ready. Old guests fail explicitly. Native block RUN limits aggregate
stdout/stderr to 4 MiB; RUN rejects mismatched sessions/streams/completion types.
Hardware acceptance of this integration remains blocked.
See [ADR 0016](../docs/adr/0016-agent-owned-build-export.md) and
[original software evidence](../docs/experiments/t24-agent-owned-build-export.md),
then [ADR 0017](../docs/adr/0017-opt-in-block-build-workspaces.md) and
[block integration evidence](../docs/experiments/t24-opt-in-block-build-workspaces.md).

### Security notes

- Authentication proves the two ends share the per-boot key. It does **not**
  make a compromised guest trusted: a guest can read its own secrets.
- Reconnecting requires a new handshake; a reconnecting host must not duplicate
  a `start` unless the guest reports no active workload.
- The protocol carries no per-frame MAC; confidentiality and integrity rely on
  the private vsock channel.

## Interactive exec and metrics

`exec` includes `container`, argument-array `args`, optional `tty`, `stdin`,
initial `rows`/`cols` and nonzero `session`. Incoming stdin/resize data frames
route only to that active session; empty stdin means EOF, resize is exactly four
bytes with nonzero dimensions. Input queues hold at most 16 frames per request;
unknown sessions, malformed input or overflow terminate the connection and cancel
requests. Writes have five-second deadlines. TTY output merges stderr into stdout;
non-TTY keeps separation. Completion is `exec_result` with the actual exit code.
Closing the dedicated host connection cancels exec; it does not stop the workload.
Canonical TTY EOF uses EOT and is not guaranteed for applications in raw mode.

`metrics` has no payload. `metrics_result` includes nullable `memoryTotalBytes`,
`memoryAvailableBytes`, `cpuBusyTicks`, `cpuIdleTicks` and at most 128 `containers`
with unique `name`, nullable `memoryBytes` and `cpuUsec`. Guest counters come from
`/proc/meminfo` and `/proc/stat`; busy excludes idle/iowait/steal and does not
double-count guest ticks. Ticks use USER_HZ, not guessed seconds or percentages.
Container values come from PID-selected cgroup v2 `memory.current` and
`cpu.stat usage_usec`. Missing/exited/unsupported data is absent, not zero.
Sampling/parsers are bounded and cancellation-aware. Authenticated guest reports
are not independent host attestation.

Production QEMU boot paths verify private kernel/initramfs snapshots and replace
the fixture key with a fresh private initramfs overlay on each new boot; see
[ADR 0009](../docs/adr/0009-verified-boot-and-private-key-overlay.md).

## Retained container logs

`logs` payload is `{ "after": 0 }`, a guest-wide sequence cursor. A response has
`chunks`, `next` and optional `gap`. Each chunk has increasing `seq`, declared
`container`, `stream` (`stdout` or `stderr`) and base64 `data` (raw bytes).
Chunks are at most 4096 bytes, batches at most eight chunks. `next` is the last
chunk sequence, or the request cursor if the batch is empty. `gap` reports that
records after the request cursor were evicted; a gap response must have data.
The client validates batch/chunk bounds, streams, cursor progression and gap
reporting; the executor checks container names against the sandbox declaration.

The guest ring retains at most 256 chunks (1 MiB payload plus bounded metadata)
across all containers. Init output uses the same ring; application stdio pipes
are continuously drained without waiting for host/API consumers or writing
unbounded guest log files. Restarts preserve the guest sequence; a new VM starts
a new cursor. The daemon polls bounded batches, about once per second, and writes
a separately sequenced/rotated host spool. Heavy output may overrun retention;
a host gap record makes that loss visible. This is not lossless delivery, and
crashes or final shutdown output may be lost before collection.

Executors with a log spool require the `logs` capability and reject stale agents
with a rebuild instruction. Capture failures appear as redacted view diagnostics,
not readiness failure or raw tool output. Ordinary workload logs may contain any
value the application deliberately prints, including credentials; no arbitrary
stdout secret-redaction guarantee is made.

## Errors

| Code | Meaning |
| --- | --- |
| `unauthorized` | handshake or authorization failure |
| `version_mismatch` | no shared protocol version |
| `bad_request` | malformed payload for the message type |
| `unknown_type` | unrecognized message type |
| `not_found` | referenced resource does not exist |
| `canceled` | the request was canceled |
| `deadline_exceeded` | the request deadline passed |
| `busy` | the guest cannot serve the request yet |
| `unsupported` | the guest does not implement the request |
| `internal` | unexpected guest failure |

## Handling rules

- Validate the frame length before allocating; never trust a peer's length.
- Handle short reads and short writes; the codec uses full reads and loops on
  writes.
- Unknown frame kinds and malformed control JSON are fatal to the connection:
  the stream can no longer be resynchronized.
- Bound the number of sessions; T06 allows one in-flight request per
  connection and one stream group per exec.
- Apply deadlines to reads and writes; a canceled context unblocks the
  transport.
- The guest cancels handler work when the request deadline passes or a `cancel`
  message arrives, and when the connection closes.
