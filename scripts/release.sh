#!/usr/bin/env bash
set -Eeuo pipefail
root=$(cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  printf 'Usage: scripts/release.sh (CI only; uses GITHUB_REF_NAME and GITHUB_REPOSITORY)\n'
  exit 0
fi
[[ $# -eq 0 ]] || { printf 'Unexpected arguments\n' >&2; exit 2; }
for required in docker gh git grep gzip sha256sum python3; do
  command -v "$required" >/dev/null || { printf 'Missing command: %s\n' "$required" >&2; exit 1; }
done
trap 'printf "RIO release failed at line %s\n" "$LINENO" >&2' ERR
version=${GITHUB_REF_NAME:?Expected a release tag}
[[ ${GITHUB_REPOSITORY:-} == otelx/rio && ${GITHUB_REF_TYPE:-} == tag ]] || {
  printf 'Release publishing is restricted to tags in otelx/rio\n' >&2; exit 1;
}
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-alpha\.[0-9]+$ ]] || {
  printf 'Expected an alpha version tag, got %q\n' "$version" >&2; exit 2;
}
"$root/scripts/check-release-source.sh"
for file in scripts/install.sh config/install.yaml config/example.yaml examples/quickstart/policy.yaml docs/quickstart.md docs/configuration.md CHANGELOG.md; do
  grep -Fq -- "$version" "$file" || { printf 'Release reference missing from %s\n' "$file" >&2; exit 1; }
done
for arch in amd64 arm64; do
  gzip -dc ".artifacts/release/tested-release-images-$arch/release-images.tar.gz" | docker load
done
"$root/scripts/publish-images.sh" ghcr.io/otelx "$version"
archive="rio-$version.tar.gz"
git archive --format=tar.gz --prefix="rio-$version/" "$version" > ".artifacts/release/$archive"
(cd .artifacts/release && sha256sum "$archive" > SHA256SUMS)
cat > .artifacts/release/notes.md <<EOF
RIO is an experimental Kubernetes operator for OpenTelemetry Zero-Code instrumentation.

Select namespaces, detect supported runtimes through the upstream Injector, and manage instrumentation rollouts and recovery. This alpha targets Linux amd64 and arm64, including glibc and musl application images. Both image tags select the matching architecture on mixed Linux clusters.

Start with the [quickstart](https://github.com/otelx/rio/blob/$version/docs/quickstart.md) and read the [compatibility limits](https://github.com/otelx/rio/blob/$version/docs/compatibility.md).

Node.js HTTP server trace export is tested on glibc and musl. Java, .NET, Python, Ruby, and metrics/log capture remain experimental; propagation is untested. The API remains \`v1alpha1\`. Use matching operator and instrumentation versions; see [compatibility and migration limits](https://github.com/otelx/rio/blob/$version/docs/compatibility.md) and [security reporting](https://github.com/otelx/rio/blob/$version/SECURITY.md).

Images validated by this release workflow:

- Operator: \`ghcr.io/otelx/rio/operator:$version\`
- Instrumentation (OpenTelemetry Injector and language agents): \`ghcr.io/otelx/rio/instrumentation:$version\`

See [CHANGELOG.md](https://github.com/otelx/rio/blob/$version/CHANGELOG.md) for release details.
EOF
gh release create "$version" --repo otelx/rio --verify-tag --prerelease \
  --title "RIO $version" --notes-file .artifacts/release/notes.md \
  ".artifacts/release/$archive" .artifacts/release/SHA256SUMS
