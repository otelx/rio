#!/usr/bin/env bash
# Component integration fixture only: no application demo or OBI installation.
set -Eeuo pipefail
component=$(cd -- "$(dirname -- "$0")/.." && pwd)
root=$component

if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  printf '%s\n' 'Usage: scripts/e2e.sh'
  exit 0
fi
if [[ $# -gt 0 ]]; then
  printf '%s\n' 'Unexpected arguments; use --help.' >&2
  exit 2
fi
for required in kubectl kind docker python3; do
  command -v "$required" >/dev/null || { printf 'Missing command: %s\n' "$required" >&2; exit 1; }
done
trap 'printf "RIO: command failed at line %s\n" "$LINENO" >&2' ERR
cluster=${CLUSTER_NAME:-rio-lifecycle}
if [[ ! "$cluster" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ || ${#cluster} -gt 50 ]]; then
  printf 'Invalid test cluster name: %q\n' "$cluster" >&2
  exit 2
fi
for flag in "${REUSE_CLUSTER:-false}" "${KEEP_CLUSTER:-false}" "${IMAGE_VOLUME_ENABLED:-true}" "${LOAD_IMAGES:-true}"; do
  [[ "$flag" == true || "$flag" == false ]] || { printf 'Expected true or false, got %q\n' "$flag" >&2; exit 2; }
done
artifacts="$root/.artifacts/e2e/$cluster"
mkdir -p "$artifacts/tmp"
export TMPDIR="$artifacts/tmp"
export KUBECONFIG="$artifacts/kubeconfig"
kubectl() { command kubectl --cache-dir="$artifacts/kubectl-cache" "$@"; }
operator_image=${OPERATOR_IMAGE:-ghcr.io/otelx/rio:dev}
payload_image=${PAYLOAD_IMAGE:-ghcr.io/otelx/rio-payload:dev}
node_image=${NODE_IMAGE:-kindest/node:v1.35.0}
create_cluster=true
existing_clusters=$(kind get clusters)
if printf '%s\n' "$existing_clusters" | grep -Fxq -- "$cluster"; then
  if [ "${REUSE_CLUSTER:-false}" != true ] || [ ! -s "$KUBECONFIG" ]; then
    echo "Refusing to replace existing cluster $cluster (explicit REUSE_CLUSTER and its saved kubeconfig are required)" >&2
    exit 1
  fi
  create_cluster=false
fi
cleanup() {
  result=$?
  trap - EXIT
  if [ "$result" -eq 0 ] && [ "${KEEP_CLUSTER:-false}" != true ]; then
    kind delete cluster --name "$cluster" || result=1
  else
    echo "Cluster and evidence retained: $cluster; KUBECONFIG=$KUBECONFIG" >&2
  fi
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
cat > "$artifacts/kind.yaml" <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
featureGates:
  ImageVolume: ${IMAGE_VOLUME_ENABLED:-true}
nodes:
  - role: control-plane
  - role: worker
EOF
if [[ -n "${KIND_CONFIG:-}" ]]; then
  cp -- "$KIND_CONFIG" "$artifacts/kind.yaml"
fi
if [ "$create_cluster" = true ]; then
  cluster_args=(--name "$cluster" --config "$artifacts/kind.yaml" --kubeconfig "$KUBECONFIG" --wait 180s)
  if [[ -z "${KIND_CONFIG:-}" ]]; then cluster_args+=(--image "$node_image"); fi
  kind create cluster "${cluster_args[@]}"
fi
if [[ "${LOAD_IMAGES:-true}" == true ]]; then
  kind load docker-image --name "$cluster" "$operator_image" "$payload_image"
fi
kubectl get nodes -o json > "$artifacts/nodes.json"
python3 - "$artifacts/nodes.json" "${EXPECT_ARCHITECTURES:-}" <<'PY'
import json,sys
nodes=json.load(open(sys.argv[1]))['items']
actual={n['status']['nodeInfo']['architecture'] for n in nodes}
if sys.argv[2]: assert actual==set(sys.argv[2].split(',')), actual
assert all(n['status']['nodeInfo']['operatingSystem']=='linux' for n in nodes)
print('Cluster architectures:', sorted(actual))
PY
"$component/scripts/install.sh" "$operator_image"
if [ "$create_cluster" = false ]; then
  kubectl rollout restart deployment/rio -n rio-system
  kubectl rollout status deployment/rio -n rio-system --timeout=120s
  kubectl delete namespace injector-component-test --ignore-not-found --wait=true --timeout=120s
  kubectl delete instrumentations.rio.otelx.github.io cluster --ignore-not-found --wait=true --timeout=120s
fi
kubectl create namespace injector-component-test
python3 - "$payload_image" > "$artifacts/workloads.json" <<'PY'
import json,sys
image=sys.argv[1]; items=[]
for kind in ('Deployment','StatefulSet','DaemonSet'):
    name=kind.lower()
    template={'metadata':{'labels':{'fixture':name}},'spec':{'terminationGracePeriodSeconds':5,'tolerations':[{'operator':'Exists'}],'containers':[{
        'name':'fixture','image':image,'imagePullPolicy':'IfNotPresent',
        'command':['/bin/sh','-ec','if [ "${OTEL_RIO_TEST_BREAK:-false}" = true ]; then exit 1; fi; exec sleep infinity'],
        'readinessProbe':{'exec':{'command':['/bin/sh','-c','kill -0 1']},'periodSeconds':1}}]}}
    spec={'selector':{'matchLabels':{'fixture':name}},'template':template,'minReadySeconds':1}
    if kind!='DaemonSet':spec['replicas']=1
    if kind=='Deployment':spec['strategy']={'type':'RollingUpdate','rollingUpdate':{'maxUnavailable':0,'maxSurge':1}}
    if kind=='StatefulSet':spec['serviceName']='fixture'
    items.append({'apiVersion':'apps/v1','kind':kind,'metadata':{'name':name,'namespace':'injector-component-test'},'spec':spec})
print(json.dumps({'apiVersion':'v1','kind':'List','items':items}))
PY
kubectl apply -f "$artifacts/workloads.json"
for kind in deployment statefulset daemonset; do
  kubectl rollout status "$kind/$kind" -n injector-component-test --timeout=180s
done
python3 - "$payload_image" > "$artifacts/policy.json" <<'PY'
import json,sys
print(json.dumps({'apiVersion':'rio.otelx.github.io/v1alpha1','kind':'Instrumentation','metadata':{'name':'cluster'},'spec':{
    'image':sys.argv[1],'endpoint':'http://collector.invalid:4318','signals':{'traces':True,'metrics':True,'logs':True},
    'timeoutSeconds':60,'rules':[{'name':'fixture','selector':{'namespaces':['injector-component-test']},'action':'Inject'}]}}))
PY
wait_phase() {
  local wanted=$1
  for attempt in $(seq 1 100); do
    kubectl get instrumentations.rio.otelx.github.io cluster -o json > "$artifacts/status.json"
    if python3 - "$artifacts/status.json" "$wanted" <<'PY'
import json,sys
w=[x for x in json.load(open(sys.argv[1])).get('status',{}).get('workloads',[]) if x['namespace']=='injector-component-test']
sys.exit(0 if len(w)==3 and all(x['phase']==sys.argv[2] for x in w) else 1)
PY
    then cp "$artifacts/status.json" "$artifacts/$wanted.json"; return 0; fi
    sleep 2
  done
  cat "$artifacts/status.json"
  kubectl logs deployment/rio -n rio-system > "$artifacts/operator.log"
  return 1
}
kubectl apply -f "$artifacts/policy.json"
wait_phase Ready
kubectl get pods -n injector-component-test -o json > "$artifacts/injected-pods.json"
python3 - "$artifacts/injected-pods.json" "${EXPECT_DELIVERY:-}" "$artifacts/nodes.json" <<'PY'
import json,sys
pods=[p for p in json.load(open(sys.argv[1]))['items'] if not p['metadata'].get('deletionTimestamp')]
nodes=json.load(open(sys.argv[3]))['items']
assert len(pods)==2+len(nodes), len(pods)
daemon_nodes={p['spec']['nodeName'] for p in pods if p['metadata']['labels']['fixture']=='daemonset'}
assert daemon_nodes=={n['metadata']['name'] for n in nodes}, daemon_nodes
for p in pods:
    a=p['metadata']['annotations']; assert a['rio.otelx.github.io/applied-revision']==a['rio.otelx.github.io/revision']
    if sys.argv[2]: assert a['rio.otelx.github.io/delivery']==sys.argv[2], a
print('Verified injection:', [p['metadata']['annotations']['rio.otelx.github.io/delivery'] for p in pods])
PY
kubectl patch instrumentations.rio.otelx.github.io cluster --type=merge -p '{"spec":{"timeoutSeconds":15,"env":[{"name":"OTEL_RIO_TEST_BREAK","value":"true"}]}}'
wait_phase RolledBack
kubectl get pods -n injector-component-test -o json > "$artifacts/rolled-back-pods.json"
# A blocked revision must remain blocked across a controller restart.
kubectl rollout restart deployment/rio -n rio-system
kubectl rollout status deployment/rio -n rio-system --timeout=120s
wait_phase RolledBack
kubectl patch instrumentations.rio.otelx.github.io cluster --type=merge -p '{"spec":{"timeoutSeconds":60,"retryToken":"corrected","env":[]}}'
wait_phase Ready
kubectl delete instrumentations.rio.otelx.github.io cluster --wait=true --timeout=180s
kubectl get pods -n injector-component-test -o json > "$artifacts/removed-pods.json"
python3 - "$artifacts/removed-pods.json" "$artifacts/nodes.json" <<'PY'
import json,sys
pods=[p for p in json.load(open(sys.argv[1]))['items'] if not p['metadata'].get('deletionTimestamp')]
assert len(pods)==2+len(json.load(open(sys.argv[2]))['items'])
for pod in pods:
    assert not any(v['name']=='rio-payload' for v in pod['spec'].get('volumes',[]))
    assert not any(e['name']=='LD_PRELOAD' for c in pod['spec']['containers'] for e in c.get('env',[]))
print('Verified removal from all three workload types.')
PY
kubectl logs deployment/rio -n rio-system > "$artifacts/operator.log"
kubectl get nodes -o json > "$artifacts/nodes.json"
kubectl get pods -n rio-system -o json > "$artifacts/capability-probes.json"
# Verify HTTP trace export from both glibc and musl application images.
kubectl apply -f "$root/examples/quickstart/app.yaml"
kubectl rollout status deployment/collector -n rio-telemetry --timeout=180s
mapfile -t architectures < <(python3 - "$artifacts/nodes.json" <<'PY'
import json,sys
print('\n'.join(sorted({n['status']['nodeInfo']['architecture'] for n in json.load(open(sys.argv[1]))['items']})))
PY
)
for arch in "${architectures[@]}"; do
  for libc in glibc musl; do
    logs="$artifacts/$arch-$libc"
    mkdir -p "$logs"
    if [[ "$libc" == glibc ]]; then app_image=node:24; else app_image=node:24-alpine; fi
    kubectl patch deployment/hello -n rio-example --type=merge -p "{\"spec\":{\"template\":{\"spec\":{\"nodeSelector\":{\"kubernetes.io/arch\":\"$arch\"}}}}}"
    kubectl set image deployment/hello -n rio-example "hello=$app_image"
    kubectl rollout status deployment/hello -n rio-example --timeout=180s
    # Baseline is uninstrumented before the namespace is selected.
    kubectl get pods -n rio-example -o json > "$logs/hello-before.json"
    python3 - "$logs/hello-before.json" <<'CHECK'
import json, sys
for pod in json.load(open(sys.argv[1]))['items']:
    if pod['metadata'].get('deletionTimestamp'): continue
    assert not any(e['name'] == 'LD_PRELOAD' for c in pod['spec']['containers'] for e in c.get('env', []))
CHECK
    python3 - "$root/examples/quickstart/policy.yaml" "$payload_image" > "$artifacts/quickstart-policy.yaml" <<'RENDER'
from pathlib import Path
import sys
print(Path(sys.argv[1]).read_text().replace('ghcr.io/otelx/rio-payload:v0.1.0-alpha.1', sys.argv[2]))
RENDER
    kubectl apply -f "$artifacts/quickstart-policy.yaml"
    kubectl wait --for=jsonpath='{.status.phase}'=Ready instrumentations.rio.otelx.github.io/cluster --timeout=300s
    kubectl get pods -n rio-example -o json > "$logs/hello-injected.json"
    python3 - "$logs/hello-injected.json" "${EXPECT_DELIVERY:-}" <<'CHECK'
import json,sys
pods=[p for p in json.load(open(sys.argv[1]))['items'] if not p['metadata'].get('deletionTimestamp')]
assert len(pods)==1, len(pods)
if sys.argv[2]: assert pods[0]['metadata']['annotations']['rio.otelx.github.io/delivery']==sys.argv[2]
CHECK
    # Start a fresh Collector so spans from the previous libc cannot pass this check.
    kubectl rollout restart deployment/collector -n rio-telemetry
    kubectl rollout status deployment/collector -n rio-telemetry --timeout=180s
    kubectl exec -n rio-example deployment/hello -- node -e \
      'const http=require("node:http"); for(let i=0;i<5;i++) http.get("http://127.0.0.1:8080/hello",r=>r.resume());'
    telemetry_verified=false
    for ((attempt=0; attempt<30; attempt++)); do
      kubectl logs -n rio-telemetry deployment/collector > "$logs/collector.log"
      if python3 - "$logs/collector.log" <<'CHECK'
from pathlib import Path
import sys
text=Path(sys.argv[1]).read_text()
assert 'service.name: Str(hello)' in text and 'Trace ID' in text and 'Kind           : Server' in text
CHECK
      then telemetry_verified=true; break; fi
      sleep 2
    done
    [[ "$telemetry_verified" == true ]] || { printf 'No HTTP server spans found; see %s\n' "$logs/collector.log" >&2; exit 1; }
    kubectl delete instrumentations.rio.otelx.github.io cluster --wait=true --timeout=180s
  done
done
# Exercise the documented removal path, including webhook and RBAC cleanup.
"$component/scripts/uninstall.sh"
if kubectl get crd instrumentations.rio.otelx.github.io >/dev/null 2>&1; then
  printf 'CRD was not removed\n' >&2; exit 1
fi
printf 'RIO lifecycle and telemetry integration checks passed.\n'
