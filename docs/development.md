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
make images
make smoke
EXPECT_DELIVERY=imageVolume make e2e
CLUSTER_NAME=rio-fallback IMAGE_VOLUME_ENABLED=false EXPECT_DELIVERY=initContainer make e2e
```

Run clusters sequentially on a local machine. The integration script creates its own kind cluster and private kubeconfig, refuses to replace an existing cluster, and preserves failed runs for inspection. Successful runs remove their cluster. `KEEP_CLUSTER=true` preserves a successful run. To rerun a retained cluster, use `REUSE_CLUSTER=true` and its original `CLUSTER_NAME`; the saved private kubeconfig is required.

The suite covers injection and removal on Deployments, StatefulSets, and DaemonSets, failed configuration, recovery, operator restart, correction, and real HTTP trace export from the quickstart service. Loader checks cover all five agents; they disable exporters and are distinct from telemetry verification.

For a disposable cluster you created yourself, load local images before installing:

```bash
kind load docker-image --name MY_CLUSTER ghcr.io/otelx/rio:dev ghcr.io/otelx/rio-payload:dev
make install
```

The installer uses your current kubectl context. For a local quickstart, change `examples/quickstart/policy.yaml` to reference `ghcr.io/otelx/rio-payload:dev`, or use the integration script, which renders that reference automatically.

## Update payload inputs

Update [versions.json](../payload/versions.json) and the language lockfiles together. Downloaded release assets are checked against SHA-256 pins. Use `make -C payload lock` to regenerate Node.js and Python locks (npm and uv required), and Bundler to update Ruby's frozen graph when its upstream revision changes. Retest layout, startup, telemetry, and lifecycle behavior before publishing.

## Releases

Versions follow `vMAJOR.MINOR.PATCH`, with alpha suffixes while experimental. The initial version is `v0.1.0-alpha.1`. The release workflow validates the tag, builds and tests both images, runs the two delivery paths, then publishes those exact tested image archives to GHCR. Images are Linux amd64 initially. It creates a GitHub prerelease with a source archive and SHA-256 checksum; no `latest` tag is published for alpha versions.

Before tagging, update the release references in the quickstart, install template, installer default, policy, and changelog. Push a tag only after CI succeeds. Image packages must be publicly readable before announcing a release. Verify an anonymous pull and repeat the quickstart using the released image references.
