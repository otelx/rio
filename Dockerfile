FROM golang:1.26.1-bookworm AS build
WORKDIR /source
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /operator ./cmd/operator
FROM scratch
LABEL org.opencontainers.image.source="https://github.com/otelx/rio" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=build /operator /operator
COPY LICENSE NOTICE /licenses/
USER 65532:65532
ENTRYPOINT ["/operator"]
