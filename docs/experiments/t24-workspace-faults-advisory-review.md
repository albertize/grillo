# T24 — workspace interruption/load and external artifact blockers

Follow-up: [isolated candidate builds](t24-external-candidates.md) now provide
patched runc/Helm source/binary scans and bounded local KVM/resource acceptance.
Original artifact failures below remain valid; candidates are not promoted and
a combined-load workspace timeout remains unresolved.

Date: 2026-10-16. **T24/F3/D0 remain BLOCKED.** This bounded continuation adds
hardware failure-path acceptance and refreshes the external-artifact diagnosis.
It does not change runtime defaults, replace tools or publish a release.

## Dependencies and environment

T06 protocol, T09 OCI/CAS, T10 block storage, T18/T18b native builder and the
[real workspace gate](t24-block-workspace-kvm.md) are verified prerequisites.
Same Linux/amd64 host, UID 1000, Go 1.27.2, QEMU 10.2.2, virtiofsd 1.14.0 and
mke2fs 1.47.3. KVM tests run outside the session sandbox; no sudo or host policy
change. Existing source archives/vendor were used without downloading new
runtime tools or dependencies.

## Real workspace failure paths

The test tracks only handles created by its private backend. After a real block
guest boots it captures the backend-observed VMM process identity; SIGKILL uses
the existing PID/boot/start-time validation, never a process-name match.

| Scenario | Actual required result |
| --- | --- |
| Cancel after the guest writes a file and emits its marker | context.Canceled reaches the builder |
| SIGKILL the owned VMM after the same write/marker | RUN reports connection failure |
| Guest produces unbounded output | Aggregate output exceeds 4 MiB and RUN fails |
| Each failed build | Prior image reference/digest unchanged; captured VMM no longer alive; scratch empty |
| Fresh build after each failure | ENOSPC/delete/retry, two RUNs with intervening COPY and final content pass on fresh disks |

Commands and results:

- `go test -tags kvm -run '^$' ./internal/build`: PASS, compilation only.
- `make test-t24-build-workspace`: PASS, package 7.811 s. Expected failures:
  context canceled, vsock connection reset, aggregate output limit.
- `make test-t24-build-workspace-load`: PASS, initial rebuilt gate 6.831 s,
  then ten sequential race-detector repetitions, package 123.769 s, no skips.
  Each repetition includes all three failures and their fresh successful builds.
- Final `make test-t24-build-workspace-load` after requiring the pre-marker
  guest write to succeed: PASS, rebuilt initial gate 7.821 s, ten race-detector
  repetitions 132.045 s, no skips. Final initramfs SHA-256:
  `47caa696f79b6ab620f6bde1a8078720daee0b0b4393e108cf93f67357914c48`.
- `make check`: PASS outside sandbox, including frontend, formatting, vet,
  unit/scripts/race tests, builds and module verification/tidy.

The load target rebuilds the agent/initramfs first. Its initramfs SHA-256 was
`472a1d9f9d4f7ec4b0b4f4d87d1c139b09592a9d870824a2fd710b5296b81fc5`.
Times identify actual runs, not throughput/latency guarantees. This is sequential
fault repetition, not concurrent multi-Pod pressure or peak memory/disk evidence.
SIGKILL after a guest write is not host power-loss testing or ext4 repair.
Build disks are disposable; a successful fresh build is not recovery of the
interrupted filesystem. Daemon/host crash, interrupted export/adoption and
persistent-volume fsck/repair still require separate acceptance.

## Fresh advisory evidence

The cached `golang.org/x/vuln@v1.8.0` source was built offline into
`/tmp/grillo-govulncheck-t24-20261016` using Go 1.27.2; the repository module was
not changed. Initial `go run ...@v1.8.0` with GOPROXY=off failed on module lookup.
Building directly from its cached module succeeded. The sandbox scan then failed
on DNS to vuln.go.dev; host scans against the public database completed:

```sh
/tmp/grillo-govulncheck-t24-20261016 -mode binary experiments/artifacts/t02/runc
/tmp/grillo-govulncheck-t24-20261016 -mode binary /usr/bin/helm
```

Both FAIL, scanner exit 3: runc 14 advisories (two modules plus Go standard
library), Helm 15 (four modules). These match the prior counts and remain open.

Observed SHA-256 identities:

| File | SHA-256 |
| --- | --- |
| Fixture runc | `599f6f94ff8c5057241eff0d54c3c74f95c34935b6457b33fe545defc61e9488` |
| /usr/bin/helm | `deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d` |
| Locally built scanner | `610f499b84085b46cf354071bb4c1f35d06a6a69a7af3c69279c738d71acf2d7` |

| Artifact | Observed build inputs | Scanner's fixed-version inputs for review |
| --- | --- | --- |
| runc 1.5.2 | Go 1.26.8, x/net 0.55.0, cilium/ebpf 0.17.3 | Go 1.26.9 for reported stdlib issues (current Grillo toolchain is 1.27.2); x/net 0.60.0; cilium/ebpf 0.22.0 |
| Helm 4.2.2 | Go 1.26.4-X:nodwarf5, x/net 0.55.0, x/text 0.38.0, x/crypto 0.53.0, oras-go/v2 2.6.1 | x/net 0.60.0; x/text 0.41.0; x/crypto 0.56.0; oras-go/v2 2.6.2; old OpenPGP advisory has no fixed version |

These are scan-reported fix inputs, not reviewed replacement pins or proof of
exploitability. Both files are stripped (`file` verified). The cached scanner's
`internal/vulncheck/binary.go` falls back to module precision when symbol
extraction yields no symbols; its displayed symbol lists do not establish
actual reachability for every finding.

### Helm source comparison

Existing Helm upstream/vendor archives were unpacked into the unique private
`/tmp/grillo-helm-review-9oooz8ru` directory with Python's data extraction filter,
entry/expanded-size bounds and no execution of upstream scripts. Initial safe
extraction refused a test fixture linking to `/dev/null`. A fresh extraction
excluded only three unsafe links under testdata, retaining the safety filter.
No source tests needing these omitted hostile fixtures were run.

`go list -deps ./cmd/helm`, with GOTOOLCHAIN=local, GOPROXY=off,
GOFLAGS='-mod=vendor -buildvcs=false' and a private writable GOCACHE, PASS:
1033 dependency packages, zero old golang.org/x/crypto/openpgp packages and 18
ProtonMail OpenPGP packages. Earlier attempts failed on sandbox cache writes
and archive VCS stamping; explicit private cache and disabled VCS stamping fixed
those source-inspection prerequisites without mutating the source inputs.

Source scan from that Helm directory:

```sh
GOTOOLCHAIN=local GOPROXY=off GOFLAGS='-mod=vendor -buildvcs=false' \
  GOMAXPROCS=2 /tmp/grillo-govulncheck-t24-20261016 ./cmd/helm
```

FAIL, exit 3: eight called advisories from three modules; zero additional
imported-package findings and seven module-only findings. This scans the full
Helm CLI source with Go 1.27.2, not the original Fedora executable or only
Grillo's restricted render path. It supports further review of the conservative
OpenPGP finding; it does not clear the original binary or prove all findings
are exploitable through Grillo.

### What blocks replacement

For runc, pkg-config finds libseccomp 2.6.1 but cannot find libpathrs. Its reviewed
recipe enables seccomp/libpathrs and requires static native build inputs.
Rebuilding with those features disabled would change the security boundary.
The exact native sysroot/compiler lineage and source/notice obligations still
need a reproducible isolated build. The runtime guest builder pins runc's hash,
so replacement also requires updating reviewed selection/provenance and guest
regression evidence, not overwriting the fixture.

Helm has source-level findings even after conservative binary findings are
separated. A candidate needs dependency review, an honestly identified isolated
rebuild, source/binary rescans, local/OCI rendering regressions and installed
runtime gates. No candidate was built or selected here; /usr/bin/helm and the
existing runc fixture remain unchanged.

## Distribution closure blockers

| Gate | Missing evidence or decision |
| --- | --- |
| Clean external runtime host | Existing second-Fedora report is partial; exact payload/helper identity, clean environment, lifetime/DNS and complete cleanup receipts remain incomplete |
| Standard host policy | Recorded Fedora Enforcing pasta blocker remains; no broad policy weakening is an acceptance path |
| Artifact/source delivery | Complete corresponding sources/notices and native/compiler lineage; artifact provenance and publisher verification |
| Release operation | Verified publication channel, security contact, supported-version/response decisions and hosted current-revision CI |
| Packaging lifecycle | Package-manager E2E for installation/upgrade/uninstall with preservation of user data |

Sources: [second-host report](d0-second-host-fedora44.md),
[host boundaries](../host-support.md), [D0 gates](d0-installed-runtime.md),
[distribution specification](../distribution-onboarding-spec.md).
No new external host, publication channel or maintainer identity is invented.

Next bounded tasks: isolated reviewed runc native build/dependency updates and
Helm dependency rebuild; persistent-volume crash acceptance and daemon-crash
interruption tests remain separate. No T24/D0 DONE or T25 start is claimed.
