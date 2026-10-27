FROM --platform=$BUILDPLATFORM golang:1.26.1-bookworm AS build
WORKDIR /source
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY LICENSE NOTICE ./
ARG TARGETOS
ARG TARGETARCH
# Collect the compiled dependency graph's redistribution material as well as the
# Go runtime license. These files travel with the exact binary being tested.
RUN go install github.com/google/go-licenses/v2@v2.0.1 \
    && GOOS=$TARGETOS GOARCH=$TARGETARCH go-licenses save ./cmd/operator --save_path=/third-party \
    && cp /usr/local/go/LICENSE /third-party/GO-LICENSE
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /operator ./cmd/operator
FROM scratch
LABEL org.opencontainers.image.source="https://github.com/otelx/rio" \
      org.opencontainers.image.title="RIO operator" \
      org.opencontainers.image.description="Kubernetes operator for automatic runtime instrumentation and managed rollouts" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=build /operator /operator
COPY LICENSE NOTICE /licenses/
COPY --from=build /third-party /licenses/go/
USER 65532:65532
ENTRYPOINT ["/operator"]
