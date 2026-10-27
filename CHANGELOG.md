# Changelog

## Unreleased

- Support stable and prerelease SemVer publication with version-specific notes, draft reservations, collision refusal, and a documented failure/retry policy.
- Publish image-index/architecture digests, source checksums, SPDX SBOMs, and authenticated artifact attestations; require anonymous quickstart checks on both architectures before announcing releases.
- Preserve Go dependency notices and the Injector license in final images, include tested-image notice inventories in source distributions, and gate releases on a current redistribution review.
- Document the [support scope](docs/compatibility.md) for `v0.1.0`, including matching image versions, API/recovery-state compatibility, and migration limits. Node.js HTTP trace export is validated; other language paths remain experimental. The CRD stays `v1alpha1`.
- Define [maintained versions and security response targets](SECURITY.md): 7 days for acknowledgement and 14 days for initial triage, on a best-effort basis.
- Retire and delete the original `ghcr.io/otelx/rio` and `ghcr.io/otelx/rio-payload` packages to make the current image names clear. Their alpha.1 and alpha.2 images can no longer be pulled from GHCR; use alpha.3 or later at `ghcr.io/otelx/rio/operator` and `ghcr.io/otelx/rio/instrumentation`. Source tags and release archives remain available.

## v0.1.0-alpha.3

- **Breaking:** Rename the operator image from `ghcr.io/otelx/rio` to `ghcr.io/otelx/rio/operator`, and the instrumentation image from `ghcr.io/otelx/rio-payload` to `ghcr.io/otelx/rio/instrumentation`. Builds and releases use the new names without compatibility aliases. Update the operator Deployment image and instrumentation policy image references, including any rule overrides; see [upgrade instructions](docs/configuration.md#retired-image-references).
- Add image titles and descriptions identifying the operator and the instrumentation bundle.

## v0.1.0-alpha.2

- Linux arm64 controller and payload images, with native architecture CI and multi-platform release tags for amd64 and arm64.
- Loader coverage for all five language agents with glibc and Alpine/musl, plus HTTP trace export checks with both libc variants.
- Mixed architecture capability checks and an integration harness that verifies delivery on every node and HTTP export on each architecture.

## v0.1.0-alpha.1

Initial experimental release of RIO, extracted from OpenTelemetry Far and Wide.

- Cluster-scoped policy with ordered namespace and optional workload selection.
- Upstream Injector payload with Java, Node.js, .NET, Python, and Ruby agents.
- Probed native image-volume delivery and init-container copy fallback.
- Managed rolling updates, persistent recovery state, and finalizer-based removal.
- Standalone quickstart, compatibility reference, CI, and release automation.

The API and payload contract are experimental. See [compatibility](docs/compatibility.md) for release limitations.
