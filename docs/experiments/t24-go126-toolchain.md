# T24 — Go 1.26 family pin and fail-closed advisory identity

**Status: bounded local gates PASS; T24/D0 overall BLOCKED.** User requested
continuation of the [hosted-CI correction](t24-ci-log-review.md), pinning the Go
1.26 family. Use exact **1.26.9**, not vulnerable 1.26.0/1.26.8 or floating 1.27.
Dependencies: existing CI/build/bootstrap contracts, govulncheck v1.8.0, verified
runtime-only image/installed gates and T23 browser/UI contracts. No new Go/npm
module, framework, SDK, ADR, automatic sudo, system Go replacement or commit/push.

## Policy and private preparation

- `.go-version` pins 1.26.9; CI setup-go reads it. `go.mod` minimum and bootstrap
  version agree. Offline tests reject drift. Updating to another reviewed patch
  is deliberate; policy refuses a different family and a pin below 1.26.9.
- Make exports `GOTOOLCHAIN=local`. Build/vet/test/race/audit/fuzz checks require
  exact upstream compiler identity plus matching GOROOT VERSION metadata.
  This is consistency, **not publisher authentication or a patched-source proof**.
- Python-stdlib development checker rejects vendor/development strings and any
  external `GOVERSION` advisory override. It does not normalize them into a PASS.
  Scanner child uses the selected actual bin/go/PATH, local toolchain, pinned
  official scanner, 240-second deadline with cleanup of only its own process
  group on timeout/cancellation. Selected compiler identity query has a
  15-second deadline; unknown/missing/conflicting responses fail closed.
- Python 3 is now an explicit development policy prerequisite, not runtime tooling.
- Bootstrap `--go-download` prepares only Go in a new private directory, retaining
  complete upstream archive/source/notices. Existing full dependencies are not
  overwritten or re-downloaded. Same pinned checksum/fetch bounds as full setup,
  version/completeness validation before publication, refusal of existing/symlink
  destinations, explicit failure propagation and cleanup only of owned stage.

Read official `https://go.dev/dl/?mode=json&include=all` metadata with 30-second/
4-MiB bounds; exact Linux/amd64 archive `go1.26.9.linux-amd64.tar.gz`, **66,935,201
bytes**, SHA-256:

```text
42d158b4d8f7b61ac0a830567c940a86098fb7aac52e467a5ebec03ef5cc2f8d
```

Verified before extraction, no code executed by bootstrap. Private activation:

```sh
bash scripts/bootstrap.sh --go-download
source experiments/artifacts/go-toolchain-1.26.9/env.sh
# go version: go1.26.9 linux/amd64
make check-go
```

Activation unsets conflicting GOROOT/GOVERSION and sets PATH only in the caller's
shell. `/usr/lib/golang` and the Fedora Go 1.26.8-X:nodwarf5 installation remain
unchanged; no persistent shell/host configuration edits.

## Actual verification

Local normal UID 1000, established Fedora 44/Linux/amd64 QEMU/KVM environment,
SELinux already Permissive, official Go 1.26.9. No default-policy support claim.

| Executed check | Result |
| --- | --- |
| Python policy unit tests | PASS: exact/old/vendor/devel/other-family versions, minimum pin, override, missing executable, selected PATH, root mismatch, scanner failure/timeout and repo pin agreement |
| Bootstrap offline tests | PASS: checksum refusal/full staging and Go-only preservation/activation/no-overwrite/symlink/fetch-failure owned cleanup; fixtures never executed |
| System-Go `make check-go` | Expected FAIL, Make exit 2 for unrecognized old vendor identity |
| Fixed-Go `GOVERSION=go1.26.9 make vulncheck` | Expected FAIL, Make exit 2 before scanning; no override even when equal to actual compiler |
| Fixed-Go old `GOROOT=/usr/lib/golang make check-go` | Expected FAIL, Make exit 2 for stdlib/compiler mismatch |
| Fixed-Go `make vulncheck` | PASS: actual go1.26.9 identity, `No vulnerabilities found` |
| Fixed-Go `make check` | PASS: fresh UI, formatting/vet/unit/script/race/CGO-free builds/module verify/tidy diff |
| Fixed-Go `make fuzz` | PASS: five existing bounded 5-second/2-worker smoke targets; not load/latency/long-duration proof |
| Fixed-Go `make test-ui-browser` | PASS: Firefox fixture 7.48s, no SKIP |
| Fixed-Go `make test-ui` | PASS: real daemon/Helm/KVM/Firefox UI, exec/logs/auth/lifetime/owned teardown, 25.79s, no SKIP |
| Fixed-Go `make test-installed-runtime` | PASS: new runtime-only guest, moved/read-only prefix, actual HTTP/DNS/UI/lifetime and teardown, 5.98s, no SKIP |
| CGO-free example backend build | PASS, separate ignored `backend-go1.26.9` artifact; no live image/process replaced |
| Live example gateway HTTP | **FAIL**, curl exit 7: loopback port 18088 unavailable; not concealed by passing build/KVM gates |

Hardware invocation used explicit trusted Helm binary/digest:

```sh
make test-ui-browser test-ui test-installed-runtime \
  HELM_BINARY=/usr/bin/helm \
  HELM_SHA256=deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d
```

Fresh CLI/daemon/helper/agent binaries report actual Go 1.26.9; runtime-only guest
selection now points to content-addressed store
`77c20ecb756417152824502b9001eff6f9cd5f76dd98fc16381df48de061bc7b`.
Prior stores retained. Existing process-owned verified snapshots are not replaced
by updating the build output or future-boot selection.

## Active review session correction and remaining gates

The final gateway probe failed after successful gates; the compound command thus
returned exit 7. Read-only inspection found recorded daemon 49525 and console
54507 still alive with their old `(deleted)` executable inodes after rebuilding
bin/, but `ui-stack-HkWRTQjf: no containers` and no corresponding VMM/helpers.
Network bookkeeping timestamp 11:44:35 +0200 predates `.go-version` creation at
11:54:00 and runtime gates at 12:01 +0200. This is evidence of earlier state,
**not an attribution of who stopped the workloads or why**. No automatic restart,
blanket termination, state cleanup or removal was performed. Older full-stack
report proves the earlier run, not current availability. Bootstrap token omitted.

Not run: remote hosted rerun (supplied old CI advisory result remains FAIL), new
transfer payload/publication, complete external-tool/guest/vendor security/source/
license review, other-host/default-policy/full-resource/load gates. Fixed Grillo
source scan does not clear external Helm (previously recorded Go 1.26.4), runc,
kernel, native helpers or third-party images. Previous archives/Go 1.26.8 images
have not been upgraded merely by changing the pin. Old running management
processes remain old; new demo backend artifact is not a rebuilt/running OCI image.

Next: hosted run with the actual reviewed pin, deliberately rebuild transfer/demo
images and replace only verified owned old management processes when coordinated;
retain broader T24/D0 release/source/provenance gates. No secret/generated archive
or toolchain binaries tracked.
