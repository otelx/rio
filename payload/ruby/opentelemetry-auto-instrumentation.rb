# The upstream gem lives in Bundler's pinned git checkout; its loader discovers
# the dependency gems through OTEL_RUBY_ADDITIONAL_GEM_PATH set by the Injector.
# Resolve the application's ordinary dependencies before the SDK requires Ruby
# standard-library gems such as logger. Otherwise Ruby may activate its default
# logger version and make a valid application's pinned bundle fail to start.
require 'bundler'
require 'bundler/setup' if Bundler::SharedHelpers.in_bundle?
entry = Dir[File.join(__dir__, 'bundler/gems/*/lib/opentelemetry-auto-instrumentation.rb')].fetch(0)
require entry
# Register the real upstream standard Logger bridge before the application's
# Bundler.require triggers the distro's use_all initialization. No app SDK setup
# is necessary. The pinned instrumentation-all version predates this bridge.
require 'opentelemetry-instrumentation-logger'
