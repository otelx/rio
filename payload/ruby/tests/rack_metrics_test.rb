# frozen_string_literal: true
# Unit regressions for the bundle extension. Not an application fixture.
require 'bundler/setup'
require 'opentelemetry-metrics-sdk'
require ENV.fetch('RIO_METRICS_EXTENSION')

module Checks
  def self.assert(value, message = 'assertion failed')
    raise message unless value
  end
  def self.raises(error)
    yield
    raise 'expected exception'
  rescue error => e
    e
  end
end

original_provider = OpenTelemetry.meter_provider
provider = OpenTelemetry::SDK::Metrics::MeterProvider.new
reader = OpenTelemetry::SDK::Metrics::Export::InMemoryMetricPullExporter.new
provider.add_metric_reader(reader)
OpenTelemetry.meter_provider = provider
histogram = Rio::RackMetrics.histogram
Checks.assert(Rio::RackMetrics.histogram.equal?(histogram), 'registered duplicate instrument')
histogram.record(0.012, attributes: {'http.request.method'=>'GET'})
sample = reader.collect.fetch(0)
Checks.assert(sample.name == Rio::RackMetrics::METRIC_NAME && sample.unit == 's')
Checks.assert(sample.data_points.fetch(0).explicit_bounds == Rio::RackMetrics::BOUNDARIES, 'seconds buckets missing')
Checks.assert(provider.registered_views.length == 1 && OpenTelemetry.meter_provider.equal?(provider), 'reconfigured global provider')
OpenTelemetry.meter_provider = original_provider

Sink = Struct.new(:samples) do
  def record(value, attributes:)
    samples << [value, attributes]
  end
end
SINK = Sink.new([])
Rio::RackMetrics.define_singleton_method(:histogram) { SINK }
config = OpenTelemetry::Instrumentation::Rack::Instrumentation.instance.config
config[:send_metrics] = true
config[:untraced_endpoints] = []
config[:untraced_requests] = nil
ENV['OTEL_RUBY_INSTRUMENTATION_RACK_CONFIG_OPTS'] = 'send_metrics=false'
Checks.assert(!OpenTelemetry::Instrumentation::Rack::Instrumentation.instance.send(:config_options, {})[:send_metrics], 'standard config opt-out ignored')
ENV.delete('OTEL_RUBY_INSTRUMENTATION_RACK_CONFIG_OPTS')
ENV['OTEL_RUBY_INSTRUMENTATION_RACK_ENABLED'] = 'false'
Checks.assert(!OpenTelemetry::Instrumentation::Rack::Instrumentation.instance.enabled?, 'Rack instrumentation opt-out ignored')
ENV.delete('OTEL_RUBY_INSTRUMENTATION_RACK_ENABLED')
passthrough = Class.new do
  def initialize(app)
    @app = app
  end
  def call(env)
    @app.call(env)
  end
end
Env = lambda do
  {'REQUEST_METHOD' => 'GET', 'rack.url_scheme' => 'http', 'SERVER_PROTOCOL' => 'HTTP/1.1',
   'PATH_INFO' => '/unique/private', 'QUERY_STRING' => 'secret=value', 'HTTP_HOST' => 'untrusted'}
end
wrap = ->(app) { Rio::RackMetrics::Middleware.new(app, passthrough) }
check = Checks.method(:assert)
cases = 3

body = wrap.call(->(_env) { [200, {}, ['hello']] }).call(Env.call)[2]
check.call(SINK.samples.empty?, 'recorded before response body completion')
body.each { |part| check.call(part == 'hello') }
sleep 0.01
body.close
body.close
check.call(SINK.samples.length == 1, 'duplicate close recorded twice')
value, attrs = SINK.samples.last
check.call(value >= 0.01, 'duration excluded body consumption')
check.call(attrs == {'http.request.method'=>'GET','url.scheme'=>'http','network.protocol.version'=>'1.1','http.response.status_code'=>200}, attrs.inspect)
cases += 1

%w[/work/one /work/two].each do |path|
  env = Env.call.merge('PATH_INFO'=>path, 'QUERY_STRING'=>'unique=secret', 'sinatra.route'=>'GET /work/:marker')
  wrap.call(->(_env) { [200, {}, []] }).call(env)[2].close
  check.call(SINK.samples.last[1]['http.route'] == '/work/:marker', 'lost bounded route template')
end
cases += 1

class RequestFailure < StandardError; end
failure = RequestFailure
error = failure.new('secret')
Checks.raises(failure) { wrap.call(->(_env) { raise error }).call(Env.call) }
check.call(SINK.samples.last[1]['error.type'] == failure.name)
check.call(!SINK.samples.last[1].key?('http.response.status_code'))
cases += 1

anonymous_error = Class.new(StandardError).new('private detail')
check.call(Checks.raises(StandardError) { wrap.call(->(_env) { raise anonymous_error }).call(Env.call) }.equal?(anonymous_error))
check.call(SINK.samples.last[1]['error.type'] == '_OTHER', 'anonymous error type is not bounded')
cases += 1

%w[each close call].each do |method|
  failing_body = Object.new
  failing_body.define_singleton_method(method) { |*_args| raise error }
  proxy = wrap.call(->(_env) { [200, {}, failing_body] }).call(Env.call)[2]
  count = SINK.samples.length
  check.call(Checks.raises(failure) { proxy.public_send(method) {} }.equal?(error), 'exception changed')
  begin
    proxy.close
  rescue failure
  end
  check.call(SINK.samples.length == count + 1, 'streaming failure recorded twice')
  check.call(SINK.samples.last[1]['error.type'] == failure.name)
  cases += 1
end

stream = Object.new
stream.define_singleton_method(:call) { |output| output << 'stream' }
proxy = wrap.call(->(_env) { [204, {}, stream] }).call(Env.call)[2]
check.call(!proxy.respond_to?(:each) && proxy.respond_to?(:call), 'streaming body interface changed')
output = []
proxy.call(output)
proxy.close
check.call(output == ['stream'])
cases += 1

app = wrap.call(wrap.call(->(_env) { [503, {}, []] }))
count = SINK.samples.length
app.call(Env.call)[2].close
check.call(SINK.samples.length == count + 1, 'nested middleware duplicate')
check.call(SINK.samples.last[1]['error.type'] == '503')
cases += 1

request = Rio::RackMetrics::Request.new(Env.call)
count = SINK.samples.length
Array.new(8) { Thread.new { request.finish } }.each(&:join)
check.call(SINK.samples.length == count + 1, 'concurrent completion recorded twice')
cases += 1

env = Env.call.merge('REQUEST_METHOD'=>'CUSTOM', 'SERVER_PROTOCOL'=>'garbage', 'rack.url_scheme'=>'other')
wrap.call(->(_env) { [200, {}, []] }).call(env)[2].close
check.call(SINK.samples.last[1] == {'http.request.method'=>'_OTHER','http.response.status_code'=>200})
ENV['OTEL_INSTRUMENTATION_HTTP_KNOWN_METHODS'] = 'GET,CUSTOM'
wrap.call(->(_env) { [200, {}, []] }).call(env.dup.tap { |e| e.delete(Rio::RackMetrics::STATE_KEY) })[2].close
check.call(SINK.samples.last[1]['http.request.method'] == 'CUSTOM')
ENV.delete('OTEL_INSTRUMENTATION_HTTP_KNOWN_METHODS')
cases += 1

[[:send_metrics, false]].each do |key, value|
  previous = config[key]
  config[key] = value
  count = SINK.samples.length
  wrap.call(->(_env) { [200, {}, []] }).call(Env.call)[2].each {}
  check.call(SINK.samples.length == count, "ignored #{key}")
  config[key] = previous
  cases += 1
end
predicate_calls = 0
config[:untraced_requests] = ->(_env) { predicate_calls += 1; true }
check.call(Rio::RackMetrics.enabled?(config))
check.call(predicate_calls.zero?, 'metrics reevaluated the tracing exclusion predicate')
config[:untraced_requests] = nil
cases += 1
ENV['OTEL_SDK_DISABLED'] = 'true'
check.call(!Rio::RackMetrics.enabled?(config))
ENV.delete('OTEL_SDK_DISABLED')
cases += 1

# Instrumentation failure must not break normal response completion.
SINK.define_singleton_method(:record) { |*_args, **_kwargs| raise 'sink unavailable' }
wrap.call(->(_env) { [200, {}, []] }).call(Env.call)[2].close
cases += 1
puts "PASS #{cases} RIO Rack metrics regression cases"
