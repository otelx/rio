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
platform=${PLATFORM:-$(docker version --format '{{.Server.Os}}/{{.Server.Arch}}')}
case "$platform" in
  linux/amd64|linux/arm64) ;;
  *) printf 'Unsupported test platform: %q\n' "$platform" >&2; exit 2 ;;
esac
arch=${platform#linux/}
artifacts="$root/.artifacts/image-smoke/$arch"
mkdir -p "$artifacts/payload" "$artifacts/tmp"
export TMPDIR="$artifacts/tmp"
payload=${PAYLOAD_IMAGE:-ghcr.io/otelx/rio/instrumentation:dev}
mount=/__otel_sdk_auto_instrumentation__
docker run --rm --platform "$platform" "$payload" /verify-layout.sh
docker run --rm --platform "$platform" --mount "type=bind,src=$artifacts/payload,dst=$mount" "$payload" /copy-to-volume.sh
common=(--rm --platform "$platform" --mount "type=bind,src=$artifacts/payload,dst=$mount,readonly"
  -e "LD_PRELOAD=$mount/dist/injector/libotelinject.so"
  -e "OTEL_INJECTOR_CONFIG_FILE=$mount/dist/injector/otelinject.conf"
  -e OTEL_TRACES_EXPORTER=none -e OTEL_METRICS_EXPORTER=none -e OTEL_LOGS_EXPORTER=none
  -e OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf)
for libc in glibc musl; do
  logs="$artifacts/$libc"
  mkdir -p "$logs"
  if [[ "$libc" == glibc ]]; then
    java=${JAVA_IMAGE:-eclipse-temurin:21-jre}
    node=${NODE_IMAGE:-node:24}
    python=${PYTHON_IMAGE:-python:3.14.6-trixie}
    ruby=${RUBY_IMAGE:-ruby:3.3.12-slim-bookworm}
    dotnet=${DOTNET_IMAGE:-mcr.microsoft.com/dotnet/aspnet:8.0}
  else
    java=${JAVA_MUSL_IMAGE:-eclipse-temurin:21-jre-alpine}
    node=${NODE_MUSL_IMAGE:-node:24-alpine}
    python=${PYTHON_MUSL_IMAGE:-python:3.14.6-alpine3.23}
    ruby=${RUBY_MUSL_IMAGE:-ruby:3.3.12-alpine3.23}
    dotnet=${DOTNET_MUSL_IMAGE:-mcr.microsoft.com/dotnet/aspnet:8.0-alpine}
  fi
  printf 'Checking all five language loaders on %s (%s)\n' "$platform" "$libc"
  docker run "${common[@]}" "$java" java -version > "$logs/java.log" 2>&1
  grep -q 'opentelemetry-javaagent.*version: 2.33.1' "$logs/java.log"
  docker run "${common[@]}" "$node" node -e 'if (!Object.keys(require.cache).some(p => p.endsWith("auto-instrumentations-node/dist/register.cjs"))) throw new Error("Node agent not loaded"); console.log("Node.js Injector activated")' > "$logs/node.log" 2>&1
  docker run "${common[@]}" "$python" python -c 'import sys; assert "opentelemetry.instrumentation.auto_instrumentation" in sys.modules; from opentelemetry import trace; assert type(trace.get_tracer_provider()).__name__ == "TracerProvider"; print("Python Injector activated")' > "$logs/python.log" 2>&1
  docker run "${common[@]}" "$ruby" ruby -e 'raise "Ruby agent not loaded" unless defined?(OpenTelemetry::SDK); puts "Ruby Injector activated"' > "$logs/ruby.log" 2>&1
  fixture="${DOTNET_SMOKE_IMAGE:-rio-dotnet-smoke}:$arch-$libc"
  docker build --platform "$platform" --build-arg "RUNTIME_IMAGE=$dotnet" -t "$fixture" "$component/tests/dotnet" > "$logs/dotnet-build.log" 2>&1
  docker run "${common[@]}" "$fixture" > "$logs/dotnet.log" 2>&1
done
printf 'All five language agents activated on %s with glibc and musl.\n' "$platform"
