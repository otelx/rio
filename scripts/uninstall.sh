#!/usr/bin/env bash
set -Eeuo pipefail
component=$(cd -- "$(dirname -- "$0")/.." && pwd)
root=$component

if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  printf '%s\n' 'Usage: scripts/uninstall.sh'
  exit 0
fi
if [[ $# -gt 0 ]]; then
  printf '%s\n' 'Unexpected arguments; use --help.' >&2
  exit 2
fi
command -v kubectl >/dev/null || { printf 'Missing command: kubectl\n' >&2; exit 1; }
trap 'printf "RIO: command failed at line %s\n" "$LINENO" >&2' ERR
target_context=$(command kubectl config current-context)
artifacts="$root/.artifacts/uninstall"
mkdir -p "$artifacts"
export TMPDIR="$artifacts"
kubectl() { command kubectl --context="$target_context" --cache-dir="$artifacts/kubectl-cache" "$@"; }
printf 'Uninstalling RIO from context %s\n' "$target_context"
# Keep the operator and webhook running while the finalizer removes injection.
kubectl delete instrumentations.rio.otelx.github.io cluster --ignore-not-found --wait=true --timeout=15m
kubectl delete mutatingwebhookconfiguration rio --ignore-not-found
kubectl delete namespace rio-system --ignore-not-found
kubectl delete clusterrolebinding rio --ignore-not-found
kubectl delete clusterrole rio --ignore-not-found
kubectl delete crd instrumentations.rio.otelx.github.io --ignore-not-found
