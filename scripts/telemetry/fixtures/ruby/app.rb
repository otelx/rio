# frozen_string_literal: true

require 'bundler/setup'
Bundler.require
require 'json'
require 'uri'

class Fixture < Sinatra::Base
  set :environment, :production
  set :host_authorization, permitted_hosts: []
  LOGGER = Logger.new($stdout)

  get '/health' do
    content_type :json
    JSON.generate(status: 'ok', language: 'ruby')
  end

  get '/work/:marker' do
    marker = params.fetch('marker')
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
