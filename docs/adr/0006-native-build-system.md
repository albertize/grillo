# 0006 — Native build system with an optional Podman accelerator

- Status: Proposed (awaiting maintainer acceptance)
- Date: 2026-10-03
- Related tasks: T18, T19
- Supersedes: the Podman-only reading of plan task T18
- Amends: specification §23

## Context

Specification §23 says Grillo "should integrate with an existing OCI build engine
rather than initially implementing a full Dockerfile/BuildKit replacement", and
plan task T18 originally specified "rootless Podman build/export/import". The
first T18 implementation therefore required Podman: `grillo build` and Compose
`build:` could not run on a host without Podman or Docker.

That is a product defect. Building a local image is a core workflow, and a
microVM runtime must not depend on a container engine being installed. The
maintainer requires Grillo to build without Podman or Docker, while still
allowing Podman as an opt-in accelerator.

Grillo already requires a VMM to run anything. A build system that executes
`RUN` steps inside the same sandboxed guest used for workloads adds no new
privileged host primitives, no daemon, and no new trust boundary.

## Decision

1. Grillo ships a **native build system as the default and required path**. It
   parses a supported Dockerfile subset, assembles the root filesystem, executes
   `RUN` steps inside a Grillo microVM sandbox, and publishes a verified OCI
   image into the existing CAS and image store. It depends only on the VMM
   Grillo already needs.
2. `RUN` executes inside a **sandboxed guest** sharing the build root filesystem
   over virtiofs (option A). `FROM scratch` and `COPY`/`ADD`-only stages need no
   guest at all.
3. Podman remains available **only as an explicit opt-in backend**, selected
   with `--podman` (CLI) or `builder: podman` (API). It is never selected
   automatically and never required.
4. Docker is not supported and is not a dependency.

## Unsupported Dockerfile behavior

Instructions and features outside the supported subset are rejected with a
structured diagnostic. Grillo does not silently pass unknown instructions to a
builder. `HEALTHCHECK`, `SHELL`, `ONBUILD`, `VOLUME`, `STOPSIGNAL`, `USER`
namespace hints, and BuildKit-only frontends are out of scope for the native
builder and reported as unsupported (Podman, when opted in, may support more).

## Consequences

- The `build.Builder` contract stays; `build.PodmanBuilder` becomes one
  implementation and a new `build.NativeBuilder` is the default.
- Build output is content-addressed and verified through `internal/oci`, so it
  flows through the same image store and garbage collection as pulled images.
- Building requires the guest kernel/initramfs artifacts, exactly like running a
  workload. A missing artifact is an actionable error, not a silent fallback.
- Per-instruction caching is introduced in a bounded first version; the layer
  cache is content-addressed and safe under interruption.

## Evidence

Required before acceptance: a real `RUN` build executed end-to-end on KVM
(`make test-builder-kvm`), a copy-only build with no guest, `.dockerignore`
handling, multi-stage `COPY --from`, and a negative test that an unsupported
instruction fails with a diagnostic. Podman remains covered by the existing
`builder`-tagged tests as the opt-in path.
