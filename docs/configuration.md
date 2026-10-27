# Configuration and behavior reference

RIO is an experimental, single-process Go operator. It uses one cluster-scoped `Instrumentation` CR named `cluster`, a Pod admission webhook, and Kubernetes rolling updates. OBI is installed and configured separately.

The payload comes from [the image component](../payload/README.md). Application images do not need an SDK, an Injector, or instrumentation annotations. The operator owns a few bookkeeping annotations on workload templates and Pods.

Start with the [quickstart](quickstart.md) for a complete local installation.

See [compatibility](compatibility.md) for supported environments, signals, API compatibility, and migration limits.

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

Unselected Pods without a complete snapshot from an authenticated controller bypass mutation, pinned-configuration parsing, and unnecessary owner lookups. A complete candidate still requires live owner and signed-state verification; invalid hints leave an unselected Pod unchanged. If a ReplicaSet or Job owner cannot be read and selection cannot be established, RIO logs the problem and admits the Pod unchanged. A selected Pod whose mutation is unsafe (for example, a conflicting `rio-payload` volume or invalid pinned configuration) is explicitly rejected. Kubernetes [failure policy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) ignores webhook call failures, including transport errors and timeouts; it does not override an explicit rejection returned by a reachable webhook.

Malformed persisted rollout state, unreadable templates, failed workload updates, and StatefulSet recovery errors appear as phase `Error` on the affected workload in `status.workloads` and in operator logs. The overall policy is `Degraded`, while healthy workloads continue reconciling and the admission snapshot remains available. RIO preserves unreadable state for repair rather than guessing a rollback revision. Restore that workload's state annotation from a known-good copy and fix any reported API permissions or update conflict. Cleanup keeps the finalizer until every managed workload can be safely restored; an unreadable workload must be repaired before CR deletion can finish.

The operator reports `InjectionMissing` for a healthy managed Pod that missed injection instead of claiming that instrumentation succeeded or rolling back a healthy application. Correct the webhook or policy synchronization and request a new revision to retry. Standalone Pods and Jobs are not automatically restarted; recreate them when instrumentation is needed.

### Snapshot authority and admission

The cluster policy authorizes new instrumentation configurations. Saved configurations are a recovery mechanism for Deployments, StatefulSets, and DaemonSets managed by RIO; they are not a Pod opt-in API. All annotation names below use the `rio.otelx.github.io/` prefix.

| Annotation | Authority and admission behavior |
| --- | --- |
| `configuration` | Immutable resolved configuration, accepted only when it equals the authenticated active rollout state and the live workload template. |
| `workload-uid` | Binds the snapshot to the live managed workload UID in the Pod's namespace. Admission also resolves and verifies every controller reference, including the Deployment's ReplicaSet. Copying annotations to another workload or namespace does not authorize injection. |
| `revision` | Configuration consistency hash. It must match the authenticated active configuration; a matching hash alone grants no authority. Pods from stale templates are rejected rather than injected with an obsolete or latest failing configuration. |
| `state` and `state-signature` | Current, successful, and failed rollout history authenticated with HMAC-SHA256, bound to API version, kind, namespace, name, and workload UID. The controller verifies state before using it or signing its successor. Editing state or copying it to a recreated workload invalidates it. |
| `applied-revision` | Requests validation of an existing injection. Admission verifies the authorized revision, delivery mode, payload volume, application mounts, copy init container when used, and managed environment variables before accepting reinvocation. Missing or conflicting injection components reject the Pod. |

Owner-based selection and saved snapshots require an authenticated Pod CREATE from the Kubernetes controller manager (`system:kube-controller-manager`) or its standard `kube-system` controller service accounts: `replicaset-controller`, `statefulset-controller`, `daemon-set-controller`, or `job-controller`. Kubernetes supplies the admission identity; Pod annotations and owner references cannot supply it. Owner references from other identities are checked for availability but cannot select workload-specific policy rules or authorize saved snapshots. Clusters using different controller identities need an explicit implementation adaptation before using managed snapshots. Do not grant application users permission to impersonate these identities.

Direct Pods and Pods created by other identities use the current central policy as kind `Pod`, even if they supply owner references. Standalone ReplicaSets, Jobs, and CronJobs created by the standard controllers use their resolved workload kind with the current central policy. For selected Pods, these unmanaged workloads do not accept saved `configuration`, `workload-uid`, or `revision` annotations. Unselected Pods without authenticated active history and requests whose owner cannot be verified are admitted unchanged; arbitrary annotations cannot select them for injection. Remove those annotations from application manifests. They cannot enable injection or choose an alternative payload. A forged `applied-revision` cannot suppress authorized injection: admission rejects it unless the complete injection contract is present.

The operator generates an immutable `rio-snapshot-authority` Secret in its namespace on first startup and reuses it after restarts. The installation grants the operator permission to create Secrets only in that namespace and to read only this named Secret. Protect and back up this Secret alongside workload state. Policy writers, operator administrators, holders of the signing key, and identities allowed to impersonate Kubernetes controllers are trusted administrators. Workload editors can change application templates, but cannot mint or edit authenticated configurations. Authenticated history is scoped to the same workload UID; it is not a general authorization token for another workload. This mechanism governs RIO's mutation; it does not restrict users from installing instrumentation themselves in application images or Pod specifications.

Rollback atomically makes the saved successful configuration the active authenticated state and template. Admission uses that configuration even while the central policy still contains the failing candidate, including after an operator restart. Authenticated active recovery history also remains valid when the current policy is absent, deleting, or no longer selects the workload, so a failed removal can restore the successful revision. Ordinary creators and invalid snapshots cannot use this exception. No configuration fields are inherited from the failing candidate.

### Migrating unsigned state and recovering the authority key

Versions predating authenticated snapshots leave unsigned `state` annotations. RIO refuses to adopt that history automatically, because it cannot distinguish genuine old state from user-supplied configuration. Export the policy, manifests, and state. With the old operator running, disable selection and verify healthy uninstrumented replacements with both current and stable configurations disabled. Stop the old operator, remove `state` and `state-signature` from those verified workload objects and `configuration`, `revision`, and `workload-uid` from their Pod templates, then install the candidate with updated RBAC. Reapply a known-good policy to start authenticated history with an uninstrumented recovery baseline. Keep the backup until this rollout is healthy. Never clear active or failed history as a shortcut around cleanup.

Do not delete or rotate `rio-snapshot-authority` while retaining signed workload state. Uninstalling the operator namespace deletes this key; preserve it for reinstall or explicitly reset verified disabled history before reinstalling. TLS certificate renewal preserves the separate authority Secret. If the Secret is lost, restore the original key from a trusted backup before restarting the operator. If it cannot be restored, use the same explicit administrator verification and state reset procedure above, also removing `state-signature`. A new key cannot authenticate old recovery history. Invalid signatures and malformed state are reported as `Error` for the affected workload, without blocking policy admission or reconciliation for other workloads. Stale Pod snapshots are rejected instead of silently becoming trusted configuration.

<a id="upgrading-from-alpha1-or-alpha2"></a>
<a id="retired-image-references"></a>

## Upgrading existing releases

Replace any retired image references with the current repositories:

| Component | Deleted repository | Current repository |
| --- | --- | --- |
| Operator | `ghcr.io/otelx/rio` | `ghcr.io/otelx/rio/operator` |
| Instrumentation | `ghcr.io/otelx/rio-payload` | `ghcr.io/otelx/rio/instrumentation` |

The retired `ghcr.io/otelx/rio` and `ghcr.io/otelx/rio-payload` packages have been deleted. Source tags and release archives remain available, but the old images cannot be pulled from GHCR. Do not recreate or reschedule old instrumented Pods before migrating: both their saved configuration and last successful recovery revision can reference the deleted payload. An image still cached on a node is not a durable recovery source. Keep a copy of your policy manifest and workload objects, including annotations, before upgrading.

The integration baselines are alpha.2 and alpha.3. Use this cleanup-first procedure for those releases; alpha.1 and other unvalidated transitions require the [clean reset](compatibility.md#operatorinstrumentation-versions-and-upgrades).

1. If workloads are already `RollingBack`, wait for `RolledBack` or `RecoveryFailed` and inspect their status before proceeding. Temporarily replace the rules with a single `Skip` rule selecting `namespaces: ["*"]` and allow a sufficient cleanup deadline (for example `timeoutSeconds: 300`), keeping the operator, webhook, and CR running. For each previously managed workload, wait for `Disabled` and healthy replacement Pods without `rio-payload` volumes or `LD_PRELOAD`. Check that both `current.enabled` and `stable.enabled` in the workload's `rio.otelx.github.io/state` annotation are false. This establishes an uninstrumented recovery baseline without pulling the retired payload. Standalone Pods and other unsupported rollout resources must be recreated from their original, uninstrumented manifests while selection is disabled.
2. Alpha.2 and alpha.3 history is unsigned. After the disabled baseline is verified, stop the historical operator and reset its bookkeeping as described in [unsigned-state migration](#migrating-unsigned-state-and-recovering-the-authority-key). Run the candidate's `./scripts/install.sh` with its operator image. This applies the CRD and installation resources and waits for the operator Deployment. It preserves the TLS Secret; use the renewal command below if certificates need replacement. Admission can be unavailable briefly during the single replica's Recreate rollout.
3. Update `spec.image` **and every `rules[].image` override** in your saved policy to `ghcr.io/otelx/rio/instrumentation` with the intended immutable release tag or digest. Restore the desired rules and apply the policy. Update deployment automation and registry mirrors as well. Wait for `Ready`, then verify replacement Pods have matching `revision` and `applied-revision` annotations and use the intended payload. Once healthy, both current and stable snapshots should reference the new image. A failed first rollout recovers to the uninstrumented baseline, without requiring deleted packages.
4. Verify real application telemetry. Recreate selected standalone Pods using the new policy. Keep the baseline and evidence until the upgrade is accepted.

For example, save the policy and inspect state before disabling selection:

```bash
kubectl get instrumentations.rio.otelx.github.io cluster -o yaml > policy-before-upgrade.yaml
kubectl get deployments,statefulsets,daemonsets -A -o yaml > workloads-before-upgrade.yaml
kubectl patch instrumentations.rio.otelx.github.io cluster --type=merge -p '{"spec":{"timeoutSeconds":300,"rules":[{"name":"migration-baseline","selector":{"namespaces":["*"]},"action":"Skip"}]}}'
kubectl get instrumentations.rio.otelx.github.io cluster -o yaml
```

Do not apply the exported status/resource metadata as a replacement manifest; edit your original policy manifest. Keep backups private if they contain application configuration. The integration suite verifies that an attempted upgrade before cleanup reports unsigned state as an error without overwriting successful or blocked history. It returns to the historical controller to establish the disabled baseline, then resets unsigned bookkeeping and upgrades. No historical annotation is automatically authorized.

### Controller rollback and payload rollback

The historical alpha.2 and alpha.3 controllers can read the candidate's CRD and configuration JSON, but cannot authenticate or issue signed rollout history. The integration suite exercises a temporary diagnostic rollback with unchanged policy/configuration and verifies that both raw state and its signature remain byte-for-byte unchanged before returning to the candidate. Do not change instrumentation policy or recovery state under a historical controller: any unsigned rewrite requires cleanup and explicit reset before returning to the candidate. Keep the authority Secret and candidate CRD intact. Historical controllers also lack current admission safeguards; restrict selection to named, noncritical application workloads during diagnosis. Use retained or privately mirrored images, never deleted GHCR packages. These tests do not establish general cross-version downgrade support.

Payload rollback is an explicit policy update to an available, immutable image, including overrides. It changes the effective revision and goes through the normal readiness and recovery checks. The alpha.3 upgrade scenario explicitly switches the default and override images back to its retained alpha.3 fixture and then restores the candidate, checking admission and current/stable snapshots after each rollout. Restoring a saved annotation manually is unsupported. Rollback to the deleted alpha.1/alpha.2 payload packages is unsupported without a maintained mirror; the supported recovery when those images are unavailable is to disable selection, establish the uninstrumented baseline, and reapply a corrected policy under the new paths. A failed revision remains blocked until its effective configuration or `retryToken` changes. The test deliberately removes the historical payload fixture from every test node after cleanup and verifies that a failed migration still recovers.

## Webhook certificate renewal

The installer creates a one-year self-signed certificate and reuses it on reinstall. There is no automatic renewal. Monitor its expiration, schedule renewal at least 30 days before expiry, and use the candidate's supported single-replica renewal command in the intended kubectl context:

```bash
kubectl config current-context
kubectl get secret rio-tls -n rio-system -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -dates -fingerprint -sha256
./scripts/renew-certificates.sh
```

The command first adds overlapping old/new certificate trust to the webhook CA bundle, replaces `rio-tls`, and restarts the operator (the serving certificate is loaded at startup). It verifies the actual serving certificate through a local port-forward with TLS hostname and trust validation before replacing the CA bundle with only the new certificate. It requires kubectl access to read/update the Secret and webhook configuration, restart/read the Deployment, and port-forward Pods, plus OpenSSL and Python 3. Private keys are generated under private `.artifacts/renewal/` directories and removed on exit. Run one installer or renewal operation at a time per cluster. If interrupted, retain the overlapping trust and rerun the renewal command; do not delete the Secret or reduce trust by hand. An installer rerun is not a certificate renewal.

The same command recovers an already expired certificate. The integration suite installs an expiring certificate and an expired one, observes real API-server admission (with `failurePolicy: Fail` only in its isolated test cluster), renews each, and verifies injected dry-run Pods and the final CA bundle. Production keeps `failurePolicy: Ignore`; during certificate failure or a single-replica restart, Pods can be admitted without instrumentation. Pause application rollouts if that gap is unacceptable. After renewal, inspect `InjectionMissing` workloads and change `spec.retryToken` to request fresh instrumented Pods. Recreate standalone Pods that missed injection. Deployment readiness alone does not prove certificate health or admission success.

### Certificate and cleanup diagnostics

Inspect the certificate dates with the command above, compare the Secret certificate with `webhooks[0].clientConfig.caBundle` in `kubectl get mutatingwebhookconfiguration rio -o yaml`, inspect `kubectl get endpointslices -n rio-system -l kubernetes.io/service-name=rio`, and check `kubectl logs -n rio-system deployment/rio`. Look for expired/not-yet-valid certificates, unknown authority, TLS handshake errors, missing endpoints, and `InjectionMissing`. Check cluster/node clocks for validity errors. Renew certificates rather than disabling TLS checks. Verify a newly created selected Pod has `rio.otelx.github.io/applied-revision` matching its expected revision, then verify application telemetry.

For a policy stuck deleting, keep the operator, webhook, namespace, and CRD installed. Inspect the policy's per-workload phase/message, the workload's `rio.otelx.github.io/state`, rollout status, Pods, events, and controller logs. Restore working admission first. Fix unsupported rollout strategies, paused Deployments, resource shortages, image pull failures, or application readiness. `RollingBack` waits for recovery or its saved deadline; `RecoveryFailed` stops automatic retries. Correct the effective policy or retry token when the policy is still editable; deletion resolves toward an uninstrumented configuration. Do not remove revision/configuration/state annotations to retry a live instrumented workload: admission uses them to keep recovery consistent.

If removal remains stuck, restore each affected workload from its original uninstrumented template while preserving application changes, and replace its Pods. Check that no live Pod has the `rio-payload` volume, copy init container, injected environment, or preload settings, including standalone Pods. Only after all affected Pods are verified uninstrumented may an administrator remove the `rio.otelx.github.io/remove-instrumentation` finalizer from the deleting policy, preserving any unrelated finalizers, and rerun `./scripts/uninstall.sh`. Removing the finalizer earlier can strand instrumented Pods and saved references. Never delete the CRD first as a cleanup shortcut.

The steps above describe the image rename, not a validated in-place cross-release upgrade. For the supported migration approach and treatment of saved state, follow [operator/instrumentation versions and upgrades](compatibility.md#operatorinstrumentation-versions-and-upgrades). Cached retired images do not provide a durable recovery guarantee.

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
