# RIO .NET propagation bootstrap

RIO's .NET 8 propagation checks found that selecting `OTEL_PROPAGATORS=tracecontext`
still forwarded incoming baggage through the runtime's native HTTP propagation.
The shipped auto-instrumentation 1.17.0 includes HTTP instrumentation 1.19.0 and
DiagnosticSource 10.0.0. Its HTTP client instrumentation
[skips SDK injection for an exact TraceContextPropagator](https://github.com/open-telemetry/opentelemetry-dotnet-contrib/blob/Instrumentation.Http-1.19.0/src/OpenTelemetry.Instrumentation.Http/Implementation/HttpHandlerDiagnosticListener.cs).
ASP.NET Core similarly
[skips extraction for that propagator](https://github.com/open-telemetry/opentelemetry-dotnet-contrib/blob/Instrumentation.AspNetCore-1.19.0/src/OpenTelemetry.Instrumentation.AspNetCore/Implementation/HttpInListener.cs).
However, DiagnosticSource's
[independent default runtime propagator](https://github.com/dotnet/runtime/blob/b0f34d51fccc69fd334253924abd8d6853fad7aa/src/libraries/System.Diagnostics.DiagnosticSource/src/System/Diagnostics/DistributedContextPropagator.cs)
also handles baggage. It does not read `OTEL_PROPAGATORS`.

`PropagationPlugin` uses the upstream
[1.17.0 plugin API](https://github.com/open-telemetry/opentelemetry-dotnet-instrumentation/blob/v1.17.0/docs/plugins.md)
to align `DistributedContextPropagator.Current` with the SDK's configured
`TextMapPropagator` after initialization. Native HTTP injection and extraction
then use the same selected propagators as the SDK. It creates no spans,
instruments, providers, exporters, or application log records.

The bundle installs a BCL-only `Rio.Bootstrap` assembly at the Injector's existing
startup-hook path. The unmodified upstream hook is retained alongside it as
`OpenTelemetry.AutoInstrumentation.StartupHook.original.dll`, preserving its
assembly identity and bytes. The bootstrap prepends the plugin for .NET 8+,
then invokes that original hook. Its target is netstandard2.1, so unsupported
older runtimes can still reach the original hook's compatibility handling.
RIO has not validated or promoted additional runtime versions through this fix.

Activation belongs to the bundle: the operator adds no new environment variable
or dependency to older payload images. This preserves rollback to payloads that
contain neither RIO assembly. User plugin strings, including values resolved
from Kubernetes `valueFrom`, are preserved when the bootstrap reads the process
environment. RIO is prepended before user plugins; explicit custom plugins can
still override runtime propagation later in their lifecycle. An already listed
RIO plugin is not added again. Explicit file-based plugin lists must include
this plugin themselves; that configuration is outside the validated matrix.

Both assemblies use the .NET 9 compiler and the .NET 8 reference packs and test runtime from the immutable SDK images recorded in `versions.json`. The plugin references the exact API and DiagnosticSource
assemblies from the checksum-verified upstream agent, with no NuGet downloads.
The image build runs bootstrap regression checks for identity, dependency
isolation, plugin-list preservation, ordering, and idempotence. Shared admission
tests own exported span ancestry, headers, baggage, signal independence, and
the architecture/libc/delivery matrix. The application fixtures remain free of
OpenTelemetry dependencies and setup.
