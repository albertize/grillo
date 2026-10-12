# T24 — autonomous local admission, resource and security campaign

Historical 2026-10-11 snapshot: its failures below remain evidence. The
[2026-10-12 continuation](t24-pinned-cache-block-quota-sources.md) adds verified
seed reuse, opt-in block enforcement, candidate local budgets and source inputs;
workspace/security/source/release acceptance remains BLOCKED.

Date: 2026-10-11. **T24/D0 remain BLOCKED. The requested six-part campaign is
not complete.** This report records actual local work, including the final failed
resource batch; it does not replace whole-volume quotas, corresponding sources,
external-host/hosted evidence or full performance acceptance.

Dependencies verified: T23 and prior CAS/rootfs quotas, runtime-only guest and
installed-prefix contracts. Baseline `7a1f440`; source changes in this campaign
are uncommitted. No push/publication, sudo, host-policy change, user workload
termination or unsupported cleanup occurred.

## Contracts delivered

- API asynchronous apply/down/restart/build/prune admit at most **8 active
  operations**, no waiting queue. Refusal returns 429/busy/retryable/Retry-After,
  with no operation ID, callback or effects. One-hour contexts bound cooperating
  callbacks; ignoring context does not release a slot or create another worker.
- Operation history caps at **256 records**, evicting oldest finished records
  only. Returned acceptance metadata is a detached snapshot (no background-write
  race). Shutdown closes admission and cancels active work.
- Unix API admits **128 connections**, before parsing bodies; headers have a
  5-second deadline and whole request reads a 15-second deadline. Slow-body and
  slot-release tests cover refusal, no mutation, and idempotent Close. Shutdown
  force-closes HTTP connections if graceful shutdown times out. Interactive
  upgrade and SSE retain their independent lifetime semantics.
- Host TCP publication caps **128 connections/listener** and a five-minute total
  read/write connection lifetime. Excess connections close immediately. Close
  waits for the accept loop before worker Wait, then both copy directions; a
  half-close regression preserves responses after request EOF. A net.Pipe regression separately exercises a
  non-reading upstream and blocked writes against the same whole-deadline helper.
  These new
  operational limits are documented; long-lived published TCP clients reconnect.
- `make test-t24-admission`, `bench-t24-network`, `test-t24-resources` provide
  bounded repeatable entrypoints. Resource sampling is opt-in on the real
  installed runtime: fresh-cache HTTP readiness then warm restart, 100ms sampled
  owned-process-tree RSS, one-second idle CPU tick deltas and logical/allocated
  work-directory bytes. It never follows disk symlink targets or flushes caches.
- [Acceptance runbook](../t24-acceptance.md) prepares local and external receipts
  without claiming installation/release approval. API, architecture, compatibility
  and testing docs describe limits and measured scope.

## Actual checks

Environment: normal UID 1000, Fedora 44, Linux 7.2.9-200.fc44.x86_64,
Intel Core Ultra 7 155H (22 online CPUs), about 15 GiB RAM; Go 1.27.2,
QEMU 10.2.2, virtiofsd 1.14.0, pasta Fedora
`0^20261002.gcba3570-1.fc44`, Helm 4.2.2. SELinux was already Permissive and was
not modified. Explicit Helm SHA-256:
`deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d`.

| Command | Result and scope |
| --- | --- |
| `make check` | PASS before changes and in final source verification, including formatting/vet/unit/script/race/build/module checks |
| `make vulncheck` | PASS, actual pinned upstream Go 1.27.2, no vulnerabilities found; not external binary clearance |
| `(cd web && npm audit --json)` | PASS, zero reported advisories |
| `make fuzz FUZZTIME=5m` | PASS, five targets/two workers, about 25 minutes total; no failing corpus |
| `make test-cache-load test-license-inventory` | PASS; OCI repeated race suite 150.99s, rootfs 7.34s; inventory tests PASS |
| `make test-t24-admission` | PASS, final 30 repeated operation/Unix-connection/proxy campaigns; refusal/cancel/failure/retry/history and blocked-write whole-deadline regression |
| `go test -race -count=1 ./internal/api ./internal/network` | PASS after listener changes; real 15-second partial-body deadline and 128-connection refusal |
| `make bench-t24-network` | PASS, final isolated run 47.26s, 30 repetitions per benchmark, response validation |
| `make test-t24-resources HELM_BINARY=... HELM_SHA256=...` first complete batch | PASS, 30 tests / 90 real workload boots, 351.34s, before later Unix-listener changes |
| `make check vulncheck fuzz test-t24-admission` | PASS after admission changes; current advisory scan, five 5-second post-change smoke targets, final race campaigns; later deadline-helper refactor also passed `make check vulncheck test-t24-admission` |
| Same resource command after final runtime rebuild | **FAIL**, Make exit 2; one passing iteration then 29 registry HTTP 429 failures, 94.60s; not current budget acceptance |

Hardware campaign executed sequentially, rebuilding the guest/host code:

```sh
make test-t07 test-t10 test-executor test-f2 test-k8s test-helm test-ui \
  test-builder-kvm test-compose-dependencies test-t24-verified-lifecycle \
  test-installed-runtime HELM_BINARY=/usr/bin/helm \
  HELM_SHA256=deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d
```

PASS, no SKIP: multi-container/init, bind persistence/read-only, apply/liveness,
Compose persistence/recovery, Kubernetes grouping, Helm scenario C and daemon
remediation, real Chrome/CLI/UI/stdin/PTY/metrics/lifetime, native builds,
dependency health, verified boot/key rotation and installed runtime cleanup.
Latest runtime guest store identity:
`0bc94a1bbe706e001ef0c001143e77b71cbb1f2718a6e39aed0b7818f28661b0`.
100 verified boots PASS: agent-ready median 846,965 µs / p95 854,509 µs,
post-auth VMM RSS median 164,900,864 B / PSS 153,001,984 B, no retained VMMs.
These are agent-only observations, not application or full idle-system budgets.

### Fuzz counters and limitations

| Target | Final executions |
| --- | ---: |
| ReadFrame | 12,329,373 |
| ReadMessage | 10,424,763 |
| Kubernetes Compile | 1,028,755 |
| DNSRespond | 4,907,218 |
| UnpackLayer | 1,164,024 |

Long zero-throughput intervals occurred, especially OCI. Successful exit/no
failing corpus is not proof of per-input latency or sustained exhaustion safety.
The long campaign precedes admission-only changes; parsers were not changed.
Hardware campaigns precede the final behavior-preserving forwarding helper
extraction and Retry-After metadata adjustment; new code passed final unit/race
and builds, but the registry block prevented another installed hardware batch.
Ordinary race tests and the post-change smoke campaign remain distinct evidence.

## Resource observations, not complete budgets

[Machine-readable observations](t24-local-resource-observations.json) preserve
30-sample earlier batch summaries and explicit final-gate BLOCKED status.
Nearest-rank median/p95 (median uses lower middle for an even sample count):

| Measurement | Fresh private cache | Warm restart |
| --- | --- | --- |
| HTTP-ready median / p95 | 3.279s / 3.585s | 1.769s / 2.073s |
| Sampled process-tree RSS peak median / p95 | 231,759,872 / 236,249,088 B | 228,540,416 / 234,557,440 B |
| Idle RSS median | 222,576,640 B | 224,407,552 B |
| Idle CPU ticks median / p95 (about 1s) | 3 / 5 USER_HZ ticks | 3 / 5 USER_HZ ticks |
| Work-directory logical / allocated bytes median | 581,730,160 / 582,893,568 B | 581,730,161 / 582,893,568 B |

Disk includes the staged payload, caches, runtime state and test files: it is not
application volume usage or peak whole-host allocation. Summed RSS double-counts
shared memory, excludes exited workers between samples and is not PSS or an
instantaneous maximum. No guest/container/system accounting is silently summed.
Fresh cache pulls use the registry; host page caches are not cold-flushed.
No multi-replica/load, sustained guest-network, cold pull/unpack phase separation,
peak-disk or adopted regression threshold is claimed.

Final isolated loopback benchmarks with two parallel workers: DNS UDP median
7,244 ns/op / p95 8,063 ns/op; TCP 4 KiB roundtrip median 15,584 ns/op / p95
17,300 ns/op. These are Go benchmark iteration averages across 30 repetitions,
not per-request tail percentiles or guest/production throughput guarantees.
The earlier benchmark overlapped unit/audit work and is not the reported baseline.

## Failure retained and mitigation

The final repeat campaign exhausted Docker Hub unauthenticated pull allowance.
The image digest was unchanged; manifest fetches returned HTTP 429. Cleanup down
also reported failed operations for failed image resolution. Test-owned directories
were intentionally retained by the existing failed-gate policy, about 3.9 GiB in
`/tmp` after the run; no blanket removal or cleanup of preexisting resources was
performed. Read-only process inspection found no QEMU/grillod/pasta remaining.
Resource target now uses `-failfast`, preventing 29 additional failing copies on
future retries. This does not fix the external registry limit or turn the run PASS.

Do not retry this gate blindly. A future explicitly provisioned, digest-verified
fixture cache must be measured separately from a fresh online pull, and actual
fresh-pull evidence still needs a successful real registry run. The first
30-sample success and final failure are both retained; no success marker overrides
failure/cleanup. No user credentials or alternate registry provenance were guessed.

## License/provenance review

Updated [inventory](t24-license-inventory-go127.json) records available notice
hashes, module sums, npm integrity/license data and actual Go 1.27.2 toolchain.
Seven Go graph source entries remain materialized. Declared npm metadata is not
asset/source redistribution clearance. Existing Font Awesome/Red Hat notices and
staging's explicit partial-notice status remain required.

The earlier [component review](t24-cache-load-license-review.md) and
[payload provenance report](t24-payload-notices-provenance-support.md) still identify
missing exact corresponding-source/build/notice inputs for runc, BusyBox/glibc
and Helm. No source was auto-fetched/executed, no publisher identity or legal
approval invented. Kernel source availability alone does not assemble its release
source/config/build bundle. Grillo scans cannot certify external binaries, guest
kernel or old distributed archives. Review and inventory are delivered locally;
redistribution/provenance clearance remains BLOCKED.

## Open acceptance blockers

- Enforced workload-volume writes and remaining workspace/chart/build-store quotas;
  CAS/rootfs logical quotas and connection caps do not implement these. Virtiofs
  directory sharing does not by itself enforce requested capacity. This needs a
  real storage enforcement design/evidence, not a mock admission counter.
- Complete load/fault/hostile-guest/slow-input matrix, addressing fuzz stalls;
  current saturation/cancellation and 15-second slow-body tests are bounded evidence.
- Successful final resource batch, full phase/guest-network/peak-disk budgets,
  reviewed thresholds; registry 429 now blocks online reproduction.
- Exact corresponding-source/notices/provenance, publisher/release decisions.
- Hosted rerun and complete independent clean-host acceptance.

The six requested areas are not all finished. T24 remains BLOCKED rather than
mislabeling this substantial local campaign as completed product hardening.
