# T24 — bounded OCI extraction and CAS concurrency

Status: **bounded local delivery PASS; T24/D0 remain BLOCKED overall**.
External-host checks remain explicitly deferred to the user. Dependencies:
existing T09 CAS/registry, T18 native build and T24/D0 local runtime contracts.
No new module, release claim, privilege escalation or commit.

## Policy and effects

`UnpackOptions{}` no longer means unlimited extraction. It selects finite defaults:

- 4 GiB cumulative regular-file payload writes per image;
- 100,000 surfaced tar entries per image, including whiteouts;
- decompressed tar-stream budget derived from payload + 2 KiB per allowed entry
  + 1 MiB metadata allowance, capped at 8 GiB.

Explicit positive limits override defaults; negative limits are rejected. Callers
needing a larger image must explicitly configure appropriate positive policies.
These are defensive limits, not measured workstation budgets or universal image
compatibility claims. Small policies also bound decompression/metadata work.
`ErrUnpackLimit` preserves policy rejection through wrapped errors.

A single budget spans all image layers. Overwriting/deleting a file does not refund
payload writes or entry work. Whiteouts are charged before deletion. Stream bytes
include padding, tar metadata, skipped bodies and trailing expansion, not just
materialized files. The separate diff_id verification pass is bounded by the
remaining layer/image allowance before extraction, rather than hashing arbitrarily
large gzip expansion. Verification and extraction each read the layer; the budget
is not a promise of one-pass CPU cost.

Extraction drains the bounded stream after tar EOF, so gzip CRC errors and
out-of-quota trailing expansion no longer escape validation. This does not add a
strict prohibition on every concatenated tar/gzip representation.

An optional context is checked between reads/entries and passed from runtime
image resolution and native build. It cannot interrupt a reader blocked within
`Read`, synchronous filesystem writes or recursive filesystem deletion. No whole
operation wall-clock guarantee is claimed. A direct extraction failure can leave
partial files; callers must use owned staging. Runtime cache publication only
marks completion after successful extraction, removes its unique failed stage,
and permits retry; no bind paths or preexisting user data are removed.

Global cache/volume quotas, concurrent extraction admission, retroactive cache
validation and physical disk allocation accounting are **not** implemented here.
Existing completed rootfs caches are not traversed to impose these new per-work
limits. Default runtime/native builder callers now inherit finite policies.

## Tests executed

```sh
go test ./internal/oci ./internal/executor ./internal/build
make check
go test ./internal/oci -run '^$' -fuzz '^FuzzUnpackLayer$' \
  -fuzztime=60s -parallel=2
go test -race ./internal/oci \
  -run 'TestCASFetchDeduplicates|TestCASAdmittedLeaderRechecksPublishedBlob|TestUnpack' \
  -count=100
make test-installed-runtime HELM_BINARY=/usr/bin/helm \
  HELM_SHA256=deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d
make test-builder-kvm
```

Final complete `make check` PASS: frontend/fmt/vet/unit/race/build/module checks.
100 repeated selected race suites PASS. Tests cover finite defaults, oversized
headers without allocating huge payloads, negative quotas, exact byte boundaries,
aggregate layers, overwrites, whiteouts, bounded diff_id/gzip expansion, invalid
CRC, cancellation and failed-stage cleanup/retry. Cache publication tests use a
fake puller to prove the failure contract; actual quota enforcement is tested
separately with real CAS-backed compressed layers.

The first full check **FAIL**: `TestCASFetchDeduplicates` observed two downloads
instead of one. Inspection found a window between the optimistic `Has` and leader
admission after an earlier flight publishes/retires. `fetchOnce` now rechecks after
admission before opening the downloader. A deterministic admitted-leader test and
100 repeated race suites cover the correction. No fallback or assertion weakening.
The successful retry does not erase the original failure.

OCI fuzz PASS: 539,090 executions, two workers, 60-second campaign. Counters still
stalled during parts of the run. This is not per-input latency, sustained DoS or
full load evidence; no claim that the earlier fuzz-stall blocker is resolved.

Real KVM checks PASS **without SKIP**:

- Installed runtime-only guest entry
  `109ee27163346da870cf39bf27ef815245faa7873bcbb4619d6dd07b47e1a9dd`:
  relocated/read-only CLI/daemon/helper/Helm, fresh OCI cache, actual Compose/Helm
  HTTP/DNS and embedded UI lifetime; teardown checked. Final gate 5.89 seconds.
- Rebuilt separate T07 fixture: guest RUN, native RUN, multistage build and guest
  networking all pass. Fixture digest
  `8f00eeeb1ccc1725920a5572cafb13d78f82c340db077075e93e5db7a6c45e6f`.
  This legacy fixture is not the distributable/runtime-only base.

Same Fedora fc44/Linux 7.2.9 development host, UID 1000, QEMU 10.2.2,
SELinux already Permissive and unchanged. No independent-host, Enforcing,
license/provenance, cold/idle/peak-system or physical-disk budget acceptance.

Subsequent [aggregate cache/load/license review](t24-cache-load-license-review.md)
adds CAS/rootfs family admission and contention tests; the exclusions below remain
explicit rather than a complete whole-host quota claim.

## Next bounded work

Global cache/volume and concurrent-operation quota/admission policy, sustained
fault/load and slow-input review, complete performance budget collection and
third-party source/notice/provenance review. External-host reproduction remains a
pending user-run gate, not a reason to stop independent work or claim DONE.
