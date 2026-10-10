# T24 — rejected registry token refresh

## Reported external-host failure

The user reports private experimental prefix execution on a second Linux host
(kernel `7.2.9-200.fc44.x86_64`; OS/MAC policy still unreported): installed doctor
probes pass with expected first-run/support warnings, Compose first up succeeds
and HTTP returns `grillo-compose-ok`. A later up after down fails with exit 1:
`CreateSandbox` cannot resolve public BusyBox because the manifest request returns
401 Unauthorized. Inspection then shows no containers; ps retains the registered
application name. Private user paths are omitted here.

Reported build: version dev, commit `49861203f1deca1173d0431372051e753d577bbc`,
build time unknown, guest ABI 1.1, inventory digest
`4be77fb72bf28ecd7cfc79af2d2ebfac8d627671eb73337322be4da3b57702e6`.
This is partial user-provided external evidence, not complete clean-host acceptance
or an independently verified supported Fedora/security-policy result. Endpoint
inspection also reported null despite reachable HTTP; separate unresolved issue.

## Verified code defect and bounded correction

T09 registry authentication caches opaque Bearer tokens indefinitely. After a
401 challenge the client reused the cached value, with no recovery if that value
expired or was revoked. This is compatible with the report; the server's actual
token expiry is not established by the available output.

`internal/oci/registry.go` now invalidates a rejected token only if it still matches
the cached value (protect concurrent replacements), reacquires authorization from
the original challenge and retries **once**. Maximum: unauthenticated request,
authenticated request, one refreshed authenticated request; at most two token
endpoint calls. A still-rejected fresh token remains an error. The second response
cannot redirect token acquisition to a new challenge realm. Cache keys now include
the token service/audience and effective fallback scope. No JWT claim is trusted
or custom authentication/cryptography introduced.

No login recommendation, credentials, privilege change, cache/volume deletion or
registry-specific fallback is required. Existing redirect credential isolation and
bounded token/body reads remain in force. Token expiry is handled reactively on
rejection, not by parsing unverified JWT payloads or promising proactive TTLs.

## Verification

```sh
go test ./internal/oci -run 'TestRegistry|TestBearer' -v -count=1
go test -race ./internal/oci ./internal/executor ./internal/build
make check
go test -race -count=100 -timeout 120s \
  -run 'TestRegistryPullRefreshes|TestRegistryRejectedFresh|TestRegistryTokenRefresh|TestBearerCacheKey' ./internal/oci
make test-installed-runtime HELM_BINARY=/usr/bin/helm \
  HELM_SHA256=deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d
```

PASS. Regression uses the real Puller/CAS against an HTTP distribution fixture:
two successive pulls share one registry client; token generation changes between
pulls, the old token is refused and the second pull succeeds with identical image
identity and exactly two token acquisitions. Negative tests reject persistently
unauthorized responses without an infinite retry, separate service cache keys,
and propagate cancellation during token refresh. The 100 repeated selected race
suites pass in 3.81 seconds. Full checks pass; final installed real KVM
Compose/Helm/UI gate passes without SKIP in 7.30 seconds on the previously recorded
local development host, unchanged already-Permissive SELinux.

The installed gate proves no ordinary runtime regression, not actual remote token
expiry recovery. Updated host binaries/prefix and daemon restart are required on
the second host before retesting; an already-running daemon retains old code.
Do not overwrite/delete an active prefix or use blanket process termination.
No release/commit/publication or external-host remediation has been performed.

## Remaining gates

User-run `up/down/up` with updated daemon, OS/MAC evidence, DNS/Helm/UI/lifetime/
cleanup checks and separate missing-endpoint inspection investigation. T24/D0
remain BLOCKED; quota/performance/license/source/provenance/hosted-CI gates from
[the preceding report](t24-cache-load-license-review.md) remain unchanged.
