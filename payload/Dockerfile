ARG RUNTIME_IMAGE=alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a
FROM ${RUNTIME_IMAGE} AS runtime-base

FROM python:3.15.0-slim-trixie@sha256:f5b92f2a669a4c347044f29367f31983280b648250ca94dea66f98c53bc87e58 AS downloads
ARG TARGETARCH
WORKDIR /build
COPY versions.json scripts/download.py ./
RUN python download.py versions.json "$TARGETARCH" /payload
COPY scripts/check_managed_notices.py ./
COPY licenses/dotnet/managed/ /managed-notices/
RUN python check_managed_notices.py /payload/dotnet /managed-notices versions.json

FROM downloads AS alpine-sources
ARG RUNTIME_IMAGE
COPY --from=runtime-base /lib/apk/db/installed /installed-packages
COPY alpine-sources.lock.json scripts/alpine_sources.py ./
RUN python alpine_sources.py /installed-packages alpine-sources.lock.json /alpine-source-inputs.tar.gz --base-image "$RUNTIME_IMAGE"

# Supply the net8.0 reference packs and test runtime without NuGet downloads.
FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:8.0.427-bookworm-slim@sha256:ff39a765f04619c8d146796fc09b38499ae5984f4a2a4c3a0a57d0022855daee AS dotnet-net8

FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:9.0.320-bookworm-slim@sha256:7e3a4bd66701a451823db2028154ae16ff191bc452596ba618ef4d0c58f9cd01 AS dotnet-propagation
COPY --from=dotnet-net8 /usr/share/dotnet/packs/ /usr/share/dotnet/packs/
COPY --from=dotnet-net8 /usr/share/dotnet/shared/ /usr/share/dotnet/shared/
WORKDIR /source
COPY --from=downloads /payload/dotnet/glibc/net /agent/net
COPY dotnet/Rio.Propagation.csproj dotnet/PropagationPlugin.cs dotnet/NuGet.Config ./
COPY dotnet/bootstrap/ ./bootstrap/
COPY dotnet/tests/ ./tests/
RUN dotnet publish Rio.Propagation.csproj --configuration Release --output /out/plugin -p:AgentDirectory=/agent/net \
    && dotnet publish bootstrap/Rio.Bootstrap.csproj --configuration Release --output /out/bootstrap \
    && dotnet publish tests/throwing-hook/ThrowingHook.csproj --configuration Release --output /out/test-hook \
    && dotnet run --project tests/Rio.Bootstrap.Tests.csproj --configuration Release -- /out/bootstrap/Rio.Bootstrap.dll /out/test-hook/Rio.Bootstrap.ThrowingHook.dll

FROM downloads AS dotnet-inventory
COPY --from=dotnet-propagation /out/ /generated/
COPY dotnet/ /sources/dotnet/
COPY Dockerfile /sources/Dockerfile
COPY scripts/dotnet_inventory.py /sources/scripts/dotnet_inventory.py
RUN python /sources/scripts/dotnet_inventory.py /sources /generated /build/versions.json /rio-dotnet-inventory.json

FROM node:25@sha256:78839ac448c23517f8eab2e8f7943d9b4f73979eb7f8bed2c73dbf72ff869e7b AS node
WORKDIR /build
COPY node/package.json node/package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts && npm cache clean --force

FROM python:3.15.0-trixie@sha256:d5ae371dc36dd95f3bb453fb64ca03377ba455d3351339f16be6c47aa10177b9 AS python-glibc
WORKDIR /build
COPY python/requirements.lock ./
RUN mkdir -p /build/tmp && TMPDIR=/build/tmp pip install --no-cache-dir --require-hashes --no-compile --target /payload -r requirements.lock
COPY python/patch_trace_flags.py python/test_trace_flags.py ./
RUN python patch_trace_flags.py /payload && PYTHONPATH=/payload OTEL_SDK_DISABLED=true python test_trace_flags.py
COPY python/patch_aiohttp_context.py ./
RUN python patch_aiohttp_context.py /payload
COPY python/sitecustomize.py /payload/

FROM python:3.15.0-alpine3.23@sha256:d64bc17e0dff27c5c4a31f616450d032aa73d6325855127ce3a305bf1384c8d5 AS python-musl
WORKDIR /build
COPY python/requirements.lock ./
RUN mkdir -p /build/tmp && TMPDIR=/build/tmp pip install --no-cache-dir --require-hashes --no-compile --target /payload -r requirements.lock
COPY python/patch_trace_flags.py python/test_trace_flags.py ./
RUN python patch_trace_flags.py /payload && PYTHONPATH=/payload OTEL_SDK_DISABLED=true python test_trace_flags.py
COPY python/patch_aiohttp_context.py ./
RUN python patch_aiohttp_context.py /payload
COPY python/sitecustomize.py /payload/

FROM ruby:3.3.12-slim-bookworm@sha256:74f681701c1e7e862d5f143e4a70bdb8b3ed08b2150c6209f54d5972feef517d AS ruby-glibc
WORKDIR /build
RUN apt-get update && apt-get install -y --no-install-recommends git build-essential && rm -rf /var/lib/apt/lists/*
COPY ruby/Gemfile ruby/Gemfile.lock ./
ENV BUNDLE_PATH=/build/bundle BUNDLE_FROZEN=true BUNDLE_APP_CONFIG=/build/.bundle TMPDIR=/build/tmp
RUN mkdir -p "$TMPDIR" && bundle install && cp -R /build/bundle/ruby/3.3.0 /payload
COPY ruby/opentelemetry-auto-instrumentation.rb ruby/rio-rack-metrics.rb /payload/
COPY ruby/tests/ /build/tests/
RUN RIO_METRICS_EXTENSION=/payload/rio-rack-metrics.rb bundle exec ruby /build/tests/rack_metrics_test.rb

FROM ruby:3.3.13-alpine3.23@sha256:6d55efd6b3b3e1352d496143e83f5fc732b2beb4a8a5a1e329e9d97325b2dfb3 AS ruby-musl
WORKDIR /build
RUN apk add --no-cache git build-base
COPY ruby/Gemfile ruby/Gemfile.lock ./
ENV BUNDLE_PATH=/build/bundle BUNDLE_FROZEN=true BUNDLE_APP_CONFIG=/build/.bundle TMPDIR=/build/tmp
RUN mkdir -p "$TMPDIR" && bundle install && cp -R /build/bundle/ruby/3.3.0 /payload
COPY ruby/opentelemetry-auto-instrumentation.rb ruby/rio-rack-metrics.rb /payload/
COPY ruby/tests/ /build/tests/
RUN RIO_METRICS_EXTENSION=/payload/rio-rack-metrics.rb bundle exec ruby /build/tests/rack_metrics_test.rb

FROM runtime-base
LABEL org.opencontainers.image.source="https://github.com/otelx/rio" \
      org.opencontainers.image.title="RIO instrumentation" \
      org.opencontainers.image.description="OpenTelemetry Injector and language agents for automatic runtime instrumentation" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=downloads /payload/libotelinject.so /dist/injector/libotelinject.so
COPY otelinject.conf /dist/injector/otelinject.conf
COPY --from=downloads /payload/opentelemetry-javaagent.jar /dist/agents/java/opentelemetry-javaagent.jar
COPY --from=downloads /payload/dotnet /dist/agents/dotnet
RUN mv /dist/agents/dotnet/glibc/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll /dist/agents/dotnet/glibc/net/OpenTelemetry.AutoInstrumentation.StartupHook.original.dll \
    && mv /dist/agents/dotnet/musl/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll /dist/agents/dotnet/musl/net/OpenTelemetry.AutoInstrumentation.StartupHook.original.dll
COPY --from=dotnet-propagation /out/plugin/Rio.Propagation.dll /dist/agents/dotnet/glibc/net/Rio.Propagation.dll
COPY --from=dotnet-propagation /out/plugin/Rio.Propagation.dll /dist/agents/dotnet/musl/net/Rio.Propagation.dll
COPY --from=dotnet-propagation /out/bootstrap/Rio.Bootstrap.dll /dist/agents/dotnet/glibc/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll
COPY --from=dotnet-propagation /out/bootstrap/Rio.Bootstrap.dll /dist/agents/dotnet/musl/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll
COPY --from=dotnet-inventory /rio-dotnet-inventory.json /dist/agents/dotnet/rio-inventory.json
COPY --from=node /build/node_modules /dist/agents/node/node_modules
COPY --from=python-glibc /payload /dist/agents/python/glibc
COPY --from=python-musl /payload /dist/agents/python/musl
COPY --from=ruby-glibc /payload /dist/agents/ruby/glibc
COPY --from=ruby-musl /payload /dist/agents/ruby/musl
COPY versions.json /dist/versions.json
COPY LICENSE NOTICE /licenses/
COPY licenses/ /licenses/upstream/
COPY --from=alpine-sources /alpine-source-inputs.tar.gz /licenses/sources/alpine-source-inputs.tar.gz
COPY copy-to-volume.sh verify-layout.sh /
# COPY preserves the checkout's umask-derived modes. All application users need
# to traverse agent directories and read the Injector configuration and agents.
RUN find /dist -type d ! -perm -005 -exec chmod a+rx {} + \
    && find /dist -type f ! -perm -004 -exec chmod a+r {} + \
    && chmod a+rx /copy-to-volume.sh /verify-layout.sh && /verify-layout.sh
CMD ["/copy-to-volume.sh"]
