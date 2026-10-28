# Ruby HTTP fixture

Ruby 3.3.12 serves Sinatra 4.2.1 on Rack 3.2.7, Rackup 2.3.1, and WEBrick 1.9.2.
`Gemfile.lock` pins all application dependencies; neither the application nor its
Gemfile contains OpenTelemetry packages or setup. Application logging uses
Logger 1.7.0, and outgoing requests use Net::HTTP 0.10.0 with bounded timeouts.
The standard Logger formatter emits the marker message with a trailing newline
so container runtimes promptly expose the complete application log line. The
shipped Logger bridge captures this formatted message, including its newline;
`logs.bodySuffix` records the exact expected OTLP body terminator.
`RIO_FIXTURE_DELAY_MS` optionally delays each work handler before logging and
calling the next service, bounded to 0..1000ms (default 0), for overlap tests.
The two runtime manifest digests in `fixture.json` match the shipped Ruby bundle
build inputs and include Linux amd64 and arm64 variants.

Sinatra is deliberate: the bundled Sinatra instrumentation automatically adds
the Rack server instrumentation. Bare Rack would require application middleware
configuration. The application uses normal `Bundler.require`, which the shipped
auto-instrumentation loader intercepts after dependencies load.

The pinned Rackup WEBrick input retains the original `WEBrick::HTTPRequest`.
The fixture reads its header arrays to preserve duplicate received values,
which Rack's standard environment otherwise joins. This is observation only;
the application never copies context headers into outgoing requests.

## Automatic HTTP metrics

The bundle's separately versioned RIO Rack metrics extension 1.0.0 measures
ordinary Rack requests through response body completion using the distro's
configured metrics provider and exporter. It emits the stable seconds histogram
`http.server.request.duration`; the application creates no meters or records.
The upstream Rack 0.30.0 gem itself still supplies traces only. See the
[bundle extension contract](../../../../payload/ruby/README.md) for attributes,
streaming semantics, signal controls and the precise implementation boundary.

The shared metrics scenario requires a newer cumulative sample count after
three fresh work requests, four inherited/rule enable/disable cases, a bare
negative control, metrics-only export and unsampled incoming requests. The logs
scenario separately requires metrics to remain available while log export is
disabled. These scenarios establish only the exact matrix cells actually run.

## Validation boundary

Fixture builds and uninstrumented HTTP checks alone do not establish signal or
propagator support. The shared Kubernetes/Collector harness owns admission,
fresh export assertions, negative controls, and both delivery methods across
the architecture/libc matrix. Support documentation must follow that evidence.
