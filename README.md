# RIO — Runtime Instrumentation Operator

[![CI](https://github.com/otelx/rio/actions/workflows/ci.yml/badge.svg)](https://github.com/otelx/rio/actions/workflows/ci.yml) [![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

**OpenTelemetry Zero-Code instrumentation across Kubernetes namespaces, with runtime detection and managed rollouts.**

RIO lets a platform team select application namespaces using one central policy. Its Kubernetes controller delivers the [OpenTelemetry Injector](https://github.com/open-telemetry/opentelemetry-injector) and language agents to selected Pods, and manages rolling updates when the policy changes. The Injector detects supported runtimes when application processes start. Application images do not need to be rebuilt.

> **Status: experimental, alpha.** Use a disposable cluster to evaluate RIO. The API and payload contract may change. The [first normal release support contract](docs/compatibility.md) is limited to Kubernetes 1.35.0, Linux amd64/arm64, and Node.js 24 HTTP trace export with the tested glibc/musl images. Java, .NET, Python, and Ruby paths, metrics/log capture, and propagation remain experimental or unvalidated. No normal release or production-readiness claim is announced here.

## Try it

Follow the **[quickstart](docs/quickstart.md)** to run a small Node.js service, select its namespace, and inspect HTTP spans in an OpenTelemetry Collector.

The central selection looks like this; the full policy also supplies a payload image, OTLP endpoint, and signal settings:

```yaml
rules:
  - name: applications
    selector:
      namespaces: [my-applications]
    action: Inject
```

RIO uses one **cluster-scoped** `Instrumentation` resource named `cluster`. Namespace selection lives in that resource; it does not require a resource or instrumentation annotation in each application namespace. Rules support ordered exclusions and optional workload selection.

## How it works

1. **Select:** resolve the first matching policy rule for each workload.
2. **Deliver:** mount the payload using a verified native image volume, or copy it with an init container when native delivery is unavailable.
3. **Activate:** the upstream Injector detects the process runtime and loads its language agent. The payload bundles Node.js plus experimental Java, .NET, Python, and Ruby paths; bundling and loader checks do not establish application telemetry support.
4. **Manage:** roll supported workloads when their effective configuration changes; restore the previous instrumentation configuration if the rollout fails.

RIO uses two container images:

| Image | Purpose |
| --- | --- |
| `ghcr.io/otelx/rio/operator` | Kubernetes controller and admission webhook that select workloads and manage instrumentation rollouts and recovery. |
| `ghcr.io/otelx/rio/instrumentation` | OpenTelemetry Injector and language agents delivered into application Pods for automatic runtime instrumentation. |

The operator Deployment uses `rio/operator`; the `Instrumentation` policy's `spec.image` selects `rio/instrumentation`.

RIO targets Linux amd64 and arm64, including glibc and Alpine/musl application images. See [compatibility](docs/compatibility.md) for supported environments and limitations.

Use operator and instrumentation images from the same full release. In-place cross-release upgrade/downgrade and mixed-version recovery are unvalidated; read the [version and migration policy](docs/compatibility.md#operatorinstrumentation-versions-and-upgrades). Only the latest alpha is currently maintained; the first normal release will use the [maintenance and security policy](SECURITY.md). [@MrAlias](https://github.com/MrAlias) owns release decisions, triage, and dependency maintenance; there is no appointed backup responder.

## Learn more

- [Quickstart](docs/quickstart.md): install, generate telemetry, and clean up.
- [Configuration](docs/configuration.md): selection, signals, rollout, retry, and removal.
- [Compatibility](docs/compatibility.md): tested paths and operational limitations.
- [Development](docs/development.md): build, run tests, and prepare releases.
- [Provenance](docs/provenance.md): origins, attribution, and upstream relationships.
- [Contributing](CONTRIBUTING.md) and [security reporting](SECURITY.md).

## Relationship to OpenTelemetry

RIO is an independent **otelx** project using upstream OpenTelemetry components. See [provenance](docs/provenance.md) for origins and attribution.

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) and [payload/NOTICE](payload/NOTICE) for attribution and bundled component information.
