# T24 — real KVM block workspace acceptance

Follow-up: [fault/load and advisory review](t24-workspace-faults-advisory-review.md)
extends this gate with real RUN cancellation, VMM crash, output-limit failures
and repeated recovery via fresh workspaces. Limits below describe this initial
bounded run; overall T24/F3/D0 remain open.

Date: 2026-10-16. **T24/F3/D0 remain BLOCKED overall.** The bounded opt-in
workspace hardware gate now passes; runtime defaults remain unchanged.

## Environment and prerequisites

Linux/amd64, kernel `7.2.9-200.fc44.x86_64`, Intel Core Ultra 7 155H with VT-x,
Go 1.27.2, QEMU 10.2.2, virtiofsd 1.14.0 and mke2fs 1.47.3.
The session sandbox hides `/dev/kvm` and `/dev/vhost-vsock`. Outside it, as
UID 1000, opening KVM read/write and querying KVM_GET_API_VERSION returned 12;
vhost-vsock exists and read/write access checks passed. No host configuration,
dependency installation, source download or privilege elevation to root occurred.

Dependencies: T06 authenticated bounded protocol, T09 OCI/CAS, T10 block storage,
T18/T18b native builder and the [opt-in integration](t24-opt-in-block-build-workspaces.md).
Existing kernel, runc and BusyBox fixtures were available. The target rebuilt
the actual guest agent and initramfs before every hardware run.

## Failures and correction

The first `make test-t24-build-workspace` failed with RUN exit 255. The test
copied the dynamically linked fixture BusyBox into a scratch image without its
guest loader/libraries. Its shell ENOSPC expression also contained unwanted
literal backslashes around quotes. Both were corrected in the hardware fixture;
progress output is now logged by the test.

The next attempt still failed with `exec /bin/busybox: no such file or directory`:
COPY of the `lib64` symlink to a destination ending in `/` appended its basename,
placing the loader link at the wrong path. Using exact destinations preserves
the fixture's `/lib64 -> lib` link. The following run passed (2.291 s package
time). These are fixture corrections, not a runtime implementation change.

Failed owned state was retained at `/tmp/grillo-block-kvm-2528334992` and
`/tmp/grillo-block-kvm-533931193`; no automatic deletion was attempted.

## Final verification

`make test-t24-build-workspace` PASS, exit 0, final package time 2.309 s.
The final rebuilt initramfs SHA-256 was
`3f5dea028a11d97d6e82740d06718b7bbe1be80f1c91fed0ef0c8743c8fff1be`.
This is a local receipt, not publisher authentication or a performance claim.

Actual guest execution verifies a 32 MiB workspace reaches ENOSPC, deletion
permits a subsequent write, COPY/RUN steps preserve coherent content and
deletions, and successful scratch cleanup leaves no entries. Added assertions
inspect the published OCI layer for root mode 0750 and output UID/GID 1234:1235
with mode 04750. Host unpack alone deliberately clears privilege bits and is
not used as proof of those metadata contracts.

`make check` initially failed inside the sandbox on lchown invalid-argument
errors and denied local test sockets (build, CLI and Helm test packages).
The host rerun PASS: frontend checks, formatting, vet, unit tests, script tests,
race tests, binary builds and module verification/tidy. The final hardware
metadata assertions were added afterward and exercised by the final KVM run.

## Remaining limits

Two requests for host process inspection were interrupted; no independent
host-wide residual-process check is claimed. Successful test-owned scratch
cleanup was asserted, while failed directories remain retained.

No crash/fsck/recovery, full metadata matrix, sustained resource load, benchmark,
fresh advisory scan, tool replacement, hosted CI or independent clean-host
campaign was run. Default native RUN remains unbounded virtiofs; sparse,
no-journal block geometry does not reserve physical disk space. Existing binary
advisory and source/license/release blockers remain. ADR 0017 stays Proposed.

Next: bounded crash/recovery and load acceptance; separately review replacement
runc/Helm artifacts and rerun their advisory scans.
