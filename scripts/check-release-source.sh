#!/usr/bin/env bash
# Fail before publication unless this exact tag passed CI on protected main.
set -Eeuo pipefail
if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  printf 'Usage: scripts/check-release-source.sh (CI only; requires GITHUB_SHA, GITHUB_REF_NAME, GH_TOKEN)\n'
  exit 0
fi
[[ $# -eq 0 ]] || { printf 'Unexpected arguments\n' >&2; exit 2; }
for required in git gh; do
  command -v "$required" >/dev/null || { printf 'Missing command: %s\n' "$required" >&2; exit 1; }
done
trap 'printf "Release source verification failed at line %s\n" "$LINENO" >&2' ERR
[[ ${GITHUB_REPOSITORY:-} == otelx/rio && ${GITHUB_REF_TYPE:-} == tag && ${GITHUB_EVENT_NAME:-} == push ]] || {
  printf 'Release source verification requires a tag push in otelx/rio\n' >&2; exit 1;
}
version=${GITHUB_REF_NAME:?Expected a release tag}
commit=${GITHUB_SHA:?Expected the tested release commit}
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-alpha\.[0-9]+$ && "$commit" =~ ^[a-f0-9]{40}$ ]] || {
  printf 'Expected an alpha tag and a full commit SHA\n' >&2; exit 2;
}
# Explicit private refs work with detached, shallow Actions checkouts. Fetch the
# actual repository, rather than trusting a caller-supplied local main branch.
git fetch --no-tags https://github.com/otelx/rio.git \
  '+refs/heads/main:refs/rio-release/main' \
  "+refs/tags/$version:refs/rio-release/tag"
[[ $(git rev-parse HEAD) == "$commit" && $(git rev-parse 'refs/rio-release/tag^{commit}') == "$commit" ]] || {
  printf 'Checkout, tag, and workflow commit must match\n' >&2; exit 1;
}
git merge-base --is-ancestor "$commit" refs/rio-release/main || {
  printf 'Release commit is not in protected main history\n' >&2; exit 1;
}
# A passing tag build alone is insufficient: the exact commit must already
# have completed the full CI workflow on main, after its PR was merged.
passed=$(gh api --method GET repos/otelx/rio/actions/workflows/ci.yml/runs \
  -f branch=main -f event=push -f head_sha="$commit" -f status=completed \
  --jq '[.workflow_runs[] | select(.conclusion == "success")] | length')
[[ "$passed" =~ ^[0-9]+$ && "$passed" -gt 0 ]] || {
  printf 'Release commit has no successful CI push run on main\n' >&2; exit 1;
}
printf 'Verified release source: %s at %s passed CI on main\n' "$version" "$commit"
