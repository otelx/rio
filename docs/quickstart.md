# Quickstart

Run this example in a new, disposable kind cluster. It uses a Node.js HTTP service
and an OpenTelemetry Collector with the debug exporter. Successful completion
shows HTTP spans produced by a language agent loaded through the Injector.

## Prerequisites

Install Docker, [kind](https://kind.sigs.k8s.io/), kubectl, Git, Bash, Python 3,
and OpenSSL. Docker must be running. The example uses Linux containers and
Kubernetes 1.35. The release currently targets Linux amd64; see
[compatibility](compatibility.md) for architecture limits.

Cluster creation, installation, and application rollout require cluster-admin
access. Network access is needed to download container images.

## 1. Create the cluster

```bash
git clone --branch v0.1.0-alpha.1 https://github.com/otelx/rio.git
cd rio
mkdir -p .artifacts/tmp
export TMPDIR="$PWD/.artifacts/tmp"
export KUBECONFIG="$PWD/.artifacts/quickstart.kubeconfig"
kind create cluster --name rio-quickstart --image kindest/node:v1.35.0 \
  --config examples/quickstart/kind.yaml --kubeconfig "$KUBECONFIG" --wait 120s
kubectl config current-context
```

Keep `KUBECONFIG` set in this terminal for the following commands. RIO's install
and uninstall scripts print and use the context selected when they start.

## 2. Install RIO and the example

```bash
./scripts/install.sh
kubectl apply -f examples/quickstart/app.yaml
kubectl rollout status deployment/collector -n rio-telemetry --timeout=180s
kubectl rollout status deployment/hello -n rio-example --timeout=180s
```

At this point the application has no language agent. Its image contains only
Node.js; its server source is supplied by a ConfigMap and contains no
OpenTelemetry API calls.

## 3. Select the application namespace

```bash
kubectl apply -f examples/quickstart/policy.yaml
kubectl wait --for=jsonpath='{.status.phase}'=Ready \
  instrumentations.rio.otelx.github.io/cluster --timeout=300s
kubectl get instrumentations.rio.otelx.github.io cluster -o yaml
```

RIO replaces the existing application Pod using its Deployment's rolling-update
settings. The policy selects `rio-example`; the Collector runs in a different,
unselected namespace. An agent rollout does not require an application rebuild.

Inspect the delivery method:

```bash
kubectl get pods -n rio-example \
  -o 'custom-columns=NAME:.metadata.name,DELIVERY:.metadata.annotations.rio\.otelx\.github\.io/delivery'
```

## 4. Generate and inspect traces

Generate requests from the application container without adding a test image:

```bash
kubectl exec -n rio-example deployment/hello -- node -e \
  'const http=require("node:http"); for(let i=0;i<5;i++) http.get("http://127.0.0.1:8080/hello",r=>r.resume());'
kubectl logs -n rio-telemetry deployment/collector --since=2m
```

Allow a few seconds for export. The Collector debug output should include
`service.name` with the value `hello`, an HTTP server span, and a trace ID.
The policy enables traces and disables metrics and logs to keep this example
focused. RIO's `Ready` status describes workload health and injection, not proof
that a backend received telemetry; the Collector output supplies that evidence.

## 5. Remove instrumentation and clean up

Delete the policy while RIO is still running. Its finalizer waits for selected
managed workloads to return to uninstrumented Pods:

```bash
kubectl delete instrumentations.rio.otelx.github.io cluster --timeout=300s
./scripts/uninstall.sh
kind delete cluster --name rio-quickstart
unset KUBECONFIG
```

If removal times out, inspect the policy status and operator logs before retrying.
Do not remove the finalizer to force a normal uninstall.

## Troubleshooting

- **Policy is not Ready:** inspect `kubectl get instrumentations.rio.otelx.github.io cluster -o yaml`
  and `kubectl logs -n rio-system deployment/rio`.
- **ImagePullBackOff:** verify registry access and the exact release tags in the policy.
- **Healthy Pod but no spans:** generate fresh HTTP requests, inspect application
  logs for agent startup, and check the OTLP endpoint. Signal support depends on
  the language agent and the libraries used by the application.
- **InjectionMissing:** inspect the webhook and operator availability. Admission
  fails open on webhook outages; request a new revision with `spec.retryToken`
  after correcting the cause.

For a source build, use the commands in [development](development.md); do not
substitute mutable image tags when testing updates or rollback.
