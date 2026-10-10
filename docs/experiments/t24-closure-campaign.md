# T24 — verified lifecycle and dependency hardening

Date: 2026-10-09. **T24 and D0 remain BLOCKED; not a closure or release claim.**
Dependency T23 and the local D0 contracts are verified by their existing reports.
This bounded continuation addresses current dependency advisories and the missing
production verified-boot lifecycle measurements. Prior user changes are retained;
no commit, push, publication, privilege or host-policy modification occurred.

## Changes

- Add `make test-t24-verified-lifecycle`, consuming a freshly rebuilt runtime-only
  guest and its portable inventory. It measures the actual verified snapshot /
  fresh-key overlay path, unlike historical fixture-key lifecycle measurements.
- 100 warm create/authenticated-start/inspect/idempotent-start/stop/delete cycles;
  per-boot credentials checked for uniqueness without printing keys or their hashes.
  Strong VMM identities and empty owned backend directories checked every cycle.
  Failure cleanup deletes only test-created handles, not preexisting workloads.
- Record nearest-rank median/p95 phase time, VMM RSS/PSS just after authentication,
  and owned regular-file logical bytes while running. Pure regression tests cover
  invalid/missing/duplicate/overflowed PSS, percentile boundaries, mutation,
  symlinks, cancellation and socket handling. Missing PSS is not fabricated as zero.
- Update the existing official `golang.org/x/net` pin from v0.59.0 to v0.60.0;
  preserve module path, Go version and other dependency pins. `go mod tidy`,
  module checksum verification and complete checks pass. This is an upstream
  module fix, not custom cryptography or a new framework.

## Advisory inspection

Initial `make vulncheck` passed at symbol level but reported five **module-only**
advisories in x/net v0.59.0. Explicit verbose inspection identified
GO-2026-6617, GO-2026-6612, GO-2026-6611, GO-2026-6610 and GO-2026-6603,
all fixed in v0.60.0. No imported affected packages or reachable affected symbols
were reported. The pin was nevertheless updated for dependency hygiene.
Post-update `make vulncheck`: **No vulnerabilities found**, not merely no reachable
symbols. This is the current advisory snapshot, not an independent security audit.
`npm audit --json`: zero known advisories across all reported severities.

## Actual checks

```sh
make vulncheck
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -show verbose ./...
(cd web && npm audit --json)
make fuzz FUZZTIME=60s
GOTOOLCHAIN=local go get golang.org/x/net@v0.60.0
go mod tidy
make check
make vulncheck
make test-t24-verified-lifecycle
make test-installed-runtime HELM_BINARY=/usr/bin/helm \
  HELM_SHA256=deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d
```

The lifecycle target passed twice without SKIP, 100 cycles per complete run.
The final run used the rebuilt post-update guest store entry
`109ee27163346da870cf39bf27ef815245faa7873bcbb4619d6dd07b47e1a9dd`.
The prior entry `87c55caaef1043a47fa4975ba5f5b0892319a5a208cdbbaab47b98b54625ae82`
was retained. Installed Compose/Helm/UI passed again without SKIP with the new
entry; HTTP/DNS, read-only relocation, build-tool-free runtime PATH and process/
resource cleanup assertions passed. Final installed gate: 5.78 seconds.

Environment: UID 1000, Fedora fc44/Linux 7.2.9, QEMU 10.2.2, virtiofsd 1.14.0,
Go 1.26.8-X:nodwarf5. SELinux was already Permissive and remained unchanged.
These are local development-host observations, not independent clean-host proof.

### Production-path warm observations (final run)

| Measurement | Samples | Median | p95 |
|---|---:|---:|---:|
| Create metadata | 100 | 68 µs | 92 µs |
| Verified-copy/overlay start to authenticated agent | 100 | 419,497 µs | 422,478 µs |
| Stop/delete | 100 | 38,138 µs | 46,276 µs |
| VMM RSS after authentication | 100 | 160,047,104 B | 161,697,792 B |
| VMM PSS after authentication | 100 | 148,180,992 B | 149,750,784 B |
| Owned regular-file logical bytes while running | 100 | 20,643,610 B | 20,643,610 B |

100 cycles completed in 46.30 seconds, with zero retained VMMs or backend entries.
Logical bytes exclude directory/socket inodes, physical allocation, image cache
and unrelated user data. RSS/PSS are a point-in-time sample, not peak or idle
system/daemon overhead. This agent-only boot has no application, volume-sharing
helper, namespace networking, pull/unpack or readiness cost. No full-resource
budget, cold-start, throughput or regression-threshold claim follows.

First measurement attempt FAIL: the collector rejected the real QMP socket as
an unsafe entry. Fixed to explicitly ignore socket payload bytes while still
rejecting symlinks/other special files; positive socket and negative symlink
regressions passed before both complete runs. The failed run's owned VM was
cleaned up; no success marker overrode the failure.

### Fuzz campaign (before dependency update)

Five targets, 60 seconds each, two workers; all processes exited successfully.

| Target | Executions |
|---|---:|
| ReadFrame | 2,946,789 |
| ReadMessage | 2,667,037 |
| Kubernetes Compile | 33,408 |
| DNSRespond | 212,423 |
| UnpackLayer | 138,935 |

Kubernetes/DNS/OCI counters stalled during portions of the campaign. These
successful exits do **not** prove per-input latency bounds or sustained exhaustion
resistance. No failing corpus was reported. This campaign is not post-update
coverage or a full adversarial/load/quota matrix.

Subsequent [OCI extraction budgets/CAS correction](t24-oci-extraction-budgets.md)
adds finite image-work quotas, cancellation and repeated concurrency regressions.
It does not close global disk/operation quotas, slow-input or other gates below.

## Closure requirements still open

| Gate | Current disposition |
|---|---|
| T23 / local §28 runtime and surfaces | Existing evidence, installed smoke revalidated; limitations remain explicit |
| Current dependency advisory scan | PASS after official module update; independent audit not claimed |
| Warm verified boot/fresh keys/resource cleanup | PASS here, not full application/system budgets |
| Complete cold/warm startup, idle CPU, peak memory/disk and DNS/proxy/load budgets | Still incomplete; this bounded collector does not replace them |
| Sustained adversarial/fault/quota/slow-input review | Still incomplete; fuzz stalls must not be called latency evidence |
| Clean Linux runtime host outside development environment | BLOCKED / explicitly deferred by user: no second host available; user will run these checks later |
| Hosted CI for these uncommitted changes | NOT RUN: no publication/workflow execution claimed |
| Third-party notices, complete corresponding sources and provenance | Pending release/maintainer review; hashes are not publisher authentication |
| Verifiable release delivery/support/security-response decisions | Pending; local staging is not official release installation |
| D0 formal acceptance | Depends on T24 and its own guest redistribution/provenance decisions |

The plan says every §28 requirement needs evidence or an explicit blocker, but
also requires the external-host test and says blockers do not erase gates.
Recording blockers is traceability, not authorization to set DONE.

**User scheduling decision:** No second host is available. The user explicitly
plans to perform the external-host checks later. This defers execution, not the
acceptance requirement, and does not defer independent local implementation.

**Next task:** Complete remaining local quota/load/performance work and
redistribution/provenance review. Retain external-host reproduction as a pending
user-run gate, then review T24 closure before D0. Never relabel this same-host
prefix test as external evidence.
