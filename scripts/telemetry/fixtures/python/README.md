# Python HTTP fixture

Python 3.14.6 runs aiohttp 3.13.3 as both server and HTTP client. The pinned
glibc and musl images match the Python payload build inputs. `requirements.lock`
pins and hashes the complete application dependency graph; Docker requires
binary wheels so an unavailable architecture/libc wheel fails the build.
Application dependencies contain no OpenTelemetry package.

The fixture implements the [shared interface](../../README.md). aiohttp's
`raw_headers` preserves duplicate header fields for propagation assertions.
`ClientSession.get` receives no application-supplied propagation headers. Each
work request logs once through standard-library `logging`, preserving the
agent's root handler and normal console output. Both client and server use
instrumentation already included in RIO's Python distro 0.66b1 / SDK 1.45.1.

With `OTEL_SEMCONV_STABILITY_OPT_IN=http`, the automatic server duration source
is `http.server.request.duration`, an OTLP histogram in seconds. The SDK's
default OTLP aggregation temporality is cumulative. Request logs have INFO
severity and automatically acquired request trace/span IDs when tracing is
enabled. These are expected assertions for the shared Collector harness, not
a claim that every environment has passed.

Build either libc variant with the corresponding immutable `images` value from
`fixture.json`:

```sh
docker build --build-arg BASE_IMAGE=IMAGE -t rio-issue-17-python:LIBC scripts/telemetry/fixtures/python
```

Regenerate the dependency lock from this directory with uv 0.13.0:

```sh
uv pip compile requirements.in --python-version 3.14 --generate-hashes -o requirements.lock
```

Admission, signal suppression, negative controls, propagation ancestry, matrix
validation, and saved diagnostics belong to the shared telemetry harness. A
Docker-only agent test does not establish Kubernetes delivery support.
