# 0012 — Compose startup dependency and health gates

- Status: Proposed (implementation explicitly requested by the user; formal review pending)
- Related tasks: T17, T19, T24
- Supersedes: startup-order-only and liveness-healthcheck limitations for the implemented subset

## Context

The user requested reducing Compose differences, specifically `depends_on`.
Inspection showed that the planner sorted sandbox IDs alphabetically without
using dependency declarations, and Compose healthchecks became liveness probes.
A compiler warning about ordering was not proof of actual startup ordering;
restart-on-unhealthy was not Compose behavior.

## Options considered

- Preserve degraded compilation without real ordering: misleading.
- Gate in the UI: violates the shared-runtime/frontend boundary and CLI parity.
- Add pure dependency ordering and actual runtime startup gates: appropriate
  shared behavior with explicit rejection of still-unimplemented contracts.

## Decision

Use the third option. Existing `dependsOn` names order all replicas deterministically
before consumers in the pure planner. Add optional typed `dependencyConditions`
(`service_started` / `service_healthy`) to the experimental IR. References, cycles,
zero-replica dependencies, duplicate dependencies and invalid conditions fail pure
validation. Healthy gates require an explicit health/readiness probe.

Before creating a consumer, the executor requires all dependency replicas to have
successfully started; `service_healthy` additionally requires actual observed
readiness. Gate waits are cancellation-aware and capped at two minutes. No sleeps
or simulated status in the browser, no continuous restart/stop coupling after
startup. The CLI and UI consume the same runtime observations.

Compose healthchecks map to readiness/health instead of liveness; unhealthy does
not invoke restart callbacks. Grillo still removes unhealthy Service endpoints and
models `start_period` as initial delay; those differences require explicit
`compose.degraded` consent. Command strings, CMD/CMD-SHELL arrays and NONE/disable
are represented. Fractional-second durations, unknown fields, `start_interval`
and image-inherited healthcheck definitions are rejected rather than discarded.
This is not full Docker/Compose health or lifecycle parity.

Long `depends_on` rejects `service_completed_successfully`, `restart: true` and
`required: false`. The guest status currently does not reliably report primary
container exit outcomes; a stopped state/default zero must not satisfy successful
completion. Adding real exit outcome evidence is a separate protocol/runtime task.

## Security and compatibility

The new IR field is additive and typed within the experimental v1alpha1 model;
public secret separation remains intact. No frontends control VMMs, no host shell,
new dependency, privileged fallback or automatic tool download. Probe commands run
inside their declared container. Updated compiler registries and human support
notes stay consistent. Invalid dependency syntax has structured diagnostics.

## Evidence and follow-up

[Unit/real-KVM evidence](../experiments/t24-browser-terminal-compose-dependencies.md)
proves ordering despite alphabetically earlier consumer IDs, no consumer during
failed health, release after actual guest probe success, unchanged dependency PID,
cancellation and negative parsing/validation paths. Record failed checks, not just
passing reruns. Implement completion outcomes, optional dependency semantics and
explicit-update restart propagation separately before expanding the support claim.
