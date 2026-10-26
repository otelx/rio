#!/usr/bin/env bash
set -Eeuo pipefail
root=$(cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  printf 'Usage: scripts/release.sh (CI only; uses GITHUB_REF_NAME and GITHUB_REPOSITORY)\n'
  exit 0
fi
[[ $# -eq 0 ]] || { printf 'Unexpected arguments\n' >&2; exit 2; }
for required in docker gh git grep gzip sha256sum; do
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
for file in scripts/install.sh config/install.yaml config/example.yaml examples/quickstart/policy.yaml docs/quickstart.md docs/configuration.md CHANGELOG.md; do
  grep -Fq -- "$version" "$file" || { printf 'Release reference missing from %s\n' "$file" >&2; exit 1; }
done
gzip -dc .artifacts/release/release-images.tar.gz | docker load
for image in rio rio-payload; do
  docker tag "ghcr.io/otelx/$image:dev" "ghcr.io/otelx/$image:$version"
  docker push "ghcr.io/otelx/$image:$version"
done
archive="rio-$version.tar.gz"
git archive --format=tar.gz --prefix="rio-$version/" "$version" > ".artifacts/release/$archive"
(cd .artifacts/release && sha256sum "$archive" > SHA256SUMS)
cat > .artifacts/release/notes.md <<EOF
RIO is an experimental Kubernetes operator for OpenTelemetry Zero-Code instrumentation.

Select namespaces, detect supported runtimes through the upstream Injector, and manage instrumentation rollouts and recovery. This alpha targets Linux amd64.

Start with the [quickstart](https://github.com/otelx/rio/blob/$version/docs/quickstart.md) and read the [compatibility limits](https://github.com/otelx/rio/blob/$version/docs/compatibility.md).

Images validated by this release workflow:

- \`ghcr.io/otelx/rio:$version\`
- \`ghcr.io/otelx/rio-payload:$version\`

See [CHANGELOG.md](https://github.com/otelx/rio/blob/$version/CHANGELOG.md) for release details.
EOF
gh release create "$version" --repo otelx/rio --verify-tag --prerelease \
  --title "RIO $version" --notes-file .artifacts/release/notes.md \
  ".artifacts/release/$archive" .artifacts/release/SHA256SUMS
