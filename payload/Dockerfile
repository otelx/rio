ARG RUNTIME_IMAGE=alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a
FROM ${RUNTIME_IMAGE} AS runtime-base

FROM python:3.14.6-slim-trixie@sha256:7bec7ddcddeff7975d6ba9b4be7dd6f6b2f55e7491539145e2978f7f97ce9144 AS downloads
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

FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:8.0.427-bookworm-slim@sha256:ff39a765f04619c8d146796fc09b38499ae5984f4a2a4c3a0a57d0022855daee AS dotnet-propagation
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

FROM python:3.14.6-trixie@sha256:55dd7c1ea6c2b2c45a26d797187706eb9e5e0046f8e54fd759e38647b505c36b AS python-glibc
WORKDIR /build
COPY python/requirements.lock ./
RUN mkdir -p /build/tmp && TMPDIR=/build/tmp pip install --no-cache-dir --require-hashes --no-compile --target /payload -r requirements.lock
COPY python/patch_trace_flags.py python/test_trace_flags.py ./
RUN python patch_trace_flags.py /payload && PYTHONPATH=/payload OTEL_SDK_DISABLED=true python test_trace_flags.py
COPY python/patch_aiohttp_context.py ./
RUN python patch_aiohttp_context.py /payload
COPY python/sitecustomize.py /payload/

FROM python:3.14.6-alpine3.23@sha256:b165067c5afc37fa5608a3c05609cc3d51aafd808a30fbfd822ee594fef55ad4 AS python-musl
WORKDIR /build
COPY python/requirements.lock ./
RUN mkdir -p /build/tmp && TMPDIR=/build/tmp pip install --no-cache-dir --require-hashes --no-compile --target /payload -r requirements.lock
COPY python/patch_trace_flags.py python/test_trace_flags.py ./
RUN python patch_trace_flags.py /payload && PYTHONPATH=/payload OTEL_SDK_DISABLED=true python test_trace_flags.py
COPY python/patch_aiohttp_context.py ./
RUN python patch_aiohttp_context.py /payload
COPY python/sitecustomize.py /payload/

FROM ruby:3.3.12-slim-bookworm@sha256:df209d82bb55aeb26b9b08922ee22c15f19b3458455690132dc07a5c52dc1c99 AS ruby-glibc
WORKDIR /build
RUN apt-get update && apt-get install -y --no-install-recommends git build-essential && rm -rf /var/lib/apt/lists/*
COPY ruby/Gemfile ruby/Gemfile.lock ./
ENV BUNDLE_PATH=/build/bundle BUNDLE_FROZEN=true BUNDLE_APP_CONFIG=/build/.bundle TMPDIR=/build/tmp
RUN mkdir -p "$TMPDIR" && bundle install && cp -R /build/bundle/ruby/3.3.0 /payload
COPY ruby/opentelemetry-auto-instrumentation.rb ruby/rio-rack-metrics.rb /payload/
COPY ruby/tests/ /build/tests/
RUN RIO_METRICS_EXTENSION=/payload/rio-rack-metrics.rb bundle exec ruby /build/tests/rack_metrics_test.rb

FROM ruby:3.3.12-alpine3.23@sha256:11da101dfad607c6193a921abc815c989bc9f19b43f5f686bbcc7d424298d596 AS ruby-musl
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
