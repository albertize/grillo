# T19 — F2 gate: Compose application

- Status: complete (real KVM evidence)
- Date: 2026-10-03
- Related tasks: T12–T18, T19
- Command: `make test-f2` (also `make test-bridged`, `make test-executor`)

## Goal

Run a complete Compose application on the real runtime and prove the F2 outcome:
web/API/database-style topology, service DNS, persistence, differential updates,
and cleanup, with no Docker daemon and no root.

## Environment

Linux/amd64, Fedora, kernel `7.2.8-200.fc44.x86_64`, UID 1000, no `sudo`,
SELinux Permissive, `/dev/kvm` API 12. QEMU 10.2.2, virtiofsd 1.14.0, pasta, and
the guest artifacts from `make t07-guest` and `make oci-guest`.

## Prerequisites and commands

```sh
make t07-guest oci-guest      # guest agent + OCI rootfs artifacts
make build                    # bin/grillo, bin/grillod, bin/grillo-netns
make test-f2                  # the gate below
```

A user runs the example with `bin/grillo up examples/f2-compose/compose.yaml`
(see that directory's README).

## What the gate exercises

`TestKVMComposeF2Application` compiles a Compose file with the real frontend and
applies it with the production rootless topology (per-application pasta
namespace, bridge, one TAP per sandbox):

- two services (`web`, `worker`) on one named network, `depends_on` ordering;
- service DNS: the worker resolves `web` through the in-guest resolver;
- cross-VM reachability: the worker fetches the web page over the bridge;
- a managed volume: the worker writes the fetched page to `/data`;
- differential update: re-applying an unchanged application is a no-op (the
  sandbox identities are unchanged);
- `down` without `--volumes` keeps the volume and removes the sandboxes;
- recovery: `up` again reuses the same volume source;
- `down --volumes` deletes the owned volume and leaves no sandbox behind.

## Evidence

```
--- PASS: TestKVMComposeF2Application (3.6s)
ok  grillo.local/grillo/internal/executor
```

The worker's diagnostic inside the guest showed the resolved configuration and
the fetched content:

```
nameserver 127.0.0.1
search default.svc.cluster.local svc.cluster.local cluster.local
options ndots:5
---
grillo-oci-localhost-ok
```

The volume directory contained `result.html` with the web page content, and the
volume source was unchanged across `down`/`up`.

## Defects found and fixed during the gate

1. **DNS records were built only for services selecting the current workload.**
   A workload could not resolve services that did not select it, so `web` did not
   resolve from `worker`. Fixed: every sandbox resolves every Service in the
   application.
2. **Volume metadata was shared into the workload.** Managed volumes exposed
   `volume.json` inside the workload's mount. Fixed: only `<volume>/data` is
   shared; metadata stays outside.
3. **Externally launched VMMs were tracked by a namespace-local PID.** The pasta
   supervisor runs in a PID namespace, so the QEMU PID it returns has no host
   `/proc` entry. The backend tracked an unrelated host PID or failed on
   re-launch. Fixed with `sandbox.VMM`: an external launcher returns a handle
   that reports liveness and stops the VMM through the supervisor
   (`netns.VMM`), and the backend no longer assumes a host PID.
4. **`test-bridged` isolation assertion was order-dependent.** It fetched a
   nondeterministic sandbox address. Fixed to compare against the other
   application's own address deterministically.

## Limitations

- One application has one network. Multiple distinct named-network topologies are
  rejected by the Compose compiler (`compose.multi_network`) rather than silently
  flattened.
- The gate uses prebuilt images; Compose `build:` is supported by the native
  builder but is not yet wired to run automatically from `up`.
- `down` preserves volumes by default; `--volumes` deletes only owned volumes.

## Conclusion

F2 is met for the Compose subset: a clean example runs without root, resolves
services by name, persists data, updates idempotently, recovers, and cleans up,
with no Docker daemon. The KVM evidence is attached above.
