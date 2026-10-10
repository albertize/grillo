# D0/T24 — user-reported second-host Fedora 44 checks

**Status: partial external-host evidence; acceptance remains incomplete.**
This records the user's submitted command/output transcript, not commands executed
by the coding agent. Private paths and the UI bootstrap credential are omitted.
No support, publisher authentication or redistribution clearance is inferred.

## Environment and identity

- User-reported second host: Fedora Linux 44 Workstation Edition, stable.
- Kernel: `Linux 7.2.9-200.fc44.x86_64`.
- SELinux: **Permissive**. Standard Enforcing policy compatibility is not proven;
  the previously recorded Enforcing networking blocker remains.
- Version: dev; commit `49861203f1deca1173d0431372051e753d577bbc`;
  build time unknown; guest ABI 1.1.
- Inventory digest:
  `4be77fb72bf28ecd7cfc79af2d2ebfac8d627671eb73337322be4da3b57702e6`.
- Actual archive/CLI/daemon byte hashes, host helper versions, fresh-cache status,
  source/build-tool absence and moved/read-only prefix are not recorded here.
  Unchanged dev commit/guest inventory does not establish host patch identity.

## Submitted results

| Check | User-reported result | Scope |
|---|---|---|
| Compose first up | `apply: succeeded`; container web running | Installed example workload starts |
| Compose HTTP | `grillo-compose-ok` | Real loopback port 18080 response |
| Compose down | `down: succeeded`; inspection containers empty; curl connection refused | Workload/container and HTTP stop observations |
| Compose second up | `apply: succeeded`; HTTP `grillo-compose-ok` | Immediate up/down/up succeeds; no 401 in this transcript |
| UI | Loopback launch output and later user confirmation that UI works | User-confirmed functionality; closing-UI/terminal lifetime not demonstrated |
| Helm plan | One workload/sandbox/container, one service/route; CreateSandbox/UpdateEndpoints | Managed chart planning succeeds |
| Helm up | `apply: succeeded`; web running; loopback Ingress endpoint reported | Actual chart workload starts |
| Helm HTTP | `grillo-helm-ok` | Real reported Ingress endpoint response |
| Helm down | `down: succeeded`; both example applications inspect with empty containers | Container-level stop observations |
| Process listing | No output reported, but regex contains `qemu-system,` | Netns/pasta patterns included; QEMU cleanup not established by this filter |

The user typed `http:/127.0.0.1:18080/`; curl accepted it and returned the stated
responses. Reproduction instructions should use `http://127.0.0.1:18080/`.
No durations or resource/performance measurements were provided.

## Open findings and follow-up

1. Compose `published_endpoints` remains null despite reachable published HTTP.
   This is an inspection diagnostic defect, not evidence that no port exists.
2. Helm's stopped inspection retains route configuration without an `endpoint`.
   This alone is not evidence of a still-live listener; probe the previously
   returned URL after down to establish listener teardown.
3. The corrected process filter is:

   ```sh
   ps -u "$(id -u)" -o pid,args |
     grep -E '[q]emu-system|[g]rillo-netns|[p]asta'
   ```

   Account for any unrelated workloads; do not terminate processes by pattern.
   The always-available user daemon may remain running after workload down.
4. No ten-minute delayed retry/token rejection is demonstrated. The earlier
   [401 report and bounded token fix](t24-registry-token-refresh.md) remain
   relevant; immediate success does not prove the expired-token path remotely.
5. UI functionality is now confirmed by the user; still confirm CLI/terminal-independent
   lifetime and closing-only-UI lifetime,
   guest Service DNS, post-Helm-down HTTP refusal and complete owned backend cleanup.
6. Establish whether this is a clean target runtime environment and capture exact
   payload/helper identities before claiming clean-host installation acceptance.

The submitted URL contained a live-looking bootstrap credential. It must not be
copied to logs/reports. If the console is still active, stop that console instance
and use a newly launched instance for further testing; do not publish its URL.

## Verification and remaining gates

This is a documentation-only evidence update. Local Markdown links/fences and
`git diff --check` verified; no new Go/KVM/remote commands executed for this entry.
External tests above are explicitly attributed to the user. Prior local payload,
registry refresh and installed KVM results remain in their respective reports.
No complete external-host gate, standard-policy Fedora support, performance
budget, third-party source/license/provenance or release acceptance follows.
T24/D0 stay BLOCKED overall. Next: finish the bounded missing checks and fix
endpoint/ps presentation separately; preserve the earlier failures.
