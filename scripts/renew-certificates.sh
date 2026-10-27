#!/usr/bin/env bash
# Manual renewal for the documented single-replica installation.
set -Eeuo pipefail
umask 077
component=$(cd -- "$(dirname -- "$0")/.." && pwd)
if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  printf '%s\n' 'Usage: scripts/renew-certificates.sh' 'Renews rio-tls in the current context and restarts the webhook.'
  exit 0
fi
[[ $# -eq 0 ]] || { printf 'Unexpected arguments; use --help.\n' >&2; exit 2; }
for required in kubectl openssl python3; do
  command -v "$required" >/dev/null || { printf 'Missing command: %s\n' "$required" >&2; exit 1; }
done
target_context=$(command kubectl config current-context)
mkdir -p "$component/.artifacts/renewal"
artifacts=$(mktemp -d "$component/.artifacts/renewal/run.XXXXXXXX")
forward_pid=''
cleanup() {
  local result=$?
  trap - EXIT
  if [[ -n "$forward_pid" ]]; then kill "$forward_pid" 2>/dev/null || true; wait "$forward_pid" 2>/dev/null || true; fi
  rm -f -- "$artifacts/tls.key" "$artifacts/secret.json"
  if [[ $result -ne 0 ]]; then printf 'Renewal incomplete; retain overlapping CA trust and rerun this command. Evidence: %s\n' "$artifacts" >&2; fi
  exit "$result"
}
trap cleanup EXIT
trap 'printf "RIO: renewal failed at line %s\n" "$LINENO" >&2' ERR
trap 'exit 130' INT
trap 'exit 143' TERM
kubectl() { command kubectl --context="$target_context" --cache-dir="$artifacts/kubectl-cache" "$@"; }
printf 'Renewing RIO certificates in context %s\n' "$target_context"
# Read all prerequisites before making changes; API errors must not create a new installation.
kubectl get secret rio-tls -n rio-system -o json > "$artifacts/secret.json"
kubectl get mutatingwebhookconfiguration rio -o json > "$artifacts/webhook.json"
kubectl get deployment rio -n rio-system -o json > "$artifacts/deployment.json"
python3 - "$artifacts" <<'PY'
import json,pathlib,sys
p=pathlib.Path(sys.argv[1])
d=json.loads((p/'deployment.json').read_text())
assert d['spec'].get('replicas',1)==1, 'Renewal supports the single-replica installation only'
w=json.loads((p/'webhook.json').read_text())['webhooks']
assert len(w)==1 and w[0]['name']=='pods.rio.otelx.github.io', 'Unexpected webhook configuration'
PY
openssl req -x509 -newkey rsa:2048 -nodes -days 365 -keyout "$artifacts/tls.key" -out "$artifacts/tls.crt" \
  -subj '/CN=rio.rio-system.svc' \
  -addext 'subjectAltName=DNS:rio.rio-system.svc,DNS:rio.rio-system.svc.cluster.local'
python3 - "$artifacts" <<'PY'
import base64,json,pathlib,sys
p=pathlib.Path(sys.argv[1]); w=json.loads((p/'webhook.json').read_text())
old=base64.b64decode(w['webhooks'][0]['clientConfig']['caBundle'])
secret=base64.b64decode(json.loads((p/'secret.json').read_text())['data']['tls.crt'])
new=(p/'tls.crt').read_bytes()
for name,cert in [('overlap',old+b'\n'+secret+b'\n'+new),('new',new)]:
    patch=[{'op':'replace','path':'/webhooks/0/clientConfig/caBundle','value':base64.b64encode(cert).decode()}]
    (p/(name+'.json')).write_text(json.dumps(patch))
PY
# Trust both generations before the single replica starts serving the replacement.
kubectl patch mutatingwebhookconfiguration rio --type=json --patch-file="$artifacts/overlap.json"
kubectl create secret tls rio-tls -n rio-system --cert="$artifacts/tls.crt" --key="$artifacts/tls.key" --dry-run=client -o yaml | kubectl apply -f -
kubectl rollout restart deployment/rio -n rio-system
kubectl rollout status deployment/rio -n rio-system --timeout=180s
# Readiness alone does not prove that the new certificate is active. Verify TLS
# against only the new certificate before removing the overlap from the API server.
kubectl port-forward --address=127.0.0.1 deployment/rio -n rio-system :9443 > "$artifacts/forward.log" 2>&1 &
forward_pid=$!
python3 - "$artifacts" <<'PY'
import pathlib,re,socket,ssl,sys,time
p=pathlib.Path(sys.argv[1]); context=ssl.create_default_context(cafile=str(p/'tls.crt'))
for attempt in range(60):
    match=re.search(r'127\.0\.0\.1:(\d+) ->', (p/'forward.log').read_text())
    if match:
        with socket.create_connection(('127.0.0.1',int(match[1])),timeout=5) as sock:
            with context.wrap_socket(sock,server_hostname='rio.rio-system.svc') as tls:
                expected=ssl.PEM_cert_to_DER_cert((p/'tls.crt').read_text())
                assert tls.getpeercert(binary_form=True)==expected, 'Replacement certificate is not active'
        break
    time.sleep(1)
else: raise SystemExit('Timed out waiting for port-forward')
PY
kubectl patch mutatingwebhookconfiguration rio --type=json --patch-file="$artifacts/new.json"
kubectl get mutatingwebhookconfiguration rio -o json > "$artifacts/active-webhook.json"
python3 - "$artifacts" <<'PY'
import base64,json,pathlib,sys
p=pathlib.Path(sys.argv[1]); w=json.loads((p/'active-webhook.json').read_text())
assert base64.b64decode(w['webhooks'][0]['clientConfig']['caBundle'])==(p/'tls.crt').read_bytes(), 'CA bundle update was not retained'
PY
printf 'RIO certificate renewed, activated, and CA bundle updated. Evidence: %s\n' "$artifacts"
