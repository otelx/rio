# Compatibility and operational limits

RIO is alpha and intended for evaluation. The scope below is planned for `v0.1.0`. Support covers bug investigation and fixes under the [maintenance policy](../SECURITY.md), without an availability or resolution SLA.

The five-language telemetry validation below applies to matching `ghcr.io/otelx/rio/operator` and `ghcr.io/otelx/rio/instrumentation` images built from commit `da444787d2be3544324b1bae157b2eb5c6b52176`, with exact native image IDs recorded in the linked CI artifacts. This evidence covers those candidate source builds. The [quickstart](quickstart.md) remains an alpha.3 Node.js HTTP trace example.

## First normal release scope

| Component | Supported scope | Validation |
| --- | --- | --- |
| Kubernetes | 1.35.0, Linux amd64 and arm64 | Native workers per architecture: two-node kind clusters for lifecycle checks and separate one-node kind clusters for telemetry; heterogeneous clusters are unvalidated |
| Container runtime | containerd 2.2.0 in `kindest/node:v1.35.0` | CRI-O is detected but remains experimental |
| Application | The five pinned runtime/framework combinations below | HTTP server traces, automatic request metrics, application logs, and defined propagation paths received by a real OpenTelemetry Collector |
| libc | The per-language OS/libc versions below | Digest-pinned fixtures on both architectures |
| Delivery | Native image volume or init-container copy to `emptyDir` | Both paths tested with each architecture and libc |
| Workloads | Deployments, StatefulSets, DaemonSets with supported rolling-update strategies | Injection, failure recovery, operator restart, and removal |

Other Kubernetes versions, runtimes, and application images need separate validation. Application base images are pinned by version and immutable multi-architecture index digest in each fixture's `fixture.json`; dependency locks pin framework and logging-library inputs. [Supported-branch CI](https://github.com/otelx/rio/actions/runs/40615729597) at commit `da444787d2be3544324b1bae157b2eb5c6b52176` records actual image IDs, runtime/OS/libc output, agent versions, and Collector output per cell. See the [payload validation](../payload/README.md#validation-and-updating).

## Delivery and security constraints

Native delivery requires image-volume feature gates, containerd 2.1+ or CRI-O 1.31+, and a successful payload-mount probe on every registered Linux node. These detection thresholds do not extend the supported versions above. Otherwise RIO uses the copy init container, which runs as UID 0 with all capabilities dropped. Namespaces requiring every container to run nonroot need a volume-permissions adaptation.

Application profiles must permit `LD_PRELOAD` and readable/executable agent mounts. Pod Security Restricted and custom seccomp, AppArmor, or SELinux policies are unvalidated. Test coexistence with existing agents and preload libraries before adoption.

Windows containers are unsupported. RIO does not change scheduling; application images must support every architecture on which they can run.

## Runtimes and signals

The matrix below is validated through Kubernetes admission, the shipped Injector/agent bundle, and OpenTelemetry Collector Contrib 0.163.2 using OTLP HTTP/protobuf. Each language runs on native Linux amd64 and arm64, with both listed libc images and both native image-volume and init-container delivery. The telemetry harness uses a single-node kind cluster per architecture/delivery cell; lifecycle checks use the separate two-node clusters above. This does not validate heterogeneous or other cluster topologies.

| Language | Pinned runtime and HTTP framework/client | Agent/bundle inputs | glibc fixture | musl fixture |
| --- | --- | --- | --- | --- |
| [Node.js](../scripts/telemetry/fixtures/node/fixture.json) | Node.js 24.22.1; built-in `node:http` server/client | Auto-instrumentations 0.78.0; Winston transport 0.30.0 | Debian 12; glibc 2.36 (`2.36-9+deb12u14`) | Alpine 3.24.3; musl 1.2.6 |
| [Java](../scripts/telemetry/fixtures/java/fixture.json) | Temurin OpenJDK 21.0.12.1+1; JDK `HttpServer`/`HttpClient`; Gson 2.13.1 | Java agent 2.30.0 | Ubuntu 26.04.1; glibc 2.43 (`2.43-2ubuntu2.4`) | Alpine 3.24.3; musl 1.2.6 |
| [.NET](../scripts/telemetry/fixtures/dotnet/fixture.json) | .NET/ASP.NET Core 8.0.33; Kestrel and `System.Net.Http.HttpClient` | Automatic instrumentation 1.17.0; RIO propagation/bootstrap extension 1 | Debian 12; glibc 2.36 (`2.36-9+deb12u14`) | Alpine 3.24.3; musl 1.2.6 |
| [Python](../scripts/telemetry/fixtures/python/fixture.json) | Python 3.14.6; aiohttp 3.13.3 server/`ClientSession` | Distro/instrumentations 0.66b0; SDK 1.45.0; RIO source-checked OTLP flags and aiohttp context repairs | Debian 13.6; glibc 2.41 (`2.41-12+deb13u3`) | Alpine 3.23.5; musl 1.2.5 |
| [Ruby](../scripts/telemetry/fixtures/ruby/fixture.json) | Ruby 3.3.12; Sinatra 4.2.1, Rack 3.2.7, Rackup 2.3.1, WEBrick 1.9.2; Net::HTTP 0.10.0 | Auto-instrumentation 0.1.0 at pinned commit `68af0a92d6d9c2cd0ee38a9327e98797673227d1`; Rack 0.30.0, Sinatra 0.29.0, Logger 0.4.0; RIO Rack metrics 1.0.0; metrics API 0.5.0 / SDK 0.13.1 / OTLP exporter 0.8.0 | Debian 12; glibc 2.36 (`2.36-9+deb12u14`) | Alpine 3.23.5; musl 1.2.5 |

All assertions use fresh request UUIDs and unique service identities. Application images contain ordinary framework/client/logging dependencies, with no application OpenTelemetry provider, exporter, custom span, meter, or log-record setup. Bounded export polling inspects Collector data; workload readiness and loader messages cannot pass a telemetry assertion. Uninstrumented applications must still serve requests and log normally while exporting no telemetry. Application/operator/Collector logs, Pod diagnostics, policy manifests, and raw signal files are retained. Known SDK/bootstrap startup errors and application restarts fail the checks.

Signal switches select `otlp` or `none` through `OTEL_TRACES_EXPORTER`, `OTEL_METRICS_EXPORTER`, and `OTEL_LOGS_EXPORTER`. The default fixture settings include `OTEL_SEMCONV_STABILITY_OPT_IN=http`; .NET includes formatted log messages and console SDK diagnostics, and Ruby uses `OTEL_RUBY_REQUIRE_BUNDLER=true`. Exact environment settings and effective admission configuration are recorded per case. Loader-only smoke checks remain separate and disable exporters.

### Traces

Real `GET /work/<UUID>` requests produce exactly one matching server span with service/resource identity, valid nonzero trace/span IDs, a valid span time range, and successful HTTP method/status attributes. `signals.traces: false` suppresses export while the same application continues serving. Coverage is limited to the HTTP frameworks and clients above; other protocols, framework/runtime versions, transport settings, and export during backend outages need separate validation.

### Automatic metrics

| Language | Automatic source | Validated metric |
| --- | --- | --- |
| Node.js | Bundled HTTP server instrumentation | `http.server.request.duration`: histogram, seconds (`s`), cumulative OTLP temporality `2` |
| Java | Bundled JDK HTTP server instrumentation | Same histogram, unit, and temporality |
| .NET | Automatic ASP.NET Core request instrumentation | Same histogram, unit, and temporality |
| Python | Bundled aiohttp server instrumentation | Same histogram, unit, and temporality |
| Ruby | RIO Rack metrics extension 1.0.0, using the distro's existing meter provider/resource | Same histogram, unit, and temporality |

The metric cases explicitly set `OTEL_METRIC_EXPORT_INTERVAL=1000` and `OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE=cumulative`. The shared policy sets `OTEL_BSP_SCHEDULE_DELAY=500` and `OTEL_BLRP_SCHEDULE_DELAY=500`; these test settings accompany the bounded export and negative-control observations, without establishing other batching or temporality configurations.

The harness validates resource identity, type, unit, temporality, valid buckets/timestamps, and a fresh cumulative count increase after three additional real requests. Repeated exports of unchanged cumulative samples cannot pass. Each language exercises inherited metrics on/off and rule overrides in both directions, with traces/logs continuing when metrics are disabled. Two further cases require automatic measurements with both trace/log export disabled, and with a valid unsampled incoming parent under `OTEL_TRACES_SAMPLER=parentbased_always_on`; both require trace/log silence and fresh metric activity.

Ruby measures monotonic request duration through response-body completion or failure, with one measurement despite nested middleware or repeated completion. Its seconds histogram uses HTTP duration boundaries; attributes include bounded known method, protocol/scheme, status/error class, and Sinatra's matched route template when available. Raw paths, queries, Host headers, request markers, and exception messages are not metric labels. Metrics do not depend on a recording span. The normal Sinatra/Rack-events fixture settings are the full admission-matrix scope. Rack-specific opt-outs and the non-Events fallback have targeted build/unit and local amd64/image-volume evidence; they are not additional fully validated matrix configurations. No runtime gauges, arbitrary application metrics, or other Ruby frameworks are promoted by this request-duration check.

### Automatic application logs

| Language | Pinned logging API/library | Validated capture |
| --- | --- | --- |
| Node.js | Winston 3.19.0; agent-side Winston transport 0.30.0 | One correlated INFO record for the normal application message |
| Java | Logback 1.5.18 through SLF4J 2.0.17 | Same body/severity/time/resource/correlation checks |
| .NET | Framework-provided `Microsoft.Extensions.Logging.ILogger` in ASP.NET Core 8.0.33, formatted messages enabled | Same checks; ordinary console logging remains enabled |
| Python | Standard-library `logging` from Python 3.14.6 | Same checks |
| Ruby | Logger 1.7.0 through Logger instrumentation 0.4.0 | Same checks; the fixture's formatted message includes an exact trailing newline |

Every language must emit its unique message once through ordinary console logging and export exactly one matching OTLP log with expected body, INFO severity, current timestamp, and service/resource identity. The log's trace/span IDs must match the instrumented request's exported server span. Enabled cases continue observing for delayed duplicates. Inherited log settings and rule overrides are tested in both directions; disabling logs preserves normal application logging and separately enabled traces and automatic metrics for all five languages. The Collector receives OTLP only: RIO does not scrape container stdout/stderr. Other logging libraries, formatters, severities, and asynchronous logging patterns need separate validation.

### W3C HTTP propagation

The defined HTTP/1.1 paths are five separately exercised caller/callee pairs: **Node.js → Java, Java → .NET, .NET → Python, Python → Ruby, and Ruby → Node.js**. Each request crosses one real Service/network boundary using the normal client above; application code neither injects propagation headers nor creates spans. Every language acts as a caller and a callee. This does not establish every language pair, a five-hop transaction, other protocols, or interoperability with additional agents.

With `OTEL_PROPAGATORS=tracecontext,baggage` and `OTEL_TRACES_SAMPLER=parentbased_always_on`, known valid version-00 incoming context retains its trace ID and single-entry `tracestate`. Collector assertions establish incoming remote-parent → caller server → caller client → callee server ancestry, not just a shared trace ID. The downstream application must receive exactly one traceparent/tracestate header with the caller-client span ID and sampled flag `01`. Three overlapping requests with distinct contexts and baggage markers must remain isolated. A separate `tracecontext`-only configuration verifies baggage is absent downstream. Baggage is checked as a received header, not assumed to become span attributes.

With tracecontext and baggage enabled, absent context and the defined malformed-context input must create valid distinct roots without failing requests or retaining earlier state/baggage. Fresh sampled roots may carry W3C's Random flag in addition to Sampled (`03`, observed for Java). A valid unsampled incoming parent must propagate the same trace with flag `00` and a fresh outgoing span ID, while exporting no matching spans during bounded observation and a final sweep after later positive exports. These checks cover the specified inputs and propagator settings; they do not validate all W3C versions, malformed-input combinations, baggage grammar/size limits, or sampling strategies.

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
