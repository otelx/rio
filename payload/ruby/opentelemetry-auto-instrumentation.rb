# The upstream gem lives in Bundler's pinned git checkout; its loader discovers
# the dependency gems through OTEL_RUBY_ADDITIONAL_GEM_PATH set by the Injector.
entry = Dir[File.join(__dir__, 'bundler/gems/*/lib/opentelemetry-auto-instrumentation.rb')].fetch(0)
require entry
