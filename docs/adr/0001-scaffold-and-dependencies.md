# 0001 — Scaffold and dependency baseline

- Status: Proposed
- Date: 2026-10-03
- Related tasks: T00
- Supersedes: none

## Context

T00 needs reproducible, KVM-independent development without prematurely choosing
runtime components. No repository remote is configured. See the
[plan](../../IMPLEMENTATION_PLAN.md#3-dependency-and-tooling-policy).

## Options considered

- Standard library command dispatch versus a CLI framework: the scaffold needs
  only help, version, and an explicitly incomplete doctor.
- Eagerly adding every permitted dependency versus adding modules at first use:
  unused requirements obscure the actual dependency and vulnerability surface.
- Go 1.27.1 versus 1.26.8: both are stable supported release lines at inspection;
  use 1.26.8 to match the available environment.

## Decision

Propose Go 1.26.8 in go.mod and CI, with local builds using that toolchain.
The module identifier is `github.com/albertize/grillo`, matching the canonical
repository URL established by the maintainer. It was initially `grillo.local/grillo`
while no remote existed; the earlier history records that scaffold identifier.

The runtime module starts with **no external dependencies**. The planned YAML
parser is `go.yaml.in/yaml/v3` at `v3.0.5`; add and checksum it when frontend
work starts, after rechecking vulnerabilities. The plan-approved
`golang.org/x/sys/unix` module was added later at `v0.48.0` for the T01 guest
vsock listener (see [ADR 0003](0003-guest-initiated-shutdown.md) and
[progress](../progress.md)); it is confined to Linux/guest code. Do not add an
unused dependency just to produce a go.sum. Official x/* modules otherwise await
concrete consumers.

Pin the separately invoked official audit tool to
`golang.org/x/vuln/cmd/govulncheck@v1.8.0`. `make vulncheck` explicitly downloads
and executes it; ordinary `make check` does not. Tool dependencies are not
runtime dependencies. CI opts into this tool. No VMM, guest image, Helm, or
network helper is downloaded by the scaffold.

## Evidence

- go.dev release metadata lists Go 1.27.1 and 1.26.8 as stable.
- proxy.golang.org lists yaml v3.0.5; its go.mod declares `go.yaml.in/yaml/v3`.
- [Upstream README](https://github.com/yaml/go-yaml/blob/v3.0.5/README.md)
  identifies ongoing maintenance by the YAML organization.
- [Upstream license](https://github.com/yaml/go-yaml/blob/v3.0.5/LICENSE)
  includes MIT and Apache terms; preserve both when distributing it.
- The govulncheck v1.8.0 module requires Go 1.26.0 and has a separate tool-only
  dependency graph. The Go project uses BSD-style licensing.
- CI action commits were resolved from upstream v4.2.2 checkout and v5.5.0
  setup-go tags, rather than using floating action references.
- See [progress](../progress.md) for executed checks and limitations.

## Consequences

No framework, CGO runtime requirement, or speculative backend is introduced.
The Linux race detector requires a C compiler; produced binaries disable CGO.
Version output deliberately says `dev` and `runtime unavailable`; it is not a
release identifier. Agent builds validate the cross-build pipeline only and
must not be installed as guest PID 1. Doctor exits unsuccessfully until real
readiness checks exist. KVM tests fail closed until T01 provides real tests.

## Validation and follow-up

Maintainer review is pending; this ADR is not marked accepted. Revisit pins
through reviewed updates and rerun module/license/vulnerability checks. T01
must establish hardware evidence independently of the passing unit suite.
