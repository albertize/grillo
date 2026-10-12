# T24 acceptance checklist

This is a prepared runbook, not a release approval. T24/D0 remain **BLOCKED**.
Use the exact reviewed checkout, pinned compiler and rebuilt guest/binaries.
Dependencies T23 and its runtime gates have recorded evidence; the latest local
[continuation](experiments/t24-pinned-cache-block-quota-sources.md) distinguishes
verified pinned reuse, real opt-in volume enforcement and candidate local budgets
from incomplete workspace/security/source acceptance. The earlier
[registry failure](experiments/t24-autonomous-local-campaign.md) is preserved.

## Evidence required

| Gate | Receipt required | Current disposition |
| --- | --- | --- |
| Routine checks | `make check`, environment, commit plus uncommitted diff | Local PASS; rerun after changes |
| Advisory/module/frontend review | `make vulncheck`, `make audit`, npm advisory result, tool versions | Local Go 1.27.2 source scan and prior npm PASS; current runc/Helm binary scans FAIL (14/15 findings) |
| Fuzz | Smoke in hosted CI; longer local campaign, exit status and any reviewed corpus | Five 5-minute targets PASS locally; stalls explicitly retained |
| Cache and operation admission | Quota boundaries, concurrent processes, failure/cancel/retry and history bounds | CAS/rootfs and new API/TCP limits tested |
| Remaining storage/workspaces | Real enforced limits for volume writes, build work, other caches; no bind deletion | Opt-in block ENOSPC/persistence PASS; default directory/binds and native RUN remain unbounded; workspace/crash/repair gates BLOCKED |
| Runtime A–H | Actual KVM/frontends/probes/storage/recovery/UI gates; no SKIP | Local subset campaign PASS; not every fault/load combination |
| Application budgets | 30 cold/warm samples, failures, idle CPU, memory/disk definitions, regression thresholds | Seeded-CAS 30-repeat candidate local budgets PASS; not fresh online pulls or full scale/peak acceptance |
| Network budgets | Actual loopback and guest-network load separately | Loopback DNS/TCP measured; guest-load budget pending |
| Install | Verified artifact inventory, moved/read-only prefix, runtime PATH without build tools, cleanup | Local installed gate PASS; independent-host gate still required |
| Distribution review | Exact corresponding sources/config/build recipes/notices for every bundled component | 19 source inputs/83 crates and graph/ELF correspondence reviewed; exact compiler/native lineage, assembled source/notices and clearance still pending |
| Hosted CI | Published revision and actual hosted logs at the current toolchain | Pending; no hosted run claimed |
| External clean host | OS/kernel/device/helper versions, default policy, functional and cleanup/lifetime results | Pending; historical partial second-host observations do not close this gate |
| Maintainer decisions | Supported versions, publisher trust, release channels, security response | Pending; never infer or fabricate these |

## Local execution

Provision dependencies explicitly using [getting started](getting-started.md).
Do not silently download a VMM/browser or change device permissions/SELinux.
Set `HELM_BINARY` and `HELM_SHA256` to a deliberately trusted installed executable,
not a placeholder or a checksum inferred as publisher authentication.

```sh
make check
make vulncheck
(cd web && npm audit)
make fuzz FUZZTIME=5m
make test-cache-load test-license-inventory test-t24-admission
make bench-t24-network
make test-t24-volume-load test-t24-volume-quota
make test-t24-resources HELM_BINARY="$HELM_BINARY" HELM_SHA256="$HELM_SHA256"
# Optional deliberate live provisioning for verified repeated offline reuse:
make prepare-t24-fixture T24_FIXTURE_OUT=/new/explicit/output
make test-t24-resource-budget T24_SEED_CAS=/new/explicit/output/cas \
  HELM_BINARY="$HELM_BINARY" HELM_SHA256="$HELM_SHA256"
make test-t24-verified-lifecycle test-installed-runtime \
  HELM_BINARY="$HELM_BINARY" HELM_SHA256="$HELM_SHA256"
```

Record every command's exit status. Check for SKIP, FAIL, timeout and cleanup
failure, not just success markers. Resource tests need registry access for fresh
caches and can hit unauthenticated rate limits. Stop on failure; do not keep
repeating pulls or call cached/mocked execution a fresh-cache pass. Failed test
state may be retained: identify its unique operation before any explicit cleanup.
The collector does not change host page-cache state or establish release budgets.
Seeded profiles are explicitly named and verify their whole graph; no mocks or
mutable-tag offline reuse. Candidate local limits are checked separately, not
adopted as portable SLOs. mke2fs 1.47.3 is required only for the opt-in volume
path; no privileged host filesystem mount or automatic data migration is added.

## Independent runtime-host receipt

Follow [runtime layout](runtime-layout.md) and [host evidence](host-support.md)
with a deliberately transferred, verified prefix. No source checkout/build tool
on runtime PATH may be required. Do not claim distribution clearance merely
because the transferred package runs.

Record:

1. Exact package SHA-256, VERSION inventory and all file verification results;
   reviewed origin and binary/compiler/guest identities.
2. Linux/architecture/kernel, normal UID, KVM/vsock/tun access, helper versions,
   security-policy mode and namespace restrictions, without changing them.
3. `doctor` results and actual installed Compose/Helm plan/up/status/HTTP/DNS.
4. Moved/read-only prefix and arbitrary-CWD execution; local UI, safe logs/exec
   and workload lifetime after CLI/browser close. Never record bootstrap tokens.
5. Persistence across down/up, explicit ownership-aware teardown, no retained
   operation-created processes/leases/sockets, and retained user data.
6. Every failure/degradation/skip, with sanitized diagnostics and no secrets.

T24 closure must review specification §28 and scenarios A–H against these
receipts. Recording a blocker is traceability, not a completed acceptance gate.
Do not start T25 or mark a supported MVP release on the strength of local tests.
