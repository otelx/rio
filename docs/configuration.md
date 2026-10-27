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

`action: Skip` provides exclusions. A rule can override `image`, `endpoint`, any individual `signals` switch, and `env`. Environment overrides merge by name. All unspecified settings inherit the CR defaults. The operator's own namespace, `kube-system`, `kube-public`, and `kube-node-lease` are always excluded. Pods and workload templates using `system-node-critical` or `system-cluster-critical` priority are also excluded. These exclusions apply in both the operator and the installed webhook's matching configuration; wildcard rules cannot override them. There is no supported override for these safeguards. If an older installation instrumented these workloads, restart those workloads after upgrading to replace their existing injected Pods; RIO no longer rolls them automatically. Prefer selecting named application namespaces, and add explicit Skip rules for telemetry namespaces before any catch-all rule.

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

The webhook uses `failurePolicy: Ignore`. If it is unavailable, Kubernetes may create healthy uninstrumented Pods. Startup before the first successful synchronization, an invalid policy, and a global synchronization failure also admit Pods unchanged. A global failure discards the cached policy instead of continuing to apply stale selection. Readiness is false while unsynchronized or invalid, and an invalid policy is reported with status phase `Invalid`. Once synchronization succeeds, injection resumes for new Pods.

Unselected Pods bypass mutation, pinned-configuration parsing, and unnecessary owner lookups. If a ReplicaSet or Job owner cannot be read and selection cannot be established, RIO logs the problem and admits the Pod unchanged. A selected Pod whose mutation is unsafe (for example, a conflicting `rio-payload` volume or invalid pinned configuration) is explicitly rejected. Kubernetes [failure policy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) ignores webhook call failures, including transport errors and timeouts; it does not override an explicit rejection returned by a reachable webhook.

Malformed persisted rollout state, unreadable templates, failed workload updates, and StatefulSet recovery errors appear as phase `Error` on the affected workload in `status.workloads` and in operator logs. The overall policy is `Degraded`, while healthy workloads continue reconciling and the admission snapshot remains available. RIO preserves unreadable state for repair rather than guessing a rollback revision. Restore that workload's state annotation from a known-good copy and fix any reported API permissions or update conflict. Cleanup keeps the finalizer until every managed workload can be safely restored; an unreadable workload must be repaired before CR deletion can finish.

The operator reports `InjectionMissing` for a healthy managed Pod that missed injection instead of claiming that instrumentation succeeded or rolling back a healthy application. Correct the webhook or policy synchronization and request a new revision to retry. Standalone Pods and Jobs are not automatically restarted; recreate them when instrumentation is needed.

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
