# Compatibility and operational limits

RIO is currently alpha and supports evaluation only. This document defines the deliberately limited support contract for the first normal release, `v0.1.0`; it does not announce that release or establish production readiness. “Supported” means eligible for investigation and fixes within the [maintenance policy](../SECURITY.md), without an availability or resolution SLA. All other combinations remain experimental or unsupported as specified below.

## First normal release scope

| Dimension | Supported scope for `v0.1.0` | Evidence and limits |
| --- | --- | --- |
| Kubernetes | 1.35.0, Linux nodes | Two-node kind clusters in CI. Other Kubernetes versions and distributions are evaluation targets until separately validated. |
| Architecture | amd64 and arm64 (aarch64) | Native CI workers test each architecture; common image tags select the matching platform. A heterogeneous cluster is not an end-to-end CI fixture. |
| Container runtime | containerd as packaged in `kindest/node:v1.35.0` | CRI-O is capability-detected but has no end-to-end validation and remains experimental. |
| Application runtime and libc | Node.js 24.22.1 with glibc 2.36 on Debian 12 (`node:24`), or musl 1.2.6 on Alpine 3.24 (`node:24-alpine`) | Built-in `node:http` server trace export only. These are the current fixture versions, not a promise for arbitrary libc or Node.js versions. The fixture tags float; release evidence must identify the exact resolved images and update this matrix if they change. |
| Delivery | Native image volume after successful per-node probes; init-container copy into `emptyDir` otherwise | Both delivery paths, architectures, and Node.js libc variants are integration-tested. |
| Workload lifecycle | Deployments, StatefulSets, and DaemonSets with supported rolling-update strategies | Injection, failed rollout, recovery, restart, and removal are tested. Other Pod producers have admission-only behavior, with no managed restart or recovery guarantee. |
| Security profile | Application profiles permitting dynamic preloading and readable/executable agent mounts; root copy init container allowed when fallback is needed | Arbitrary seccomp, AppArmor, SELinux, and admission-policy combinations are not certified. Pod Security Restricted is not an end-to-end validated support profile. |

The [alpha.3 release validation](https://github.com/otelx/rio/actions/runs/40559269374) establishes the current baseline, not evidence for a future tag. Before a normal release, rerun the complete [CI workflow](../.github/workflows/ci.yml) on the candidate, retain its `hello-injected.json` image IDs, node/runtime metadata, and `runtime.log` version evidence, and narrow this matrix if any path fails. No language, version, distribution, or signal becomes supported solely because an upstream agent advertises it. See [payload validation](../payload/README.md#validation-and-updating) for the exact fixtures.

## Kubernetes and delivery

- CI runs kind with Kubernetes 1.35.0 on native Linux amd64 and arm64 workers and checks both delivery paths. Older Kubernetes versions accepted by capability detection are not thereby supported.
- Native image-volume delivery is selected only after runtime checks and a probe on every registered Linux node succeed. Known minimum runtimes are containerd 2.1 and CRI-O 1.31. Kubernetes feature gates must also permit image volumes.
- Otherwise RIO copies the same payload into an `emptyDir` with an init container. This copy runs as UID 0 with all capabilities dropped. Restricted namespaces requiring every container to run nonroot need a volume-permissions adaptation.
- Windows containers are unsupported. RIO does not change application scheduling.
- Starting with `v0.1.0-alpha.2`, the controller and payload release tags contain amd64 and arm64 images. Mixed Linux clusters can use the same tags on both architectures; image selection is validated separately from heterogeneous-cluster lifecycle behavior. Capability probes must succeed on every Linux node before native delivery is selected. Application images must also provide the architectures on which they can be scheduled.
- The historical `v0.1.0-alpha.1` images were amd64 only. The retired packages for alpha.1 and alpha.2 have been deleted; their images can no longer be pulled from GHCR.
- Starting with `v0.1.0-alpha.3`, releases use `ghcr.io/otelx/rio/operator` and `ghcr.io/otelx/rio/instrumentation`. Use these paths for installations and upgrades. See [upgrade instructions](configuration.md#upgrading-from-alpha1-or-alpha2).

## Runtimes and signals

| Language path | Runtime loader fixtures | Application telemetry evidence | Support classification for `v0.1.0` |
| --- | --- | --- | --- |
| Node.js | 24, glibc and musl, amd64 and arm64 | Built-in HTTP server spans reach a Collector over OTLP HTTP/protobuf with both delivery methods | Supported only within the matrix above |
| Java | 21, glibc and musl, amd64 and arm64 | No application export check | Experimental |
| .NET | 8, glibc and musl, amd64 and arm64 | No application export check | Experimental |
| Python | 3.14.6, glibc and musl, amd64 and arm64 | No application export check | Experimental |
| Ruby | 3.3.12, glibc and musl, amd64 and arm64 | No application export check | Experimental |

All five agents are bundled, but loader checks disable exporters. Java, .NET, Python, and Ruby remain experimental until representative application telemetry reaches a Collector in repeatable integration checks for their advertised environments. Unsupported runtime versions, frameworks, native extensions, and application images require separate validation. See [payload inputs](../payload/README.md).

The Injector library is preloaded into application processes. Test compatibility with existing agents, `LD_PRELOAD` libraries, and restrictive security profiles.

| Capability | Promise and validation boundary |
| --- | --- |
| Exporter configuration | Signal switches configure each agent's `OTEL_TRACES_EXPORTER`, `OTEL_METRICS_EXPORTER`, and `OTEL_LOGS_EXPORTER` to `otlp` or `none`, with OTLP HTTP/protobuf. This configures exporters; it does not establish automatic capture. |
| Traces | Integration checks require Node.js HTTP server spans with a trace ID and service name at the Collector. Other protocols, frameworks, sampling behavior, and delivery under failures are not guaranteed. |
| Metrics and logs | Exporter configuration is available, but automatic capture and end-to-end export are experimental for every language. Logs do not collect container stdout/stderr. |
| Propagation | No integration assertion checks incoming `traceparent`/`tracestate`, outgoing context, parent-child relationships, baggage, or cross-service/cross-language propagation. A received span with a trace ID proves export only. Propagation remains unvalidated. |

RIO does not instrument Go using these language payloads; OBI is a separate installation. Coexistence with OBI or another agent is unvalidated. A healthy rollout does not prove telemetry arrival or application correctness.

## Operator/instrumentation versions and upgrades

Use immutable release tags or digests and the matching installation manifests and CRD. The normal-release contract supports `ghcr.io/otelx/rio/operator:VERSION` with `ghcr.io/otelx/rio/instrumentation:VERSION` from the **same full release**, including any rule image overrides. Custom builds, mutable tags, and arbitrary upstream Injector/agent replacements are experimental.

| Combination or transition | Support policy |
| --- | --- |
| Same full release pair, including `v0.1.0` when published | Supported within that release's environment matrix. Recovery restores saved configuration referencing that release's instrumentation image; keep it pullable. |
| Different operator and instrumentation releases, including different patches or alpha suffixes | Experimental unless a future release explicitly lists and validates that exact combination. Old running Pods can retain their agents, but newly created or recovery Pods may pull the image from their saved configuration. |
| Alpha.2 or alpha.3 to the candidate tested by the current upgrade suite | The [tested procedure](configuration.md#upgrading-from-alpha1-or-alpha2) preserves persisted state across the controller upgrade, then establishes an uninstrumented stable baseline before migrating both default and override payload images. The alpha.2 test removes retired image fixtures from every node before failed migration/recovery. No uninterrupted instrumentation guarantee. |
| Temporary controller rollback to the tested alpha.2/alpha.3 baseline | Validated with the candidate CRD, existing annotations, and new payload path; retain the unchanged CRD/state/hash format and an available historical controller image. This is a diagnostic rollback, not a permanently supported mixed release pair. |
| Other in-place upgrade/downgrade with persisted state across releases | Unsupported until that exact transition is listed and passes cross-release tests. Do not infer compatibility from same-version restart/recovery or the pinned alpha baselines. |
| Migration without a validated persisted-state transition | Use the clean reset described below. This trades instrumentation continuity for avoiding cross-version state and mixed-version recovery. |
| Alpha.1/alpha.2 image paths | Retired and deleted from GHCR, with no pull or recovery guarantee. Cached copies are not a durable recovery plan. Preserve verified copies in a maintained mirror before attempting recovery, or migrate through uninstrumented workloads. |

For transitions outside the tested alpha.2/alpha.3 upgrade procedure, use a clean reset. First save the policy, installation manifests, workload manifests and annotations, and exact image digests in a restricted backup (annotations can contain configuration values). While the old operator/webhook are functioning, remove selection, wait for managed workloads to finish removing instrumentation, and recreate admission-only Pods without injection. Verify replacement Pods have no RIO volume, copy init container, or injected environment settings. If a recovery already requires an unavailable old payload, use a verified mirror or plan an application maintenance window; do not claim automatic recovery is available. The tested cleanup-first path avoids pulling a retired payload by establishing an uninstrumented stable configuration while existing workloads are healthy.

Then run the old release's normal uninstall procedure while it can still finish finalizer cleanup. After confirming all affected Pods are uninstrumented and the old operator is stopped, remove the RIO bookkeeping annotations listed below from workload metadata and Pod templates before installing the new release's CRD/operator and applying a policy with its matching instrumentation image. Keep old ReplicaSet/ControllerRevision templates and backups out of application rollback automation: they can reintroduce old snapshots and deleted image references. Recreate affected workload objects from clean source manifests if their revision histories cannot be safely excluded. This is a deliberate reset of instrumentation history, not an in-place state migration. Never remove state or finalizers to bypass an active or failed instrumentation rollout.

## API, configuration, and persisted state

The Kubernetes API remains `rio.otelx.github.io/v1alpha1` at `v0.1.0`. A normal version under major zero does not freeze the API or promote the CRD; see [Semantic Versioning](https://semver.org/#spec-item-4). The public configuration surface includes the CRD fields/defaults, policy selection and signal semantics, supported installation options, and documented environment overrides. Status fields and rollout messages are diagnostic and should not be treated as a stable automation protocol.

For normal `0.1.x` patches, preserve existing valid configuration and its meaning; additive optional fields must preserve existing defaults. Breaking configuration changes require a new minor release, explicit release notes, and a migration procedure. Announce deprecations in the changelog and configuration reference at least one normal minor release before removal, except for an urgent security fix with a documented mitigation. Pre-release changes may break compatibility but still require migration notes. Upstream agent changes that alter configuration or capture behavior must be called out separately; they are not covered by a frozen agent API promise.

Persisted annotations under `rio.otelx.github.io/` are part of the recovery format, not disposable caches:

| Location | Annotation suffixes | Meaning |
| --- | --- | --- |
| Workload metadata | `state` | JSON containing current/stable configurations, phase, timing, and failed revision; used after operator restarts |
| Workload Pod template | `configuration`, `revision`, `workload-uid` | Resolved configuration snapshot, its hash, and owning workload identity |
| Admitted Pod | Template annotations plus `applied-revision`, `delivery` | Actual mutation revision and chosen delivery path |

Do not edit these annotations, copy them to another workload, or delete them during rollout/recovery. Saved configurations can retain older image references even after the central policy changes. Source-controlled application manifests should omit these operator-owned values. Normal patch releases must retain the ability to read their minor line's persisted format and preserve revision identity, or provide and validate an explicit migration before release. Cross-release state migration and downgrade support require the evidence described above; the upgrade matrix covers only its pinned alpha.2/alpha.3 baselines, not arbitrary future transitions. A format change must document backup, conversion/reset, rollback limits, and restoration steps. Deleting the CRD or stripping annotations is not a migration strategy while workloads remain instrumented.

## Rollout and recovery

Automatic rollout and removal cover Deployments, StatefulSets, and DaemonSets using supported rolling-update strategies. Paused Deployments, Recreate, OnDelete, and partitioned StatefulSet updates are reported as unsupported. Other Pod-producing resources can receive injection when new Pods are admitted; RIO does not restart their existing Pods automatically.

Recovery restores instrumentation settings, preserving concurrent application changes. It uses readiness, crash state, and deadlines; it does not validate telemetry arrival, application correctness, latency, or business outcomes.

Use immutable payload tags or digests. Replacing bytes behind an existing image reference can invalidate capability-probe caching and revision-based recovery.

## Operational boundaries

- One operator replica, with Recreate during its own upgrades; no leader election. The [upgrade and rollback procedure](configuration.md#upgrading-from-alpha1-or-alpha2) covers alpha.2-to-candidate migration and controller-only rollback with an unchanged CRD/state format.
- Self-signed webhook certificates created by the installer expire after one year; no automatic rotation. Reinstallation reuses the current Secret. Use the [tested renewal and recovery procedure](configuration.md#webhook-certificate-renewal) before expiration or to recover expired certificates.
- The webhook uses `failurePolicy: Ignore`. A webhook outage can admit healthy uninstrumented Pods; status reports `InjectionMissing` for managed workloads.
- Cluster-wide RBAC can patch supported workload templates and delete failed recovery Pods. Only trusted administrators should install RIO or change policy.
- Policy `env` can resolve values from Secrets in selected application namespaces. Limit permission to edit the cluster policy accordingly.
- The API is `rio.otelx.github.io/v1alpha1`. It is independent of upstream OpenTelemetry APIs and the presentation's historical demo API. No in-place migration from those APIs is provided.

See [configuration](configuration.md) for detailed selection and recovery behavior.
