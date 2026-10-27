# Development

## Repository layout

```text
cmd/operator/          Operator entry point
internal/operator/     Policy, admission, capabilities, rollout, and recovery
config/                CRD, installation template, and policy example
payload/               Upstream Injector and five language-agent packages
examples/quickstart/   Small application, Collector, and namespace policy
scripts/               Installation, removal, and lifecycle integration checks
.artifacts/            Ignored binaries, caches, certificates, logs, and evidence
```

Go 1.25+, Docker with Buildx, Bash, kubectl, kind, Python 3, OpenSSL, and ShellCheck are used by the development workflow. The CI Go version follows `go.mod`. Build caches and working files stay under `.artifacts/`.

## Build and test

```bash
make test vet build
make admission-e2e
make images
make smoke
EXPECT_DELIVERY=imageVolume make e2e
CLUSTER_NAME=rio-fallback IMAGE_VOLUME_ENABLED=false EXPECT_DELIVERY=initContainer make e2e
```

Run clusters sequentially on a local machine. The integration script creates its own kind cluster and private kubeconfig, refuses to replace an existing cluster, and preserves failed runs for inspection. Successful runs remove their cluster. `KEEP_CLUSTER=true` preserves a successful run. To rerun a retained cluster, use `REUSE_CLUSTER=true` and its original `CLUSTER_NAME`; the saved private kubeconfig is required.

The admission suite creates a separate disposable kind cluster and runs the real TLS webhook against Kubernetes Pod creation requests. It covers startup, invalid policy, synchronization failure, missing owners, malformed workload state alongside a healthy selected workload, system namespace and critical-priority matching exclusions, explicit rejection with `failurePolicy: Ignore`, and transport failure with `Ignore`. It needs no payload images. Its host webhook endpoint uses the Linux Docker bridge gateway; run it on a Linux Docker host. Evidence is saved under `.artifacts/admission/`.

The lifecycle suite covers injection and removal on Deployments, StatefulSets, and DaemonSets, failed configuration, recovery, operator restart, correction, and real HTTP trace export from the quickstart service. Loader checks cover all five agents; they disable exporters and are distinct from telemetry verification.

CI builds each architecture once and passes the same image archives to both delivery tests and release publication. Integration checks use two-node kind clusters on native amd64 and arm64 workers. Loader checks exercise all five agents with both glibc and musl, and integration checks verify Node.js HTTP export from both Debian and Alpine images. On a machine with arm64 emulation registered, `PLATFORM=linux/arm64 make smoke` selects the arm64 payload and fixtures explicitly. Logs are separated by architecture and libc under `.artifacts/image-smoke/`. A separate CI gate assembles multi-platform tags in a disposable registry and executes the controller and Injector on both architectures through those common tags before permitting a release.

Build both controller and payload images as multi-platform OCI archives with `make archive`. `PLATFORMS` defaults to `linux/amd64,linux/arm64`; the builder must be able to execute the payload's native dependency installation for both architectures. The controller uses Go cross-compilation. Local `make images` still builds for the current worker.

Image defaults are `ghcr.io/otelx/rio/operator:dev` and `ghcr.io/otelx/rio/instrumentation:dev`. Override them with `OPERATOR_IMAGE` and `PAYLOAD_IMAGE`, respectively. The instrumentation build context is `payload/`.

For custom multi-node clusters, set `KIND_CONFIG` to a kind configuration file. Set `LOAD_IMAGES=false` when image references are available from a registry instead of the local Docker store, and use `EXPECT_ARCHITECTURES=amd64,arm64` to require both node types. The integration harness checks a DaemonSet on every node and tests HTTP export from both libc variants on each architecture. A custom configuration supplies its own node images and feature gates.

For a disposable cluster you created yourself, load local images before installing:

```bash
kind load docker-image --name MY_CLUSTER ghcr.io/otelx/rio/operator:dev ghcr.io/otelx/rio/instrumentation:dev
make install
```

The installer uses your current kubectl context. For a local quickstart, change `examples/quickstart/policy.yaml` to reference `ghcr.io/otelx/rio/instrumentation:dev`, or use the integration script, which renders that reference automatically.

## Update payload inputs

Update [versions.json](../payload/versions.json) and the language lockfiles together. Downloaded release assets are checked against SHA-256 pins. Use `make -C payload lock` to regenerate Node.js and Python locks (npm and uv required), and Bundler to update Ruby's frozen graph when its upstream revision changes. Retest layout, startup, telemetry, and lifecycle behavior before publishing.

## Releases

Versions follow `vMAJOR.MINOR.PATCH`, with alpha suffixes while experimental. The release workflow validates the tag, builds and tests both images on native amd64 and arm64 workers, and runs the two delivery paths for each architecture. It loads the tested archives and publishes architecture tags (`VERSION-amd64` and `VERSION-arm64`), then combines them into the common `VERSION` tag without rebuilding. The common tag selects the matching image on mixed Linux clusters. It creates a GitHub prerelease with a source archive and SHA-256 checksum; no `latest` tag is published for alpha versions.

Before tagging, update the release references in the quickstart, install template, installer default, policy, and changelog. Merge through a PR and wait for the exact merged commit's CI push run on protected `main` to succeed before tagging it. Release tags are immutable and can only be created by authorized administrators. The release source guard rejects commits outside `main` or without successful CI on `main` before registry authentication or publication. Follow [maintainer access and release authority](maintainers.md) for the required protections and access review. Both image packages must be linked to `otelx/rio` and publicly readable before announcing a release; each Dockerfile supplies the source label used for repository linking. Verify an anonymous pull and repeat the quickstart using the released image references.

The publisher accepts alpha tags only. Before enabling normal releases, run candidate CI and update the [support matrix](compatibility.md) using its image IDs and runtime/libc logs. Keep the CRD at `v1alpha1`. Expand language or signal support only after end-to-end validation. Cross-release upgrade or downgrade support requires tests with persisted rollout state.
