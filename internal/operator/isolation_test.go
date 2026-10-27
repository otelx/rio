package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	admission "k8s.io/api/admission/v1"
	apps "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func admissionResponse(t *testing.T, w *Webhook, pod core.Pod) *admission.AdmissionResponse {
	t.Helper()
	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(admission.AdmissionReview{Request: &admission.AdmissionRequest{UID: "request", Namespace: pod.Namespace, Operation: admission.Create, Resource: meta.GroupVersionResource{Resource: "pods"}, Object: runtime.RawExtension{Raw: raw}}})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	w.ServeHTTP(recorder, httptest.NewRequest("POST", "/mutate", bytes.NewReader(request)))
	var review admission.AdmissionReview
	if err := json.Unmarshal(recorder.Body.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if review.Response == nil || review.Response.UID != "request" {
		t.Fatalf("invalid response: %s", recorder.Body.String())
	}
	return review.Response
}

func TestAdmissionIsolation(t *testing.T) {
	yes := true
	for _, tc := range []struct {
		name, namespace, ownerKind, priority string
		synced, selected, wantLookup         bool
	}{
		{name: "startup", namespace: "app"},
		{name: "unselected missing ReplicaSet", namespace: "other", ownerKind: "ReplicaSet", synced: true},
		{name: "unselected missing Job", namespace: "other", ownerKind: "Job", synced: true},
		{name: "selected missing owner fails open", namespace: "app", ownerKind: "ReplicaSet", synced: true, selected: true, wantLookup: true},
		{name: "unselected workload in selected namespace", namespace: "app", ownerKind: "StatefulSet", synced: true, selected: true},
		{name: "no applicable parent rule", namespace: "app", ownerKind: "Job", synced: true, selected: true},
		{name: "critical priority", namespace: "app", priority: "system-cluster-critical", synced: true, selected: true},
		{name: "operator", namespace: "operator", ownerKind: "ReplicaSet", synced: true, selected: true},
		{name: "kube-system", namespace: "kube-system", ownerKind: "ReplicaSet", synced: true, selected: true},
		{name: "kube-public", namespace: "kube-public", ownerKind: "ReplicaSet", synced: true, selected: true},
		{name: "kube-node-lease", namespace: "kube-node-lease", ownerKind: "ReplicaSet", synced: true, selected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testPolicy()
			p.Spec.Rules[0].Selector = Selector{Namespaces: []string{"app"}, Workloads: []WorkloadSelector{{Kind: "Deployment", Name: "web"}}}
			if !tc.selected {
				p.Spec.Rules[0].Selector.Namespaces = []string{"elsewhere"}
			}
			kube := fake.NewClientset()
			store := &Store{}
			store.Set(Snapshot{Policy: p, Synced: tc.synced})
			pod := core.Pod{ObjectMeta: meta.ObjectMeta{Name: "web", Namespace: tc.namespace}, Spec: core.PodSpec{PriorityClassName: tc.priority}}
			if tc.ownerKind != "" {
				pod.OwnerReferences = []meta.OwnerReference{{Kind: tc.ownerKind, Name: "missing", Controller: &yes}}
			}
			// Unselected Pods must not parse this annotation or modify their spec.
			if !tc.wantLookup {
				pod.Annotations = map[string]string{ConfigAnnotation: "invalid"}
			}
			response := admissionResponse(t, &Webhook{Store: store, Kube: kube, Namespace: "operator"}, pod)
			if !response.Allowed || response.Patch != nil || response.PatchType != nil {
				t.Fatalf("expected unchanged admission: %+v", response)
			}
			if got := len(kube.Actions()); (got > 0) != tc.wantLookup {
				t.Fatalf("owner lookups=%d, wanted lookup=%v", got, tc.wantLookup)
			}
		})
	}
}

func TestAdmissionResolvesSelectedParentsAndHonorsSkip(t *testing.T) {
	yes := true
	for _, kind := range []string{"Deployment", "CronJob"} {
		t.Run(kind, func(t *testing.T) {
			childKind := "ReplicaSet"
			var child runtime.Object = &apps.ReplicaSet{ObjectMeta: meta.ObjectMeta{Name: "child", Namespace: "app", OwnerReferences: []meta.OwnerReference{{Kind: kind, Name: "web", Controller: &yes}}}}
			if kind == "CronJob" {
				childKind = "Job"
				child = &batch.Job{ObjectMeta: meta.ObjectMeta{Name: "child", Namespace: "app", OwnerReferences: []meta.OwnerReference{{Kind: kind, Name: "web", Controller: &yes}}}}
			}
			p := testPolicy()
			p.Spec.Rules[0].Selector.Workloads = []WorkloadSelector{{Kind: kind, Name: "web"}}
			kube := fake.NewClientset(child)
			store := &Store{}
			store.Set(Snapshot{Policy: p, Synced: true})
			w := &Webhook{Store: store, Kube: kube, Namespace: "operator"}
			pod := core.Pod{ObjectMeta: meta.ObjectMeta{Name: "pod", Namespace: "app", OwnerReferences: []meta.OwnerReference{{Kind: childKind, Name: "child", Controller: &yes}}}, Spec: core.PodSpec{Containers: []core.Container{{Name: "app", Image: "app"}}}}
			response := admissionResponse(t, w, pod)
			if !response.Allowed || !bytes.Contains(response.Patch, []byte(VolumeName)) {
				t.Fatalf("selected parent was missed: %+v", response)
			}
			// A parent name cannot be known until the lookup. Unselected
			// parents must still bypass malformed pinned configurations.
			p.Spec.Rules[0].Selector.Workloads[0].Name = "some-other-workload"
			pod.Annotations = map[string]string{ConfigAnnotation: "invalid"}
			response = admissionResponse(t, w, pod)
			if !response.Allowed || response.Patch != nil {
				t.Fatalf("unselected parent reached mutation: %+v", response)
			}
			p.Spec.Rules = append([]Rule{{Name: "skip", Action: "Skip", Selector: Selector{Namespaces: []string{"app"}}}}, p.Spec.Rules...)
			kube.ClearActions()
			response = admissionResponse(t, w, pod)
			if !response.Allowed || response.Patch != nil || len(kube.Actions()) != 0 {
				t.Fatalf("namespace Skip performed work: %+v", response)
			}
		})
	}
}

func isolationController(t *testing.T, p *Policy, objects ...runtime.Object) (*Controller, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	p.APIVersion, p.Kind = Group+"/v1alpha1", "Instrumentation"
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var policy map[string]any
	if err := json.Unmarshal(b, &policy); err != nil {
		t.Fatal(err)
	}
	lists := map[schema.GroupVersionResource]string{PolicyResource: "InstrumentationList"}
	for _, entry := range []struct{ resource, kind string }{{"deployments", "DeploymentList"}, {"statefulsets", "StatefulSetList"}, {"daemonsets", "DaemonSetList"}} {
		lists[schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: entry.resource}] = entry.kind
	}
	objects = append(objects, &unstructured.Unstructured{Object: policy})
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), lists, objects...)
	return &Controller{Dynamic: dyn, Kube: fake.NewClientset(), Store: &Store{}, Namespace: "operator"}, dyn
}

func isolationDeployment(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": name, "namespace": namespace, "uid": name}, "spec": map[string]any{"replicas": int64(0), "template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"name": "app", "image": "app"}}}}}}}
}

func TestWorkloadErrorsDoNotBlockHealthyWorkloads(t *testing.T) {
	for _, failure := range []string{"malformed state", "unreadable template", "update failure"} {
		t.Run(failure, func(t *testing.T) {
			p := testPolicy()
			p.Spec.Rules[0].Selector.Namespaces = []string{"app"}
			bad := isolationDeployment("a-broken", "app")
			unselected := isolationDeployment("a-unselected", "other")
			unselected.SetAnnotations(map[string]string{StateAnnotation: "{"})
			switch failure {
			case "malformed state":
				bad.SetAnnotations(map[string]string{StateAnnotation: "{"})
			case "unreadable template":
				_ = unstructured.SetNestedField(bad.Object, "bad", "spec", "template", "metadata", "annotations")
			}
			healthy := isolationDeployment("z-healthy", "app")
			c, dyn := isolationController(t, p, bad, unselected, healthy)
			if failure == "update failure" {
				dyn.PrependReactor("update", "deployments", func(a clienttesting.Action) (bool, runtime.Object, error) {
					if a.(clienttesting.UpdateAction).GetObject().(meta.Object).GetName() == "a-broken" {
						return true, nil, fmt.Errorf("workload update forbidden")
					}
					return false, nil, nil
				})
			}
			if err := c.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !c.Store.Get().Synced {
				t.Fatal("workload error prevented snapshot publication")
			}
			cr, err := dyn.Resource(PolicyResource).Get(context.Background(), PolicyName, meta.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			phase, _, _ := unstructured.NestedString(cr.Object, "status", "phase")
			if phase != "Degraded" {
				t.Fatalf("phase=%s", phase)
			}
			reports, _, _ := unstructured.NestedSlice(cr.Object, "status", "workloads")
			phases := map[string]string{}
			for _, r := range reports {
				report := r.(map[string]any)
				phases[report["name"].(string)] = report["phase"].(string)
				if report["phase"] == "Error" && report["message"] == "" {
					t.Fatal("error lacks diagnostics")
				}
			}
			if phases["a-broken"] != "Error" || phases["a-unselected"] != "Error" || phases["z-healthy"] != "RollingOut" {
				t.Fatalf("reports=%v", reports)
			}
			if err := c.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			got, err := dyn.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace("app").Get(context.Background(), "z-healthy", meta.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			state, err := readState(got)
			if err != nil || state == nil || state.Phase != "Ready" {
				t.Fatalf("healthy rollout failed to complete: %+v, %v", state, err)
			}
			// The invalid workload must keep deletion pending rather than silently
			// discard unknown instrumentation or remove the finalizer.
			cr, _ = dyn.Resource(PolicyResource).Get(context.Background(), PolicyName, meta.GetOptions{})
			now := meta.Now()
			cr.SetDeletionTimestamp(&now)
			if _, err := dyn.Resource(PolicyResource).Update(context.Background(), cr, meta.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := c.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			cr, _ = dyn.Resource(PolicyResource).Get(context.Background(), PolicyName, meta.GetOptions{})
			if len(cr.GetFinalizers()) == 0 {
				t.Fatal("removed finalizer despite unreadable state")
			}
		})
	}
}

func TestInvalidPolicyAndSyncFailureDisableAdmission(t *testing.T) {
	for _, failure := range []string{"invalid policy", "decode failure", "policy get failure", "list failure", "pod list failure"} {
		t.Run(failure, func(t *testing.T) {
			p := testPolicy()
			c, dyn := isolationController(t, p)
			if err := c.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !c.Store.Get().Synced {
				t.Fatal("valid policy did not synchronize")
			}
			switch failure {
			case "invalid policy", "decode failure":
				cr, _ := dyn.Resource(PolicyResource).Get(context.Background(), PolicyName, meta.GetOptions{})
				var value any = "invalid"
				if failure == "decode failure" {
					value = int64(42)
				}
				_ = unstructured.SetNestedField(cr.Object, value, "spec", "endpoint")
				if _, err := dyn.Resource(PolicyResource).Update(context.Background(), cr, meta.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			case "policy get failure":
				dyn.PrependReactor("get", "instrumentations", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("API unavailable")
				})
			case "list failure":
				dyn.PrependReactor("list", "deployments", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("API unavailable")
				})
			case "pod list failure":
				c.Kube.(*fake.Clientset).PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("API unavailable")
				})
			}
			err := c.Reconcile(context.Background())
			if failure != "invalid policy" && err == nil {
				t.Fatal("expected synchronization error")
			}
			if c.Store.Get().Synced {
				t.Fatal("stale policy retained after failure")
			}
			response := admissionResponse(t, &Webhook{Store: c.Store, Kube: c.Kube, Namespace: "operator"}, core.Pod{ObjectMeta: meta.ObjectMeta{Name: "web", Namespace: "app"}})
			if !response.Allowed || response.Patch != nil {
				t.Fatalf("failure blocked unrelated admission: %+v", response)
			}
			if failure == "invalid policy" {
				cr, _ := dyn.Resource(PolicyResource).Get(context.Background(), PolicyName, meta.GetOptions{})
				phase, _, _ := unstructured.NestedString(cr.Object, "status", "phase")
				message, _, _ := unstructured.NestedString(cr.Object, "status", "message")
				if phase != "Invalid" || !strings.Contains(message, "endpoint") {
					t.Fatalf("invalid policy not reported: %v", cr.Object["status"])
				}
			}
		})
	}
}

func TestReconcileExcludesCriticalWorkloads(t *testing.T) {
	objects := []runtime.Object{}
	for _, ns := range []string{"operator", "kube-system", "kube-public", "kube-node-lease"} {
		dep := isolationDeployment("excluded", ns)
		dep.SetAnnotations(map[string]string{StateAnnotation: "{"})
		objects = append(objects, dep)
	}
	for _, priority := range []string{"system-node-critical", "system-cluster-critical"} {
		dep := isolationDeployment(priority, "app")
		_ = unstructured.SetNestedField(dep.Object, priority, "spec", "template", "spec", "priorityClassName")
		dep.SetAnnotations(map[string]string{StateAnnotation: "{"})
		objects = append(objects, dep)
	}
	c, dyn := isolationController(t, testPolicy(), objects...)
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	cr, err := dyn.Resource(PolicyResource).Get(context.Background(), PolicyName, meta.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reports, _, _ := unstructured.NestedSlice(cr.Object, "status", "workloads")
	if len(reports) != 0 {
		t.Fatalf("critical workload was reconciled: %v", reports)
	}
	for _, action := range dyn.Actions() {
		if action.GetVerb() == "update" && action.GetResource().Resource != "instrumentations" {
			t.Fatalf("critical workload was updated: %v", action)
		}
	}
}
