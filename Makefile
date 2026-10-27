ROOT := $(CURDIR)
ARTIFACTS := $(ROOT)/.artifacts
export GOCACHE := $(ARTIFACTS)/go-cache
export GOMODCACHE := $(ARTIFACTS)/go-mod-cache
export GOPATH := $(ARTIFACTS)/go
export GOTMPDIR := $(ARTIFACTS)/tmp
export TMPDIR := $(ARTIFACTS)/tmp
OPERATOR_IMAGE ?= ghcr.io/otelx/rio/operator:dev
PAYLOAD_IMAGE ?= ghcr.io/otelx/rio/instrumentation:dev
PLATFORMS ?= linux/amd64,linux/arm64
export OPERATOR_IMAGE PAYLOAD_IMAGE

.PHONY: prepare test build vet image archive payload images smoke e2e e2e-upgrade admission-e2e renew-certificates install uninstall
prepare:
	mkdir -p "$(GOCACHE)" "$(GOMODCACHE)" "$(GOTMPDIR)" "$(ARTIFACTS)/bin"
test: prepare
	go test -race ./...
vet: prepare
	go vet ./...
build: prepare
	CGO_ENABLED=0 go build -trimpath -o "$(ARTIFACTS)/bin/rio" ./cmd/operator
image: prepare
	docker build -t "$(OPERATOR_IMAGE)" .
archive: prepare
	mkdir -p "$(ARTIFACTS)/images"
	docker buildx build --platform "$(PLATFORMS)" -t "$(OPERATOR_IMAGE)" --output "type=oci,dest=$(ARTIFACTS)/images/operator.oci.tar" .
	$(MAKE) -C payload archive PLATFORMS="$(PLATFORMS)" IMAGE="$(PAYLOAD_IMAGE)"
payload: prepare
	$(MAKE) -C payload image IMAGE="$(PAYLOAD_IMAGE)"
images: image payload
smoke:
	./payload/scripts/smoke.sh
admission-e2e: prepare
	./scripts/admission-e2e.sh
e2e:
	./scripts/e2e.sh
e2e-upgrade: OPERATOR_IMAGE = ghcr.io/otelx/rio/operator:issue-6-candidate
e2e-upgrade: PAYLOAD_IMAGE = ghcr.io/otelx/rio/instrumentation:issue-6-candidate
e2e-upgrade:
	./scripts/e2e-upgrade.sh
renew-certificates:
	./scripts/renew-certificates.sh
install:
	./scripts/install.sh "$(OPERATOR_IMAGE)"
uninstall:
	./scripts/uninstall.sh
