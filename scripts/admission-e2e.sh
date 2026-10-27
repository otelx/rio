#!/usr/bin/env bash
# Exercise admission through a real API server in an isolated disposable cluster.
set -Eeuo pipefail
root=$(cd -- "$(dirname -- "$0")/.." && pwd)
if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  printf '%s\n' 'Usage: scripts/admission-e2e.sh' 'CLUSTER_NAME must contain issue-1; defaults to rio-issue-1-admission.'
  exit 0
fi
[[ $# -eq 0 ]] || { printf 'Unexpected arguments; use --help.\n' >&2; exit 2; }
for required in go kubectl kind docker; do
  command -v "$required" >/dev/null || { printf 'Missing command: %s\n' "$required" >&2; exit 1; }
done
cluster=${CLUSTER_NAME:-rio-issue-1-admission}
if [[ ! "$cluster" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ || "$cluster" != *issue-1* || ${#cluster} -gt 50 ]]; then
  printf 'Invalid cluster name (must contain issue-1): %q\n' "$cluster" >&2; exit 2
fi
existing_clusters=$(kind get clusters)
if [[ $'\n'"$existing_clusters"$'\n' == *$'\n'"$cluster"$'\n'* ]]; then
  printf 'Refusing to use an existing cluster: %s\n' "$cluster" >&2; exit 1
fi
artifacts="$root/.artifacts/admission/$cluster"
mkdir -p "$artifacts"
export KUBECONFIG="$artifacts/kubeconfig"
export GOTMPDIR=${GOTMPDIR:-"$artifacts/tmp"}
export TMPDIR="$GOTMPDIR"
mkdir -p "$GOTMPDIR"
export GOCACHE=${GOCACHE:-"$root/.artifacts/go-cache"}
export GOMODCACHE=${GOMODCACHE:-"$root/.artifacts/go-mod-cache"}
export GOPATH=${GOPATH:-"$root/.artifacts/go"}
created=false
cleanup() {
  result=$?
  trap - EXIT
  if [[ "$created" == true ]]; then
    if [[ "$result" -eq 0 ]]; then
      kind delete cluster --name "$cluster" || result=1
    else
      printf 'Retained failed cluster %s; KUBECONFIG=%s\n' "$cluster" "$KUBECONFIG" >&2
    fi
  fi
  exit "$result"
}
trap cleanup EXIT
trap 'printf "Admission check failed at line %s\n" "$LINENO" >&2' ERR
trap 'exit 130' INT
trap 'exit 143' TERM
kind create cluster --name "$cluster" --image "${NODE_IMAGE:-kindest/node:v1.35.0}" --kubeconfig "$KUBECONFIG" --wait 120s
created=true
kubectl --context="kind-$cluster" apply -f "$root/config/crd.yaml"
kubectl --context="kind-$cluster" wait --for=condition=Established crd/instrumentations.rio.otelx.github.io --timeout=60s
# Linux Docker exposes host listeners to kind containers via this bridge gateway.
RIO_TEST_WEBHOOK_HOST=$(docker inspect "$cluster-control-plane" --format '{{range .NetworkSettings.Networks}}{{.Gateway}}{{end}}')
export RIO_TEST_WEBHOOK_HOST
cd -- "$root"
go test -race -v ./internal/operator -run '^TestAdmissionKubernetesAPI$' -count=1 | tee "$artifacts/admission.log"
