# Contributing to RIO

RIO is experimental. Bug reports, reproducible compatibility results, documentation improvements, and focused fixes are welcome.

Before a substantial API or architecture change, open an issue describing the problem, proposed behavior, and operational impact. For a bug, include the RIO version, Kubernetes and container-runtime versions, application runtime, delivery method, and sanitized policy/status/logs. Do not upload kubeconfigs, credentials, certificate keys, or application data.

Follow [development](docs/development.md) to run Go tests, loader checks, and lifecycle integration checks. Match validation to the change. Policy, admission, or recovery changes need regression coverage; documentation changes should have working links and commands. Run ShellCheck on changed shell scripts.

Use **OpenTelemetry**, **OTel**, and **Zero-Code instrumentation** consistently. Follow the [OpenTelemetry documentation style guide](https://opentelemetry.io/docs/contributing/style-guide/) and [glossary](https://opentelemetry.io/docs/concepts/glossary/). Preserve exact upstream package names, configuration identifiers, and environment variables. New telemetry attributes and metrics should follow the applicable OpenTelemetry Semantic Conventions; do not invent names under the reserved `otel.*` namespace.

Keep generated files in ignored `.artifacts/`. Do not commit certificates, private keys, build output, or recordings. Preserve upstream licenses and attribution. Contributions are made under the repository's Apache License 2.0.

Write each Markdown prose paragraph or list item on a single source line; do not hard-wrap prose. Preserve line breaks inside code blocks and other structural Markdown.

## Maintenance responsibilities and recovery

[@MrAlias](https://github.com/MrAlias) handles releases, issue/security triage, and dependency updates, including Go modules, Actions, base images, and Injector/agent pins and lockfiles. Validate dependency changes using the [development workflow](docs/development.md). Public issues can be followed up after 14 days; private reports follow [SECURITY.md](SECURITY.md).

Maintenance is best effort, with no appointed backup maintainer or fixed update schedule. Maintainer absence can delay releases and responses; announce pauses or end of support in the changelog. Keep verified image copies, manifests, and workload state if you need independent recovery. The [source-build instructions](docs/development.md) support rebuilding or maintaining a fork, subject to dependency availability. Any backup maintainer needs repository, package, and private-advisory access before taking responsibility.
