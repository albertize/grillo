# Guest protocol

Status: version 1.0 (implemented in `internal/guestproto`).

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
- The sandbox ID is part of the MAC input and is checked explicitly.

Version negotiation: the host offers a version; the guest accepts if the major
versions match and replies with the lower minor version. A different major is a
`version_mismatch` error. The negotiated version is fixed for the connection.

Capabilities the guest may advertise: `exec`, `tty`, `resize`, `stdin`,
`probe`, `files`. A client must not send a message the guest did not advertise.

### Security notes

- Authentication proves the two ends share the per-boot key. It does **not**
  make a compromised guest trusted: a guest can read its own secrets.
- Reconnecting requires a new handshake; a reconnecting host must not duplicate
  a `start` unless the guest reports no active workload.
- The protocol carries no per-frame MAC; confidentiality and integrity rely on
  the private vsock channel.

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
