#!/usr/bin/env bash
# Publish the tested per-architecture images without rebuilding them.
set -Eeuo pipefail
if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  printf 'Usage: scripts/publish-images.sh REGISTRY/NAMESPACE VERSION\n'
  exit 0
fi
[[ $# -eq 2 ]] || { printf 'Expected a registry namespace and version; use --help.\n' >&2; exit 2; }
registry=$1
version=$2
root=$(cd -- "$(dirname -- "$0")/.." && pwd)
source_tag_prefix=${SOURCE_TAG_PREFIX:-dev}
[[ "$registry" != *[[:space:]]* && "$registry" != */ && -n "$registry" ]] || { printf 'Invalid registry namespace\n' >&2; exit 2; }
[[ "$version" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]*$ && ${#version} -le 120 ]] || { printf 'Invalid image version\n' >&2; exit 2; }
[[ "$source_tag_prefix" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]*$ && ${#source_tag_prefix} -le 120 ]] || { printf 'Invalid source image tag prefix\n' >&2; exit 2; }
for required in docker python3 mktemp grep; do
  command -v "$required" >/dev/null || { printf 'Missing command: %s\n' "$required" >&2; exit 1; }
done
trap 'printf "Image publication failed at line %s\n" "$LINENO" >&2' ERR
# Check ALL common and architecture references before the first push. Failed
# attempts consume the version; reruns must use a new version, never overwrite.
references=()
for image in rio/operator rio/instrumentation; do
  for suffix in '' -amd64 -arm64; do
    references+=("$registry/$image:$version$suffix")
  done
done
"$root/scripts/check-image-absent.sh" "${references[@]}"
# Check every input before publishing any image.
for image in rio/operator rio/instrumentation; do
  for arch in amd64 arm64; do
    source="ghcr.io/otelx/$image:$source_tag_prefix-$arch"
    actual=$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$source")
    [[ "$actual" == "linux/$arch" ]] || { printf 'Expected linux/%s for %s, got %s\n' "$arch" "$source" "$actual" >&2; exit 1; }
  done
done
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
report=${IMAGE_DIGESTS_FILE:-/dev/stdout}
: > "$temporary/digests"
for image in rio/operator rio/instrumentation; do
  sources=()
  for arch in amd64 arm64; do
    reference="$registry/$image:$version-$arch"
    docker tag "ghcr.io/otelx/$image:$source_tag_prefix-$arch" "$reference"
    docker push "$reference"
    digest=$(docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "$reference")
    [[ "$digest" =~ ^sha256:[a-f0-9]{64}$ ]] || { printf 'Invalid published image digest: %q\n' "$digest" >&2; exit 1; }
    sources+=("$registry/$image@$digest")
    printf '%s %s:%s-%s\n' "$digest" "$registry/$image" "$version" "$arch" >> "$temporary/digests"
  done
  # Validate the index before making the common release tag visible.
  docker buildx imagetools create --dry-run "${sources[@]}" > "$temporary/index.json"
  python3 - "$temporary/index.json" <<'PY'
import json, sys
index=json.load(open(sys.argv[1]))
manifests=index['manifests']
images=[m for m in manifests if m.get('annotations',{}).get('vnd.docker.reference.type')!='attestation-manifest']
platforms=[(m['platform']['os'],m['platform']['architecture']) for m in images]
assert sorted(platforms)==[('linux','amd64'),('linux','arm64')], platforms
digests={m['digest'] for m in images}
for manifest in manifests:
    if manifest not in images:
        assert manifest['annotations']['vnd.docker.reference.digest'] in digests, manifest
PY
  docker buildx imagetools create --tag "$registry/$image:$version" "${sources[@]}"
  docker buildx imagetools inspect "$registry/$image:$version" --raw > "$temporary/published.json"
  python3 - "$temporary/index.json" "$temporary/published.json" <<'PY'
import json, sys
expected,actual=[json.load(open(p)) for p in sys.argv[1:]]
assert actual['manifests']==expected['manifests'], actual
PY
  digest=$(docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "$registry/$image:$version")
  [[ "$digest" =~ ^sha256:[a-f0-9]{64}$ ]] || { printf 'Invalid image-index digest\n' >&2; exit 1; }
  printf '%s %s:%s\n' "$digest" "$registry/$image" "$version" >> "$temporary/digests"
done
cat "$temporary/digests" > "$report"
