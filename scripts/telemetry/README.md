# Language telemetry integration fixtures

This shared interface supports issues #17–#24. `scripts/telemetry_e2e.py` owns cluster lifecycle, RIO installation/policy, Collector, request generation, bounded assertions, and diagnostics. Language fixtures must not build separate Kubernetes or Collector harnesses.

## Fixture interface

Each language owns `scripts/telemetry/fixtures/LANGUAGE/`, containing `Dockerfile`, application sources and dependency locks, and `fixture.json`. Docker builds use that directory as context and `--build-arg BASE_IMAGE=...`. `fixture.json` contains `language`, `runtime`, `framework`, `agent`, `images` (keys `glibc` and `musl`, each an immutable runtime image reference), and optional `env` (object of agent configuration), `metrics` and `logs` descriptions. Multi-stage builds may pin a separate build image. Record exact versions; do not claim a matrix cell passed before it ran.

Applications listen on `0.0.0.0:8080`. `GET /work/MARKER` returns JSON with `marker`, `language`, and `headers` (lowercase received header names mapped to arrays of values, preserving duplicates). An optional `next` query parameter contains a downstream URL; call it with the normal HTTP client and return its parsed JSON as `downstream`. Never copy incoming propagation headers into the outgoing request: the shipped agent must inject them. Return a successful response only when the downstream call succeeds. `/health` may serve a simple health response.

For each `/work/` request, emit exactly one INFO application log with body `rio-fixture MARKER` through the language's ordinary logging library. Include no OpenTelemetry dependencies, SDK setup, custom telemetry, or propagation API in application code. A request may perform bounded CPU/allocation activity for automatic runtime metrics. The harness configures exporter/metric intervals through RIO policy, never an application SDK.

If a logging library captures a formatted line terminator, declare its exact
suffix in `logs.bodySuffix` (for example `"\n"`). Collector assertions append
that declared suffix to the marker and compare the complete body exactly;
they never trim or normalize captured records.

The harness creates fresh service identities per scenario, tests serving without injection, trace export and trace suppression, and inspects Collector OTLP JSON. Signal and propagation scenarios reuse these same images. Build outputs, resolved image IDs, runtime output, policy/Pod JSON, application/operator/Collector logs and Collector signal files belong under ignored `.artifacts/telemetry/UNIQUE-RUN/`.

Workers must report changes, exact commands and outcomes, commit/branch identifiers, and every remaining acceptance criterion. Fixture implementation alone does not resolve a child; the parent integrates it and validates admission, both delivery methods, architectures and libc variants in CI before closure.

## Running the checks

Build the current operator and instrumentation images, then run `python3 scripts/telemetry_e2e.py --arch amd64 --delivery initContainer` (or the corresponding native architecture and `imageVolume`). `OPERATOR_IMAGE` and `PAYLOAD_IMAGE` select the built images. The default local tags end in `:issue-17`; CI uses the tested `:dev-ARCH` images. `--languages`, `--libcs`, and `--scenarios` select a diagnostic subset; subset success never establishes the unselected cells. Use kind 0.33.0 with the Kubernetes 1.35.0 node image.

The full default command fails for incomplete automatic metrics coverage. Ruby's bundled Rack instrumentation does not automatically produce metrics; [upstream metrics work](https://github.com/open-telemetry/opentelemetry-ruby-contrib/pull/1129) remains unmerged. CI explicitly uses `--allow-known-blockers` to gate the implemented checks while saving `metrics-coverage.json` with the unresolved language and follow-up. This exception does not resolve #22 or parent #17. All other selected scenarios must pass.

Failed clusters are retained. `--resume rio-issue-17-RUNID` explicitly retries a cluster whose kubeconfig belongs to this worktree, deleting its fixture/Collector namespaces and policy, then saving new evidence in a separate retry directory. Use the same architecture/delivery as the retained cluster. Cold init-container copies receive a bounded five-minute rollout budget; telemetry waits remain sixty seconds or less. Stop the previous invocation before retrying its cluster.

The propagation scenario exercises the HTTP/1.1 ring Node.js → Java → .NET → Python → Ruby → Node.js on each selected libc, architecture and delivery method. It tests sampled incoming parents and tracestate, real client/server ancestry, W3C tracecontext with baggage enabled/disabled, malformed/absent context, unsampled parents and overlapping contexts. It asserts received downstream headers as well as Collector spans. This enumerated ring does not imply coverage of every language pair or protocol.

Logs use Winston, Logback, Microsoft.Extensions.Logging, Python stdlib logging and Ruby Logger. Metrics use automatic HTTP server request-duration histograms for Node.js, Java, .NET and Python; a newer cumulative count after real requests must increase without summing repeated exports. The application code contains no OpenTelemetry SDK setup or telemetry construction. Every run retains exact fixture/agent/image inputs and bounded assertion output under `.artifacts/telemetry/`. CI archives logs and JSON diagnostics but excludes kubeconfigs and private keys.
