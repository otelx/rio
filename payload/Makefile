ROOT := $(abspath ..)
ARTIFACTS := $(ROOT)/.artifacts
IMAGE ?= ghcr.io/otelx/rio/instrumentation:dev
PLATFORMS ?= linux/amd64,linux/arm64
export TMPDIR := $(ARTIFACTS)/tmp
export UV_CACHE_DIR := $(ARTIFACTS)/uv-cache
export npm_config_cache := $(ARTIFACTS)/npm-cache

.PHONY: image archive publish test lock prepare
image: prepare
	docker build -t "$(IMAGE)" .
archive: prepare
	mkdir -p "$(ARTIFACTS)/images"
	docker buildx build --platform "$(PLATFORMS)" -t "$(IMAGE)" --output "type=oci,dest=$(ARTIFACTS)/images/payload.oci.tar" .
publish: prepare
	docker buildx build --platform "$(PLATFORMS)" -t "$(IMAGE)" --push .
test: prepare
	docker run --rm "$(IMAGE)" /verify-layout.sh
lock: prepare
	npm --prefix node install --package-lock-only --ignore-scripts
	uv pip compile python/requirements.in --python-version 3.14 --generate-hashes -o python/requirements.lock
prepare:
	mkdir -p "$(TMPDIR)" "$(UV_CACHE_DIR)" "$(npm_config_cache)"
