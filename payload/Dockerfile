# syntax=docker/dockerfile:1
FROM python:3.14.6-slim-trixie AS downloads
ARG TARGETARCH
WORKDIR /build
COPY versions.json scripts/download.py ./
RUN python download.py versions.json "$TARGETARCH" /payload

FROM node:24@sha256:934240a162082fd8b8a2f90cd5114446443f1eba1c5378f6687167ca405e6584 AS node
WORKDIR /build
COPY node/package.json node/package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts && npm cache clean --force

FROM python:3.14.6-trixie@sha256:55dd7c1ea6c2b2c45a26d797187706eb9e5e0046f8e54fd759e38647b505c36b AS python-glibc
WORKDIR /build
COPY python/requirements.lock ./
RUN mkdir -p /build/tmp && TMPDIR=/build/tmp pip install --no-cache-dir --require-hashes --no-compile --target /payload -r requirements.lock
COPY python/sitecustomize.py /payload/

FROM python:3.14.6-alpine3.23@sha256:b165067c5afc37fa5608a3c05609cc3d51aafd808a30fbfd822ee594fef55ad4 AS python-musl
WORKDIR /build
COPY python/requirements.lock ./
RUN mkdir -p /build/tmp && TMPDIR=/build/tmp pip install --no-cache-dir --require-hashes --no-compile --target /payload -r requirements.lock
COPY python/sitecustomize.py /payload/

FROM ruby:3.3.12-slim-bookworm@sha256:df209d82bb55aeb26b9b08922ee22c15f19b3458455690132dc07a5c52dc1c99 AS ruby-glibc
WORKDIR /build
RUN apt-get update && apt-get install -y --no-install-recommends git build-essential && rm -rf /var/lib/apt/lists/*
COPY ruby/Gemfile ruby/Gemfile.lock ./
ENV BUNDLE_PATH=/build/bundle BUNDLE_FROZEN=true BUNDLE_APP_CONFIG=/build/.bundle TMPDIR=/build/tmp
RUN mkdir -p "$TMPDIR" && bundle install && cp -R /build/bundle/ruby/3.3.0 /payload
COPY ruby/opentelemetry-auto-instrumentation.rb /payload/

FROM ruby:3.3.12-alpine3.23@sha256:11da101dfad607c6193a921abc815c989bc9f19b43f5f686bbcc7d424298d596 AS ruby-musl
WORKDIR /build
RUN apk add --no-cache git build-base
COPY ruby/Gemfile ruby/Gemfile.lock ./
ENV BUNDLE_PATH=/build/bundle BUNDLE_FROZEN=true BUNDLE_APP_CONFIG=/build/.bundle TMPDIR=/build/tmp
RUN mkdir -p "$TMPDIR" && bundle install && cp -R /build/bundle/ruby/3.3.0 /payload
COPY ruby/opentelemetry-auto-instrumentation.rb /payload/

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
LABEL org.opencontainers.image.source="https://github.com/otelx/rio" \
      org.opencontainers.image.title="RIO instrumentation" \
      org.opencontainers.image.description="OpenTelemetry Injector and language agents for automatic runtime instrumentation" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=downloads /payload/libotelinject.so /dist/injector/libotelinject.so
COPY otelinject.conf /dist/injector/otelinject.conf
COPY --from=downloads /payload/opentelemetry-javaagent.jar /dist/agents/java/opentelemetry-javaagent.jar
COPY --from=downloads /payload/dotnet /dist/agents/dotnet
COPY --from=node /build/node_modules /dist/agents/node/node_modules
COPY --from=python-glibc /payload /dist/agents/python/glibc
COPY --from=python-musl /payload /dist/agents/python/musl
COPY --from=ruby-glibc /payload /dist/agents/ruby/glibc
COPY --from=ruby-musl /payload /dist/agents/ruby/musl
COPY versions.json /dist/versions.json
COPY LICENSE NOTICE /licenses/
COPY copy-to-volume.sh verify-layout.sh /
# COPY preserves the checkout's umask-derived modes. All application users need
# to traverse agent directories and read the Injector configuration and agents.
RUN find /dist -type d ! -perm -005 -exec chmod a+rx {} + \
    && find /dist -type f ! -perm -004 -exec chmod a+r {} + \
    && chmod a+rx /copy-to-volume.sh /verify-layout.sh && /verify-layout.sh
CMD ["/copy-to-volume.sh"]
