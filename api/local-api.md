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
| `GET /v1/applications/{id}` | application container status |
| `POST /v1/exec` | run a command in a container and return captured output |
| `POST /v1/applications` | submit a desired application; `202` + `operationId` |
| `POST /v1/applications/{id}/down` | stop an application; `?volumes=true` deletes owned managed volumes |
| `GET /v1/operations/{id}` | operation status |
| `POST /v1/operations/{id}/cancel` | cancel a running operation |
| `GET /v1/events` | Server-Sent Events of the sequenced event stream |
| `GET /v1/logs` | log records; `?follow=true` streams them |
| `POST /v1/shutdown` | stop the daemon (not applications) |

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
`resource`, and `container`; with `follow=true` it streams.

## Daemon lifetime versus application lifetime

`down` stops an application and releases its runtime resources; it never stops
the daemon. `shutdown` stops the daemon but leaves accepted applications running
so a later CLI invocation can reconnect to a new daemon and recover them.
Closing the browser or CLI does not stop workloads.
