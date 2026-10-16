# T24: guest exit delivery and workspace timeout investigation

Date: 2026-10-16. Status: concrete reaper race fixed; historical combined-load
timeout not conclusively attributed; T24 remains BLOCKED.

## Failure and correction

The [candidate campaign](t24-external-candidates.md) retained a fresh workspace
retry that reached its roughly 30-second context deadline under software load.
Cleanup had removed its guest logs. That failure remains valid evidence.

Inspection found a lost wakeup in `Reaper.deliver`: after observing no waiter,
it released the mutex before retaining the exit. `Wait` could register a channel
in that interval, leaving both a retained status and a waiter that nobody would
notify. `Runc.waitWithCancel` waits for exit delivery even after cancellation,
so losing delivery can strand a RUN operation. The fix keeps lookup and status
retention within one critical section; it changes no timeout or protocol limit.

The new concurrent regression launches Wait/delivery for 20,000 synthetic PIDs,
checks that retained status and a stranded waiter cannot coexist, and verifies
the exact status with a ten-second whole-test deadline. It does not start wait4
or claim hardware coverage. Before the fix:

```sh
go test -race -count=1 -run '^TestReaperConcurrentWaitAndDeliveryNeverLosesExit$' ./internal/guest
```

FAIL immediately at PID 117 (`stranded=true`). After the fix:

```sh
go test -race -count=10 -run '^TestReaper' ./internal/guest
```

PASS, including 200,000 synthetic Wait/delivery races and existing real-child
reaper tests. The initial pass took 5.037s; the final bounded-test revision was
also checked independently and by `make check`.

## Diagnostics before cleanup

The hardware workspace harness records boot, RUN and export phase durations and
bounded errors in a private JSONL file (64 KiB total, 2,048 bytes per error).
Before deleting an operation-owned guest it saves the first 64 KiB each of
`serial.log` and `vmm.log`. At most 32 unique owned guest handles are accepted.
The diagnostics directory is mode 0700 and files mode 0600; log opens reject
symlinks/nonregular files. It does not copy sandbox state, keys or overlays,
nor record request commands or workload output in phase metadata. Serial logs
are untrusted and are not automatically published or promised secret-free.
Successful roots are removed; failed roots retain these logs even after normal
VMM/scratch cleanup. Preservation failures fail the gate without skipping cleanup.

`go test -tags kvm -run '^TestBlockKVMDiagnostics' ./internal/build` PASS (0.005s)
checks bounded private copies, credential-file exclusion and owned cleanup with
a prepared backend handle, without launching a VM. Hardware-tagged compilation
was also checked with `go test -tags kvm -run '^$' ./internal/build`.

## Actual guest rebuild and hardware gates

Host tests ran as UID 1000 with KVM API 12/vsock accessible outside the session
sandbox and SELinux already Permissive. No host policy was changed. Toolchain:
Go 1.27.2. Inputs and candidate runc/Helm hashes remain those in the
[candidate lock](t24-external-candidate-lock.json). The old immutable guest and
normal selection were preserved.

The operation-local artifact root is
`experiments/artifacts/t24-candidates-20261016-ae096cr9`. In these commands
`candidate` denotes its absolute path; `seed` denotes the absolute
`experiments/artifacts/t24-fixture-20261011/cas` path.

```sh
make runtime-guest VERSION=dev-t24-candidate COMMIT=1bda63156002a6862a028a303468d484c23d9019-dirty \
  GUEST_INPUTS="$candidate/inputs" RUNTIME_RUNC_VERSION=1.5.2+grillo.1 \
  RUNTIME_RUNC_SHA256=5bca7accb4a212f748899ac54dee87a470be9af5b8f34a84110c7cc425c045fb \
  RUNTIME_STORE="$candidate/runtime" RUNTIME_SELECTION="$candidate/runtime-reaper-fixed.txt"
GRILLO_TEST_BUILD_GUEST_SELECTION="$candidate/runtime-reaper-fixed.txt" make test-t24-build-workspace-load
```

Rebuild PASS. Immutable inventory:
`288d96dbbd859007ecf92d5b18c889d63f4cff4aa96f723849e5e6b6a1db44a1`;
agent SHA256 `0e958a42a4c022fb2f41dfe5dbf3a2d18661ed619594e3104ebd7bd618049b80`;
initramfs SHA256 `3bab9c1704cad74dfe119992a23199d19539c7b0255d691ec42e53e8f426016e`.
The workspace target rebuilds the legacy fixture too; that artifact is not the
selected portable guest. Manifest verification/fresh-key snapshots used the
new inventory above.

| Actual run | Result |
|---|---|
| Old portable guest, same load target and added diagnostics | PASS: initial 9.196s; ten KVM/race repetitions 134.462s |
| Corrected portable guest, same target during software load | PASS: initial 7.453s; ten KVM/race repetitions 136.375s |
| First concurrent `go test -race -count=10 -timeout=300s ./...` | PASS all packages |
| Second identical software-load batch | FAIL: three network TCP bind collisions; all other packages passed |
| `make test-t07` after rebuilding guest | PASS real KVM, package 3.625s |
| Installed runtime smoke with corrected portable guest | PASS real KVM, package 4.636s |
| Final `make check` | PASS, including formatting, UI, vet, ordinary/race suites, builds and module checks; some unchanged package results cached |

The installed smoke used `make test-installed-runtime` with the exact rebuild
arguments above, `GRILLO_TEST_SEED_CAS="$seed"`,
`HELM_BINARY="$candidate/helm-candidate"` and
`HELM_SHA256=b82ff5240c4a1385a2ebc4e967fc497593bfe0bfdb98835aea19fb573ea444b9`.
It checked actual moved/read-only installation, Compose/Helm HTTP/DNS, exec,
CLI/UI lifetime, embedded UI, tool-free runtime PATH and cleanup.

The workspace batches include cancellation, owned VMM SIGKILL, excessive output
and a successful fresh-disk retry after each fault. No workspace timeout recurred,
including with the old guest. Thus these results fix a demonstrated source race
but do not prove it caused the earlier hardware timeout or establish saturation
performance. Future failures now retain phase/guest diagnostics.

## Separate failure and remaining gates

The second software-load batch failed `TestDNSAllFourServiceNames`, `TestDNSSRV`
and `FuzzDNSRespond` with `listen tcp 127.0.0.1:<port>: bind: address already in use`.
A focused `go test -race -count=10 -timeout=180s ./internal/network` also FAIL
(21.887s): two seed executions of `FuzzDNSRespond` hit the same error.
`NewDNSServer("127.0.0.1:0")` chooses a UDP port before binding TCP to it; the TCP
port can already be occupied. No DNS code was changed in this bounded reaper task.
A passing routine cached suite does not erase these repeated failures.

No new fuzz campaign, external source/native vulnerability scan, independent-host
test, persistent-volume repair, daemon crash, source/license promotion or release
acceptance was performed. Dependencies were unchanged. Prior candidate and F3
blockers remain; there was no commit, publication or supported-version claim.
Next: repair/test DNS dual-protocol port allocation separately, and use retained
diagnostics if the historical workspace timeout recurs.
