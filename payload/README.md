# RIO instrumentation image

This component builds `ghcr.io/otelx/rio/instrumentation`, the instrumentation image consumed by the [RIO operator](../README.md) (`ghcr.io/otelx/rio/operator`). It bundles the upstream OpenTelemetry Injector and agents for Java, Node.js, .NET, Python, and Ruby. The Injector detects the runtime when each application process starts.

Bundled language paths have different validation levels: only Node.js 24 built-in HTTP server trace export has application integration coverage. Java, .NET, Python, and Ruby remain **experimental**, as do metrics/log capture and export for every language. Propagation has no integration assertions. Use the [support matrix and signal guarantees](../docs/compatibility.md#runtimes-and-signals), and match the instrumentation image's full release version to `rio/operator`.

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

[versions.json](versions.json) records the release assets and SHA-256 checksums:

| Component | Pinned input |
| --- | --- |
| Injector | 0.11.0, separate amd64 and arm64 libraries |
| Java | Upstream OpenTelemetry Java agent 2.33.1 |
| Node.js | Zero-Code package 0.82.0, npm lockfile |
| Python | Distro 0.66b1 / SDK 1.45.1, hashed dependency lock |
| .NET | Zero-Code package 1.17.0, both libc variants per architecture |
| Ruby | Upstream commit `68af0a92d6d9c2cd0ee38a9327e98797673227d1`, Bundler lockfile |

Downloads fail on a checksum mismatch. Python requirements derive from the upstream Operator v0.160.0, with dependency security updates, and include the HTTP/protobuf exporter, rather than a Python-version-dependent gRPC exporter. Node uses `npm ci`. Ruby uses a frozen Bundler dependency graph and a pinned upstream checkout. Its tiny local loader only locates the upstream entry point; it does not implement an SDK or change signal behavior. Python's `sitecustomize.py` calls the upstream Zero-Code loader. See [dependency security](../docs/dependency-security.md) for final-image scans and supplemental Java/.NET inventories.

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

Exporters are disabled for the loader checks. The integration test separately checks Node.js HTTP export with both libc variants and both delivery methods. These checks do not establish cross-service propagation or OBI coexistence. Test images, logs, and copied payload files are scoped to this component and `.artifacts/`.

To update dependencies, change the explicit inputs, regenerate lockfiles with `make lock` (Node/npm and `uv` required), refresh the Ruby lock with Bundler when changing its upstream revision, and rerun build/loader tests for both architectures. Update downloaded asset checksums from the corresponding upstream release metadata alongside their versions. Keep the image tag immutable once used by an operator.

The packaging layout is inspired by [Beyla's webhook image](https://github.com/grafana/beyla/tree/main/pkg/webhook/image). This image uses upstream OpenTelemetry agents, including the upstream Java agent. See [NOTICE](NOTICE) for source attribution.
