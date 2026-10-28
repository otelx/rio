# frozen_string_literal: true

# RIO's bundle-owned extension for the pinned Rack instrumentation. It uses the
# meter provider configured by the upstream distro; it never configures an SDK.
require 'opentelemetry-instrumentation-rack'

module Rio
  module RackMetrics
    VERSION = '1.0.0'
    METER_NAME = 'io.otelx.rio.rack'
    METRIC_NAME = 'http.server.request.duration'
    STATE_KEY = 'rio.rack.request_metrics'
    METHODS = %w[CONNECT DELETE GET HEAD OPTIONS PATCH POST PUT QUERY TRACE].freeze
    BOUNDARIES = [0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10].freeze
    MUTEX = Mutex.new

    def self.histogram
      MUTEX.synchronize do
        provider = OpenTelemetry.meter_provider
        # Bind to the configured provider, rather than caching a no-op provider
        # before SDK.configure. A replacement provider gets its own instrument.
        unless @provider.equal?(provider)
          if provider.respond_to?(:add_view)
            provider.add_view(METRIC_NAME, type: :histogram, unit: 's', meter_name: METER_NAME,
                              meter_version: VERSION,
                              aggregation: OpenTelemetry::SDK::Metrics::Aggregation::ExplicitBucketHistogram.new(boundaries: BOUNDARIES))
          end
          @histogram = provider.meter(METER_NAME, version: VERSION).create_histogram(
            METRIC_NAME, unit: 's', description: 'Duration of HTTP server requests.')
          @provider = provider
        end
        @histogram
      end
    end

    def self.enabled?(config)
      return false if ENV['OTEL_SDK_DISABLED']&.casecmp?('true')
      return false unless config[:send_metrics]

      true
    end

    def self.report(error)
      OpenTelemetry.handle_error(message: 'RIO Rack HTTP metrics failed', exception: error)
    rescue StandardError
      # Reporting telemetry errors must not change application behavior.
    end

    class Request
      def initialize(env)
        @env = env
        @started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
        @histogram = RackMetrics.histogram
        @mutex = Mutex.new
      end

      attr_writer :status

      def finish(error = nil)
        @mutex.synchronize do
          return if @finished

          @finished = true
        end
        duration = Process.clock_gettime(Process::CLOCK_MONOTONIC) - @started
        methods = ENV.fetch('OTEL_INSTRUMENTATION_HTTP_KNOWN_METHODS', METHODS.join(',')).split(',').map(&:strip)
        method = @env['REQUEST_METHOD']
        attributes = { 'http.request.method' => methods.include?(method) ? method : '_OTHER' }
        scheme = @env['rack.url_scheme']
        attributes['url.scheme'] = scheme if %w[http https].include?(scheme)
        protocol = @env['SERVER_PROTOCOL'].to_s.delete_prefix('HTTP/')
        attributes['network.protocol.version'] = protocol if %w[1.0 1.1 2 3].include?(protocol)
        attributes['http.response.status_code'] = @status if @status
        # This is Sinatra's matched route template, never the raw request path.
        route = @env['sinatra.route']
        attributes['http.route'] = route.split(' ', 2).last if route.is_a?(String) && route.include?(' ')
        if error
          error_name = error.class.name
          attributes['error.type'] = error_name.nil? || error_name.empty? ? '_OTHER' : error_name
        elsif @status && @status >= 500
          attributes['error.type'] = @status.to_s
        end
        @histogram.record(duration, attributes: attributes)
      rescue StandardError => e
        RackMetrics.report(e)
      end
    end

    # Delegate the body's actual interface: streaming #call bodies must not
    # suddenly advertise #each, and sendfile/to_path behavior is preserved.
    class Body
      def initialize(body, request)
        @body = body
        @request = request
      end

      def close
        @body.close if @body.respond_to?(:close)
      rescue Exception => e # record, then preserve the original body exception
        @request.finish(e)
        raise
      ensure
        @request.finish
      end

      def method_missing(name, *args, **kwargs, &block)
        @body.public_send(name, *args, **kwargs, &block)
      rescue Exception => e
        @request.finish(e) if %i[each call].include?(name)
        raise
      end

      def respond_to_missing?(name, include_private = false)
        @body.respond_to?(name, include_private) || super
      end
    end

    class Middleware
      def initialize(app, tracer_middleware, *args)
        @app = tracer_middleware.new(app, *args)
        @config = OpenTelemetry::Instrumentation::Rack::Instrumentation.instance.config
      end

      def call(env)
        # Nested Rack middleware and repeated installations share one request
        # measurement. Keep the sentinel for the response body's full lifetime.
        return @app.call(env) if env.key?(STATE_KEY)

        begin
          request = Request.new(env) if RackMetrics.enabled?(@config)
        rescue StandardError => e
          RackMetrics.report(e)
        end
        return @app.call(env) unless request

        env[STATE_KEY] = request
        begin
          status, headers, body = @app.call(env)
          request.status = status
          [status, headers, Body.new(body, request)]
        rescue Exception => e
          request.finish(e)
          raise
        end
      end
    end

    class PassThrough
      def initialize(app)
        @app = app
      end

      def call(env)
        @app.call(env)
      end
    end

    module MiddlewareArgs
      def middleware_args
        # Sinatra requests Rack middleware even when Rack instrumentation was
        # explicitly disabled. Avoid constructing its uninstalled tracer.
        return [PassThrough] unless installed && enabled?

        args = super
        return args unless config[:send_metrics]

        [Middleware, *args]
      end
    end
  end
end

rack_instrumentation = OpenTelemetry::Instrumentation::Rack::Instrumentation
rack_instrumentation.option(:send_metrics, default: true, validate: :boolean)
rack_instrumentation.prepend(Rio::RackMetrics::MiddlewareArgs)
