# 0003 — Guest-initiated shutdown for the Firecracker spike

- Status: Proposed
- Date: 2026-10-03
- Related tasks: T01, T03, T08
- Supersedes: none

## Context

T01 must start and stop a real microVM. Firecracker has no `InstanceStop` API; the
host can either request a guest reset (`SendCtrlAltDel`) or terminate the VMM
process. A stop path that silently fails or requires `SIGKILL` every time would
undermine the lifecycle contract planned for T08.

## Options considered

- **Host `SendCtrlAltDel` only.** Matches a common Firecracker example, but the
  pinned guest config (`microvm-kernel-ci-x86_64-6.1.config`) has `CONFIG_VT`
  without `SERIO`/`I8042`, so the injected keys have no guest path. Observed: the
  API returns 204 and the VM keeps running.
- **Host `SIGKILL`/`SIGTERM` of the VMM.** Reliable but abrupt; it hides whether
  the guest could shut down and complicates clean teardown and measurement.
- **Guest-initiated reset.** The guest calls `reboot(LINUX_REBOOT_CMD_RESTART)`;
  with `reboot=k` the x86 machine-restart path uses the keyboard-controller reset
  that Firecracker turns into process exit. Optionally keep host
  `SendCtrlAltDel` and `SIGKILL` as fallbacks.

## Decision

For the T01 spike, stop is guest-initiated via `reboot(LINUX_REBOOT_CMD_RESTART)`
after the agent replies `BYE`. The host keeps `SendCtrlAltDel` as a best-effort
fallback and escalates to `SIGKILL` on the VMM process group if the process does
not exit within the configured timeout. This is an experiment-scoped decision;
T08 owns the production lifecycle and T03 may revisit it once the backend is
confirmed.

## Evidence

See [the T01 boot report](../experiments/t01-boot-spike.md). With guest-initiated
restart, 30/30 cycles stopped cleanly (stop median 127 ms) with no orphan
processes. `SendCtrlAltDel` alone did not stop the VM under the pinned config.

## Consequences

The guest agent must be able to request its own shutdown, and the runtime must
distinguish a clean guest exit from a timeout/kill. Guest-initiated shutdown
couples lifecycle to a cooperating agent; a hung or hostile guest still requires
host-side escalation, which bounds but does not eliminate abrupt termination.
Enabling i8042/ATKBD in a future guest kernel is an alternative to revisit, but
was avoided here to stay close to the pinned upstream config.

## Validation and follow-up

Maintainer review pending. T08 must define clean, timeout, and hung-guest
behavior and test each. If the backend changes after T03, re-evaluate whether
host-driven shutdown or a paravirtual signal is preferable.
