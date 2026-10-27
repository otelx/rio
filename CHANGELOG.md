# Changelog

## Unreleased

- Support stable and prerelease SemVer publication with version-specific notes, draft reservations, collision refusal, and a documented failure/retry policy.
- Publish image-index/architecture digests, source checksums, SPDX SBOMs, and authenticated artifact attestations; require anonymous quickstart checks on both architectures before announcing releases.
- Preserve Go dependency notices and the Injector license in final images, include tested-image notice inventories in source distributions, and gate releases on a current redistribution review.

- Define the limited support contract for the first normal `v0.1.0` release: Kubernetes 1.35.0, native Linux amd64/arm64 CI, Node.js 24 built-in HTTP trace export on the tested glibc/musl images, and both delivery paths. Other bundled language paths remain experimental; metrics/log capture and propagation are unvalidated. This does not announce a normal release or production readiness, and the CRD remains `v1alpha1`.
- Define matching `rio/operator`/`rio/instrumentation` release pairs, uninstrumented migration boundaries, cross-release recovery limits, API/configuration and persisted-annotation compatibility expectations, and the maintained release range. Name the sole maintainer and recovery limits; set best-effort private security-report targets of 7 days for acknowledgement and 14 days for initial triage. See [compatibility](docs/compatibility.md), [SECURITY.md](SECURITY.md), and [maintenance responsibilities](CONTRIBUTING.md#maintenance-responsibilities-and-recovery).
- Retire and delete the original `ghcr.io/otelx/rio` and `ghcr.io/otelx/rio-payload` packages to make the current image names clear. Their alpha.1 and alpha.2 images can no longer be pulled from GHCR; use alpha.3 or later at `ghcr.io/otelx/rio/operator` and `ghcr.io/otelx/rio/instrumentation`. Source tags and release archives remain available.

## v0.1.0-alpha.3

- **Breaking:** Rename the operator image from `ghcr.io/otelx/rio` to `ghcr.io/otelx/rio/operator`, and the instrumentation image from `ghcr.io/otelx/rio-payload` to `ghcr.io/otelx/rio/instrumentation`. Builds and releases use the new names without compatibility aliases. Update the operator Deployment image and instrumentation policy image references, including any rule overrides; see [upgrade instructions](docs/configuration.md#upgrading-from-alpha1-or-alpha2).
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
