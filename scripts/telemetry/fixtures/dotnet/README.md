# ASP.NET Core fixture

The fixture uses the ASP.NET Core shared framework for Kestrel, `HttpClient`, and `ILogger`. It has no NuGet package dependencies; the cleared package sources and locked restore make that property explicit. Both runtime images and the build SDK are pinned in `fixture.json` and the Dockerfile. The runtime images' manifest lists include Linux amd64 and arm64. Runtime and ASP.NET Core are 8.0.33; the compiler SDK is 9.0.320. The pinned 8.0.427 SDK supplies the .NET 8 framework and host packs for the restore with cleared package sources. Both SDK images appear in `fixture.json`. The runtime image supplies its matching framework libraries.

`GET /work/MARKER` logs one `ILogger` INFO message, returns the received headers
as arrays, and optionally calls `next` through a normal `HttpClient`. A failed
downstream request fails the caller request. The application does not copy
context headers and does not depend on OpenTelemetry packages or APIs.

The agent's [1.17.0 instrumentation configuration](https://github.com/open-telemetry/opentelemetry-dotnet-instrumentation/blob/v1.17.0/docs/config.md)
documents ASPNETCORE and HTTPCLIENT tracing/metrics and ILOGGER capture. The
fixture enables formatted log bodies through agent configuration so assertions
can compare the complete marker message. `http.server.request.duration` is the
automatic HTTP metric candidate; the shared Collector harness must verify its
actual name, histogram samples, unit, and temporality before promoting support.

ASP.NET Core and HttpClient can propagate `traceparent` through native
`System.Diagnostics.Activity` even without RIO. An uninstrumented negative
control must assert absent Collector telemetry, rather than assume all outgoing
context headers are absent. On the pinned runtime, an uninstrumented smoke test
also observed incoming `baggage` forwarded as legacy `correlation-context`.
Agent tests must establish the configured W3C baggage behavior independently.

This fixture alone does not establish admission, export, suppression, logs,
metrics, or propagation support. Those assertions belong to the shared
Kubernetes/Collector harness across both delivery methods, architectures, and
libc variants.
