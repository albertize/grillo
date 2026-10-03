# T20 — Kubernetes MVP compiler

- Status: complete (real KVM evidence for multi-container execution)
- Date: 2026-10-03
- Related tasks: T04, T14, T20
- Command: `make test-k8s`, `make check`

## Goal

Compile a supported Kubernetes subset into the Grillo IR, reject what cannot be
represented faithfully, keep Secret values out of the public IR and diffs, and
run a compiled multi-container Pod on the real runtime.

## What is implemented

`internal/frontend/kubernetes` parses multi-document YAML and `kind: List` and
compiles:

- **Pod / Deployment / DaemonSet** — containers, init containers, sidecars,
  command/args, env and `envFrom` (configMap/secret refs with `optional`),
  volume mounts, ports, resources, exec/httpGet/tcpSocket probes, and the
  supported security context (runAsUser/runAsGroup, readOnlyRootFilesystem,
  capabilities, seccomp profile).
- **Service** — TCP ClusterIP, headless, selector, numeric or named
  `targetPort`.
- **Ingress** — `Exact` and segment-aware `Prefix` rules to Services, with TLS
  secret references.
- **ConfigMap** — `data` and `binaryData`.
- **Secret** — `data` and `stringData` (which takes precedence), `Opaque` only.
- **PersistentVolumeClaim** — `ReadWriteOnce`, Filesystem, storage request.

Selectors must match the template labels; only the default namespace is
accepted; unknown fields are rejected. A `Deployment` with the default
`RollingUpdate` strategy is a downgrade: Grillo replaces sandboxes (Recreate),
and the diagnostic must be accepted with
`--allow-degraded=kubernetes.rollout_strategy`.

## Evidence

```
--- PASS: TestKVMKubernetesMultiContainer (1.2s)
ok  grillo.local/grillo/internal/executor
```

The KVM test compiles a Deployment with one init container and two application
containers (app + sidecar), applies it with the production rootless topology,
and verifies that all three containers run, the sidecar reaches the app over
shared localhost, and writes the fetched page to an `emptyDir` volume.

Compiler unit tests cover the deployment golden, the secret-leak check, the
RollingUpdate consent gate, explicit Recreate, privileged/hostNetwork/Tier3/
NodePort rejection, multi-container Pods, `List`, namespace rejection, unknown
fields, and selector mismatch. `TestPlanKubernetesInput` and
`TestPlanKubernetesRejectsPrivileged` exercise the CLI path.

## Secrets

Secret values are returned separately from the compiler and written to the local
secret store by the CLI. The public IR contains only `SecretRef`s
(name + version). `TestSecretValuesNeverEnterPublicIR` asserts the value never
appears in the canonical IR or the redacted public view. The executor resolves
`secretKeyRef` env variables from the store at apply time.

## Known limitations

- The MVP omits StatefulSet, Job, and CronJob (F4), NetworkPolicy, and
  projected/configMap/secret/downwardAPI volumes; all are rejected with a code.
- `fieldRef` environment variables are not implemented.
- Scheduling fields (nodeSelector, affinity, tolerations) are rejected.
- `docs/compatibility.md` records the full matrix.

## Conclusion

The Kubernetes MVP compiler meets its gate: goldens and rejection tests pass,
secrets never enter diffs, the default RollingUpdate is disclosed as a Recreate
downgrade requiring consent, and a compiled multi-container Pod runs on KVM.
