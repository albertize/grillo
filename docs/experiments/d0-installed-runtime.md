# D0 — runtime-only guest and installed-prefix lifecycle

## Status and scope

**Local gates PASS; D0 remains BLOCKED overall.** T24 acceptance is not waived.
This delivers the next independent D0 contract after
[layout](d0-runtime-layout.md) and [portable inventory](d0-portable-manifest.md):
a fixture/key-free base, coherent local staging and actual installed CLI/daemon
Compose/Helm/UI lifecycle. It is not release publication, supported-host,
clean-host, license/provenance or package-manager acceptance.

## Contracts delivered

- `guest/runtime-image` / `internal/guest/runtime_image_linux.go` consume only
  explicitly provisioned agent/kernel and pinned runc/BusyBox/glibc bytes. No
  download or input execution. Deterministic gzip/newc contains the minimal agent,
  runtime, guest networking utility/dependencies and essential directories.
- No `/rootfs-{setup,app,sidecar}`, `/shared` fixture or `/etc/grillo/key` in the
  reusable base. Per-boot verified snapshots/fresh credentials remain mandatory.
- Bounded scanner checks exact archive namespace/metadata, duplicates, trailing
  gzip streams and optional forbidden markers without filesystem extraction.
  Actual regression scan uses both raw/base64 development fixture credentials;
  no credential values are printed. This cannot certify all unknown secret bytes
  or authenticate an opaque binary/publisher.
- Content-identity store publication uses atomic no-replace rename; existing
  entries are checked before reuse. Interrupted/incomplete/corrupt entries fail
  closed; old artifacts are not silently overwritten. A selection file is an
  explicit developer convenience, not a mutable installed asset pointer.
- `scripts/stage` / `internal/distribution` copy host CLI, daemon, network helper,
  exact Helm with explicit byte pin, runtime guest, portable manifest/build record,
  standalone examples, Grillo LICENSE/NOTICE and self-contained local guide.
  CLI/daemon/build identity and ABI match; VERSION inventory records file hashes
  and **pending redistribution review**, not a signed attestation.
- Output prefix is atomically published only after verification; existing output
  is never replaced. Unsafe/special inputs, symlink traversal, digest mismatch,
  cancellation and bounded aggregate/output are tested. This is not an installer.
- Runtime first-run prepares private XDG directories before daemon log creation.
  Unsafe directory ownership/modes/ancestors and log symlinks/hardlinks are refused
  without chmodding preexisting paths. Host CLI/daemon reject root runtime use;
  version/doctor remain read-only entry points. No automatic sudo/fallback.
- Doctor adds bounded fixed-argument helper probes, KVM API query (no VM), transient
  namespace creation (child exits without persistent state), QEMU device/options
  and exact renderer checks, explicit known MAC blocker and support-boundary warning.
  Virtiofsd selection agrees between daemon/doctor, including explicit override.
  Structured Why/Check/Fix/Docs are capability-specific; no raw helper output.
- Version JSON reports shared identity/ABI and bounded manifest-byte digest with
  explicit inventory-only semantics. Successful `up` points to `inspect`, which
  includes loopback TCP publications and actual Ingress URLs, never guessed HTTP.

## Environment captured

2026-10-09, unprivileged UID 1000, Fedora fc44, host Linux
`7.2.9-200.fc44.x86_64`, QEMU `10.2.2`, virtiofsd `1.14.0`, pasta
`0^20261002.gcba3570-1.fc44.x86_64`, Go `1.26.8-X:nodwarf5`.
SELinux was already **Permissive** and was not changed. This does not validate
Enforcing, Ubuntu or a clean second developer/host.

The staged renderer is the existing `/usr/bin/helm`, exact `v4.2.2`, local byte pin
`deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d`.
That observed development pin is not an upstream publisher-authentication result.

Final runtime build-store identity:
`87c55caaef1043a47fa4975ba5f5b0892319a5a208cdbbaab47b98b54625ae82`.

| File | SHA-256 |
|---|---|
| `bzImage` (guest kernel 6.1.188) | `550b5916433e758e814039f8324a9f1cdfdd2b3b8e4a1e44cc6836cdc92d5942` |
| `initramfs.cpio.gz` | `26a49e9ed1cea8df1213f7b25a1b7a55cd645fc956cb2259d7d0e8ce083c9835` |
| `manifest.json` | `c3c6a3735ba1397a0e8c85e0a0133ecec56000f081faaba80f974e556fcea4fc` |

Earlier independent build identities `e7c9d3d4f0a9e1ccc7e072bae9871c3bf670991ff19020046cf2e362b201cb74`
and `3c3bb7718e6d1ea66e63155bb41efe54da99bb86037308e43cc8a203b91d3a38`
were retained, not overwritten. Final gates use freshly rebuilt host/guest files.
Binaries are `dev` with HEAD commit identity and uncommitted delivery changes;
file digests, not HEAD alone, identify the actual payload. No commit/release made.

## Verification executed

```sh
go test ./internal/distribution ./internal/guest ./scripts/stage ./guest/runtime-image
go test ./internal/api ./internal/state
go test ./internal/cli ./internal/distribution ./internal/guest ./internal/state \
  ./internal/api ./internal/backend/qemu ./cmd/grillo ./cmd/grillod
make check
go test ./internal/guest -run '^$' -fuzz '^FuzzRuntimeImageScanner$' \
  -fuzztime=10s -parallel=2
make test-installed-runtime HELM_BINARY=/usr/bin/helm \
  HELM_SHA256=deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d
make test-helm
make stage-runtime STAGE_PREFIX='experiments/artifacts/staged runtime d0-20261009' \
  HELM_BINARY=/usr/bin/helm \
  HELM_SHA256=deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d
```

Final ordinary checks PASS: repository formatting, frontend locked assets/tests,
vet, all Go unit/race tests, build, module verification/tidy. Scanner fuzz PASS
(213,090 executions, 2 workers, bounded 10-second campaign; not exhaustive).
Actual stage wrapper PASS and retained a local ignored payload with spaces.

Final installed gate PASS **without SKIP**, two real KVM workloads:

1. Stage then move the prefix; every directory/file is made read-only.
2. Empty arbitrary CWD; clear asset overrides. Runtime PATH contains only symlinks
   to distro QEMU/pasta/ip/nft, not Go/Node/npm/Podman/Helm/Grillo helpers.
3. Version, doctor, plan and on-demand daemon startup from installed CLI.
   Peer process identity proves `libexec/grillo/grillod`, not checkout/PATH daemon.
4. Compose digest-pinned image pull, real loopback HTTP `grillo-compose-ok`, ps,
   inspect/status and exec from a new CLI after the earlier CLI exits.
5. Installed embedded UI serves its entrypoint without Node. Bootstrap credential
   is never logged; stopping only the owned UI process leaves Compose HTTP alive.
6. Compose down; installed managed Helm renders the standalone chart, real
   Pod/microVM, Service/Ingress URL and HTTP `grillo-helm-ok`, in-guest Service DNS.
7. Explicit down, daemon shutdown, strong VMM/daemon identities dead, backend
   directories empty and no operation-owned live helper processes. Successful
   cleanup removes only the operation's unique temporary tree. Failed evidence
   trees are retained, never used as justification to delete unrelated resources.

The final installed gate took 5.86s (an earlier complete run took 5.64s) on this prepared development host. This is
not a cold-host onboarding benchmark. Its new XDG OCI cache pulled an application
image; offline first-run and a fully air-gapped payload are not claimed.

Existing `make test-helm` also PASS without SKIP after updating developer harnesses
to explicitly select the renderer/legacy manifest. It revalidates full T22
Service/DNS/readiness/Ingress/PVC and actual CLI/daemon logs/TTY/metrics/artifact/UI
contracts with a rebuilt **legacy fixture**, separately from the D0 base gate.

## Failed runs and corrections

- First installed gate FAIL before VM launch: missing runtime directory prevented
  opening `grillod.sock.log`. Private first-run preparation and symlink/hardlink/
  ownership/mode tests now cover this; retry passed.
- New output-limit regression FAIL: embedding `bytes.Buffer` promoted `ReadFrom`,
  allowing `io.Copy` to bypass the bounded `Write`. All new readiness/staging/test
  writers use a named buffer; subprocess oversize tests now pass.
- First full check FAIL on a newly mistyped `virtioFSD` variable name in daemon
  override normalization. Corrected to `virtiofsd`; complete retry passed. A late
  bounded-walk regression initially failed compilation due to a missing `errors`
  import; fixed before the final verification campaign.
- Initial existing Helm hardware gate FAIL because its test harness still relied
  on the removed implicit renderer PATH fallback. Harnesses now explicitly select
  the provisioned development renderer, preserving the installed security policy.
  Full retry passed; no compatibility fallback was restored.

No successful marker was used to override these failures. Process inspection
found no surviving QEMU/daemon/network helper/pasta processes after final gates.

## Remaining gates

Not run/proven here: T24 external clean host/full hardening, supported distribution
matrix, Enforcing MAC policy, signed/publisher-authenticated release artifacts,
third-party notices and complete corresponding-source review, official archives,
DEB/RPM lifecycle/upgrade/uninstall, retention GC, hosted CI, cold onboarding
benchmark or independent second developer. Native package privilege-boundary
and runtime UID checks still need package-manager E2E, not only pure tests.

Local staging preserves user data and refuses replacement; it does not implement
atomic system package upgrades. Keep old prefixes for active daemons/workloads.
The historical T07 builder intentionally still has fixtures/a reusable key and
must never be confused with the runtime-only builder or shipped as a runtime base.

**Next:** resolve T24 and maintainer guest/source/license/provenance decisions;
then D1 publication/compliance and clean-host acceptance, followed by D2/D3/D4.
No supported-host or distribution-track DONE claim follows from local D0 PASS.
