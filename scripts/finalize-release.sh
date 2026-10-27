#!/usr/bin/env bash
# Publish only a fully verified draft prepared by this exact workflow run.
set -Eeuo pipefail
root=$(cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
[[ $# -eq 0 ]] || { printf 'Usage: scripts/finalize-release.sh (CI only)\n' >&2; exit 2; }
[[ ${GITHUB_ACTIONS:-} == true && ${GITHUB_REPOSITORY:-} == otelx/rio && ${GITHUB_REF_TYPE:-} == tag ]] || exit 1
version=${GITHUB_REF_NAME:?}
out=.artifacts/release/prepared
python3 - "$out" "${GITHUB_SHA:?}" "${GITHUB_RUN_ID:?}" "$version" <<'PY'
import json,pathlib,sys
out=pathlib.Path(sys.argv[1]); info=json.loads((out/'release.json').read_text())
assert (info['commit'],info['run_id'],info['version'])==tuple(sys.argv[2:]), 'Stale release metadata'
for arch in ('amd64','arm64'):
    evidence=json.loads((out/f'verified-{arch}.json').read_text())
    assert evidence.pop('verified_arch')==arch
    assert evidence==info, 'Anonymous verification used different artifacts'
for name in ('source','operator-amd64','operator-arm64','instrumentation-amd64','instrumentation-arm64'):
    document=json.loads((out/f'{name}.spdx.json').read_text())
    assert document['spdxVersion']=='SPDX-2.3' and document['packages'], f'Empty SBOM: {name}'
PY
(cd "$out" && sha256sum --check SOURCE_SHA256SUMS)
# Re-check the effective setting immediately before publication. The ordinary
# Actions token lacks Administration:read, so use a narrowly scoped read token.
[[ -n ${RELEASE_SETTINGS_TOKEN:-} ]] || { printf 'Missing RELEASE_SETTINGS_TOKEN (Administration:read)\n' >&2; exit 1; }
[[ $(GH_TOKEN="$RELEASE_SETTINGS_TOKEN" gh api repos/otelx/rio/immutable-releases --jq .enabled) == true ]] || {
  printf 'Immutable releases must be enabled\n' >&2; exit 1;
}
# No replacement of an existing published release, even on reruns.
release=$(gh api --paginate --slurp 'repos/otelx/rio/releases?per_page=100')
python3 - "$version" "$release" "$out/release.json" <<'PY'
import json,sys
releases=[r for page in json.loads(sys.argv[2]) for r in page if r['tag_name']==sys.argv[1]]
assert len(releases)==1 and releases[0]['draft'], 'Expected the reserved, unpublished draft'
assert releases[0]['id']==json.load(open(sys.argv[3]))['release_id'], 'Draft reservation changed'
PY
signer=otelx/rio/.github/workflows/ci.yml
# Verify authenticated attestations for actual subjects, not just the presence
# of bundle files. No rebuild or mutable tag resolution enters this step.
gh attestation verify "$out/rio-$version.tar.gz" --repo otelx/rio --signer-workflow "$signer" --source-digest "$GITHUB_SHA"
gh attestation verify "$out/rio-$version.tar.gz" --repo otelx/rio --signer-workflow "$signer" --source-digest "$GITHUB_SHA" --predicate-type https://spdx.dev/Document/v2.3
for image in operator instrumentation; do
  reference=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))[sys.argv[2]])' "$out/release.json" "$image")
  gh attestation verify "oci://$reference" --repo otelx/rio --signer-workflow "$signer" --source-digest "$GITHUB_SHA"
  for arch in amd64 arm64; do
    reference=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))[sys.argv[2]])' "$out/release.json" "$image-$arch")
    gh attestation verify "oci://$reference" --repo otelx/rio --signer-workflow "$signer" --source-digest "$GITHUB_SHA" --predicate-type https://spdx.dev/Document/v2.3
  done
done
while read -r expected reference; do
  actual=$(docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "$reference")
  [[ "$actual" == "$expected" ]] || { printf 'Version reference changed: %s\n' "$reference" >&2; exit 1; }
done < "$out/IMAGE_DIGESTS"
# Include every distributable and verification record, excluding staging dirs.
(cd "$out" && sha256sum -- "rio-$version.tar.gz" IMAGE_DIGESTS release.json *.spdx.json *.sigstore.json verified-*.json > SHA256SUMS)
assets=("$out/rio-$version.tar.gz" "$out/IMAGE_DIGESTS" "$out/release.json" "$out/SHA256SUMS" "$out/SOURCE_SHA256SUMS" "$out/"*.spdx.json "$out/"*.sigstore.json "$out/"verified-*.json)
gh release upload "$version" --repo otelx/rio "${assets[@]}"
# Never --clobber. Failed asset uploads consume the version too.
gh release edit "$version" --repo otelx/rio --notes-file "$out/notes.md"
gh release edit "$version" --repo otelx/rio --draft=false
[[ $(gh api "repos/otelx/rio/releases/tags/$version" --jq .immutable) == true ]] || {
  printf 'Published release is unexpectedly mutable; do not announce it\n' >&2; exit 1;
}
printf 'Published verified immutable release %s\n' "$version"
