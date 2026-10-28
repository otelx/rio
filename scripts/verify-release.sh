#!/usr/bin/env bash
# Run the quickstart on a clean cluster against final, anonymous GHCR references.
set -Eeuo pipefail
root=$(cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
[[ $# -eq 1 ]] || { printf 'Usage: scripts/verify-release.sh RELEASE.json\n' >&2; exit 2; }
for required in docker python3 kind kubectl openssl; do
  command -v "$required" >/dev/null || { printf 'Missing command: %s\n' "$required" >&2; exit 1; }
done
metadata=$1
arch=${RELEASE_ARCH:?Expected the native architecture}
[[ "$arch" == amd64 || "$arch" == arm64 ]] || exit 2
cluster=${CLUSTER_NAME:-rio-issue-5-release-${GITHUB_RUN_ID:?}-$arch}
[[ "$cluster" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ && ${#cluster} -le 63 ]] || exit 2
artifacts="$root/.artifacts/release-check/$cluster"
mkdir -p "$artifacts"
# A private, empty Docker config ensures that registry operations have no login.
export DOCKER_CONFIG="$artifacts/docker-config"
mkdir "$DOCKER_CONFIG"
printf '{"auths":{}}\n' > "$DOCKER_CONFIG/config.json"
unset DOCKER_AUTH_CONFIG REGISTRY_AUTH_FILE
export KUBECONFIG="$artifacts/kubeconfig"
export TMPDIR="$artifacts/tmp"
mkdir "$TMPDIR"
operator=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["operator"])' "$metadata")
payload=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["instrumentation"])' "$metadata")
image_tag=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["image_tag"])' "$metadata")
for reference in "$operator" "$payload"; do
  [[ "$reference" =~ ^ghcr.io/otelx/rio/(operator|instrumentation)@sha256:[a-f0-9]{64}$ ]] || exit 2
  docker pull --platform "linux/$arch" "$reference"
done
# Execute through the final common tags and ensure they still select our index.
for image in operator instrumentation; do
  digest=$(docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "ghcr.io/otelx/rio/$image:$image_tag")
  expected=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))[sys.argv[2]].split("@")[1])' "$metadata" "$image")
  [[ "$digest" == "$expected" ]] || { printf 'Version reference changed\n' >&2; exit 1; }
done
docker run --rm --pull always --platform "linux/$arch" "ghcr.io/otelx/rio/operator:$image_tag" --help
docker run --rm --pull always --platform "linux/$arch" "ghcr.io/otelx/rio/instrumentation:$image_tag" /verify-layout.sh
# Refuse existing clusters before installing the cleanup handler.
clusters=$(kind get clusters)
if grep -Fxq -- "$cluster" <<< "$clusters"; then
  printf 'Refusing existing cluster %s\n' "$cluster" >&2; exit 1
fi
cleanup() {
  result=$?
  trap - EXIT
  if [[ "$result" == 0 ]]; then
    kind delete cluster --name "$cluster" || result=1
  else
    kubectl logs -n rio-system deployment/rio > "$artifacts/operator.log" 2>&1 || true
    printf 'Failure evidence retained; inspect cluster %s (%s)\n' "$cluster" "$KUBECONFIG" >&2
  fi
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
kind create cluster --name "$cluster" --image kindest/node:v1.35.0 \
  --config examples/quickstart/kind.yaml --kubeconfig "$KUBECONFIG" --wait 180s --retain
# No kind load, registry credentials, or imagePullSecrets: Kubernetes pulls itself.
scripts/install.sh "$operator"
kubectl apply -f examples/quickstart/app.yaml
kubectl rollout status deployment/collector -n rio-telemetry --timeout=180s
kubectl rollout status deployment/hello -n rio-example --timeout=180s
python3 - "$payload" > "$artifacts/policy.yaml" <<'PY'
import pathlib,re,sys
text=pathlib.Path('examples/quickstart/policy.yaml').read_text()
text,count=re.subn(r'ghcr\.io/otelx/rio/instrumentation:[^\s]+',sys.argv[1],text)
assert count==1, count
print(text)
PY
kubectl apply -f "$artifacts/policy.yaml"
kubectl wait --for=jsonpath='{.status.phase}'=Ready instrumentations.rio.otelx.github.io/cluster --timeout=300s
kubectl exec -n rio-example deployment/hello -- node -e \
  'const http=require("node:http"); for(let i=0;i<5;i++) http.get("http://127.0.0.1:8080/hello",r=>r.resume());'
verified=false
for ((attempt=0; attempt<30; attempt++)); do
  kubectl logs -n rio-telemetry deployment/collector > "$artifacts/collector.log"
  if python3 - "$artifacts/collector.log" <<'PY'
from pathlib import Path
import sys
text=Path(sys.argv[1]).read_text()
assert 'service.name: Str(hello)' in text and 'Trace ID' in text and 'Kind           : Server' in text
PY
  then verified=true; break; fi
  sleep 2
done
[[ "$verified" == true ]] || { printf 'Quickstart did not export HTTP spans\n' >&2; exit 1; }
kubectl get nodes -o json > "$artifacts/nodes.json"
python3 - "$artifacts/nodes.json" "$arch" <<'PY'
import json,sys
assert {n['status']['nodeInfo']['architecture'] for n in json.load(open(sys.argv[1]))['items']}=={sys.argv[2]}
PY
kubectl delete instrumentations.rio.otelx.github.io cluster --timeout=300s
scripts/uninstall.sh
# Bind successful evidence to the version, tested digests, commit, run, and arch.
python3 - "$metadata" "$arch" > "$artifacts/verified-$arch.json" <<'PY'
import json,sys
info=json.load(open(sys.argv[1])); info['verified_arch']=sys.argv[2]
print(json.dumps(info,indent=2))
PY
