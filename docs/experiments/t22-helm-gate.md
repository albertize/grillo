# T22 — Helm runtime gate and compatibility reporting

**Status: DONE (2026-10-06).** The full runtime scenario C and demo pass after
T12 repair, and the actual CLI/daemon chart lifecycle now passes after T16's
explicit capability policy. Historical failures below remain part of the evidence.

## Reproduce

Explicitly provision the existing documented QEMU/virtiofsd, pasta, guest kernel,
runc and busybox OCI fixture, plus official Helm v4.2.2. No tool installation,
download, privilege escalation or host configuration is performed by this gate.

```sh
make test-helm                  # rebuilds guest; runtime and real CLI/daemon gates
make check                      # ordinary checks, without KVM
make test-k8s                   # multi-container regression
make test-f2                    # Compose persistence/networking regression
```

`examples/helm/scenario-c` contains a two-replica API Deployment with init and
sidecar containers, ConfigMap/Secret env references, readiness probes, a
Service on port 80 targeting the named container port 8080, an Ingress, and a
separate storage Pod with one RWO PVC. Keeping the PVC on one Pod avoids
pretending RWO supports writable multi-VM sharing. All secret values are
synthetic fixture data; the tests compare them privately without printing them.

The test uses the actual Helm compiler, reconciler, QEMU backend, bridged
application namespace, guest protocol and guest runc. Only image resolution
uses the preexisting busybox OCI rootfs fixture. This direct-executor portion does not prove registry pulls or digest freezing.
The additional actual daemon/CLI gate below proves the chart apply path using a
locally built/cached native image.

The workspace has a short private, unique path to fit Linux Unix socket limits.
Initial long `t.TempDir()` paths exceeded the virtiofs socket limit. Cleanup
now covers attempted sandbox IDs as well as committed reconciler observations,
reports failures, and has a separate deadline. No preexisting resource is removed.
Production long-path diagnostics and partial-apply recovery remain separate
remediation concerns, not a passed acceptance claim.

## Historical initial hardware failures

Linux/amd64, kernel `7.2.8-200.fc44.x86_64`, UID 1000, Go
`go1.26.8-X:nodwarf5`, official Helm v4.2.2, SELinux Permissive. These are
single-host acceptance tests, not a benchmark or clean-host support claim.

Initial pre-repair `make test-helm`: **FAIL**, Make exit 2, no hardware skips.
`TestKVMHelmScenarioC` took 31.13 s; the simple demo test passed in 3.32 s.
The rebuilt guest initramfs SHA-256 for this run was
`86daf5760176539fc66a5cf043907dc224b8829feb3ca223e6506ce6e6412c69`.

| Check | Result | Actual evidence |
| --- | --- | --- |
| Three Pods, three VMs | PASS | Two API replicas plus one storage sandbox |
| Init / sidecar / localhost | PASS | Init file shared within each API Pod; sidecar fetches local API |
| ConfigMap / Secret env | PASS | Config response and private secret comparison inside the container; public IR excludes the value |
| Identical apply | PASS | Guest kernel boot IDs remain unchanged, not merely stable resource keys |
| Config-only update | PASS | API VM boot IDs change; storage VM boot ID stays unchanged; new env value is visible |
| Secret-only update | PASS | Random private versions; only consuming API VMs recreate; updated secret is checked privately |
| PVC down/up | PASS | A row created before updates survives down/up |
| Explicit deletion | PASS | No sandboxes or owned volume records remain after down with volumes |
| ClusterIP DNS | FAIL | A lookup returns Pod addresses, not a Service VIP |
| Service targetPort | FAIL | Guest wget to declared Service port 80 exits 1 (container serves 8080) |
| Ingress endpoint | FAIL | Runtime route has no published loopback endpoint |
| Readiness removes/restores endpoints | BLOCKED / FAIL | Positive ready-Service prerequisite fails; negative filtering is not claimed; restored Service never becomes reachable |

The initial gate also failed before booting the second API replica because the
runtime gave both Pods the same `emptyDir` identity. This defect was repaired:
ephemeral identity includes application, volume and sandbox; containers within
one sandbox still share it. Teardown consults persisted owned volumes rather
than the new desired template, propagates errors, preserves managed/PVC data,
and deletes only the sandbox's scoped ephemeral data. Unit tests cover replica
isolation, same-Pod sharing, recreation, foreign owners, competing leases and
unreadable inventory. Real `make test-k8s` and `make test-f2` regressions pass.

## Reporting results

The registry snapshots are generated from the actual Compose/Kubernetes
validator maps, including security-context and rollout rules:

- [Machine-readable registry](../compatibility-registry.json)
- [Generated Markdown matrix](../compatibility-registry.md)

```sh
go run ./scripts/compatibility -output docs
go test ./scripts/compatibility
```

Ordinary CI rejects drift, nondeterminism, duplicate entries, missing metadata
and missing fixture/suite files. These entries are compiler classification,
not an exhaustive field inventory or per-feature hardware evidence. A registry
without a declared default explicitly says so. Helm renderer restrictions and
runtime limitations remain in [compatibility.md](../compatibility.md).

ServiceAccount/PDB kinds and metadata-only Pod/container fields now produce
explicit `VALIDATE_ONLY` diagnostics instead of disappearing silently. CLI
fixtures cover degraded rollout consent, validation-only metadata, forbidden
host networking, redaction and rejection before provisioning or secret storage.

Text/JSON plans count **2 workloads, 3 sandboxes, 5 app containers, 2 init
containers, 1 Service, 1 route, 2 volume definitions, 1 ConfigMap and 1 Secret**.
Counts distinguish topology from action counts and include replica multiplication.
Plans rejected solely by capability availability can show this structural
preview, but keep a nonzero exit, an explicit text `BLOCKED` marker and JSON
`applicable: false`. JSON retains structured field diagnostics and consequences.
Frontend/security/structural errors do not produce an actionable plan.

A second prerequisite gap was verified: `loadApplication` passes an empty
capability set to model validation. Consequently the CLI still rejects PVCs,
multi/init-container workloads and probes, even though the direct executor tests
prove them. This report **does not fix it with FullCapabilities or another
blanket bypass**. T16 must obtain an accurate runtime capability policy; known
missing Service/Ingress behavior must remain unavailable.

## Acceptance and next prerequisite

| T22 requirement | State |
| --- | --- |
| Run complete C with readiness/Service/Ingress semantics | BLOCKED on T12 runtime wiring |
| Degraded/validate-only/unsupported fixtures and text/JSON counts | Implemented and tested |
| Registry-derived compatibility data | Implemented; compiler inventory only, not exhaustive per-field runtime parity |
| Unsupported input blocks provisioning and secrets stay private | Compiler/CLI unit evidence; full production chart apply remains blocked |
| Config updates replace only consumers; PVC persists | PASS on real executor/KVM |
| Real supported Helm CLI application | BLOCKED on T16 capability reporting and T12 datapaths |

`NativeExecutor.Apply` currently treats `UpdateEndpoints` as a no-op;
`Executor.Drain` and its adapter are no-ops; guest DNS receives static Pod-IP
records. The Service registry/proxy and Ingress matcher exist as components but
are not wired to the production application namespace. Earlier T12/T16 DONE
summaries overstated those gates and have been corrected to BLOCKED.

Next: restore the T12 prerequisite (production VIP/targetPort/Ingress, draining,
readiness-driven endpoint updates and live DNS behavior), then fix T16's
capability validation without bypassing unsupported features, and rerun
`make test-helm`. Reassess dependent historical completion claims against these
new findings. Do not mark T22 DONE, skip its failing subtests, or construct a
test-local proxy to turn the gate green.

Final verification: `make check` PASS; `go vet -tags kvm ./internal/executor`
PASS; the selected simple Helm, Kubernetes and Compose hardware regressions
with `-race -count=1` PASS (12.09 s total). `git diff --check` and URL-decoded
local Markdown links PASS. Process/workspace inspection found no remaining
QEMU, virtiofsd, grillo-netns or owned T22 workspace. These checks do not override
the failing full `make test-helm` gate.

Hosted CI, a second clean host, public registry pulls, full browser smoke tests
and longer fuzz/benchmark campaigns were not executed in this task.

## T12 production revalidation — 2026-10-06

Extended `make test-helm`: **PASS**, no skips, scenario C 16.26 s and demo
2.01 s. Guest SHA-256:
`9bdbd619f1f08f6e33a7e169c8e7ce0e009dd5a3d11201529209a914d3a7cd75`.
The production runtime now proves all four DNS aliases, distinct Service VIPs,
named targetPort, two-replica round-robin traffic, ready headless Pod addresses
and named SRV target ports, published Ingress, readiness removal/restoration,
actual host-loopback TCP publication/release, consumer updates and PVC retention.
No test-only proxy substitutes for namespace datapaths.

`make test-f2 test-k8s` PASS (3.35 s / 1.22 s), guest SHA-256
`7ee991443b053deb5c3aaa40522249a1f81e7a0c50fd56c89729954d7a7185ee`.
The first Compose regression failed: an implicit Compose Service with no
virtual ports cannot be a ClusterIP. Explicit headless DNS restores Docker-style
name-to-container semantics, including undeclared image ports. The first new
short-name nslookup check also failed because BusyBox tried additional search
suffixes; absolute queries now verify each alias, while actual guest wget still
proves search-based short-name resolution. Earlier malformed nftables syntax
and missing test imports were fixed before these passing runs.

`make test-bridged` PASS (5.75 s): Pod-to-Pod and Service-VIP traffic, egress
and application isolation. Its prerequisite refreshed the cached OCI bundles;
this is not new live-registry pull evidence. Guest SHA-256:
`9f1526358f6f4ef333ae38e635d855f43a10016b9d79a4f3ec791c2b7fd250cc`.
Extended Helm, Compose and Kubernetes KVM regressions with `-race -count=1`
PASS (24.357 s total).

`make check` and component race tests PASS. DNS UDP/TCP forwarding, private-zone
non-forwarding, malformed-target rejection and bounded idle-client shutdown have
socket-level unit evidence; no live Internet DNS or daemon-crash continuity claim
is added. T16 still rejects the full chart with its empty CLI capability policy,
so at this intermediate point T22/F3 remained **BLOCKED** despite the complete
direct-runtime gate. The final T16/T22 acceptance below supersedes that blocker.

## Final shared-policy and CLI/daemon acceptance

`make test-helm` now runs both executor gates and `TestKVMDaemonRemediation`
with its real `helm-cli-plan-up-inspect-down` subtest. Final PASS, no skips:
scenario C 16.23 s, demo 1.97 s; daemon 19.44 s, CLI chart 9.06 s. A whitelist
shared by CLI/executor/daemon enables only implemented features, conditional
on bridged networking for routes/host ports. Privileged, StatefulSet/Job and
unknown features remain unavailable; unsupported runtime semantics still block
before secrets or desired state are written. Offline `applicable: true` means
implementation-compatible, not a host feasibility or availability guarantee.

The actual native builder caches an OCI image assembled from the pinned fixture.
Separate real CLI processes plan, apply, inspect, reapply and down the chart.
After CLI exit, five regular containers and two completed init containers remain
visible through the real API. Inspect exposes an actual localhost route; storage
fetches the port-80 Service. Explicit volume deletion succeeds. A subsequent
real daemon crash/recovery keeps the stopped chart stopped. No kube-apiserver,
host container engine, test-only proxy or capability bypass is used.

The first attempt failed on Linux's 108-byte socket limit and a preexisting
cleanup channel wait. Test-owned surviving VMMs and helpers were stopped through
private supervisor RPCs. Short private `/tmp/g16-*` workspaces, close-on-exit
channels and cleanup registration before `up` fix the fixture; production
long-path diagnostics remain a known limitation. Follow-up failed assertions
were corrected for completed init-container and stopped-application inventories.
Final rebuilt guest SHA-256:
`a5048f7e2177a2cf62bd84b86ddd0297c69547456a61cef2a2495ffa55feb177`.
Final `make check`, KVM-tagged vet and `git diff --check` PASS; actual daemon gate
with `-race -count=1` PASS (20.491 s). Process inspection found no remaining
owned VMM/virtiofsd/network helpers.

T16/T22 are DONE; F3 still requires T23/product gates. No second clean host,
hosted CI, live pull or full browser proof is claimed.
