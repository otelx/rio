# Ruby HTTP fixture

Ruby 3.3.12 serves Sinatra 4.2.1 on Rack 3.2.7, Rackup 2.3.1, and WEBrick 1.9.2.
`Gemfile.lock` pins all application dependencies; neither the application nor its
Gemfile contains OpenTelemetry packages or setup. Application logging uses
Logger 1.7.0, and outgoing requests use Net::HTTP 0.10.0 with bounded timeouts.
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

## Automatic metrics blocker

The shipped Rack instrumentation 0.30.0 and Sinatra instrumentation 0.29.0 do
not create HTTP meters or instruments. Enabling the metrics exporter does not
create measurements. The relevant upstream work remains
[the metrics instrumentation design issue #1127](https://github.com/open-telemetry/opentelemetry-ruby-contrib/issues/1127),
[Rack metrics prototype #1129](https://github.com/open-telemetry/opentelemetry-ruby-contrib/pull/1129),
and [metrics event-handler prototype #1213](https://github.com/open-telemetry/opentelemetry-ruby-contrib/pull/1213).
The later [Rack/Net::HTTP metrics proposal #1785](https://github.com/open-telemetry/opentelemetry-ruby-contrib/pull/1785)
closed without merging. These states were checked on 2026-10-27. The fixture
therefore declares no automatic metric source. Issue #22 must remain open for
Ruby until a concrete automatic source ships and passes Collector assertions;
application-created meters are not a substitute.

## Validation boundary

Fixture builds and uninstrumented HTTP checks alone do not establish signal or
propagator support. The shared Kubernetes/Collector harness owns admission,
fresh export assertions, negative controls, and both delivery methods across
the architecture/libc matrix. Support documentation must follow that evidence.
