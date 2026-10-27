using System.Diagnostics;
using OpenTelemetry;
using OpenTelemetry.AutoInstrumentation.PluginApi;
using OpenTelemetry.Context.Propagation;

namespace Rio;

// The runtime's propagator is independent from the SDK's OTEL_PROPAGATORS.
// ASP.NET Core and HttpClient can use it even when the SDK takes a fast path
// for TraceContextPropagator, which otherwise leaks native Activity baggage.
public sealed class PropagationPlugin : IPlugin
{
    public void Initializing() { }

    public void Initialized()
    {
        DistributedContextPropagator.Current = new ConfiguredPropagator();
    }

    private sealed class ConfiguredPropagator : DistributedContextPropagator
    {
        private static TextMapPropagator Configured => Propagators.DefaultTextMapPropagator;

        public override IReadOnlyCollection<string> Fields => Configured.Fields?.ToArray() ?? Array.Empty<string>();

        public override void Inject(Activity? activity, object? carrier, PropagatorSetterCallback? setter)
        {
            if (activity is null || setter is null) return;
            Configured.Inject(new PropagationContext(activity.Context, Baggage.Current), carrier,
                (target, key, value) => setter(target, key, value));
        }

        public override void ExtractTraceIdAndState(object? carrier, PropagatorGetterCallback? getter,
            out string? traceId, out string? traceState)
        {
            var context = Extract(carrier, getter).ActivityContext;
            if (context.TraceId == default || context.SpanId == default)
            {
                traceId = traceState = null;
                return;
            }
            // This runtime API calls the complete W3C traceparent "traceId".
            traceId = $"00-{context.TraceId}-{context.SpanId}-{(byte)context.TraceFlags:x2}";
            traceState = context.TraceState;
        }

        public override IEnumerable<KeyValuePair<string, string?>>? ExtractBaggage(
            object? carrier, PropagatorGetterCallback? getter) =>
            Extract(carrier, getter).Baggage.GetBaggage()
                .Select(pair => new KeyValuePair<string, string?>(pair.Key, pair.Value));

        private static PropagationContext Extract(object? carrier, PropagatorGetterCallback? getter)
        {
            if (getter is null) return default;
            return Configured.Extract(default, carrier, (target, key) =>
            {
                getter(target, key, out var value, out var values);
                return values ?? (value is null ? Array.Empty<string>() : new[] { value });
            });
        }
    }
}
