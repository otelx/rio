#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
component=$(cd -- "$(dirname -- "$0")/.." && pwd)
root=$component

if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  printf '%s\n' 'Usage: scripts/install.sh [OPERATOR_IMAGE]'
  exit 0
fi
if [[ $# -gt 1 ]]; then
  printf '%s\n' 'Unexpected arguments; use --help.' >&2
  exit 2
fi
for required in kubectl python3 openssl; do
  command -v "$required" >/dev/null || { printf 'Missing command: %s\n' "$required" >&2; exit 1; }
done
trap 'printf "RIO: command failed at line %s\n" "$LINENO" >&2' ERR
image=${1:-ghcr.io/otelx/rio/operator:v0.1.0-alpha.3}
if [[ ! "$image" =~ ^[a-zA-Z0-9][a-zA-Z0-9._/:@-]*$ ]]; then
  printf 'Invalid image reference: %q\n' "$image" >&2
  exit 2
fi
target_context=$(command kubectl config current-context)
config_id=$(python3 -c 'import hashlib,os,sys; print(hashlib.sha256((os.environ.get("KUBECONFIG", "") + "\0" + sys.argv[1]).encode()).hexdigest()[:16])' "$target_context")
artifacts="$root/.artifacts/install/$config_id"
mkdir -p "$artifacts"
export TMPDIR="$artifacts"
kubectl() { command kubectl --context="$target_context" --cache-dir="$artifacts/kubectl-cache" "$@"; }
printf 'Installing RIO in context %s\n' "$target_context"
kubectl apply -f "$component/config/crd.yaml"
kubectl wait --for=condition=Established crd/instrumentations.rio.otelx.github.io --timeout=60s
kubectl create namespace rio-system --dry-run=client -o yaml | kubectl apply -f -
# Reuse the cluster certificate so reinstalling does not race an old server.
if kubectl get secret rio-tls -n rio-system -o json > "$artifacts/secret.json" 2>/dev/null; then
  python3 - "$artifacts" <<'PY'
import base64,json,pathlib,sys
p=pathlib.Path(sys.argv[1]); data=json.loads((p/'secret.json').read_text())['data']
for key in ('tls.crt','tls.key'):
    (p/key).write_bytes(base64.b64decode(data[key]))
PY
else
  openssl req -x509 -newkey rsa:2048 -nodes -days 365 -keyout "$artifacts/tls.key" -out "$artifacts/tls.crt" \
    -subj '/CN=rio.rio-system.svc' \
    -addext 'subjectAltName=DNS:rio.rio-system.svc,DNS:rio.rio-system.svc.cluster.local'
  kubectl create secret tls rio-tls -n rio-system --cert="$artifacts/tls.crt" --key="$artifacts/tls.key" --dry-run=client -o yaml | kubectl apply -f -
fi
chmod 600 "$artifacts/tls.key"
python3 - "$component" "$artifacts" "$image" <<'PY'
import base64,pathlib,sys
component,artifacts=map(pathlib.Path,sys.argv[1:3]); image=sys.argv[3]
if not image: raise SystemExit('Missing image reference')
text=(component/'config/install.yaml').read_text()
text=text.replace('REPLACE_CA_BUNDLE',base64.b64encode((artifacts/'tls.crt').read_bytes()).decode())
text=text.replace('ghcr.io/otelx/rio/operator:v0.1.0-alpha.3',image)
(artifacts/'install.yaml').write_text(text)
PY
kubectl apply -f "$artifacts/install.yaml"
kubectl rollout status deployment/rio -n rio-system --timeout=180s
