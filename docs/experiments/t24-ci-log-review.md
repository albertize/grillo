# T24 — supplied hosted-CI logs and Go stdlib scan correction

**Status: BLOCKED.** Supplied hosted unit/build checks pass; the advisory gate
fails. Earlier local zero-finding reports are not stdlib clearance.

## Evidence scope

User supplied `logs_103066873123.zip`; SHA-256
`0bfc4d1db7e4253f063f6e5f7d4362c755e13a119936c4cc79da5eba821d2b53`.
11 entries, 319,181 uncompressed bytes. Inspected bounded allowlisted members
without executing archive content or extracting member paths; private temporary
inspection files outside the repository. Original archive left untouched.
No credentials/raw CI archive committed. Hash identifies the supplied bytes,
not authenticated GitHub provenance; no remote run was started/fetched by agent.

Logs identify commit `55be94c05f83d98fa43a35fb39534d74b30e8111`, hosted Ubuntu
24.04.5, Go 1.26.8, Node 24.18.0, job `unit`, 2026-10-10 08:38–08:40 UTC.
This provides **user-supplied hosted log evidence**, unlike previous absence of
hosted execution evidence. It predates uncommitted UI/full-stack example changes.
No browser/KVM/rootless networking/clean installed runtime/other host gate is
proven by this hosted unit job.

## Actual results in the supplied logs

- `make ui-deps` / embedded frontend build / deterministic check and five existing
  client tests PASS.
- `make check` PASS: formatting, vet, Go unit tests, expected negative script
  cases, race, CGO-free host/agent builds, module verification and tidy diff.
- Script lines containing checksum/unsafe-existing/cache/cleanup `ERROR` are
  **expected negative fixtures**, followed by passing script targets. They are
  not the job's failure and were not hidden or classified as actual build failure.
- `make vulncheck` **FAIL**, pipeline exit 2 (`govulncheck` findings exit 3 propagated
  through `go run`/Make). It identifies **10 called-symbol stdlib advisories** in
  upstream Go 1.26.8, all reporting fixes in Go 1.26.9.
- Scan additionally reports one imported-package / two required-module advisories
  without apparent called symbols; do not turn that into an unqualified clean scan.
- `make fuzz` does not appear as executed after the failing advisory step. No
  hosted fuzz PASS inferred. No release/publication performed by this review.

| Advisory | Package / brief subject | Reported fixed Go version |
|---|---|---|
| GO-2026-6617 | net/http: HTTP/2 HPACK encoder race/server crash | 1.26.9 |
| GO-2026-6613 | net/http: server CONNECT connection desynchronization | 1.26.9 |
| GO-2026-6612 | net/http: HTTP/2 stream flow-control refund | 1.26.9 |
| GO-2026-6611 | net/http: HTTP/2 window changes / excessive CPU | 1.26.9 |
| GO-2026-6610 | net/http: malformed HTTP/2 framing headers | 1.26.9 |
| GO-2026-6608 | net/textproto, mime/multipart: MIME memory limits | 1.26.9 |
| GO-2026-6607 | crypto/tls: ECH outer-extension validation | 1.26.9 |
| GO-2026-6605 | net/http: client CONNECT connection desynchronization | 1.26.9 |
| GO-2026-6604 | os: Windows Root.Mkdir(All) junction traversal | 1.26.9 |
| GO-2026-6603 | net/http: HTTP/2 trailer memory exhaustion | 1.26.9 |

Public records are at `https://pkg.go.dev/vuln/<ID>`. This is a scanner finding
review, not confidential exploit material or independent exploitability proof.
The Windows-specific advisory is not a Linux runtime claim; conservative call
traces/HTTP feature exposure require analysis. None justifies suppressing the
Go stdlib gate or claiming Grillo's current build is cleared.

## Local discrepancy reproduced

Actual installed version:

```text
go version go1.26.8-X:nodwarf5 linux/amd64
GOVERSION=go1.26.8-X:nodwarf5
GOROOT=/usr/lib/golang
GOTOOLCHAIN=local
```

Executed, no toolchain installation/version substitution:

```sh
make vulncheck
GOVERSION=go1.26.8 make vulncheck
```

- Actual Fedora version string scan: exit **0**, `No vulnerabilities found`.
- Explicit **upstream Go 1.26.8 advisory baseline** with the same local source/
  compiler: Make exit **2**, **the same 10 stdlib advisories**.
- Inspected installed govulncheck v1.8.0 source: `internal/semver.GoTagToSemver`
  requires standard Go tags and returns empty for `go1.26.8-X:nodwarf5`; package
  graph uses that conversion as stdlib version. Scan supports the explicit
  `GOVERSION` environment value for source advisory selection. This explains the
  apparent clean local result; no actual compiler/stdlib patches changed.

The explicit baseline does not independently establish Fedora package patch/
backport provenance. **Do not use GOVERSION=go1.26.9 on an old compiler to fake a
patched build or passing gate.** Missing/unrecognized stdlib version must become
visible/fail-closed rather than silently treated as successful coverage.
Historical commands did produce exit 0, but the corresponding claim of complete
current advisory clearance is withdrawn. Linked corrections preserve those
observations instead of rewriting history into a passed stdlib gate.

## Subsequent bounded continuation

[Go 1.26 family pin and actual 1.26.9 rebuilds](t24-go126-toolchain.md) now record
local scan/check/fuzz/browser/KVM PASS and fail-closed identity regressions. The
supplied old hosted result remains FAIL; no remote rerun or patching of existing
running processes/archives is inferred. Remaining work below records the scope
at this review, not an assertion that no subsequent local upgrade occurred.

## Remaining work

- Deliberately update the verified official CI toolchain pin to Go 1.26.9 (or a
  later reviewed fixed release), review module/build/docs/toolchain notices, and
  rerun actual upstream-source checks; no pin or host install changed in this review.
- Add strict tested local advisory-version handling: preserve actual vendor version
  identity while explicitly identifying any conservative upstream baseline; refuse
  unknown/development versions unless coverage semantics are deliberately selected.
- Rebuild **actual** CLI/daemon/helper/guest/application Go executables with verified
  fixed sources, then rerun advisory/unit/race/browser/KVM gates. Updating a version
  string or scanner setting does not update a running daemon/guest image.
- Hosted rerun/fuzz/hardware/default-policy/source/release gates remain pending.
  Current live UI-stack was left untouched as requested; its Go bits have not been
  upgraded, and no stdlib security clearance is inferred from successful HTTP.

No Go/npm module pin, CI workflow, system Go install, security policy, process
lifetime, commit/push or remote workflow was changed. Next bounded task is actual
fixed-toolchain upgrade and fail-closed advisory-version regression coverage.
