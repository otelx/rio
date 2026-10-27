# Configuration and behavior reference

RIO is an experimental, single-process Go operator. It uses one cluster-scoped `Instrumentation` CR named `cluster`, a Pod admission webhook, and Kubernetes rolling updates. OBI is installed and configured separately.

The payload comes from [the image component](../payload/README.md). Application images do not need an SDK, an Injector, or instrumentation annotations. The operator owns a few bookkeeping annotations on workload templates and Pods.

Start with the [quickstart](quickstart.md) for a complete local installation.

## Configuration

Install [the CRD](../config/crd.yaml) and use [the example policy](../config/example.yaml). The API group is `rio.otelx.github.io/v1alpha1`, deliberately separate from the upstream OpenTelemetry Operator APIs.

```yaml
apiVersion: rio.otelx.github.io/v1alpha1
kind: Instrumentation
metadata:
  name: cluster
spec:
  image: ghcr.io/otelx/rio/instrumentation:v0.1.0-alpha.3
  endpoint: http://collector.observability.svc.cluster.local:4318
  signals:
    traces: true
    metrics: true
    logs: true
  timeoutSeconds: 300
  rules:
    - name: application-namespaces
      selector:
        namespaces: [checkout, inventory] # ['*'] selects all namespaces
      action: Inject
```

Rules run in order; the first match wins. No match means no injection. Namespaces are exact names or `'*'`; no namespace labels are required. An optional `selector.workloads` list narrows selection by workload `kind` and `name` (`'*'` matches all names of that kind). Namespace and workload selections are combined with AND. Entries within either list are combined with OR.

`action: Skip` provides exclusions. A rule can override `image`, `endpoint`, any individual `signals` switch, and `env`. Environment overrides merge by name. All unspecified settings inherit the CR defaults. The operator's own namespace is always excluded to keep admission and capability probes independent of the injection policy. Other namespaces, including system namespaces, are controlled by your rules. Prefer selecting named application namespaces. For a catch-all rule, add explicit Skip rules for system and telemetry namespaces before it.

Signal switches set `OTEL_TRACES_EXPORTER`, `OTEL_METRICS_EXPORTER`, and `OTEL_LOGS_EXPORTER` to `otlp` or `none`. OTLP HTTP/protobuf is used across the five runtimes. These switches configure the agents' exporters: actual signals depend on the language agent and the application's supported libraries. The logs switch does not install container stdout collection. `env` accepts additional `OTEL_*` variables, including sampler configuration and per-signal endpoints, with Kubernetes `valueFrom` references resolved in each application's namespace. Typed settings and Injector bootstrap variables cannot be overridden through `env`. Application init containers are left untouched.

## Rollout and recovery

The reconciliation loop runs every three seconds. For Deployments, StatefulSets, and DaemonSets using `RollingUpdate`, a change in effective configuration changes the Pod-template revision annotation. Kubernetes performs replacement using the workload's existing readiness checks, minimum-ready duration, rollout settings, and termination grace periods. The operator does not replace application images, commands, environment, or rollout strategies in the workload template. For Deployments, use `maxUnavailable: 0` and `maxSurge: 1` to retain an available old replica while a replacement starts.

The operator reports paused Deployments, `Recreate`, `OnDelete`, and partitioned StatefulSet updates as unsupported rather than changing those strategies. Other Pod-producing resources can receive injection at admission, but automatic restarts/removal of existing Pods are limited to the three rollout controllers.

Each workload records its current and last successful instrumentation revisions. The same resolved configuration is carried in its Pod template. Admission uses that snapshot, including during rollback, so an updated central policy cannot reinject a broken revision into recovery Pods. State survives operator restarts.

During an instrumentation rollout, three candidate-container crashes in `CrashLoopBackOff`, or expiration of `timeoutSeconds`, restores the last successful instrumentation configuration. On first-install failure, that is uninstrumented. Rollback affects the failed workload and the operator's settings; application changes made concurrently remain intact. Failed, unready StatefulSet candidate Pods are deleted with UID preconditions and ordinary grace periods after the previous template is restored, allowing `OrderedReady` recovery to proceed.

Failed configuration hashes stay blocked for that workload. Correct the effective configuration, or change `spec.retryToken` to explicitly retry. The retry token is global and requests a new revision for selected workloads. A recovery that itself fails is reported as `RecoveryFailed`; it is not retried forever. Workload health, not telemetry arrival, determines rollout success and rollback. Subsequent, unrelated application failures outside an instrumentation rollout are not treated as evidence that injection caused the failure.

Removing selection or deleting the CR rolls managed workloads back to uninstrumented Pods. A finalizer keeps the operator and webhook involved until managed-workload cleanup finishes. If cleanup fails or a workload's strategy has become unsupported, its status explains why deletion is still pending. Harmless revision/state annotations remain for bookkeeping; injected volumes, init containers, and environment settings disappear from replacement Pods.

Inspect progress with:

```bash
kubectl get instrumentations.rio.otelx.github.io cluster
kubectl get instrumentations.rio.otelx.github.io cluster -o yaml
kubectl logs -n rio-system deployment/rio
```

The webhook uses `failurePolicy: Ignore`. If it is unavailable, Kubernetes may create healthy uninstrumented Pods. The operator reports `InjectionMissing` instead of claiming that instrumentation succeeded or rolling back a healthy application. Correct the webhook and request a new revision to retry.

### Snapshot authority and admission

The cluster policy authorizes new instrumentation configurations. Saved configurations are a recovery mechanism for Deployments, StatefulSets, and DaemonSets managed by RIO; they are not a Pod opt-in API. All annotation names below use the `rio.otelx.github.io/` prefix.

| Annotation | Authority and admission behavior |
| --- | --- |
| `configuration` | Immutable resolved configuration, accepted only when it equals the authenticated active rollout state and the live workload template. |
| `workload-uid` | Binds the snapshot to the live managed workload UID in the Pod's namespace. Admission also resolves and verifies every controller reference, including the Deployment's ReplicaSet. Copying annotations to another workload or namespace does not authorize injection. |
| `revision` | Configuration consistency hash. It must match the authenticated active configuration; a matching hash alone grants no authority. Pods from stale templates are rejected rather than injected with an obsolete or latest failing configuration. |
| `state` and `state-signature` | Current, successful, and failed rollout history authenticated with HMAC-SHA256, bound to API version, kind, namespace, name, and workload UID. The controller verifies state before using it or signing its successor. Editing state or copying it to a recreated workload invalidates it. |
| `applied-revision` | Requests validation of an existing injection. Admission verifies the authorized revision, delivery mode, payload volume, application mounts, copy init container when used, and managed environment variables before accepting reinvocation. Missing or conflicting injection components reject the Pod. |

Owner-based selection and saved snapshots require an authenticated Pod CREATE from the Kubernetes controller manager (`system:kube-controller-manager`) or its standard `kube-system` controller service accounts: `replicaset-controller`, `statefulset-controller`, `daemon-set-controller`, or `job-controller`. Kubernetes supplies the admission identity; Pod annotations and owner references cannot supply it. Clusters using different controller identities need an explicit implementation adaptation before using managed snapshots. Do not grant application users permission to impersonate these identities.

Direct Pods and Pods created by other identities use the current central policy as kind `Pod`, even if they supply owner references. Standalone ReplicaSets, Jobs, and CronJobs created by the standard controllers use their resolved workload kind with the current central policy. These unmanaged workloads do not accept saved `configuration`, `workload-uid`, or `revision` annotations. Remove those annotations from application manifests. They cannot enable injection or choose an alternative payload. A forged `applied-revision` cannot suppress authorized injection: admission rejects it unless the complete injection contract is present.

The operator generates an immutable `rio-snapshot-authority` Secret in its namespace on first startup and reuses it after restarts. The installation grants the operator permission to create Secrets only in that namespace and to read only this named Secret. Protect and back up this Secret alongside workload state. Policy writers, operator administrators, holders of the signing key, and identities allowed to impersonate Kubernetes controllers are trusted administrators. Workload editors can change application templates, but cannot mint or edit authenticated configurations. Authenticated history is scoped to the same workload UID; it is not a general authorization token for another workload. This mechanism governs RIO's mutation; it does not restrict users from installing instrumentation themselves in application images or Pod specifications.

Rollback atomically makes the saved successful configuration the active authenticated state and template. Admission uses that configuration even while the central policy still contains the failing candidate, including after an operator restart. No configuration fields are inherited from that candidate.

### Migrating unsigned state and recovering the authority key

Versions predating authenticated snapshots leave unsigned `state` annotations. RIO refuses to adopt that history automatically, because it cannot distinguish genuine old state from user-supplied configuration. Before upgrading a cluster with existing managed workloads, export their manifests and state, verify the saved successful configuration as a platform administrator, and set the central policy to the intended known-good configuration. Stop the old operator, remove the legacy `state` annotation on each managed workload and the `configuration`, `revision`, and `workload-uid` annotations on its Pod template, then start the new operator with the updated installation RBAC. This starts a new managed rollout under the central policy. Keep the old backup until the new rollout is healthy; the initial recovery baseline for this new history is uninstrumented. Existing Pods are replaced through normal rollout behavior.

Do not delete or rotate `rio-snapshot-authority` while retaining signed workload state. If the Secret is lost, restore the original key from a trusted backup before restarting the operator. If it cannot be restored, use the same explicit administrator verification and state reset procedure above, also removing `state-signature`. A new key cannot authenticate old recovery history. Invalid signatures, malformed state, and stale snapshots are reported as errors instead of silently becoming trusted configuration.

## Upgrading from alpha.1 or alpha.2

The `v0.1.0-alpha.3` release changes both image paths:

| Component | Previous image | New image |
| --- | --- | --- |
| Operator | `ghcr.io/otelx/rio` | `ghcr.io/otelx/rio/operator` |
| Instrumentation | `ghcr.io/otelx/rio-payload` | `ghcr.io/otelx/rio/instrumentation` |

Use the `v0.1.0-alpha.3` installation files and run `./scripts/install.sh` to update the operator Deployment. Update your `Instrumentation` manifest's `spec.image` and any `rules[].image` overrides to the new instrumentation path and release tag, then apply the policy. The policy change triggers instrumentation rollouts for supported managed workloads. Recreate other selected Pods to apply the new image to them. Update image references in deployment automation and registry mirrors as well.

The retired `ghcr.io/otelx/rio` and `ghcr.io/otelx/rio-payload` packages have been deleted. Images for alpha.1 and alpha.2 can no longer be pulled from GHCR. Use alpha.3 or later under the new paths; source tags and release archives remain available. For local development, follow the [source build instructions](development.md).

Update old policy image references, including rule overrides, before recreating or rescheduling instrumented Pods. Saved recovery revisions may also reference retired images; a recovery that needs to pull a deleted image cannot succeed unless that image is already cached or supplied through a maintained registry mirror. Cross-release upgrade and recovery validation is tracked in [issue #6](https://github.com/otelx/rio/issues/6).

## Automatic payload delivery

Delivery is automatic and never changes application scheduling:

1. Read Kubernetes and node kubelet/container-runtime versions. Known minimum runtimes are containerd 2.1 and CRI-O 1.31; older or unknown runtimes fall back.
2. On potentially compatible clusters, create an isolated probe Pod on each registered Linux node. It mounts the actual payload image and checks that the Injector library can be loaded. This verifies API/kubelet feature gates and runtime mounting, not just version numbers.
3. Use a native read-only image volume only after all those nodes pass. Otherwise, use the same image as an init container copying into an `emptyDir`, mounted read-only in application containers.

Checks are deliberately cluster-wide, including nodes that a particular workload might not use. Mixed, unknown, or unavailable nodes therefore cause conservative fallback. New nodes are considered on the next reconciliation; already admitted Pods cannot be changed retroactively. Completed probe Pods cache results by node identity, runtime/kubelet versions, and payload image reference. They live in the operator namespace and are removed when it is uninstalled. Use immutable image tags or digests rather than changing bytes behind an existing reference.

The payload is Linux-only. The copy init container uses UID 0, drops all Linux capabilities, and copies readable files into its own volume; namespaces requiring every container to run nonroot need a compatible volume-permissions adaptation. Native image-volume injection does not add an init container.

## Development and validation

See [development](development.md) for builds and test commands, and [compatibility](compatibility.md) for supported environments and current limits.

## Origins

See [provenance](provenance.md) for the relationship to the Grafana prototype, the OpenTelemetry Injector, and the upstream OpenTelemetry Operator.
