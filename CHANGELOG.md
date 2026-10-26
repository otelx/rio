# Changelog

## Unreleased

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
