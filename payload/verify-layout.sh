#!/bin/sh
set -eu
root=${1:-/dist}
test -s "$root/versions.json"
test -s "$root/injector/libotelinject.so"
test -s "$root/injector/otelinject.conf"
test -r "$root/injector/otelinject.conf"
test -r "$root/injector/libotelinject.so"
test -s "$root/agents/java/opentelemetry-javaagent.jar"
test -s "$root/agents/node/node_modules/@opentelemetry/auto-instrumentations-node/build/src/register.js"
test -s "$root/agents/node/node_modules/@opentelemetry/winston-transport/package.json"
for libc in glibc musl; do
  test -s "$root/agents/python/$libc/sitecustomize.py"
  test -d "$root/agents/python/$libc/opentelemetry/sdk"
  test -s "$root/agents/ruby/$libc/opentelemetry-auto-instrumentation.rb"
  test -d "$root/agents/ruby/$libc/gems"
  test -s "$root/agents/dotnet/$libc/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll"
done
echo 'Injector and all five agent payloads are present.'
