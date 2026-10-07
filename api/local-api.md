# Local API

Status: version 1 (implemented in `internal/api`).

The daemon (`grillod`) serves this API over a Unix socket at
`$XDG_RUNTIME_DIR/grillo/grillod.sock` (mode `0600`, in a `0700` directory).
Public DTOs are separate from private structures and never carry secrets or raw
stderr.

## Authentication and limits

- Every connection is checked with `SO_PEERCRED`; only the owning UID is allowed.
  A connection from any other UID is closed.
- The daemon holds a single-instance state lock, so two daemons cannot run.
- Request bodies are bounded (default 8 MiB). Long mutations are asynchronous.
- Errors use a fixed DTO:
  `{"code","message","resource","retryable","details"}`.

## Endpoints

| Method and path | Purpose |
| --- | --- |
| `GET /v1/version` | daemon version and API version |
| `GET /v1/health` | liveness and uptime |
| `GET /v1/applications/{id}` | container status and public route fallback endpoints |
| `GET /v1/applications/{id}/view` | allowlisted application/resource/probe/VMM snapshot (T23) |
| `POST /v1/exec` | run a command in a container and return captured output |
| `POST /v1/exec-attach` | private Unix HTTP upgrade for interactive stdin/TTY/resize |
| `POST /v1/applications` | submit a desired application; `202` + `operationId` |
| `POST /v1/applications/{id}/down` | stop an application; `?volumes=true` deletes owned managed volumes |
| `GET /v1/operations/{id}` | operation status |
| `POST /v1/operations/{id}/cancel` | cancel a running operation |
| `GET /v1/events` | Server-Sent Events of the sequenced event stream |
| `GET /v1/logs` | log records; `?follow=true` streams them |
| `GET /v1/images` | image inventory (pull and build records) |
| `GET /v1/images/inspect?ref=` | one image record by reference |
| `POST /v1/images/prune` | garbage-collect unused images; body `{"keep":[...]}`; `202` + `operationId` |
| `POST /v1/images/pin` | protect or release an image (or a bare `sha256:` digest) from pruning |
| `POST /v1/build` | build an image; body is a build request; `202` + `operationId` |
| `POST /v1/shutdown` | stop the daemon (not applications) |

Image pruning never removes an image that is pinned or referenced by a known
application, and only then collects unreferenced CAS blobs, so active data is
preserved.

The `container` field of `POST /v1/exec` accepts either a bare container name or
`sandbox-ID/container`. A bare name is executed only when exactly one running
sandbox has that container; otherwise the request fails with an ambiguity error
listing the candidate sandbox IDs, and no arbitrary replica is chosen. An
unknown target fails with `exec_failed`.

`POST /v1/build` uses Grillo's **native builder by default**: it parses a
supported Dockerfile subset, executes `RUN` steps inside a sandboxed guest that
shares the build root, and publishes a verified OCI image. When the
`grillo-netns` supervisor and `pasta` are available the build guest gets outbound
connectivity (so `RUN` can install packages); otherwise it builds offline and the
daemon logs a warning. `builder: "podman"` selects rootless Podman as an explicit
opt-in accelerator; it is never selected automatically. Builder output streams
into the event stream (`kind=build`, `reason=progress`). A missing guest artifact
or a missing Podman binary is reported as an actionable error, not a fallback.

Application status includes `routes` when the runtime provides them. Each public
route contains only `hostname`, `path`, `pathType` and the actual loopback HTTP
`endpoint`; no guest specs or secret material are returned. `grillo inspect`
includes this inventory. Fallback ports are allocated atomically by listening
on `127.0.0.1:0`; `/etc/hosts` is never modified.

## Interactive Unix attachment

`POST /v1/exec-attach` requires `Upgrade: grillo-attach-v1` and a strict JSON
body (at most 64 KiB): `application`, `container`, argument-array `args`, optional
`tty`, `rows`, `cols`. Target resolution is the same as ordinary exec. The server
replies `101 Switching Protocols`; subsequent bytes use guest-protocol framing,
with session zero, stdin/resize input and stdout/stderr output. Empty stdin is
EOF; resize contains big-endian u16 rows/columns, both nonzero. TTY merges stderr.
Completion must be an `exec_result` control with explicit exit code; malformed
frames, truncated streams and failed output writes are errors, not success.

At most 16 API attachments are active; queues contain at most 16 frames, writes
have five-second deadlines and sessions last at most one hour. Disconnect,
request cancellation and daemon shutdown close the attachment and cancel the
exec process, not its workload. Failure details are generic. This endpoint is
not exposed by the browser bridge; browser exec remains bounded/non-TTY.

## Public console snapshot

`GET /v1/applications/{id}/view` returns `observe.ApplicationView`: source
provenance, declared workloads/containers/mounts/ports, Services and inferred
selector relationships, routes, volume classes, config/Secret metadata and
actual sandbox/container/probe observations. VMM samples carry `/proc` RSS,
cumulative CPU time and timestamp separately from guest allocation. Missing
samples are omitted, not zeroed. Optional sandbox `guest` carries nullable
`memoryTotalBytes`, `memoryAvailableBytes`, `cpuBusyTicks`, `cpuIdleTicks`; container
`usage` carries nullable `memoryBytes`, `cpuUsec`. Both include host collection
`time` and guest-data `source`. USER_HZ ticks and cgroup CPU microseconds are
cumulative counters, not percentages. Unavailable fields are omitted. VMM RSS,
guest allocation and cgroup memory are not interchangeable or additive.
`guest_metrics_unavailable` is a redacted sampling diagnostic, not readiness
failure. Sampling has a ten-second operation deadline;
errors return a generic `view_unavailable` without private backend details.

This is an allowlist, not a redacted serialization of private IR/guest specs:
no config/env values, probe commands/headers, Secret contents/versions or private
host bind/guest paths are present. Original compiler diagnostics are not retained;
that limitation is explicit. The [UI bridge](../docs/ui.md) consumes this same
API and exposes bounded authenticated non-TTY exec and cursor-preserving events.
Workload output deliberately printed by an application is not secret-redacted.

## Asynchronous operations

`POST /v1/applications` and `down` return `202 Accepted` with an `operationId`.
The operation runs on the daemon's lifetime context, so **a disconnected client
does not cancel accepted work**. Cancellation is explicit
(`POST /v1/operations/{id}/cancel`) or happens on daemon shutdown. An operation
is `running`, `succeeded`, `failed`, or `canceled`.

## Streams and cursors

`GET /v1/events` is `text/event-stream`. Each record carries its sequence as the
SSE `id:` field; a client resumes with `Last-Event-ID` (or `?since=`). If the
requested sequence is no longer retained, the server emits a synthetic
`events.gap` record so the client can resync. `GET /v1/logs` accepts `since`,
`resource`, and `container`; with `follow=true` it streams. Log records use
persistent host `seq`, collection `time`, sandbox `resource`, `container`,
`stream` and `line` (bounded text chunks, not necessarily complete lines).
A guest-retention-loss record has `gap: true`; it bypasses container filters
within the selected resource. Clients must keep loss records visible even when
filtering stdout/stderr. Guest and host sequence numbers are independent.
Output intentionally printed by an application is not secret-redacted.

## Daemon lifetime versus application lifetime

`down` stops an application and releases its runtime resources; it never stops
the daemon. `shutdown` stops the daemon but leaves accepted applications running
so a later CLI invocation can reconnect to a new daemon and recover them.
Closing the browser or CLI does not stop workloads.
