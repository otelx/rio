# The upstream gem lives in Bundler's pinned git checkout; its loader discovers
# the dependency gems through OTEL_RUBY_ADDITIONAL_GEM_PATH set by the Injector.
# Resolve the application's ordinary dependencies before the SDK requires Ruby
# standard-library gems such as logger. Otherwise Ruby may activate its default
# logger version and make a valid application's pinned bundle fail to start.
require 'bundler'
require 'bundler/setup' if Bundler::SharedHelpers.in_bundle?
entry = Dir[File.join(__dir__, 'bundler/gems/*/lib/opentelemetry-auto-instrumentation.rb')].fetch(0)
# The upstream loader can call Bundler.require immediately. Defer that call
# until the Logger bridge is registered, otherwise use_all misses the bridge.
require_bundler = ENV.delete('OTEL_RUBY_REQUIRE_BUNDLER')
begin
  require entry
  require 'opentelemetry-instrumentation-logger'
  require_relative 'rio-rack-metrics'
ensure
  ENV['OTEL_RUBY_REQUIRE_BUNDLER'] = require_bundler unless require_bundler.nil?
end
Bundler.require if require_bundler == 'true'
