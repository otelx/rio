# Language telemetry integration fixtures

This shared interface supports issues #17–#24. `scripts/telemetry_e2e.py` owns cluster lifecycle, RIO installation/policy, Collector, request generation, bounded assertions, and diagnostics. Language fixtures must not build separate Kubernetes or Collector harnesses.

## Fixture interface

Each language owns `scripts/telemetry/fixtures/LANGUAGE/`, containing `Dockerfile`, application sources and dependency locks, and `fixture.json`. Docker builds use that directory as context and `--build-arg BASE_IMAGE=...`. `fixture.json` contains `language`, `runtime`, `framework`, `agent`, `images` (keys `glibc` and `musl`, each an immutable runtime image reference), and optional `env` (object of agent configuration), `metrics` and `logs` descriptions. Multi-stage builds may pin a separate build image. Record exact versions; do not claim a matrix cell passed before it ran.

Applications listen on `0.0.0.0:8080`. `GET /work/MARKER` returns JSON with `marker`, `language`, and `headers` (lowercase received header names mapped to arrays of values, preserving duplicates). An optional `next` query parameter contains a downstream URL; call it with the normal HTTP client and return its parsed JSON as `downstream`. Never copy incoming propagation headers into the outgoing request: the shipped agent must inject them. Return a successful response only when the downstream call succeeds. `/health` may serve a simple health response.

For each `/work/` request, emit exactly one INFO application log with body `rio-fixture MARKER` through the language's ordinary logging library. Include no OpenTelemetry dependencies, SDK setup, custom telemetry, or propagation API in application code. A request may perform bounded CPU/allocation activity for automatic runtime metrics. The harness configures exporter/metric intervals through RIO policy, never an application SDK.

The harness creates fresh service identities per scenario, tests serving without injection, trace export and trace suppression, and inspects Collector OTLP JSON. Signal and propagation scenarios reuse these same images. Build outputs, resolved image IDs, runtime output, policy/Pod JSON, application/operator/Collector logs and Collector signal files belong under ignored `.artifacts/telemetry/UNIQUE-RUN/`.

Workers must report changes, exact commands and outcomes, commit/branch identifiers, and every remaining acceptance criterion. Fixture implementation alone does not resolve a child; the parent integrates it and validates admission, both delivery methods, architectures and libc variants in CI before closure.
