# Contributing to RIO

RIO is experimental. Bug reports, reproducible compatibility results, documentation improvements, and focused fixes are welcome.

Before a substantial API or architecture change, open an issue describing the problem, proposed behavior, and operational impact. For a bug, include the RIO version, Kubernetes and container-runtime versions, application runtime, delivery method, and sanitized policy/status/logs. Do not upload kubeconfigs, credentials, certificate keys, or application data.

Follow [development](docs/development.md) to run Go tests, loader checks, and lifecycle integration checks. Match validation to the change. Policy, admission, or recovery changes need regression coverage; documentation changes should have working links and commands. Run ShellCheck on changed shell scripts.

## Maintenance responsibilities and recovery

[@MrAlias](https://github.com/MrAlias) is responsible for release approval and support-scope changes, public issue triage, private security-report triage, and review/update of Go modules, pinned GitHub Actions, base images, and bundled Injector/agent inputs and lockfiles. Contributors can propose fixes and dependency updates, but upstream releases do not automatically change RIO's support matrix. Validate updates using the [development workflow](docs/development.md) and document changed behavior before publication.

The project currently has one administrator and no appointed backup maintainer. Maintenance is best effort; there is no staffed on-call rotation, fixed dependency-update cadence, or guaranteed issue-resolution time. Public issues can be followed up after 14 calendar days; private reports follow [SECURITY.md](SECURITY.md). If capacity changes, update the support policy and announce maintenance pauses or end of support in the changelog rather than implying continued coverage.

Recovery materials are the versioned source, pinned dependency inputs, installation manifests, and [source-build instructions](docs/development.md); release archives alone do not preserve deleted upstream downloads or images. Operators requiring continuity should retain verified image digests and private copies of images, configuration, and workload recovery state. If the sole maintainer becomes unavailable, there is no guaranteed automatic account or repository handover. Existing source can be forked and rebuilt under its license; any future backup maintainer must be explicitly appointed and given the necessary repository, package, and private-advisory access before being advertised as a responder. A fork is independently maintained and must publish its own support/security policy. These limits follow the principle of [documenting maintainer expectations](https://opensource.guide/best-practices/#communicate-your-expectations).

Use **OpenTelemetry**, **OTel**, and **Zero-Code instrumentation** consistently. Follow the [OpenTelemetry documentation style guide](https://opentelemetry.io/docs/contributing/style-guide/) and [glossary](https://opentelemetry.io/docs/concepts/glossary/). Preserve exact upstream package names, configuration identifiers, and environment variables. New telemetry attributes and metrics should follow the applicable OpenTelemetry Semantic Conventions; do not invent names under the reserved `otel.*` namespace.

Keep generated files in ignored `.artifacts/`. Do not commit certificates, private keys, build output, or recordings. Preserve upstream licenses and attribution. Contributions are made under the repository's Apache License 2.0.

Write each Markdown prose paragraph or list item on a single source line; do not hard-wrap prose. Preserve line breaks inside code blocks and other structural Markdown.
