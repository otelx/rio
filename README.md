# RIO — Runtime Instrumentation Operator

[![CI](https://github.com/otelx/rio/actions/workflows/ci.yml/badge.svg)](https://github.com/otelx/rio/actions/workflows/ci.yml) [![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

**OpenTelemetry Zero-Code instrumentation across Kubernetes namespaces, with runtime detection and managed rollouts.**

RIO lets a platform team select application namespaces using one central policy. Its Kubernetes controller delivers the [OpenTelemetry Injector](https://github.com/open-telemetry/opentelemetry-injector) and language agents to selected Pods, and manages rolling updates when the policy changes. The Injector detects supported runtimes when application processes start. Application images do not need to be rebuilt.

> **Status: experimental, alpha.** Use a disposable cluster to evaluate RIO. The API and payload contract may change. Read the [compatibility and operational limits](docs/compatibility.md) before trying existing workloads.

## Try it

Follow the **[quickstart](docs/quickstart.md)** to run a small Node.js service, select its namespace, and inspect real HTTP spans in an OpenTelemetry Collector. The example does not depend on a vendor backend or the presentation repository.

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
3. **Activate:** the upstream Injector detects the process runtime and loads its language agent. The payload bundles Java, Node.js, .NET, Python, and Ruby agents.
4. **Manage:** roll supported workloads when their effective configuration changes; restore the previous instrumentation configuration if the rollout fails.

The controller and payload are maintained in this repository and built as two images: `ghcr.io/otelx/rio` and `ghcr.io/otelx/rio-payload`. Agent and Injector source remain upstream dependencies, pinned in [payload/versions.json](payload/versions.json) and language lockfiles.

RIO targets Linux amd64 and arm64, including glibc and Alpine/musl application images. Starting with `v0.1.0-alpha.2`, multi-platform release tags support mixed Linux clusters. The existing `v0.1.0-alpha.1` images are amd64 only; see [compatibility](docs/compatibility.md) for the validated runtime versions and release limits.

## Learn more

- [Quickstart](docs/quickstart.md): install, generate telemetry, and clean up.
- [Configuration](docs/configuration.md): selection, signals, rollout, retry, and removal.
- [Compatibility](docs/compatibility.md): tested paths and operational limitations.
- [Development](docs/development.md): build, run tests, and prepare releases.
- [Provenance](docs/provenance.md): origins, attribution, and upstream relationships.
- [Contributing](CONTRIBUTING.md) and [security reporting](SECURITY.md).

## Relationship to OpenTelemetry

RIO is an independent **otelx** project using OpenTelemetry components. It is not an official OpenTelemetry subproject or a distribution of the upstream [OpenTelemetry Operator](https://github.com/open-telemetry/opentelemetry-operator). That Operator already supports language-specific workload and namespace opt-in. RIO explores central namespace selection, runtime detection, and instrumentation rollout management together. OpenTelemetry eBPF Instrumentation (OBI) is separate and is not installed or coordinated by RIO.

RIO grew out of the namespace-driven integration demonstrated in [OpenTelemetry Far and Wide](https://github.com/MrAlias/OpenTelemetry-Far-and-Wide). Its design builds on Grafana's composite SDK injection prototype, with credit to Jack Berg and Nikola Grcevski. See [provenance](docs/provenance.md) for precise source references and the distinction between that prototype and this controller.

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) and [payload/NOTICE](payload/NOTICE) for attribution and bundled component information.
