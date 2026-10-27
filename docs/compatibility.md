# Compatibility and operational limits

RIO is alpha and intended for evaluation. The scope below is planned for `v0.1.0`. Support covers bug investigation and fixes under the [maintenance policy](../SECURITY.md), without an availability or resolution SLA.

## First normal release scope

| Component | Supported scope | Validation |
| --- | --- | --- |
| Kubernetes | 1.35.0, Linux amd64 and arm64 | Two-node kind clusters on native workers for each architecture; heterogeneous clusters are unvalidated |
| Container runtime | containerd 2.2.0 in `kindest/node:v1.35.0` | CRI-O is detected but remains experimental |
| Application | Node.js 24.22.1, built-in `node:http` server | HTTP server spans received by an OpenTelemetry Collector |
| libc | glibc 2.36 on Debian 12; musl 1.2.6 on Alpine 3.24 | `node:24` and `node:24-alpine`, on both architectures |
| Delivery | Native image volume or init-container copy to `emptyDir` | Both paths tested with each architecture and libc |
| Workloads | Deployments, StatefulSets, DaemonSets with supported rolling-update strategies | Injection, failure recovery, operator restart, and removal |

Other Kubernetes versions, runtimes, and application images need separate validation. Fixture tags float; release evidence must record resolved image IDs and runtime/libc versions. See the [validation run](https://github.com/otelx/rio/actions/runs/40565419938) and [payload fixtures](../payload/README.md#validation-and-updating).

## Delivery and security constraints

Native delivery requires image-volume feature gates, containerd 2.1+ or CRI-O 1.31+, and a successful payload-mount probe on every registered Linux node. These detection thresholds do not extend the supported versions above. Otherwise RIO uses the copy init container, which runs as UID 0 with all capabilities dropped. Namespaces requiring every container to run nonroot need a volume-permissions adaptation.

Application profiles must permit `LD_PRELOAD` and readable/executable agent mounts. Pod Security Restricted and custom seccomp, AppArmor, or SELinux policies are unvalidated. Test coexistence with existing agents and preload libraries before adoption.

Windows containers are unsupported. RIO does not change scheduling; application images must support every architecture on which they can run.

## Runtimes and signals

Loader checks run on amd64 and arm64 with glibc and musl. They disable exporters.

| Language | Loader fixture | Application telemetry support |
| --- | --- | --- |
| Node.js | 24 | HTTP server trace export within the scope above |
| Java | 21 | Experimental; export untested |
| .NET | 8 | Experimental; export untested |
| Python | 3.14.6 | Experimental; export untested |
| Ruby | 3.3.12 | Experimental; export untested |

Promoting an experimental language requires repeatable application-to-Collector tests for its advertised environments.

Signal switches set the agents' `OTEL_TRACES_EXPORTER`, `OTEL_METRICS_EXPORTER`, and `OTEL_LOGS_EXPORTER` to `otlp` or `none`, using OTLP HTTP/protobuf. Capture depends on the agent's instrumentation of the application's libraries.

- **Traces:** tests require Node.js HTTP server spans with a trace ID and service name at the Collector. Other protocols, frameworks, sampling behavior, and delivery during failures are unvalidated.
- **Metrics and logs:** automatic capture and export remain experimental for every language. RIO does not collect container stdout/stderr.
- **Propagation:** incoming `traceparent`/`tracestate`, outgoing context, parent-child relationships, baggage, and cross-service or cross-language propagation are untested.

RIO does not instrument Go. OBI is a separate installation; coexistence is unvalidated.

## Operator/instrumentation versions and upgrades

Use `ghcr.io/otelx/rio/operator:VERSION` and `ghcr.io/otelx/rio/instrumentation:VERSION` from the same full release, with its manifests and CRD. This includes policy rule overrides. Keep image references immutable and retain images needed for recovery. Custom builds and substituted agents are experimental.

The upgrade suite validates alpha.2 and alpha.3 to the candidate by preserving and refusing unsigned history, then cleaning up and explicitly resetting it before starting authenticated history. Follow the [tested upgrade procedure](configuration.md#upgrading-existing-releases), including an uninstrumented recovery baseline before changing payload images. Temporary diagnostic controller rollback must leave the candidate CRD, raw state, signature, and policy unchanged; available alpha.3 payload rollback is also tested. Historical controllers lack later safeguards, so follow the documented diagnostic restrictions. Other mixed versions and unvalidated transitions remain unsupported.

For transitions outside those tested baselines, use a clean reset without reusing persisted state:

1. Back up the policy, manifests, workload annotations, and image digests. Restrict access to backups containing configuration values.
2. With the old operator running, remove selection and wait for managed workloads to become uninstrumented. Recreate admission-only Pods. Check replacement Pods for removal of RIO volumes, init containers, and injected environment settings.
3. Run the old release's uninstall procedure and confirm cleanup completed. Do not strip state or finalizers from an active or failed rollout.
4. With the old operator stopped, remove the bookkeeping annotations below from workload metadata and Pod templates. Exclude old ReplicaSet/ControllerRevision templates from rollback automation, or recreate workloads from clean manifests; old templates can restore stale image references.
5. Install the new release's CRD/operator and apply a policy using its matching instrumentation image.

Recovery can pull images from saved configurations even after the policy changes. If an image is unavailable, restore it from a verified mirror or plan a maintenance window. See [retired image references](configuration.md#retired-image-references).

## API, configuration, and persisted state

The API remains `rio.otelx.github.io/v1alpha1` for `v0.1.0`. A normal major-zero release does not freeze the API. Compatibility covers CRD fields/defaults, policy behavior, installation options, and documented environment overrides. Status fields and messages are diagnostic.

Normal `0.1.x` patches preserve valid configuration and its meaning. Breaking changes require a new minor release and migration instructions. Announce deprecations in the changelog and configuration reference at least one normal minor release before removal. Urgent security fixes may shorten that period with a documented mitigation. Pre-release breaks and upstream agent behavior changes also require migration notes.

Annotations under `rio.otelx.github.io/` store recovery state:

| Location | Suffixes | Contents |
| --- | --- | --- |
| Workload metadata | `state`, `state-signature` | Current/stable configurations, rollout phase, timing, failed revision |
| Pod template | `configuration`, `revision`, `workload-uid` | Configuration snapshot, hash, owning workload |
| Admitted Pod | Template annotations plus `applied-revision`, `delivery` | Applied revision and delivery method |

Omit these from source-controlled manifests. Do not edit, copy, or remove them during rollout/recovery. Normal patches must read their minor line's persisted format and preserve revision identity, or provide a tested migration. Format changes must document backup, conversion/reset, restoration, and downgrade limits.

## Rollout and recovery

Automatic rollout and removal cover Deployments, StatefulSets, and DaemonSets using supported rolling-update strategies. Paused Deployments, Recreate, OnDelete, and partitioned StatefulSet updates are reported as unsupported. Other Pod-producing resources can receive injection when new Pods are admitted; RIO does not restart their existing Pods automatically.

Recovery restores instrumentation settings, preserving concurrent application changes. It uses readiness, crash state, and deadlines; it does not validate telemetry arrival, application correctness, latency, or business outcomes.

Use immutable payload tags or digests. Replacing bytes behind an existing image reference can invalidate capability-probe caching and revision-based recovery.

## Operational boundaries

- One operator replica, with Recreate during its own upgrades; no leader election.
- Self-signed webhook certificates created by the installer expire after one year; no automatic rotation. Reinstallation reuses the current Secret. Use the [tested renewal and recovery procedure](configuration.md#webhook-certificate-renewal) before expiration or to recover expired certificates.
- The webhook uses `failurePolicy: Ignore`. A webhook outage can admit healthy uninstrumented Pods; status reports `InjectionMissing` for managed workloads.
- Cluster-wide RBAC can patch supported workload templates and delete failed recovery Pods. Only trusted administrators should install RIO or change policy.
- Policy `env` can resolve values from Secrets in selected application namespaces. Limit permission to edit the cluster policy accordingly.

See [configuration](configuration.md) for detailed selection and recovery behavior.
