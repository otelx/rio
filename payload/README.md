# RIO instrumentation image

This component builds `ghcr.io/otelx/rio/instrumentation`, the instrumentation image consumed by the [RIO operator](../README.md) (`ghcr.io/otelx/rio/operator`). It bundles the upstream OpenTelemetry Injector and agents for Java, Node.js, .NET, Python, and Ruby. The Injector detects the runtime when each application process starts.

Application HTTP traces, automatic request metrics, correlated application logs, and the defined HTTP/1.1 propagation paths are validated for the [pinned five-language matrix](../docs/compatibility.md#runtimes-and-signals). Validated by [supported-branch CI](https://github.com/otelx/rio/actions/runs/40615729597) using matching operator and instrumentation images built from commit `da444787d2be3544324b1bae157b2eb5c6b52176`; the CI artifacts record their exact native image IDs. Other runtimes, frameworks, and signal sources require separate validation.

One filesystem layout supports native Kubernetes image volumes and the init-container copy fallback:

```text
/dist/
  versions.json
  injector/{libotelinject.so,otelinject.conf}
  agents/java/opentelemetry-javaagent.jar
  agents/node/node_modules/
  agents/dotnet/{glibc,musl}/
  agents/python/{glibc,musl}/
  agents/ruby/{glibc,musl}/
```

The operator mounts the image root at `/__otel_sdk_auto_instrumentation__`. For fallback, `/copy-to-volume.sh` copies `/dist` under the same mount path. The copy is repeatable after an interrupted init container and application mounts are read-only. Agent configuration, signal selection, and OTLP destination come from the operator; the image does not configure or install OBI.

## Inputs

[versions.json](versions.json) records upstream release assets and SHA-256 checksums, dependency versions, and separate RIO bundle-extension metadata:

| Component | Pinned input |
| --- | --- |
| Injector | 0.11.0, separate amd64 and arm64 libraries |
| Java | Upstream OpenTelemetry Java agent 2.33.1 |
| Node.js | Zero-Code package 0.82.0; agent-side Winston transport 0.35.0, npm integrity lockfile |
| Python | Distro 0.66b1 / SDK 1.45.1, hashed dependency lock and source-checked RIO repairs |
| .NET | Automatic instrumentation 1.17.0, both libc variants per architecture; RIO bootstrap and propagation plugin |
| Ruby | Upstream commit `68af0a92d6d9c2cd0ee38a9327e98797673227d1`; Logger bridge 0.4.0; RIO Rack metrics extension 1.0.0; frozen Bundler lockfile |

Downloads fail on a checksum mismatch. Python requirements derive from upstream Operator v0.160.0 and include the HTTP/protobuf exporter, rather than a Python-version-dependent gRPC exporter. Node uses `npm ci`; its agent-side Winston transport supplies the transport required by the bundled logging instrumentation without adding an application OpenTelemetry dependency.

Ruby uses a frozen Bundler graph and pinned upstream checkout. Its local bootstrap resolves the application's bundle before invoking the upstream loader so the SDK cannot activate conflicting default gems. It defers eager `Bundler.require` until the Logger bridge and RIO Rack metrics extension are loaded, then restores and honors the user's eager-loading setting. The Rack extension measures request duration through automatic middleware and reuses the upstream distro's configured meter provider and resource; it does not initialize another SDK or exporter. Its version and pinned Rack instrumentation 0.30.0 / metrics SDK 0.13.1 are recorded separately in `versions.json`.

Python's `sitecustomize.py` calls the upstream Zero-Code loader. Bundle repairs preserve OTLP span/link trace flags and attach/detach the full extracted aiohttp request context. Each repair checks the exact pinned source hash before applying and updates the installed package's `RECORD` hashes. Dependency changes require reviewing or removing these repairs.

The [.NET bundle extension](dotnet/README.md) uses the upstream 1.17.0 plugin API to align the runtime's `DistributedContextPropagator` with the SDK's configured propagators. A BCL-only `Rio.Bootstrap` hook activates the plugin for .NET 8+ and invokes the unmodified original upstream hook, retained alongside it as `OpenTelemetry.AutoInstrumentation.StartupHook.original.dll`; the original assembly identity and bytes are preserved. The bootstrap targets netstandard2.1 and the plugin targets net8.0, using the immutable SDK input recorded in `versions.json` and references from the checksum-verified agent. Activation belongs to the bundle, preserving user plugin strings without adding a plugin dependency to older payloads through operator configuration. RIO extension metadata remains separate from upstream asset pins.

Python payloads are built on Python 3.14 and Ruby payloads on Ruby 3.3. Runtime, framework, libc, and native-extension compatibility still requires application testing; bundling five agents does not promise every runtime version or framework. Traces, metrics, and logs use the standard agent exporter configuration. Exporter availability and automatic capture of a particular signal are distinct: an application must use libraries that the corresponding agent instruments.

## Build

Build a local image for the current architecture:

```bash
make -C payload image
make -C payload test
```

Build both requested Linux architectures into one OCI archive:

```bash
make -C payload archive
```

This uses Docker Buildx and requires builders capable of executing amd64 and arm64 stages (native workers or emulation). Output goes to `.artifacts/images/payload.oci.tar`. To publish a multi-platform tag when a registry is available, explicitly run:

```bash
make -C payload publish IMAGE=REGISTRY/rio/instrumentation:VERSION
```

No registry publishing is part of the normal local build. The build context is limited to this component; no presentation files are included.

## Validation and updating

Every image build runs `verify-layout.sh`, checking that all five agent payloads are present. Actual startup checks run through the Injector's `LD_PRELOAD` path:

```bash
PAYLOAD_IMAGE=ghcr.io/otelx/rio/instrumentation:dev payload/scripts/smoke.sh
```

The smoke test checks Java 21, Node 24, Python 3.14, Ruby 3.3, and a .NET 8 console fixture in both glibc and Alpine/musl images. It verifies that the agents initialize and that the copied-volume layout works. `PLATFORM=linux/arm64` or `PLATFORM=linux/amd64` selects an architecture explicitly; executing a foreign architecture requires emulation. CI runs these checks on native workers for both architectures. Logs and copied files are separated by architecture under `.artifacts/image-smoke/`.

Exporters are disabled for the loader checks. The shared `scripts/telemetry_e2e.py` suite separately exercises admission and the shipped Injector/agents with a real Collector on native amd64/arm64 workers, both libc fixtures, and both delivery methods. It checks HTTP trace export and suppression, automatic request histograms with six inherited/override/independence cases, normal logging-library capture with correlation/duplicate/suppression controls, and the five defined HTTP/1.1 propagation pairs. Ruby metrics use RIO Rack extension 1.0.0 and the existing upstream meter provider; all five log scenarios verify separately enabled traces and automatic metrics. Supplemental Rack opt-out/non-Events checks have targeted build/unit and local amd64/image-volume coverage; the normal fixture defaults define the full admission matrix.

Evidence includes exact runtime/framework/agent versions, immutable fixture base-image digests and built image IDs, effective admission configuration, fresh request identities, Collector signal records, and saved application/operator/Collector/Pod diagnostics. Negative controls require uninstrumented applications to serve and log without telemetry, disabled traces and metrics/logs disabled by inherited settings or rule overrides to remain silent, and unsampled requests to retain metric capture without trace/log export. Export waits are bounded; application telemetry is never manually generated to satisfy assertions. CI artifacts exclude private kubeconfigs and signing keys. OBI coexistence remains unvalidated.

To update dependencies, change the explicit inputs, regenerate lockfiles with `make lock` (Node/npm and `uv` required), refresh the Ruby lock with Bundler when changing its upstream revision, and rerun build/loader tests for both architectures. Update downloaded asset checksums from the corresponding upstream release metadata alongside their versions. Keep the image tag immutable once used by an operator.

The packaging layout is inspired by [Beyla's webhook image](https://github.com/grafana/beyla/tree/main/pkg/webhook/image). This image uses upstream OpenTelemetry agents, including the upstream Java agent. See [NOTICE](NOTICE) for source attribution.
