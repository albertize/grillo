# D0/T24 — local transfer payload target

D0/T24 remain BLOCKED overall. This delivers the user's requested one-command
local payload convenience, not D1 signed/compliance-cleared release packaging.
Dependencies: established runtime-only guest, bounded verified Stage and existing
Make build identity/pinned Helm contracts. No new dependency or ADR.

## Usage

Set the trusted renderer expectations once in the shell, then repeat `make payload`:

```sh
export HELM_BINARY=/usr/bin/helm
export HELM_SHA256=deb3b9d7aad307ccaa7e4165204f6186e0abb51a226028d0dafdce11e8d0bb8d
make payload
```

The above exact pin is the already recorded local-development renderer, not a
universal expectation for `/usr/bin/helm` on other hosts. Other provisioned binaries
require their own explicit trusted pin. No download/hash-as-trust shortcut.

`payload` depends on `runtime-guest`/`build`, then invokes `scripts/payload.sh`
from the checkout root. It stages into a fresh private unique directory below
`PAYLOAD_ROOT` (default ignored `experiments/artifacts/payloads`), creates
`grillo-test.tar.gz` with one top-level `grillo/`, emits a relative checksum file,
verifies that checksum and removes only its own temporary staged tree. Success
prints both absolute file paths. Existing payloads and unrelated entries are
never overwritten/deleted. Failure/signaled exits clean only this invocation's
unique directory. SIGKILL/power loss can retain incomplete output; transfer only
the pair from a successful invocation. This is not atomic release publication.

The target performs no transfer, installation, privilege escalation, dependency
provisioning, publisher signature or corresponding-source compliance review.
The archive includes the host registry token correction from
[the preceding delivery](t24-registry-token-refresh.md), but an existing daemon on
the second host must still be deliberately stopped before new code is used.
No automatic termination of active workloads or upgrade orchestration is added.

## Actual checks

- `bash scripts/payload_test.sh` PASS: fake stage/real tar and SHA/extraction,
  two successive unique outputs, spaces, stage failure/archive failure cleanup,
  prior pair/sentinel preservation, symlink output-root and malformed pin refusal.
  Included in `make test-scripts`/`make check`; this is offline fixture evidence,
  not real Stage/ABI/KVM verification.
- `make check` PASS with the additional shell tests.
- Two actual `make payload` invocations with the recorded renderer PASS,
  producing distinct directories and checksum-verified archives. First:
  `experiments/artifacts/payloads/grillo-test.O35HtXTB/`; latest:
  `experiments/artifacts/payloads/grillo-test.abTsqQ6i/` (includes the updated guide).
- First actual archive extracted into a fresh private temporary directory;
  extracted CLI `version --json` and `doctor --verbose` PASS with expected
  first-run/support warnings. ABI 1.1/inventory digest
  `4be77fb72bf28ecd7cfc79af2d2ebfac8d627671eb73337322be4da3b57702e6`.
  CLI/daemon snapshot identity remains dev/commit
  `49861203f1deca1173d0431372051e753d577bbc`; dirty-source builds are not release
  attestations and this unchanged commit/inventory does not identify host patches.
- Formatting, updated Markdown links/fences and diff checks PASS.

No new second-host or archive-extracted workload hardware gate executed for this
wrapper-only delivery; the preceding rebuilt installed-prefix KVM gate already
PASS without SKIP. Rootfs/cache/load/source/provenance and incomplete external-host
acceptance remain as recorded; no D0/D1/T24 DONE claim or commit/publication.
Next: transfer updated payload into a new prefix and user-run corrected
`up/down/up` plus remaining external-host checks.
