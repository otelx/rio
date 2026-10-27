FROM --platform=$BUILDPLATFORM golang:1.27.3-bookworm@sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5 AS build
WORKDIR /source
COPY go.mod go.sum ./
RUN test "$(go env GOVERSION)" = "go$(awk '$1 == "go" {print $2}' go.mod)"
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /operator ./cmd/operator
FROM scratch
LABEL org.opencontainers.image.source="https://github.com/otelx/rio" \
      org.opencontainers.image.title="RIO operator" \
      org.opencontainers.image.description="Kubernetes operator for automatic runtime instrumentation and managed rollouts" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=build /operator /operator
COPY LICENSE NOTICE /licenses/
USER 65532:65532
ENTRYPOINT ["/operator"]
