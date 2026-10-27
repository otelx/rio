#!/usr/bin/env bash
# Sourced by e2e.sh after upgrading, with its private context and fixture policy.
# shellcheck disable=SC2154
# Variables below are owned by the caller; this file is not a standalone CLI.
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  printf 'Run scripts/e2e-upgrade.sh; this helper must be sourced by e2e.sh.\n' >&2
  exit 2
fi
for certificate_case in expiring expired; do
  cert_dir="$artifacts/certificate-$certificate_case"
  mkdir -p "$cert_dir"
  openssl req -new -newkey rsa:2048 -nodes -keyout "$cert_dir/tls.key" -out "$cert_dir/request.pem" -subj '/CN=rio.rio-system.svc'
  touch "$cert_dir/index"
  printf '01\n' > "$cert_dir/serial"
  cat > "$cert_dir/ca.cnf" <<CONF
[ca]
default_ca=fixture
[fixture]
database=$cert_dir/index
serial=$cert_dir/serial
new_certs_dir=$cert_dir
default_md=sha256
policy=names
x509_extensions=server
[names]
commonName=supplied
[server]
subjectAltName=DNS:rio.rio-system.svc,DNS:rio.rio-system.svc.cluster.local
basicConstraints=critical,CA:TRUE
keyUsage=critical,digitalSignature,keyEncipherment,keyCertSign
CONF
  certificate_end=$(python3 - "$certificate_case" <<'PY'
from datetime import datetime,timedelta,timezone
import sys
print((datetime.now(timezone.utc)+timedelta(days=1 if sys.argv[1]=='expiring' else -1)).strftime('%Y%m%d%H%M%SZ'))
PY
)
  openssl ca -selfsign -batch -config "$cert_dir/ca.cnf" -keyfile "$cert_dir/tls.key" -in "$cert_dir/request.pem" \
    -startdate 20200101000000Z -enddate "$certificate_end" -notext -out "$cert_dir/tls.crt"
  if openssl x509 -in "$cert_dir/tls.crt" -noout -checkend 2592000; then
    printf 'Fixture certificate unexpectedly valid for more than 30 days\n' >&2; exit 1
  fi
  kubectl create secret tls rio-tls -n rio-system --cert="$cert_dir/tls.crt" --key="$cert_dir/tls.key" --dry-run=client -o yaml | kubectl apply -f -
  python3 - "$cert_dir/tls.crt" > "$cert_dir/patch.json" <<'PY'
import base64,json,sys
print(json.dumps([{'op':'replace','path':'/webhooks/0/clientConfig/caBundle','value':base64.b64encode(open(sys.argv[1],'rb').read()).decode()},
                  {'op':'replace','path':'/webhooks/0/failurePolicy','value':'Fail'}]))
PY
  kubectl patch mutatingwebhookconfiguration rio --type=json --patch-file="$cert_dir/patch.json"
  kubectl rollout restart deployment/rio -n rio-system
  kubectl rollout status deployment/rio -n rio-system --timeout=180s
  # Use fail-closed admission only inside this isolated cluster so an expired
  # certificate cannot silently pass as a healthy but uninstrumented Pod.
  python3 - "$payload_image" > "$cert_dir/pod.json" <<'PY'
import json,sys
print(json.dumps({'apiVersion':'v1','kind':'Pod','metadata':{'name':'certificate-admission','namespace':'injector-component-test'},
                  'spec':{'containers':[{'name':'fixture','image':sys.argv[1],'command':['sleep','infinity']}]}}))
PY
  certificate_observed=false
  for ((attempt=0; attempt<60; attempt++)); do
    if kubectl create --dry-run=server -f "$cert_dir/pod.json" -o json > "$cert_dir/before.json" 2> "$cert_dir/before.err"; then
      if [[ "$certificate_case" == expiring ]]; then certificate_observed=true; break; fi
    elif [[ "$certificate_case" == expired ]] && python3 - "$cert_dir/before.err" <<'PY'
import pathlib,sys
text=pathlib.Path(sys.argv[1]).read_text().lower()
sys.exit(0 if 'x509: certificate has expired or is not yet valid' in text and 'is after' in text else 1)
PY
    then certificate_observed=true; break; fi
    sleep 1
  done
  [[ "$certificate_observed" == true ]] || { cat "$cert_dir/before.err" >&2; exit 1; }
  "$component/scripts/renew-certificates.sh"
  admission_verified=false
  for ((attempt=0; attempt<60; attempt++)); do
    if kubectl create --dry-run=server -f "$cert_dir/pod.json" -o json > "$cert_dir/after.json" 2> "$cert_dir/after.err"; then
      if python3 - "$cert_dir/after.json" <<'PY'
import json,sys
p=json.load(open(sys.argv[1])); assert p['metadata']['annotations']['rio.otelx.github.io/applied-revision']
assert any(v['name']=='rio-payload' for v in p['spec']['volumes'])
PY
      then admission_verified=true; break; fi
    fi
    sleep 1
  done
  [[ "$admission_verified" == true ]] || { cat "$cert_dir/after.err" >&2; exit 1; }
  kubectl get secret rio-tls -n rio-system -o jsonpath='{.data.tls\.crt}' > "$cert_dir/renewed-certificate.b64"
  kubectl get mutatingwebhookconfiguration rio -o json > "$cert_dir/renewed-webhook.json"
  python3 - "$cert_dir" <<'PY'
import json,pathlib,sys
p=pathlib.Path(sys.argv[1]); webhook=json.loads((p/'renewed-webhook.json').read_text())
assert (p/'renewed-certificate.b64').read_text()==webhook['webhooks'][0]['clientConfig']['caBundle']
PY
  kubectl patch mutatingwebhookconfiguration rio --type=json -p '[{"op":"replace","path":"/webhooks/0/failurePolicy","value":"Ignore"}]'
  rm -f -- "$cert_dir/tls.key"
done
