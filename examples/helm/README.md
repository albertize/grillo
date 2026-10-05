# Local Helm example

Requires an explicitly installed **Helm v4.2.2** in `PATH`. Run from the
repository root after `make build`:

```sh
bin/grillo plan examples/helm/demo --release demo
bin/grillo up examples/helm/demo --release demo
bin/grillo down demo
```

Rendering requires no cluster or kubeconfig. `up` additionally requires the
normal Linux/KVM runtime prerequisites and guest artifacts.

Ordered values overrides use a positional chart and repeated `-f` (paths are
relative to the invoking working directory):

```sh
bin/grillo plan examples/helm/demo --release demo -f values.yaml -f values.local.yaml
```

The release defaults to the chart directory basename. `Chart.yaml` may also be
passed directly. Only namespace `default` is supported. The example declares
`Recreate`; charts using the Kubernetes default RollingUpdate must explicitly
consent with `--allow-degraded=kubernetes.rollout_strategy`.

Local/cached `plan` does not create runtime state or populate the secret store. Offline plans
preview creation against empty observations; secret versions use a fixed
`unresolved-offline` marker and do not describe live secret-update diffs.
`up` validates before persisting secrets as private, randomly versioned references
and calling the shared local API.

## Exact-version OCI charts

```sh
# Explicit consent to first fetch; no latest/ranges or automatic refresh.
bin/grillo plan oci://registry.example.com/charts/demo --version 0.1.0 --fetch-chart

# After fetching, render/apply using only the verified cached chart.
bin/grillo plan oci://registry.example.com/charts/demo --version 0.1.0
bin/grillo up oci://registry.example.com/charts/demo --version 0.1.0
```

Replace the illustrative registry with an actual HTTPS registry. Supply
`--chart-digest sha256:<64 lowercase hex characters>` when an independently
trusted manifest digest is available. Without it, the first HTTPS resolution
establishes a trust-on-first-use pin; SHA-256 verification is not signature
verification. Cache entries under `$XDG_CACHE_HOME/grillo/helm/charts` are private,
validated on every use, and never silently refreshed or repaired. Explicit fetch
permits cache writes even during `plan`; it does not write desired runtime state
or populate the secret store. Cached charts require no network or cluster.

Self-contained v2 charts are supported; dependencies/subcharts are rejected,
even if locked, and lockfiles are never updated. The initial OCI subset accepts
one Helm config and one chart-content layer, not indexes/provenance artifacts.
No private-registry credential CLI or automatic tool installation is provided.

T21's rendering/CLI gates are complete; the T22 runtime acceptance gate is not.
See [compatibility](../../docs/compatibility.md) and
[ADR 0007](../../docs/adr/0007-controlled-helm-renderer.md) for restrictions.
