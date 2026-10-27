# frozen_string_literal: true

require 'bundler/setup'
Bundler.require
require 'json'
require 'uri'

$stdout.sync = true

class Fixture < Sinatra::Base
  set :environment, :production
  set :host_authorization, permitted_hosts: []
  LOGGER = Logger.new($stdout)
  LOGGER.formatter = proc { |_severity, _datetime, _progname, message| "#{message}\n" }
  DELAY_MS = [[ENV.fetch('RIO_FIXTURE_DELAY_MS', '0').to_i, 0].max, 1000].min

  get '/health' do
    content_type :json
    JSON.generate(status: 'ok', language: 'ruby')
  end

  get '/work/:marker' do
    marker = params.fetch('marker')
    sleep(DELAY_MS / 1000.0) if DELAY_MS.positive?
    LOGGER.info("rio-fixture #{marker}")

    # The pinned Rackup WEBrick Input retains the received request. Read its
    # raw header arrays because Rack's standard env joins duplicate values.
    received = env.fetch('rack.input').instance_variable_get(:@request)
    headers = received.header.transform_values(&:dup)
    body = { marker: marker, language: 'ruby', headers: headers }

    unless params.fetch('next', '').empty?
      uri = URI(params.fetch('next'))
      client = Net::HTTP.new(uri.host, uri.port)
      client.use_ssl = uri.scheme == 'https'
      client.open_timeout = 3
      client.read_timeout = 10
      # Normal client only: RIO's agent must inject outgoing propagation headers.
      response = client.get(uri.request_uri)
      halt 502, 'downstream request failed' unless response.is_a?(Net::HTTPSuccess)
      body[:downstream] = JSON.parse(response.body)
    end

    content_type :json
    JSON.generate(body)
  end
end

Rackup::Handler::WEBrick.run(
  Fixture, Host: '0.0.0.0', Port: 8080, AccessLog: [],
  Logger: Logger.new($stderr, level: Logger::WARN)
)
