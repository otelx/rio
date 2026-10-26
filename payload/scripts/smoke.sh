#!/usr/bin/env bash
# Startup/loader checks, not a replacement for application telemetry tests.
set -Eeuo pipefail
component=$(cd -- "$(dirname -- "$0")/.." && pwd)
root=$(cd -- "$component/.." && pwd)
if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  printf 'Usage: payload/scripts/smoke.sh (configure images through environment variables)\n'
  exit 0
fi
[[ $# -eq 0 ]] || { printf 'Unexpected arguments\n' >&2; exit 2; }
for required in docker grep; do
  command -v "$required" >/dev/null || { printf 'Missing command: %s\n' "$required" >&2; exit 1; }
done
trap 'printf "RIO loader test failed at line %s\n" "$LINENO" >&2' ERR
artifacts="$root/.artifacts/image-smoke"
mkdir -p "$artifacts/payload" "$artifacts/tmp"
export TMPDIR="$artifacts/tmp"
payload=${PAYLOAD_IMAGE:-ghcr.io/otelx/rio-payload:dev}
mount=/__otel_sdk_auto_instrumentation__
docker run --rm "$payload" /verify-layout.sh
docker run --rm --mount "type=bind,src=$artifacts/payload,dst=$mount" "$payload" /copy-to-volume.sh
common=(--rm --mount "type=bind,src=$artifacts/payload,dst=$mount,readonly"
  -e "LD_PRELOAD=$mount/dist/injector/libotelinject.so"
  -e "OTEL_INJECTOR_CONFIG_FILE=$mount/dist/injector/otelinject.conf"
  -e OTEL_TRACES_EXPORTER=none -e OTEL_METRICS_EXPORTER=none -e OTEL_LOGS_EXPORTER=none
  -e OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf)
docker run "${common[@]}" "${JAVA_IMAGE:-eclipse-temurin:21-jre}" java -version > "$artifacts/java.log" 2>&1
grep -q 'opentelemetry-javaagent.*version: 2.30.0' "$artifacts/java.log"
docker run "${common[@]}" "${NODE_IMAGE:-node:24}" node -e 'if (!Object.keys(require.cache).some(p => p.endsWith("auto-instrumentations-node/build/src/register.js"))) throw new Error("Node agent not loaded"); console.log("Node.js Injector activated")' > "$artifacts/node.log" 2>&1
docker run "${common[@]}" "${PYTHON_IMAGE:-python:3.14.6-trixie}" python -c 'import sys; assert "opentelemetry.instrumentation.auto_instrumentation" in sys.modules; from opentelemetry import trace; assert type(trace.get_tracer_provider()).__name__ == "TracerProvider"; print("Python Injector activated")' > "$artifacts/python.log" 2>&1
docker run "${common[@]}" "${RUBY_IMAGE:-ruby:3.3.12-slim-bookworm}" ruby -e 'raise "Ruby agent not loaded" unless defined?(OpenTelemetry::SDK); puts "Ruby Injector activated"' > "$artifacts/ruby.log" 2>&1
docker build -t rio-dotnet-smoke:dev "$component/tests/dotnet" > "$artifacts/dotnet-build.log" 2>&1
docker run "${common[@]}" rio-dotnet-smoke:dev > "$artifacts/dotnet.log" 2>&1
echo 'All five language agents activated through the Injector.'
