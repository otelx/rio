package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	admission "k8s.io/api/admission/v1"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	versioninfo "k8s.io/apimachinery/pkg/version"
	discoveryfake "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func testPolicy() *Policy {
	return &Policy{ObjectMeta: meta.ObjectMeta{Name: PolicyName}, Spec: Spec{Image: "payload:v1", Endpoint: "http://collector:4318", Signals: Signals{true, true, true}, TimeoutSeconds: 30,
		Rules: []Rule{{Name: "all", Selector: Selector{Namespaces: []string{"*"}}, Action: "Inject"}}}}
}

func TestOrderedRulesAndSignalOverrides(t *testing.T) {
	p := testPolicy()
	no := false
	p.Spec.Rules = append([]Rule{
		{Name: "skip", Selector: Selector{Namespaces: []string{"observability"}}, Action: "Skip"},
		{Name: "checkout", Selector: Selector{Namespaces: []string{"shop"}, Workloads: []WorkloadSelector{{Kind: "Deployment", Name: "checkout"}}}, Action: "Inject", Signals: SignalOverrides{Metrics: &no}, Env: []core.EnvVar{{Name: "OTEL_SERVICE_NAME", Value: "checkout"}}},
	}, p.Spec.Rules...)
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.Resolve("observability", "Deployment", "collector").Enabled {
		t.Fatal("skip must win")
	}
	c := p.Resolve("shop", "Deployment", "checkout")
	if c.Signals.Metrics || !c.Signals.Traces || !c.Signals.Logs {
		t.Fatalf("bad override: %+v", c.Signals)
	}
	if !p.Resolve("future-namespace", "StatefulSet", "db").Enabled {
		t.Fatal("wildcard must cover future namespaces")
	}
	if !p.Resolve("shop", "DaemonSet", "checkout").Signals.Metrics {
		t.Fatal("kind restriction was ignored")
	}
	p.Spec.Env = []core.EnvVar{{Name: "OTEL_TRACES_EXPORTER", Value: "none"}}
	if p.Validate() == nil {
		t.Fatal("conflicting signal override accepted")
	}
}

func TestRolloutRollbackRetryAndRemoval(t *testing.T) {
	p := testPolicy()
	wanted := p.Resolve("app", "Deployment", "web")
	now := time.Now()
	s, changed := Advance(nil, wanted, Health{}, now)
	if !changed || s.Phase != "RollingOut" {
		t.Fatalf("initial: %+v", s)
	}
	s, changed = Advance(&s, wanted, Health{Ready: true}, now.Add(time.Second))
	if changed || s.Phase != "Ready" {
		t.Fatalf("commit: %+v", s)
	}
	v2 := wanted
	v2.Image = "payload:v2"
	s, _ = Advance(&s, v2, Health{Ready: true}, now.Add(2*time.Second))
	s, changed = Advance(&s, v2, Health{Message: "unready"}, now.Add(time.Minute))
	if !changed || s.Phase != "RollingBack" || s.Current.Image != "payload:v1" || s.FailedRevision != v2.Revision() {
		t.Fatalf("rollback: %+v", s)
	}
	s, _ = Advance(&s, v2, Health{Ready: true}, now.Add(61*time.Second))
	s, changed = Advance(&s, v2, Health{Ready: true}, now.Add(62*time.Second))
	if changed || s.Phase != "RolledBack" {
		t.Fatal("failed configuration retried automatically")
	}
	v2.RetryToken = "retry"
	s, changed = Advance(&s, v2, Health{}, now.Add(63*time.Second))
	if !changed {
		t.Fatal("explicit retry ignored")
	}
	s, _ = Advance(&s, v2, Health{Ready: true}, now.Add(64*time.Second))
	off := (*Policy)(nil).Resolve("app", "Deployment", "web")
	s, changed = Advance(&s, off, Health{}, now.Add(65*time.Second))
	if !changed || s.Current.Enabled {
		t.Fatal("removal did not start")
	}
	s, _ = Advance(&s, off, Health{Ready: true}, now.Add(66*time.Second))
	if s.Phase != "Disabled" {
		t.Fatal(s.Phase)
	}
}

func TestFirstFailureRollsBackToUninstrumentedAndSurvivesSerialization(t *testing.T) {
	wanted := testPolicy().Resolve("app", "Deployment", "web")
	now := time.Now()
	s, _ := Advance(nil, wanted, Health{}, now)
	b, _ := json.Marshal(s)
	var restored State
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatal(err)
	}
	s, changed := Advance(&restored, wanted, Health{Failed: true, Message: "crashing"}, now.Add(time.Second))
	if !changed || s.Current.Enabled || s.Phase != "RollingBack" {
		t.Fatalf("%+v", s)
	}
	s, _ = Advance(&s, wanted, Health{Ready: true}, now.Add(2*time.Second))
	off := Config{TimeoutSeconds: 60, RetryToken: "removed"}
	s, changed = Advance(&s, off, Health{Ready: true}, now.Add(3*time.Second))
	if changed || s.Phase != "Disabled" {
		t.Fatal("removing already rolled-back injection should settle without another rollout")
	}
	s, changed = Advance(&s, Config{TimeoutSeconds: 300}, Health{}, now.Add(4*time.Second))
	if changed || s.Phase != "Disabled" {
		t.Fatal("CR disappearance caused an extra rollout")
	}
}

func TestMutateBothModesAndDisabledSignals(t *testing.T) {
	for _, native := range []bool{true, false} {
		t.Run(map[bool]string{true: "native", false: "copy"}[native], func(t *testing.T) {
			cfg := testPolicy().Resolve("app", "Deployment", "web")
			cfg.Signals = Signals{Traces: true}
			pod := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "web", Namespace: "app"}, Spec: core.PodSpec{Containers: []core.Container{{Name: "app", Env: []core.EnvVar{{Name: "APP_SETTING", Value: "keep"}, {Name: "LD_PRELOAD", Value: "/existing.so"}}}, {Name: "sidecar"}}, InitContainers: []core.Container{{Name: "existing", Image: "existing"}}}}
			if err := Mutate(pod, cfg, native, "web"); err != nil {
				t.Fatal(err)
			}
			if native != (pod.Spec.Volumes[0].Image != nil) {
				t.Fatal("wrong volume type")
			}
			wantInit := 2
			if native {
				wantInit = 1
			}
			if len(pod.Spec.InitContainers) != wantInit {
				t.Fatal("wrong init containers")
			}
			if len(pod.Spec.InitContainers[0].Env) != 0 {
				t.Fatal("instrumented an application init container")
			}
			env := map[string]string{}
			for _, e := range pod.Spec.Containers[0].Env {
				env[e.Name] = e.Value
			}
			if env["OTEL_METRICS_EXPORTER"] != "none" || env["OTEL_LOGS_EXPORTER"] != "none" || env["OTEL_TRACES_EXPORTER"] != "otlp" || env["APP_SETTING"] != "keep" || !strings.HasSuffix(env["LD_PRELOAD"], " /existing.so") {
				t.Fatalf("bad environment: %v", env)
			}
			before := pod.DeepCopy()
			if err := Mutate(pod, cfg, native, "web"); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, pod) {
				t.Fatal("not idempotent")
			}
		})
	}
}

func TestAdmissionUsesPinnedRollbackRatherThanNewPolicy(t *testing.T) {
	p := testPolicy()
	stable := p.Resolve("app", "Pod", "web")
	p.Spec.Image = "broken:v2"
	p.Spec.Env = []core.EnvVar{{Name: "OTEL_RIO_TEST_BREAK", Value: "true"}}
	p.Spec.RetryToken = "newer-policy"
	encoded, _ := json.Marshal(stable)
	pod := core.Pod{ObjectMeta: meta.ObjectMeta{Name: "web", Namespace: "app", Annotations: map[string]string{ConfigAnnotation: string(encoded), RevisionAnnotation: stable.Revision()}}, Spec: core.PodSpec{Containers: []core.Container{{Name: "app", Image: "app"}}}}
	raw, _ := json.Marshal(pod)
	review := admission.AdmissionReview{Request: &admission.AdmissionRequest{UID: "request", Namespace: "app", Operation: admission.Create, Resource: meta.GroupVersionResource{Resource: "pods"}, Object: runtime.RawExtension{Raw: raw}}}
	request, _ := json.Marshal(review)
	store := &Store{}
	store.Set(Snapshot{Policy: p, Synced: true})
	w := &Webhook{Store: store, Kube: fake.NewClientset(), Namespace: "operator"}
	recorder := httptest.NewRecorder()
	w.ServeHTTP(recorder, httptest.NewRequest("POST", "/mutate", bytes.NewReader(request)))
	var response admission.AdmissionReview
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Response == nil || !response.Response.Allowed {
		t.Fatal(recorder.Body.String())
	}
	if !bytes.Contains(response.Response.Patch, []byte(`"image":"payload:v1"`)) || bytes.Contains(response.Response.Patch, []byte(`"image":"broken:v2"`)) {
		t.Fatalf("did not honor rollback: %s", response.Response.Patch)
	}
	if bytes.Contains(response.Response.Patch, []byte("OTEL_RIO_TEST_BREAK")) {
		t.Fatal("rollback inherited environment overrides from the failing policy")
	}
}

func TestRuntimeCompatibility(t *testing.T) {
	for _, tc := range []struct {
		runtime, kubelet string
		want             bool
	}{{"containerd://2.1.0", "v1.33.2", true}, {"containerd://2.0.9", "v1.35.0", false}, {"cri-o://1.31.0", "v1.31.0", true}, {"unknown://9.0.0", "v1.36.0", false}, {"containerd://2.2.0", "v1.30.0", false}} {
		n := core.Node{Status: core.NodeStatus{NodeInfo: core.NodeSystemInfo{ContainerRuntimeVersion: tc.runtime, KubeletVersion: tc.kubelet}}}
		if runtimeSupportsImageVolume(n) != tc.want {
			t.Fatal(tc)
		}
	}
}

func TestMixedRuntimesUseCopyWithoutProbingOrChangingNodes(t *testing.T) {
	compatible := &core.Node{ObjectMeta: meta.ObjectMeta{Name: "new", UID: "new-node"}, Status: core.NodeStatus{NodeInfo: core.NodeSystemInfo{OperatingSystem: "linux", ContainerRuntimeVersion: "containerd://2.1.0", KubeletVersion: "v1.35.0"}}}
	old := compatible.DeepCopy()
	old.Name, old.UID = "old", "old-node"
	old.Status.NodeInfo.ContainerRuntimeVersion = "containerd://2.0.9"
	client := fake.NewClientset(compatible, old)
	client.Discovery().(*discoveryfake.FakeDiscovery).FakedServerVersion = &versioninfo.Info{GitVersion: "v1.35.0"}
	c := &Capabilities{Kube: client, Namespace: "operator"}
	native, pending, err := c.Check(context.Background(), "payload:v1")
	if err != nil || native || pending {
		t.Fatalf("mixed nodes must use the copy fallback: native=%v pending=%v err=%v", native, pending, err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" && action.GetVerb() != "list" {
			t.Fatalf("mixed cluster detection must be read-only: %v", action)
		}
	}
}

func TestMixedArchitecturesRequireEveryNodeProbe(t *testing.T) {
	ctx := context.Background()
	amd64 := &core.Node{ObjectMeta: meta.ObjectMeta{Name: "amd64", UID: "amd64-node"}, Status: core.NodeStatus{NodeInfo: core.NodeSystemInfo{OperatingSystem: "linux", Architecture: "amd64", ContainerRuntimeVersion: "containerd://2.1.0", KubeletVersion: "v1.35.0"}}}
	arm64 := amd64.DeepCopy()
	arm64.Name, arm64.UID = "arm64", "arm64-node"
	arm64.Status.NodeInfo.Architecture = "arm64"
	client := fake.NewClientset(amd64, arm64)
	client.Discovery().(*discoveryfake.FakeDiscovery).FakedServerVersion = &versioninfo.Info{GitVersion: "v1.35.0"}
	c := &Capabilities{Kube: client, Namespace: "operator"}
	check := func(wantNative, wantPending bool) {
		t.Helper()
		native, pending, err := c.Check(ctx, "payload:multiarch")
		if err != nil || native != wantNative || pending != wantPending {
			t.Fatalf("native=%v pending=%v err=%v; want native=%v pending=%v", native, pending, err, wantNative, wantPending)
		}
	}
	check(false, true)
	probes, err := client.CoreV1().Pods("operator").List(ctx, meta.ListOptions{})
	if err != nil || len(probes.Items) != 2 {
		t.Fatalf("expected a probe on each architecture: %v %v", probes, err)
	}
	// The fake client does not assign the API server's creation timestamp.
	for i := range probes.Items {
		probes.Items[i].CreationTimestamp = meta.Now()
		if _, err := client.CoreV1().Pods("operator").Update(ctx, &probes.Items[i], meta.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for _, probe := range probes.Items {
		seen[probe.Spec.NodeName] = true
		if probe.Spec.Volumes[0].Image.Reference != "payload:multiarch" {
			t.Fatal("probe must let each node resolve the same multiarch reference")
		}
		probe.Status.Phase = core.PodSucceeded
		if _, err := client.CoreV1().Pods("operator").UpdateStatus(ctx, &probe, meta.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		check(len(seen) == 2, len(seen) != 2)
	}
	if !seen["amd64"] || !seen["arm64"] {
		t.Fatal("missing architecture probe")
	}
	// A loader failure on either architecture must select the copy fallback.
	failed := probes.Items[0].DeepCopy()
	failed.Status.Phase = core.PodFailed
	if _, err := client.CoreV1().Pods("operator").UpdateStatus(ctx, failed, meta.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	check(false, false)
	for _, action := range client.Actions() {
		if action.GetResource().Resource == "nodes" && action.GetVerb() != "list" {
			t.Fatalf("capability detection changed node scheduling: %v", action)
		}
	}
}

func TestHealthDoesNotAcceptOldReadyPodsDuringSurge(t *testing.T) {
	current := testPolicy().Resolve("app", "DaemonSet", "agent")
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"uid": "workload", "generation": int64(2)},
		"status":   map[string]any{"observedGeneration": int64(2), "desiredNumberScheduled": int64(1), "updatedNumberScheduled": int64(1), "numberAvailable": int64(1)},
	}}
	old := core.Pod{ObjectMeta: meta.ObjectMeta{Annotations: map[string]string{OwnerAnnotation: "workload", RevisionAnnotation: "old"}}, Status: core.PodStatus{Conditions: []core.PodCondition{{Type: core.PodReady, Status: core.ConditionTrue}}}}
	candidate := core.Pod{ObjectMeta: meta.ObjectMeta{Annotations: map[string]string{OwnerAnnotation: "workload", RevisionAnnotation: current.Revision()}}}
	if h := workloadHealth(obj, "DaemonSet", []core.Pod{old, candidate}, current); h.Ready || h.Failed {
		t.Fatalf("unready replacement must keep rollout pending: %+v", h)
	}
	candidate.Status.Conditions = old.Status.Conditions
	if h := workloadHealth(obj, "DaemonSet", []core.Pod{old, candidate}, current); !h.Ready {
		t.Fatalf("ready replacement should complete the rollout: %+v", h)
	}
}

func TestReconcilePersistsTemplateAndFinalizerWithoutChangingApplication(t *testing.T) {
	p := testPolicy()
	p.APIVersion = Group + "/v1alpha1"
	p.Kind = "Instrumentation"
	b, _ := json.Marshal(p)
	var policy map[string]any
	_ = json.Unmarshal(b, &policy)
	dep := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "web", "namespace": "app", "uid": "workload-1"}, "spec": map[string]any{"replicas": int64(1), "template": map[string]any{"metadata": map[string]any{"labels": map[string]any{"app": "web"}}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app", "image": "my-app:v1"}}}}}}}
	lists := map[schema.GroupVersionResource]string{PolicyResource: "InstrumentationList"}
	for _, name := range []string{"deployments", "statefulsets", "daemonsets"} {
		lists[schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: name}] = map[string]string{"deployments": "DeploymentList", "statefulsets": "StatefulSetList", "daemonsets": "DaemonSetList"}[name]
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), lists, &unstructured.Unstructured{Object: policy}, dep)
	c := &Controller{Kube: fake.NewClientset(), Dynamic: dyn, Namespace: "operator", Store: &Store{}}
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := dyn.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace("app").Get(context.Background(), "web", meta.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	state, err := readState(got)
	if err != nil || state == nil || state.Phase != "RollingOut" {
		t.Fatalf("state: %+v %v", state, err)
	}
	annotation, _, _ := unstructured.NestedString(got.Object, "spec", "template", "metadata", "annotations", RevisionAnnotation)
	if annotation != state.Current.Revision() {
		t.Fatal("template and state revisions differ")
	}
	before, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "containers")
	after, _, _ := unstructured.NestedSlice(got.Object, "spec", "template", "spec", "containers")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("application container modified")
	}
	cr, _ := dyn.Resource(PolicyResource).Get(context.Background(), PolicyName, meta.GetOptions{})
	if len(cr.GetFinalizers()) != 1 {
		t.Fatal("missing cleanup finalizer")
	}
	// Reconciliation before readiness must not keep generating new revisions.
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStatefulRecoveryDeletesOnlyUnreadyFailedRevision(t *testing.T) {
	yes := true
	uid := types.UID("workload")
	obj := &unstructured.Unstructured{}
	obj.SetUID(uid)
	obj.SetNamespace("app")
	makePod := func(name, revision string, ready bool) core.Pod {
		p := core.Pod{ObjectMeta: meta.ObjectMeta{Name: name, Namespace: "app", UID: types.UID(name), Annotations: map[string]string{OwnerAnnotation: string(uid), RevisionAnnotation: revision}, OwnerReferences: []meta.OwnerReference{{Kind: "StatefulSet", UID: uid, Controller: &yes}}}}
		if ready {
			p.Status.Conditions = []core.PodCondition{{Type: core.PodReady, Status: core.ConditionTrue}}
		}
		return p
	}
	pods := []core.Pod{makePod("failed", "bad", false), makePod("healthy", "bad", true), makePod("old", "good", false)}
	kube := fake.NewClientset(&pods[0], &pods[1], &pods[2])
	c := &Controller{Kube: kube}
	if err := c.recoverStatefulSet(context.Background(), obj, pods, State{FailedRevision: "bad"}); err != nil {
		t.Fatal(err)
	}
	remaining, _ := kube.CoreV1().Pods("app").List(context.Background(), meta.ListOptions{})
	if len(remaining.Items) != 2 {
		t.Fatal("unexpected deletion count")
	}
	for _, p := range remaining.Items {
		if p.Name == "failed" {
			t.Fatal("failed candidate not deleted")
		}
	}
}
