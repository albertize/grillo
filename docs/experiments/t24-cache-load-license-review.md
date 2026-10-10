# T24 — aggregate cache policy, bounded load and license/provenance review

**Local implementation/review delivered; T24/D0 remain BLOCKED overall.**
External-host reproduction stays deferred to the user. This report covers CAS
and runtime rootfs caches, a bounded sustained local campaign, and review findings;
it is not a global host/storage quota or redistribution clearance.

## Cache policy

- CAS defaults: **16 GiB logical bytes / 200,000 entries**, shared by cooperating
  instances and processes using the same CAS directory.
- Runtime rootfs defaults: **32 GiB logical bytes / 200,000 entries** per cache
  directory. Existing roots, interrupted stages and temporary files count.
- Daemon flags: `--oci-cache-bytes`, `--rootfs-cache-bytes`, `--cache-entries`.
  Zero selects finite defaults, negative values fail. Explicit positive overrides
  are operator policy choices; they are not support/compatibility promises.
- `CacheGuard` uses an owned, private, regular, single-link/no-follow lock plus
  cross-process advisory flock. Waiting checks context cancellation. Root/ancestor
  symlinks and unsafe writable directory/lock permissions fail; no automatic chmod.
- Usage scans stream at most 128 entries per directory read and cap depth at 128,
  avoiding whole-directory sorting/allocation before the entry limit is checked.
- Writers serialize within each family, scan existing usage under the lock and
  refuse exhaustion with `ErrCacheQuota`. CAS limits writes including unknown-size
  streams before exceeding available bytes. Failed current temp files are removed;
  interrupted preexisting files are counted but never silently deleted.
- The global family guard replaces the old unbounded per-digest lock map;
  independent resolvers deduplicate publication without retaining those keys.
- Rootfs extraction receives remaining byte/entry budgets and verifies total usage
  before publication. Rejection removes only its unique stage. An existing active
  root is never evicted. Concurrent resolvers recheck completion after admission.
- Explicit CAS prune participates in the same lock and can free data even when
  already over quota. There is **no automatic eviction**, volume deletion, host
  filesystem modification or privilege escalation.

Logical bytes are not physical allocation: directory inodes/blocks, sparse-file
allocation and filesystem metadata are not accounted. Symlinks are counted without
following targets. Rootfs temporary metadata/implicit directories/link payload can
exceed the final-entry allowance before the post-extraction check; over-quota roots
are not published. Same-UID direct writes/noncooperating tools bypass advisory
policy. Reads of old completed roots are not retroactively audited.

Native-build intermediate workspaces, Helm chart cache, guest build-store
retention, workload volumes and general user data are **not** governed by these
family limits. A single whole-XDG/global-host quota is not claimed. Existing active
rootfs GC remains unimplemented; do not manually delete roots used by workloads.
Configure quotas deliberately or stop workloads before any separately authorized
cleanup. Per-image extraction limits from the preceding report remain in force.

## Actual verification

```sh
go test ./internal/oci ./internal/executor ./internal/build ./cmd/grillod
make check
go test -race -count=100 -run 'TestCASQuota|TestCacheGuard' ./internal/oci
go test ./internal/oci \
  -run 'TestCacheGuardCrossProcess|TestCASQuotaSustainedConcurrentInstances' -v -count=1
make test-cache-load test-license-inventory
make test-installed-runtime HELM_BINARY=/usr/bin/helm \
  HELM_SHA256=deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d
GRILLO_TEST_HELM_BINARY=/usr/bin/helm \
  GRILLO_TEST_HELM_SHA256=deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d \
  GRILLO_TEST_GUEST_SELECTION=experiments/artifacts/runtime-selected.txt \
  go test -tags kvm -count=10 -v -timeout 600s \
    -run '^TestKVMInstalledRuntime$' ./internal/distribution/
```

Final checks PASS. Eight independently opened CAS instances compete for a 128-byte
policy: one 800-write run accepts 14 unique 9-byte blobs, rejects 786, retains
126 logical bytes, and leaves no failed temp files. Repeated race suites perform
**80,000 attempted writes** across 100 campaigns (first 24.54-second run), checking
usage, publication, cancellation, retained interrupted temps and explicit prune/reopen.
Final `make test-cache-load` PASS: OCI suites 150.19 seconds including 100 child
process lock tests and race-detector exit overhead; independent rootfs resolver
suites 4.91 seconds. Do not interpret that elapsed time as active I/O throughput.
A child process proves the held lock cannot be bypassed by another process.
These are bounded contention/saturation observations, not multi-hour endurance
or a complete adversarial availability/throughput benchmark.

Final rebuilt installed gate PASS without SKIP (7.43 seconds), including after
removing the old per-key lock map. The earlier ten-cycle installed campaign also
PASS without SKIP in 75.28 seconds: **20 real KVM workloads** via moved/read-only
prefixes, fresh private OCI caches, actual Compose/Helm HTTP/DNS and embedded UI,
CLI/UI-independent lifetime and per-operation process/backend cleanup.
Runtime guest identity:
`109ee27163346da870cf39bf27ef815245faa7873bcbb4619d6dd07b47e1a9dd`.
Fedora fc44/Linux 7.2.9, UID 1000, QEMU 10.2.2, SELinux already Permissive unchanged.
No benchmark threshold, Enforcing or independent-host claim. Final formatting,
changed/new Markdown local links/fences (18 files) and `git diff --check` PASS.
Final process listing contained no QEMU/grillod/netns/pasta processes.

Initial unit run FAIL: the earlier failed-stage test expected an entirely empty
cache; the new persistent quota lock is intentional. Corrected the assertion to
allow **only that exact lock** and still reject every stage/completed-root leak.
A later full check FAIL exposed `os.NewFile(fd, "cache-scan")`: streaming `ReadDir`
entries used that label as the path for `Info`, producing failed lstat calls in
OCI/build/image/executor tests. Fixed the file name to the actual directory path,
not the assertions. Repeated full check, 100 race campaigns and rebuilt installed
KVM all PASS afterward. Streaming scan tests cover entry/depth and link-target
bounds. License inventory also reproduces byte-for-byte from arbitrary CWD.

The runtime fake-puller quota/reopen test preserves the existing root and proves
no eviction/publication after refusal. Real CAS-backed image extraction and real
installed fresh-cache gates provide separate evidence.

## License inventory and source inspection

Reproduce the read-only development inventory with a new explicit output file:

```sh
python3 scripts/license-inventory.py --output /tmp/grillo-license-review.json
```

The script never fetches or executes source packages, never chooses a publisher,
and refuses output overwrite. It records versions, available notice digests,
module sums and npm integrity/declared license metadata without private absolute
paths. Two Python regression tests cover symlink non-following and notice bounds.
Snapshot: [machine-readable inventory](t24-license-inventory.json).

The existing graph-only x/crypto v0.57.0 and x/text v0.42.0 sources were explicitly
materialized with `go mod download`, not added/upgraded or run. Initial `go list`
Dir-only inspection still reported them absent; corrected the audit to inspect
existing escaped module-cache directories too. All seven graph entries now have
available source/notice hashes. Official x/* LICENSE text contains BSD-style
redistribution and non-endorsement clauses. YAML preserves its LICENSE/NOTICE;
Grillo original contributions preserve Apache-2.0 LICENSE/NOTICE. The snapshot also
records actual Go toolchain version `go1.26.8-X:nodwarf5` and GOROOT notice hashes;
standard-library/vendor notices are separate from the application module graph and
must be included/reviewed when assembling binary redistribution notices.

Npm lock inventory: 47 dependency entries, declared 46 MIT / 1 0BSD; 25 optional
platform entries not installed. Installed notices and npm integrity are recorded,
not inferred for unavailable packages. Embedded generated `licenses.txt` exists.
Red Hat Display/Mono/Text notices are present under `web/licenses` (OFL); Font
Awesome Free's notice distinguishes CC BY 4.0 icons, OFL fonts and code terms.
Npm's coarse MIT labels alone do not replace these asset notices or attribution.
No unverified copyright holder or legal approval was added.

### Bundled guest/helper review findings

| Component | Observed provenance/license inputs | Remaining redistribution requirement |
|---|---|---|
| Linux 6.1.188 | Local full source tree, COPYING/LICENSES; COPYING states GPL-2.0 WITH Linux-syscall-note. Source archive SHA matches bootstrap pin `ed4d0acb1307c235230c89efc094e210e6290593f94a7e617f28b1001101a33a`; upstream URL is already in bootstrap | Assemble exact corresponding source/config/build scripts/notices alongside distributed kernel; assess modified config and source completeness. Publisher signature not verified here |
| runc 1.5.2 | Existing upstream release URL and binary SHA `599f6f94ff8c5057241eff0d54c3c74f95c34935b6457b33fe545defc61e9488` in fetch script | Source tree and complete static-binary dependency notices/build provenance not available in prepared inputs; obtain/review before redistribution |
| BusyBox/glibc selected bytes | Exact previously pinned BusyBox OCI digest plus per-file pins in runtime recipe; glibc version recorded 2.41-12+deb13u4 | Exact corresponding sources, build options and full GPL/LGPL obligations/notices not present in exported rootfs; image digest alone is insufficient |
| Bundled Helm 4.2.2 | Installed Fedora RPM `helm-4.2.2-1.fc44`; RPM license metadata lists Apache/BSD/ISC/MIT/MPL terms; local LICENSE available | Corresponding SRPM/build/dependency and source/notice completeness review; do not treat its top-level Apache notice as whole-binary licensing |
| Distro QEMU/virtiofsd/pasta | RPM names/versions/license metadata and source RPM identities inspected; `rpm -V` returned no differences for selected installed packages | They are host prerequisites, not copied into payload; reviewed RPM metadata is not an independent signature/publisher audit |

Additional exact inputs observed:

- Kernel config SHA: `cb11b58b7f1891c23a6989cc08bb047f13450e57a6f17f4cdf7cb5dc63936730`.
- Local Helm LICENSE SHA: `881467e52efeb1807406134b0ec04b1c6f52dc9dc49382713ef9ac4e0cae42f8`.
- Host source RPM identities: `helm-4.2.2-1.fc44.src.rpm`,
  `qemu-10.2.2-1.fc44.src.rpm`, `virtiofsd-1.14.0-1.fc44.src.rpm`,
  `passt-0^20261002.gcba3570-1.fc44.src.rpm`.

**Review conclusion:** No release redistribution clearance. Kernel source exists,
but the release corresponding-source bundle is not assembled. Guest runc/BusyBox/
glibc and bundled Helm need remaining source/notices review. This is an identified
blocker, not a missing test to waive or a request to invent publisher trust.
No source archives were silently fetched/executed, signing keys established,
package published or maintainer/legal approval fabricated.

## Remaining work

Whole-system/other-cache/volume policy, active-rootfs-aware GC, peak/idle/cold/disk
budgets, long-duration fault/slow-input analysis, exact third-party source bundles,
signature/trust/maintainer release decisions, hosted CI and deferred external-host
acceptance. Local CAS/rootfs quotas, bounded contention/KVM campaign and available
license/provenance review are complete **only within the stated scope**.
