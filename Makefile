ROOT := $(CURDIR)
ARTIFACTS := $(ROOT)/.artifacts
export GOCACHE := $(ARTIFACTS)/go-cache
export GOMODCACHE := $(ARTIFACTS)/go-mod-cache
export GOPATH := $(ARTIFACTS)/go
export GOTMPDIR := $(ARTIFACTS)/tmp
export TMPDIR := $(ARTIFACTS)/tmp
OPERATOR_IMAGE ?= ghcr.io/otelx/rio:dev
PAYLOAD_IMAGE ?= ghcr.io/otelx/rio-payload:dev
export OPERATOR_IMAGE PAYLOAD_IMAGE

.PHONY: prepare test build vet image payload images smoke e2e install uninstall
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
payload: prepare
	$(MAKE) -C payload image IMAGE="$(PAYLOAD_IMAGE)"
images: image payload
smoke:
	./payload/scripts/smoke.sh
e2e:
	./scripts/e2e.sh
install:
	./scripts/install.sh "$(OPERATOR_IMAGE)"
uninstall:
	./scripts/uninstall.sh
