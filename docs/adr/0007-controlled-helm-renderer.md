# 0007 — Controlled official Helm renderer

- Status: Proposed
- Date: 2026-10-05
- Related tasks: T21, T22
- Supersedes: none

## Context

[Specification section 9](../../grillo-project-specification.md#9-input-model-helm-and-kubernetes)
proposes Helm-compatible libraries. The [implementation plan](../../IMPLEMENTATION_PLAN.md#93-helm-and-kubernetes-mvp)
allows the official executable to avoid the Helm SDK's transitive dependencies.
Charts and values are untrusted and may contain secrets. Rendering must not
become a cluster install or an implicit dependency download.

## Options considered

- Official SDK: embeds the renderer, but adds a substantial dependency graph.
- Official executable: preserves upstream semantics with a small Go adapter,
  at the cost of explicit external-tool provisioning.
- Custom templating: rejected; recreating Helm is not this project's scope.

## Decision

Propose official Helm **v4.2.2** (Apache-2.0), invoked directly with `os/exec`.
This is the user-provisioned version, checked on every compilation; no tool
installation or download is automatic. The adapter uses `template
--dry-run=client`, fixed Kubernetes version `1.30.0`, Helm's version-pinned
built-in API set, namespace `default`, and a stable release name. No `install`,
cluster discovery, post-renderer, DNS enablement or dependency update runs.

Inputs are snapshotted into a private temporary workspace; rooted reads confine
chart access, and symlinks/special files are rejected. The child receives only
private Helm/HOME/plugin paths, an absent kubeconfig and locale settings. Values
are private files in caller order, never command-line values. Helm and Kubernetes
secrets are persisted as random-versioned references only on apply, after
validation. Offline creation plans use an unresolved version marker rather than
content-derived fingerprints and never populate the secret store.

Limits: 512 chart entries, 1 MiB per file, 16 MiB chart aggregate, 32 ordered
values files with a separate 16 MiB aggregate, 8 MiB rendered stdout, 64 KiB
stderr, and a 30-second compilation deadline (including fetch/render).
Temporary files are removed after compilation. Failures never return raw tool,
registry or YAML parser errors.

OCI charts use the existing OCI Distribution client, not Helm downloader/plugins.
Fetch is explicit (`--fetch-chart`) and requires exact SemVer (`--version`);
HTTPS-only transport also rejects redirect/token-realm downgrades and does not
inherit proxy credentials. An optional `--chart-digest` supplies a trusted
manifest SHA-256 pin. Otherwise the first HTTPS resolution is trust-on-first-use.
One Helm config and chart-content layer are supported; bytes, descriptor
sizes/hashes, metadata and archive safety are verified before atomic cache
publication and again on reuse. Linux flock serializes publication; mismatched
concurrent pins and corrupt records block rather than being overwritten.
Records are private, owner-checked, bounded and keyed by full reference/version.
Cached versions never refresh silently, even with fetch consent.

The supported subset is self-contained v2 charts: dependencies/subcharts are
rejected, with or without a lockfile. No dependency fetching/update is exposed,
and lockfiles are never rewritten. Values schemas (remote refs can fetch),
hooks/test hooks, CRDs, lookup, dynamic tpl and known random/time/crypto-salt/DNS
functions are rejected. Token scanning deliberately rejects matches in comments
and strings too. This is a compatibility restriction, not an OS sandbox.

## Evidence

- `/usr/bin/helm version --short`: `v4.2.2+gb05881c`.
- Upstream archive checksum metadata retrieved over HTTPS from
  `https://get.helm.sh/helm-v4.2.2-linux-amd64.tar.gz.sha256sum`:
  `9adafecab4d406853bba163a70e9f104f47dbbf65ce24b7653bae7e36150bcb6`.
  No tool archive was installed, and no checksum/signature verification of the
  distribution-provided `/usr/bin/helm` is claimed.
- Real Helm/package/CLI, HTTPS Distribution fixture, adversarial archive/cache,
  concurrency and cancellation tests pass; full acceptance evidence and actual
  checks are in the [T21 report](../experiments/t21-helm-renderer.md).

## Consequences

No new Go dependency is introduced. The CLI accepts local/OCI charts through the
shared Kubernetes compiler and local API; it does not manage VMMs directly.
Normal cached/local plan is read-only; explicit fetch consent permits cache
writes only. Chart archives may embed secrets, so cache records remain private.

Digest verification ensures integrity, not publisher authenticity or provenance
signatures. Public/anonymous-bearer registries are supported; no private registry
credential CLI is provided. Additional OCI layers/indexes, dependency resolution,
schemas and dynamic tpl remain unsupported. Same-user filesystem mutation and
malicious provisioned executables are not isolated by this adapter.

## Validation and follow-up

T21's acceptance gates are complete within the disclosed restricted subset.
This ADR remains Proposed pending maintainer review. Run T22's realistic chart
on the actual workload runtime; these rendering/CLI tests do not claim a KVM
Helm release. Revisit the renderer pin and add dependency/signature/credential
features only with corresponding policy, tests and documented consent boundaries.
