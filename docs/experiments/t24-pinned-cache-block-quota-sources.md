# T24 continuation: pinned OCI reuse, real volume quotas, local budgets and sources

Date: 2026-10-12. **T24/F3/D0 remain BLOCKED.** This is local bounded evidence,
not complete workspace enforcement, redistribution clearance or a supported
release. It preserves the [previous campaign failure](t24-autonomous-local-campaign.md).

## Environment and scope

Fedora 44 Linux/amd64, normal user, host kernel `7.2.9-200.fc44.x86_64`, Go 1.27.2,
QEMU 10.2.2, virtiofsd 1.14.0 and `/usr/bin/mke2fs` 1.47.3. Real KVM/vsock;
no sudo, device/policy changes, package installation, credentials, commit or
publication. Existing experimental dependency/guest artifacts were rebuilt.
Latest installed selection:
`36e21eb3679ee4e18600481ba37eaca7715edced904b6775ffb83f88eacca5a1`.
The checkout includes uncommitted campaign changes; no clean-release revision
is claimed. Logs remain under `/tmp/grillo-t24-continuation/` plus the unique
budget receipt `/tmp/grillo-t24-budget.TXE6x1`; no private log contents or
credentials are copied into this report.

## Docker Hub and verified offline reuse

The explicitly requested live provisioning succeeded for:

```text
docker.io/library/busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e
```

Fixture: `experiments/artifacts/t24-fixture-20261011`. The helper uses the real
registry client, verifies the explicit index/manifest plus config/layer digest
and size, records provision metadata and publishes only to a previously absent
output using no-replace rename. No account credentials were supplied/reused.
Existing output and symlink-parent rejection happen before network access.

`Pull` now saves the verified index too. An explicit digest can reuse a complete
verified CAS graph offline; mutable tags still require registry access. Missing
or corrupt content, unsupported platform, descriptor mismatch, special files,
symlinks and cancellation fail explicitly. Local mutable image labels cannot
replace a requested digest. Config/local metadata reads are capped at 8 MiB.
`CopyPinned` transfers only the verified graph; a source fixture is never treated
as authoritative merely because a filename resembles a digest.

```sh
make prepare-t24-fixture T24_FIXTURE_OUT=/new/explicit/output
make test-t24-resources T24_SEED_CAS=/verified/fixture/cas \
  HELM_BINARY=/usr/bin/helm \
  HELM_SHA256=deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d
```

A live fixture pull is evidence for that pull only. Seeded measurements are
**not fresh registry-pull latency**, page-cache-cold behavior or release approval.
This deliberately avoids repeatedly consuming the anonymous Docker Hub allowance.

## Real volume-write enforcement

[ADR 0014](../adr/0014-fixed-size-block-volume-quotas.md) is **Proposed**.
`OpenBounded` creates fixed-geometry raw ext4 images with aggregate reserved-byte
and image-count admission, private cross-process locks, bounded directory scans,
interruption accounting and no implicit migration/eviction. The executor uses
virtio-blk and guest ext4 mounts instead of writable host-directory shares for
these volumes. Bind paths remain intentional, unbounded virtiofs sharing.

Daemon flags: `--volume-image-bytes` (0 disables; opt-in non-bind volume default),
`--volume-reserved-bytes` (64 GiB), `--volume-images` (256), `--mke2fs` (explicit
trusted absolute formatter or reviewed PATH default). Minimum image size 16 MiB,
4096-byte alignment. Requested capacity is geometry, **not usable file payload**.
Changing existing backing/capacity requires explicit migration; no auto-resize.
Supply this opt-in policy again at daemon startup/restart; default CLI on-demand
startup does not persist custom flags and refuses block records in directory
mode. Persisted configuration/doctor/UI adoption remains pending.

The real production-executor KVM test writes more than a 32 MiB filesystem can
hold and gets guest `ENOSPC`; the host image remains exactly 32 MiB. Removing
the large file permits another write, and token/retry files persist across a
replacement VM. Explicit owned volume removal and empty owned backend state are
checked. Failed test evidence is retained, not automatically deleted while a
VMM might remain. This proves actual guest-write enforcement, not a directory
reservation counter.

```sh
make test-t24-volume-quota
make test-t24-volume-load
```

Both PASS. The load target runs 30 race-detector repetitions of block tests,
including 12 competing managers (two admissions/ten refusals at the configured
64 MiB aggregate limit), retry after deletion, a real child-process lock/cancel
check, formatting failure, interrupted stages, invalid geometry/paths, leases
and legacy preservation. Formatter failure stubs prove failure handling only;
the separately rebuilt KVM test proves ext4 behavior.

This is **not** whole-host disk isolation/free-space reservation. Images are
sparse; other host data and filesystem metadata are outside the geometry sum.
The current experimental images disable journaling: clean sync/restart passed,
not arbitrary crash/fsck/repair acceptance. Native build `RUN` workspaces still
use writable virtiofs and remain **unbounded**. Host `COPY`/guest `RUN` coherence,
bounded authenticated export/extraction and staging need a separate complete
workspace path; monitoring bytes after writes would not satisfy the requirement.

## Candidate local resource budgets

Two complete rebuilt seeded 30-repeat resource batches passed before the final
Make budget-target run. `make test-t24-resource-budget` then rebuilt/reran another
**30/30 installed tests**, no SKIP, and applied the candidate local checker:

```sh
make test-t24-resource-budget T24_SEED_CAS="$PWD/experiments/artifacts/t24-fixture-20261011/cas" \
  HELM_BINARY=/usr/bin/helm \
  HELM_SHA256=deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d
```

| Latest profile (30 each) | Median HTTP readiness | Nearest-rank p95 | Candidate p95 bound |
| --- | ---: | ---: | ---: |
| Seeded CAS, fresh rootfs | 0.836 s | 0.840 s | 2.0 s |
| Seeded CAS, warm rootfs | 0.737 s | 0.741 s | 1.8 s |

Other candidate caps apply to **every observed sample**: sampled owned-process
RSS sum <=256 MiB, logical and allocated owned-directory bytes each <=700 MiB,
idle CPU <=8 USER_HZ ticks over the validated 0.9–2.0 s idle window. Here
`getconf CLK_TCK` returned 100; raw ticks are recorded, not portable percentages.
Maximum sampled RSS was 232,710,144 bytes (fresh) / 232,255,488 bytes (warm),
maximum allocated bytes 582,983,680, maximum idle ticks 4. The
[complete scalar receipt](t24-seeded-resource-budgets.json) includes all budgets
and median/p95/max definitions.

The checker rejects failed/skipped/incomplete receipts, incorrect profile/count,
missing completion, invalid windows and budget violations. Its original Go
completion parser incorrectly required exactly one tab; the real receipt exposed
that bug. It was corrected to accept Go's space/tab separator and regression
covered; the initial checker failure is not silently described as a pass.

These are **candidate local regression budgets**, not approved product SLOs,
complete load/scalability acceptance or instantaneous peak/PSS measurements.
RSS samples are 100 ms apart; disk observations are phase snapshots, not peak
write pressure. No cache flush, pinning of host background activity, guest-network
throughput budget or multi-application memory/disk saturation claim is made.
Existing loopback DNS/TCP observations remain separate from guest networking.

## Corresponding-source and provenance inputs

Sources are locally retained in `experiments/artifacts/t24-sources-20261011`.
The read-only, non-fetching [inventory](t24-component-source-inputs.json) records
19 inputs plus 83 Rust crate archives; `scripts/source-inventory.py` recomputes
hashes and verifies crate checksums against the provided Cargo.lock. It executes
no archive/build/package code and explicitly does **not** give legal clearance.

- **Helm:** Fedora `helm-4.2.2-1.fc44.src.rpm` SHA-256
  `264ef660fe91f3a3c9eb5340b15934ed6517155b04941655e4c65107d56faee0`;
  `rpmkeys --checksig` reports valid digests/signature using existing Fedora
  keys. Extracted upstream source, vendor archive, spec and license-generation
  config were reviewed as data, not executed. Spec pins
  `b05881cf967a5a09e19866799d0edfd40675803a`; vendor/modules.txt has 172 module
  entries, with 205 license/copying/notice-like files in the vendor archive.
  Counts do not prove each license's redistribution conditions were met.
- **runc:** complete upstream source/vendor archive at
  `29dd3dc2b13b4123162e5fe132504bb4b15569f1` (the fixture's 1.5.2 revision),
  reviewed Dockerfile/Makefile/go.mod/vendor/modules.txt/LICENSE. Libseccomp
  2.6.1 and libpathrs 0.2.6 source archives were collected, and all 83 registry
  archives in the provided pathrs Cargo.lock match their checksums. These are
  dependency/build-recipe inputs, not proof of exact static native sysroot,
  compiler/container provenance or byte-for-byte reproduction of the release.
- **BusyBox:** 1.37.0 source SHA-256
  `3311dff32e746499f4df0d5df04d7eb396382d7e108bb9250e7b519b837043a4`, matching
  `latest-1/glibc/Dockerfile.builder` in docker-library revision
  `066dbf90090ab2dbb905310fac2fa10ea39a7ba6`; the archive contains the recipe,
  config and `no-cbq.patch` / `sha1_process_block64_shaNI.patch`. Do not use
  `latest/glibc` (1.38.0) as this image's recipe. The recipe archive's amd64
  image-manifest/config/rootfs SHA-256 values exactly match the selected graph:
  `66a6306db78bf2dbf3487f293aa8d6990d8e506fdffab9cc43fe422becf886e4`,
  `30ecbe1509090b38ef3260f3609d64d8261230464eaa4d235901db496e9647fb`,
  `68fe9bff2ad4ad46da3e58db03faa64eb73c0ff16d546b0c42539eaa9854cb7f`.
  Detached signature verifies at fingerprint
  `C9E9416F76E610DBD09D040F47B70C55ACC9965B` from the recipe. This old DSA/SHA-1
  upstream signature is not modern cryptographic release clearance; the
  independent SHA-256 graph/recipe correspondence is separately recorded.
- **glibc:** signed Debian `glibc_2.41-12+deb13u4.dsc`, upstream source and Debian
  patches/build recipes, binary package and copyright terms. Source archive
  SHA-256 values match the descriptor (`f24aa441021121a79266f0d75242706cab8843a47901fefe74527491807f1998`
  / `dda4153511bfd543502d18e5bc9323110996fe9c531f14ae3fad6eb17f027c7d`).
  `gpg --verify` reports VALIDSIG signing fingerprint
  `52BC8695BE34F90AD7D40CB81388C0F899E8336B`, primary
  `77462642A9EF94FD0F77196DBA9C78061DDD8C9B`; public key obtained over HTTPS
  from keyring.debian.org. A temporary artifact-local keyring was used; no
  user/system trust or CA was changed. All four fixture ELF libraries
  (`ld-linux-x86-64.so.2`, `libc.so.6`, `libm.so.6`, `libresolv.so.2`) exactly
  match the downloaded `libc6_2.41-12+deb13u4_amd64.deb` payload bytes.

Source hashes/signatures are integrity/provenance evidence, not an approved
publisher trust decision. Full staged corresponding-source/notices delivery
(including exact patched compiler/native sysroots and current Grillo inputs),
obligations review, build reproduction and independent redistribution approval
remain pending. The existing Linux source/config/Go-tree inputs and earlier
[payload review](t24-payload-notices-provenance-support.md) are not replaced by
an inventory listing. No unsupported copyright-holder/release-contact claims
or new runtime SDK dependencies were added.

## Actual checks and new security blocker

- `make check vulncheck test-t24-volume-load test-t24-volume-quota`: PASS;
  Go 1.27.2 source advisory scan found no findings. Routine check includes
  format/vet/unit/race/build, script tests, UI/module/golden checks.
- Explicit OCI/storage/executor/daemon unit and race tests: PASS. Final
  `make check vulncheck test-t24-volume-quota` rerun PASS; `make fuzz FUZZTIME=10s`
  PASS for all five targets, with zero-execution intervals still observed (not
  silently described as continuous high-throughput fuzzing).
- Installed resource batches and final local budget target: 30/30 PASS each,
  real Compose/Helm HTTP/DNS/UI/lifetime/owned teardown; no SKIP.
- Source/script JSON validation, malicious/special-file/parent-symlink tests,
  race admission and actual signatures/checksums: PASS within their stated scope.
- `go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -mode binary experiments/artifacts/t02/runc`:
  **FAIL**, scanner exit 3 (go-run wrapper 1), reports 14 vulnerabilities from
  two modules and the Go standard library.
- Same binary scan for `/usr/bin/helm`: **FAIL**, reports 15 vulnerabilities
  from four modules. Binary symbol matches are not individual exploitability
  proofs, but they block security clearance. Neither the clean Grillo source
  scan nor valid source signatures overrides these failures.

One initial seeded batch failed before daemon health because the test-created
XDG cache root had mode 0755. The test now creates its new owned `c/grillo` with
0700 **before** CAS descendants; it never chmods a preexisting user cache. That
failed prefix `/tmp/gd0-220842534` and failure log remain retained. Earlier HTTP
429/cleanup failures also remain historical evidence, not converted to passes.
No QEMU/grillod/pasta remained at the last process inspection.

## Remaining blockers / next bounded task

1. Implement and test real bounded native build workspaces, including host
   staging and coherent COPY/RUN/export, rather than a volume-only claim.
2. Replace/rebuild explicitly reviewed runc/Helm artifacts/dependencies to clear
   the reported advisories; collect exact compiler/native build lineage and
   rerun their scans. No unauthorized tool/system replacement was performed.
3. Broader power-loss/repair, multi-volume/Pod pressure, guest-network/disk peak
   and hostile/fault/load budgets; validate candidate thresholds with controlled
   independent baseline evidence. Long fuzz stalls remain documented.
4. Complete source/notices obligations and artifact publication review, hosted
   current-pin CI and independent clean Linux/KVM runtime-host acceptance.

No T24/D0 DONE, T25 start, supported release, commit or publication is claimed.
