#!/usr/bin/env bash
# Reserve an unused version, publish the tested images, and prepare a draft.
# Publication is a separate CI step after attestations and anonymous checks.
set -Eeuo pipefail
root=$(cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  printf 'Usage: scripts/release.sh (CI only; prepares an unpublished release)\n'
  exit 0
fi
[[ $# -eq 0 ]] || { printf 'Unexpected arguments\n' >&2; exit 2; }
for required in docker gh git grep gzip sha256sum python3 tar; do
  command -v "$required" >/dev/null || { printf 'Missing command: %s\n' "$required" >&2; exit 1; }
done
trap 'printf "RIO release preparation failed at line %s; retain the draft and use a new version after any push\n" "$LINENO" >&2' ERR
version=${GITHUB_REF_NAME:?Expected a release tag}
[[ ${GITHUB_REPOSITORY:-} == otelx/rio && ${GITHUB_REF_TYPE:-} == tag && ${GITHUB_ACTIONS:-} == true ]] || {
  printf 'Release publishing is restricted to tag workflows in otelx/rio\n' >&2; exit 1;
}
metadata=$(python3 scripts/release_metadata.py "$version" --changelog CHANGELOG.md --root .)
image_tag=$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["image_tag"])' "$metadata")
prerelease=$(python3 -c 'import json,sys; print(str(json.loads(sys.argv[1])["prerelease"]).lower())' "$metadata")
commit=$(git rev-parse "$version^{commit}")
[[ "$commit" == "${GITHUB_SHA:?}" && "$commit" == "$(git rev-parse HEAD)" ]] || {
  printf 'Tag, workflow commit, and checkout must agree\n' >&2; exit 1;
}
"$root/scripts/check-release-source.sh"
# Listing (including drafts) must succeed. A failed lookup is never absence.
releases=$(gh api --paginate --slurp 'repos/otelx/rio/releases?per_page=100')
python3 - "$version" "$releases" <<'PY'
import json,sys
if any(r['tag_name']==sys.argv[1] for page in json.loads(sys.argv[2]) for r in page):
    raise SystemExit('Version already reserved by a GitHub release or draft; use a new version')
PY
references=()
for image in operator instrumentation; do
  for suffix in '' -amd64 -arm64; do references+=("ghcr.io/otelx/rio/$image:$image_tag$suffix"); done
done
scripts/check-image-absent.sh "${references[@]}"
# Validate all local evidence before reserving or writing anything remotely.
for arch in amd64 arm64; do
  directory=".artifacts/release/tested-release-images-$arch"
  (cd "$directory" && sha256sum --check archive.sha256)
  gzip -dc "$directory/release-images.tar.gz" | docker load
  for image in operator instrumentation; do
    expected=$(cat "$directory/$image.id")
    actual=$(docker image inspect --format '{{.Id}}' "ghcr.io/otelx/rio/$image:dev-$arch")
    [[ "$actual" == "$expected" ]] || { printf 'Tested image identity mismatch\n' >&2; exit 1; }
    test -s "$directory/$image.spdx.json"
    test -s "$directory/notices/$image/inventory.json"
  done
done
out=.artifacts/release/prepared
mkdir "$out"
python3 - "$metadata" "$out/notes.md" <<'PY'
import json,pathlib,sys
info=json.loads(sys.argv[1]); version=info['version']
pathlib.Path(sys.argv[2]).write_text(info['notes']+f'''

Start with the [quickstart](https://github.com/otelx/rio/blob/{version}/docs/quickstart.md)
and read the [compatibility limits](https://github.com/otelx/rio/blob/{version}/docs/compatibility.md).

See [release verification](https://github.com/otelx/rio/blob/{version}/docs/releases.md)
for digest-based installation, source checksums, SBOMs, authenticated provenance,
license inventories, and anonymous quickstart evidence. Both image indexes select Linux amd64 and arm64.

Application telemetry validation covers Node.js 24 built-in HTTP server trace export on the tested glibc/musl images.
Java, .NET, Python, and Ruby have loader checks only and remain experimental. Metrics/log capture and export,
and context propagation, are unvalidated. Exporter configuration does not guarantee automatic capture.
The Kubernetes API remains `v1alpha1`.

Use operator and instrumentation images from this same full release. In-place cross-release upgrade/downgrade
and mixed-version recovery are unvalidated; follow the
[migration policy](https://github.com/otelx/rio/blob/{version}/docs/compatibility.md#operatorinstrumentation-versions-and-upgrades).
See the [maintenance and private security-report policy](https://github.com/otelx/rio/blob/{version}/SECURITY.md)
for the maintained release range, best-effort response targets, and recovery limits.
''')
PY
release_args=(--repo otelx/rio --verify-tag --draft --title "RIO $version" --notes-file "$out/notes.md")
if [[ "$prerelease" == true ]]; then release_args+=(--prerelease); fi
# GitHub's unique draft/tag reservation happens before ANY image push.
gh release create "$version" "${release_args[@]}"
releases=$(gh api --paginate --slurp 'repos/otelx/rio/releases?per_page=100')
release_id=$(python3 - "$version" "$releases" <<'PY'
import json,sys
releases=[r for page in json.loads(sys.argv[2]) for r in page if r['tag_name']==sys.argv[1]]
assert len(releases)==1 and releases[0]['draft'], 'Draft reservation was lost'
print(releases[0]['id'])
PY
)
IMAGE_DIGESTS_FILE="$out/IMAGE_DIGESTS" scripts/publish-images.sh ghcr.io/otelx "$image_tag"
mkdir "$out/source"
git archive "$version" | tar -x -C "$out/source"
for arch in amd64 arm64; do
  directory=".artifacts/release/tested-release-images-$arch"
  mkdir -p "$out/source/third_party/$arch"
  cp -R "$directory/notices/." "$out/source/third_party/$arch/"
  for image in operator instrumentation; do
    cp "$directory/$image.spdx.json" "$out/$image-$arch.spdx.json"
  done
done
archive="rio-$version.tar.gz"
# The release source includes the notices extracted from the actual tested images.
tar -czf "$out/$archive" --transform="s,^\.,rio-$version," -C "$out/source" .
(cd "$out" && sha256sum "$archive" > SOURCE_SHA256SUMS)
python3 - "$out" "$metadata" "$commit" "${GITHUB_RUN_ID:?}" "$release_id" <<'PY'
import json,pathlib,sys
out=pathlib.Path(sys.argv[1]); info=json.loads(sys.argv[2]); info.update(commit=sys.argv[3],run_id=sys.argv[4],release_id=int(sys.argv[5]))
for line in (out/'IMAGE_DIGESTS').read_text().splitlines():
    digest,reference=line.split()
    tag=reference.rsplit(':',1)[1]
    key=reference.split('/')[-1].split(':')[0]
    suffix=tag.removeprefix(info['image_tag'])
    info[key+suffix]=reference.split(':')[0]+'@'+digest
(out/'release.json').write_text(json.dumps(info,indent=2)+'\n')
with (out/'notes.md').open('a') as f:
    f.write('\nValidated image-index references (use these digests to pin consumption):\n\n')
    for image in ('operator','instrumentation'): f.write(f'- `{info[image]}`\n')
    f.write('\nSource checksum:\n\n```text\n'+(out/'SOURCE_SHA256SUMS').read_text()+'```\n')
PY
# Give subsequent attestation steps immutable subjects, including child manifests.
if [[ -n ${GITHUB_OUTPUT:-} ]]; then
  python3 - "$out/release.json" >> "$GITHUB_OUTPUT" <<'PY'
import json,sys
info=json.load(open(sys.argv[1]))
for image in ('operator','instrumentation'):
    for suffix in ('','-amd64','-arm64'):
        key=image+suffix
        print(f'{key}={info[key]}')
        print(f'{key}-digest={info[key].split("@")[1]}')
PY
fi
printf 'Prepared draft %s; no release announcement has been published.\n' "$version"
