# Compatibility and operational limits

RIO is experimental. The initial release supports evaluation; it does not claim production readiness, complete runtime coverage, or high availability.

## Kubernetes and delivery

- Evaluation targets are Linux amd64 and arm64 (aarch64), with glibc and musl application images. CI runs kind with Kubernetes 1.35.0 on native workers for each architecture and checks both delivery paths.
- Native image-volume delivery is selected only after runtime checks and a probe on every registered Linux node succeed. Known minimum runtimes are containerd 2.1 and CRI-O 1.31. Kubernetes feature gates must also permit image volumes.
- Otherwise RIO copies the same payload into an `emptyDir` with an init container. This copy runs as UID 0 with all capabilities dropped. Restricted namespaces requiring every container to run nonroot need a volume-permissions adaptation.
- Windows containers are unsupported. RIO does not change application scheduling.
- Starting with `v0.1.0-alpha.2`, the controller and payload release tags contain amd64 and arm64 images. Mixed Linux clusters use the same tags on both architectures; the container runtime selects the matching image, and capability probes must succeed on every Linux node before native delivery is selected. Application images must also provide the architectures on which they can be scheduled.
- The historical `v0.1.0-alpha.1` images were amd64 only. The retired packages for alpha.1 and alpha.2 have been deleted; their images can no longer be pulled from GHCR.
- Starting with `v0.1.0-alpha.3`, releases use `ghcr.io/otelx/rio/operator` and `ghcr.io/otelx/rio/instrumentation`. Use these paths for installations and upgrades. See [upgrade instructions](configuration.md#upgrading-from-alpha1-or-alpha2).

## Runtimes and signals

The payload includes agents for Java, Node.js, .NET, Python, and Ruby. Loader checks exercise Java 21, Node.js 24, .NET 8, Python 3.14, and Ruby 3.3 on both architectures, using glibc and Alpine/musl images. Integration checks verify HTTP trace export from Node.js with each libc. These are specific test paths, not a promise of compatibility with every version, framework, native extension, or application image. See [payload inputs](../payload/README.md).

The Injector library is preloaded into application processes. Test compatibility with existing agents, `LD_PRELOAD` libraries, and restrictive security profiles.

Traces, metrics, and logs use the language agents' exporters. Enabling an exporter does not add support for libraries that agent does not instrument. The logs switch does not collect container stdout. RIO does not instrument Go using these language payloads; OBI is a separate installation.

## Rollout and recovery

Automatic rollout and removal cover Deployments, StatefulSets, and DaemonSets using supported rolling-update strategies. Paused Deployments, Recreate, OnDelete, and partitioned StatefulSet updates are reported as unsupported. Other Pod-producing resources can receive injection when new Pods are admitted; RIO does not restart their existing Pods automatically.

Recovery restores instrumentation settings, preserving concurrent application changes. It uses readiness, crash state, and deadlines; it does not validate telemetry arrival, application correctness, latency, or business outcomes.

Use immutable payload tags or digests. Replacing bytes behind an existing image reference can invalidate capability-probe caching and revision-based recovery.

## Operational boundaries

- One operator replica, with Recreate during its own upgrades; no leader election.
- Self-signed webhook certificates created by the installer expire after one year; no automatic rotation. Reinstallation reuses the current Secret. Plan certificate replacement before expiration.
- The webhook uses `failurePolicy: Ignore`. A webhook outage can admit healthy uninstrumented Pods; status reports `InjectionMissing` for managed workloads.
- Cluster-wide RBAC can patch supported workload templates and delete failed recovery Pods. Only trusted administrators should install RIO or change policy.
- Policy `env` can resolve values from Secrets in selected application namespaces. Limit permission to edit the cluster policy accordingly.
- The API is `rio.otelx.github.io/v1alpha1`. It is independent of upstream OpenTelemetry APIs and the presentation's historical demo API. No in-place migration from those APIs is provided.

See [configuration](configuration.md) for detailed selection and recovery behavior.
