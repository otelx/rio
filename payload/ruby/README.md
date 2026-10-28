# RIO Ruby bundle extensions

The upstream auto-instrumentation commit and gems remain pinned by `Gemfile.lock`.
RIO separately versions its original Rack metrics extension as `1.0.0` in
`versions.json` (`rubyRioRackMetrics`). This is a bundle extension, not a claim
that the upstream Rack gem already implements metrics or an unreviewed copy of
its open proposals [#1129](https://github.com/open-telemetry/opentelemetry-ruby-contrib/pull/1129)
and [#1213](https://github.com/open-telemetry/opentelemetry-ruby-contrib/pull/1213).

The loader registers the extension before the upstream distro's deferred
`Bundler.require` configures instrumentation. It extends the installed Rack
0.30.0 instrumentation's middleware construction and retains the selected
upstream tracing middleware, including `use_rack_events=false`. The extension
uses the existing global meter provider configured by metrics SDK 0.13.1,
metrics API 0.5.0 and OTLP metrics exporter 0.8.0. It creates no provider, reader,
exporter, spans, log records or propagation state. No application changes or
OpenTelemetry dependencies are required.

`http.server.request.duration` is a histogram in seconds, measured with the
monotonic clock from middleware entry through response body close. The scoped
meter `io.otelx.rio.rack` version `1.0.0` uses the HTTP semantic convention's
recommended seconds boundaries. A per-request state and completion mutex prevent
nested middleware, concurrent completion or repeated close from recording twice.
Application-call, streaming `each`/`call`, and body-close failures record once and
reraise the original exception. Body methods, including sendfile `to_path`, are
delegated without adding an `each` method to streaming-only bodies. Rack servers
must honor their normal obligation to close response bodies.

Attributes are the bounded known HTTP request method (`_OTHER` otherwise),
HTTP/HTTPS scheme, known protocol version, response status when available,
and error type (exception class, `_OTHER` for anonymous classes, or 5xx code).
`OTEL_INSTRUMENTATION_HTTP_KNOWN_METHODS` overrides the default method set.
A matched Sinatra `sinatra.route` supplies its route template; raw request paths,
queries, hosts, headers and marker values never become metric attributes. Other
framework route-template conventions are outside this extension's tested scope.

The ordinary Ruby instrumentation settings remain authoritative:

- `OTEL_RUBY_INSTRUMENTATION_RACK_ENABLED=false` skips Rack tracing and metrics;
  the bundle provides a pass-through when Sinatra requests disabled Rack
  middleware, allowing the application to continue serving.
- `OTEL_RUBY_INSTRUMENTATION_RACK_CONFIG_OPTS=send_metrics=false` disables only
  this measurement. `send_metrics` is validated as a normal Rack boolean option.
- `OTEL_SDK_DISABLED=true` prevents measurement. `OTEL_METRICS_EXPORTER=none`,
  including RIO's `signals.metrics: false`, uses the upstream SDK's normal export
  suppression. Trace and log settings do not control measurement.
- Rack's `untraced_endpoints` and `untraced_requests` retain their trace-only
  behavior. The metrics extension does not reevaluate a user callback.

Both libc bundle build stages execute the targeted extension regression tests.
The shared admission/Collector fixture pins Ruby 3.3.12, Rack 3.2.7 and Sinatra
4.2.1; validation records identify the actual architecture, delivery and libc
cells run. This internal implementation description does not promote additional
runtime/framework combinations or replace the required complete native CI matrix.
