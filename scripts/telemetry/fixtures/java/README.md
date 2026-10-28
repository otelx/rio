# Java telemetry fixture

This application uses the JDK HTTP server (`jdk.httpserver`), the JDK HTTP client
(`java.net.http`), and ordinary SLF4J/Logback logging. It contains no application
OpenTelemetry dependencies, configuration, spans, meters, log records, or header
injection. RIO supplies instrumentation separately.

`fixture.json` pins the runtime image indexes for both amd64 and arm64:
Temurin 21.0.12.1+1 on Ubuntu 26.04 (glibc) and Alpine 3.24 (musl). The build JDK
is independently pinned in `Dockerfile`. Library versions and SHA256 hashes are
recorded in `dependencies.lock.json`; Docker verifies these hashes when fetching
the exact Maven Central artifacts. Gson is used only for JSON serialization.

The bundled Java agent 2.33.1 lists these instrumentations in its
[versioned supported libraries](https://github.com/open-telemetry/opentelemetry-java-instrumentation/blob/v2.33.1/docs/supported-libraries.md).
Expected automatic telemetry is a server span for each real HTTP request,
`http.server.request.duration` histogram data in seconds (cumulative temporality),
and one INFO Logback event with body `rio-fixture MARKER` per `/work/MARKER`
request. HTTP client spans are expected when `next` is provided. The normal
client receives no copied incoming headers; the agent must propagate context.

Use the shared `scripts/telemetry_e2e.py` admission/Collector runner to validate
these expectations. Building this image or running it without instrumentation
does not establish Kubernetes delivery or telemetry support. Promotion requires
the complete architecture/libc/delivery matrix on the supported branch.
