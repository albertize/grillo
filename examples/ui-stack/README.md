# Full-stack Grillo UI review environment

Three actual services, normally one microVM each: static web **UI**, **Nginx**
gateway and a standard-library **Go backend**. Only Nginx is published on host
loopback. Browser requests go through the gateway to UI assets or `/api/`.
No Docker/Podman/host-container execution is used by these instructions.

This is an **experimental, unauthenticated local demo**, not a supported release,
secure production service or redistribution-cleared bundle. Notes are bounded to
128 entries / 256 bytes each and live only in backend memory. Recreating the
backend loses them. Do not put credentials or private data in notes.

## Build

Prepare Grillo and verified runtime inputs first; see [development UI setup](../../docs/ui.md#reviewing-a-development-checkout).
Use an explicitly chosen private XDG environment if this review must not reuse or
recover existing daemon/application state. Local image tags belong to that daemon's
image store, so use the same environment for build, up, UI, inspect and down.

```sh
# Repository root; provisioned runtime and helper overrides already selected.
work=$(mktemp -d /tmp/grillo-ui-stack-build.XXXXXXXX)
chmod 700 "$work"
mkdir "$work/backend"
cp examples/ui-stack/backend/Dockerfile "$work/backend/Dockerfile"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false \
  -o "$work/backend/backend" ./examples/ui-stack/backend
export GRILLO_STACK_TAG=review
./bin/grillo build -t "grillo.local/ui-stack-backend:$GRILLO_STACK_TAG" "$work/backend"
./bin/grillo build -t "grillo.local/ui-stack-ui:$GRILLO_STACK_TAG" examples/ui-stack/ui
./bin/grillo build -t "grillo.local/ui-stack-nginx:$GRILLO_STACK_TAG" examples/ui-stack/nginx
export GRILLO_STACK_NAME=ui-stack
export GRILLO_STACK_PORT=18088
./bin/grillo plan examples/ui-stack/compose.yaml
./bin/grillo up examples/ui-stack/compose.yaml
./bin/grillo ui --port 0
```

Go compilation runs trusted local example source on the host; image assembly is
Grillo's native copy-only builder. Remote BusyBox/Nginx bases are pinned by digest
in Dockerfiles, not fetched/executed as host scripts. Integrity is not publisher
trust. Nginx 1.28.0-alpine is an exact recorded example input, **not an advisory-
cleared/recommended production version**. Review/maintain it and its source/license
obligations deliberately, not by silently replacing the pin.

Do not overwrite other applications/tags named `ui-stack` or `review`: pick unique
`GRILLO_STACK_NAME` / `GRILLO_STACK_TAG` for another session. Verify an available
loopback port before launching. Grillo has no global guest-CID allocator; multiple
daemons need deliberately coordinated workload/build CID ranges.

## Verify and stop

```sh
curl --fail http://127.0.0.1:18088/
curl --fail http://127.0.0.1:18088/api/status
curl --fail -H 'Content-Type: application/json' \
  -d '{"text":"Hello from the full-stack review"}' http://127.0.0.1:18088/api/notes
./bin/grillo status "$GRILLO_STACK_NAME"
./bin/grillo inspect "$GRILLO_STACK_NAME"
./bin/grillo down "$GRILLO_STACK_NAME"
```

Use your configured port if changed. After down, verify the previous URL refuses
connections. No volume deletion is necessary: this example has no managed volumes
or bind mounts. Explicit build contexts/caches remain and are not automatically
removed; prune only unused owned images deliberately. Closing the console does
not stop the application/daemon. Stop a dedicated review daemon only after its
workloads are stopped and after verifying the owned PID/executable.

## Current semantics

- Declare commands explicitly in Compose. Current executor does not derive
  container commands from these local images' OCI ENTRYPOINT metadata.
- `depends_on` is startup ordering **only**, not readiness. Nginx runs a bounded
  guest-side HTTP readiness loop for UI/backend before parsing/resolving upstreams.
  Failure ends the container rather than silently reporting a working gateway.
- Compose `expose` alone currently does not appear as ports in the public Service
  snapshot. DNS still resolves private services. Published endpoint inspection
  may be null even when HTTP works; test the known loopback URL and preserve that
  finding rather than infer that inspection is complete.
- The backend hostname can be `(none)` in this guest. Do not substitute a fake
  hostname or confuse it with a microVM/sandbox identifier.
- [Recorded local run](../../docs/experiments/t23-full-stack-review.md) includes
  initial command/DNS failures and narrow HTTP/guest-network evidence.
