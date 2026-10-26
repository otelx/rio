# Contributing to RIO

RIO is experimental. Bug reports, reproducible compatibility results, documentation
improvements, and focused fixes are welcome.

Before a substantial API or architecture change, open an issue describing the
problem, proposed behavior, and operational impact. For a bug, include the RIO
version, Kubernetes and container-runtime versions, application runtime, delivery
method, and sanitized policy/status/logs. Do not upload kubeconfigs, credentials,
certificate keys, or application data.

Follow [development](docs/development.md) to run Go tests, loader checks, and
lifecycle integration checks. Match validation to the change. Policy, admission,
or recovery changes need regression coverage; documentation changes should have
working links and commands. Run ShellCheck on changed shell scripts.

Use **OpenTelemetry**, **OTel**, and **Zero-Code instrumentation** consistently.
Follow the [OpenTelemetry documentation style guide](https://opentelemetry.io/docs/contributing/style-guide/)
and [glossary](https://opentelemetry.io/docs/concepts/glossary/). Preserve exact
upstream package names, configuration identifiers, and environment variables.
New telemetry attributes and metrics should follow the applicable OpenTelemetry
Semantic Conventions; do not invent names under the reserved `otel.*` namespace.

Keep generated files in ignored `.artifacts/`. Do not commit certificates, private
keys, build output, or recordings. Preserve upstream licenses and attribution.
Contributions are made under the repository's Apache License 2.0.
