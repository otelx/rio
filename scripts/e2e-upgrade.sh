#!/usr/bin/env bash
# Rebuild historical source: never republish the retired GHCR packages.
set -Eeuo pipefail
component=$(cd -- "$(dirname -- "$0")/.." && pwd)
if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  printf '%s\n' 'Usage: scripts/e2e-upgrade.sh' 'UPGRADE_BASELINE=alpha2 (default) or alpha3 selects historical release fixtures.'
  exit 0
fi
[[ $# -eq 0 ]] || { printf 'Unexpected arguments; use --help.\n' >&2; exit 2; }
for required in git docker kind kubectl python3 openssl; do
  command -v "$required" >/dev/null || { printf 'Missing command: %s\n' "$required" >&2; exit 1; }
done
trap 'printf "RIO: upgrade test failed at line %s\n" "$LINENO" >&2' ERR
baseline=${UPGRADE_BASELINE:-alpha2}
case "$baseline" in
  alpha2)
    source_commit=be271b81fae80b7bd041a81e98b3abccb18997a1
    export PREVIOUS_OPERATOR_IMAGE=${PREVIOUS_OPERATOR_IMAGE:-ghcr.io/otelx/rio:issue-6-alpha2}
    export PREVIOUS_PAYLOAD_IMAGE=${PREVIOUS_PAYLOAD_IMAGE:-ghcr.io/otelx/rio-payload:issue-6-alpha2}
    ;;
  alpha3)
    source_commit=431f265d139206e98b34b8ce1ed6ddf2c2a0fd8c
    export PREVIOUS_OPERATOR_IMAGE=${PREVIOUS_OPERATOR_IMAGE:-ghcr.io/otelx/rio/operator:issue-6-alpha3}
    export PREVIOUS_PAYLOAD_IMAGE=${PREVIOUS_PAYLOAD_IMAGE:-ghcr.io/otelx/rio/instrumentation:issue-6-alpha3}
    ;;
  *) printf 'UPGRADE_BASELINE must be alpha2 or alpha3\n' >&2; exit 2 ;;
esac
export CLUSTER_NAME=${CLUSTER_NAME:-rio-issue-6-upgrade-$baseline}
# Fixed historical source is an intentional compatibility baseline. Use a
# disposable private fixture path, not a second checkout or retired package.
mkdir -p "$component/.artifacts/upgrade"
export PREVIOUS_SOURCE
PREVIOUS_SOURCE=$(mktemp -d "$component/.artifacts/upgrade/$baseline.XXXXXXXX")
git -C "$component" archive "$source_commit" | tar -x -C "$PREVIOUS_SOURCE"
export OPERATOR_IMAGE=${OPERATOR_IMAGE:-ghcr.io/otelx/rio/operator:issue-6-candidate}
export PAYLOAD_IMAGE=${PAYLOAD_IMAGE:-ghcr.io/otelx/rio/instrumentation:issue-6-candidate}
docker build -t "$PREVIOUS_OPERATOR_IMAGE" "$PREVIOUS_SOURCE"
docker build -t "$PREVIOUS_PAYLOAD_IMAGE" "$PREVIOUS_SOURCE/payload"
case ${BUILD_CANDIDATE_IMAGES:-true} in
  true)
    docker build -t "$OPERATOR_IMAGE" "$component"
    docker build -t "$PAYLOAD_IMAGE" "$component/payload"
    ;;
  false)
    docker image inspect "$OPERATOR_IMAGE" "$PAYLOAD_IMAGE" >/dev/null
    ;;
  *) printf 'BUILD_CANDIDATE_IMAGES must be true or false\n' >&2; exit 2 ;;
esac
"$component/scripts/e2e.sh"
