package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	admission "k8s.io/api/admission/v1"
	apps "k8s.io/api/apps/v1"
	auth "k8s.io/api/authentication/v1"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func testAuthority() *Authority { return &Authority{key: bytes.Repeat([]byte{42}, 32)} }

func ownerRef(kind, name, uid string) meta.OwnerReference {
	yes := true
	version := "apps/v1"
	if kind == "Job" || kind == "CronJob" {
		version = "batch/v1"
	}
	return meta.OwnerReference{APIVersion: version, Kind: kind, Name: name, UID: types.UID(uid), Controller: &yes}
}

func admissionResult(t *testing.T, w *Webhook, pod *core.Pod, user string) *admission.AdmissionResponse {
	t.Helper()
	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatal(err)
	}
	review := admission.AdmissionReview{Request: &admission.AdmissionRequest{UID: "request", Namespace: pod.Namespace, Operation: admission.Create, Resource: meta.GroupVersionResource{Resource: "pods"}, UserInfo: auth.UserInfo{Username: user}, Object: runtime.RawExtension{Raw: raw}}}
	request, _ := json.Marshal(review)
	recorder := httptest.NewRecorder()
	w.ServeHTTP(recorder, httptest.NewRequest("POST", "/mutate", bytes.NewReader(request)))
	var response admission.AdmissionReview
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Response == nil {
		t.Fatal(recorder.Body.String())
	}
	return response.Response
}

func snapshotFixture(t *testing.T, kind string, cfg Config) (*Webhook, *core.Pod, *unstructured.Unstructured) {
	t.Helper()
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": kind, "metadata": map[string]any{"name": "web", "namespace": "app", "uid": "workload"}}}
	a := testAuthority()
	if err := a.writeState(obj, State{Current: cfg, Stable: cfg, Phase: "Ready"}, true); err != nil {
		t.Fatal(err)
	}
	annotations, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "annotations")
	pod := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "web-pod", Namespace: "app", Annotations: annotations}, Spec: core.PodSpec{Containers: []core.Container{{Name: "app", Image: "application"}}}}
	var root runtime.Object
	switch kind {
	case "Deployment":
		root = &apps.Deployment{}
	case "StatefulSet":
		root = &apps.StatefulSet{}
	case "DaemonSet":
		root = &apps.DaemonSet{}
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, root); err != nil {
		t.Fatal(err)
	}
	objects := []runtime.Object{root}
	ref := ownerRef(kind, "web", "workload")
	if kind == "Deployment" {
		objects = append(objects, &apps.ReplicaSet{ObjectMeta: meta.ObjectMeta{Name: "web-rs", Namespace: "app", UID: "replicaset", OwnerReferences: []meta.OwnerReference{ref}}})
		ref = ownerRef("ReplicaSet", "web-rs", "replicaset")
	}
	pod.OwnerReferences = []meta.OwnerReference{ref}
	store := &Store{}
	store.Set(Snapshot{Policy: testPolicy(), Synced: true})
	return &Webhook{Authority: a, Kube: fake.NewClientset(objects...), Store: store, Namespace: "operator"}, pod, obj
}

func TestAdmissionAuthenticatedSnapshotsAndRollback(t *testing.T) {
	for _, kind := range []string{"Deployment", "StatefulSet", "DaemonSet"} {
		t.Run(kind, func(t *testing.T) {
			stable := testPolicy().Resolve("app", kind, "web")
			w, pod, obj := snapshotFixture(t, kind, stable)
			failing := testPolicy()
			failing.Spec.Image = "ghcr.io/otelx/rio/instrumentation:broken"
			failing.Spec.Env = []core.EnvVar{{Name: "OTEL_RIO_TEST_BREAK", Value: "true"}}
			failing.Spec.RetryToken = "new-policy"
			// Exercise persisted recovery decisions rather than only a hand-built pin.
			now := time.Now()
			initial := State{Current: stable, Stable: stable, Phase: "Ready"}
			candidate, _ := Advance(&initial, failing.Resolve("app", kind, "web"), Health{}, now)
			recovered, changed := Advance(&candidate, candidate.Current, Health{Failed: true}, now.Add(time.Second))
			if !changed || !reflect.DeepEqual(recovered.Current, stable) {
				t.Fatal("did not restore successful configuration")
			}
			if err := w.Authority.writeState(obj, recovered, true); err != nil {
				t.Fatal(err)
			}
			// Persist recovery to the live workload read by admission.
			switch kind {
			case "Deployment":
				root := &apps.Deployment{}
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, root); err != nil {
					t.Fatal(err)
				}
				if _, err := w.Kube.AppsV1().Deployments("app").Update(context.Background(), root, meta.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			case "StatefulSet":
				root := &apps.StatefulSet{}
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, root); err != nil {
					t.Fatal(err)
				}
				if _, err := w.Kube.AppsV1().StatefulSets("app").Update(context.Background(), root, meta.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			case "DaemonSet":
				root := &apps.DaemonSet{}
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, root); err != nil {
					t.Fatal(err)
				}
				if _, err := w.Kube.AppsV1().DaemonSets("app").Update(context.Background(), root, meta.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			// A new process reads the same signing key and persisted state.
			w.Authority = &Authority{key: append([]byte(nil), w.Authority.key...)}
			state, err := w.Authority.readState(obj)
			if err != nil || !reflect.DeepEqual(state.Current, stable) {
				t.Fatalf("persisted recovery: %v", err)
			}
			w.Store.Set(Snapshot{Policy: failing, Synced: true})
			response := admissionResult(t, w, pod, "system:kube-controller-manager")
			if !response.Allowed {
				t.Fatal(response.Result)
			}
			if !bytes.Contains(response.Patch, []byte(`"image":"payload:v1"`)) || bytes.Contains(response.Patch, []byte("OTEL_RIO_TEST_BREAK")) || bytes.Contains(response.Patch, []byte(`"image":"ghcr.io/otelx/rio/instrumentation:broken"`)) {
				t.Fatalf("wrong recovery injection: %s", response.Patch)
			}
		})
	}
}

func TestAdmissionRejectsForgedStaleCopiedAndMalformedSnapshots(t *testing.T) {
	cfg := testPolicy().Resolve("app", "Deployment", "web")
	cases := []struct {
		name   string
		change func(*Webhook, *core.Pod)
		user   string
	}{
		{"direct-pod", func(w *Webhook, p *core.Pod) { p.OwnerReferences = nil }, "system:kube-controller-manager"},
		{"untrusted-creator-with-real-owner", func(w *Webhook, p *core.Pod) {}, "tenant"},
		{"forged-configuration-and-hash", func(w *Webhook, p *core.Pod) {
			c := cfg
			c.Image = "arbitrary:payload"
			b, _ := json.Marshal(c)
			p.Annotations[ConfigAnnotation] = string(b)
			p.Annotations[RevisionAnnotation] = c.Revision()
		}, "system:kube-controller-manager"},
		{"stale-revision", func(w *Webhook, p *core.Pod) { p.Annotations[RevisionAnnotation] = "old" }, "system:kube-controller-manager"},
		{"copied-workload", func(w *Webhook, p *core.Pod) { p.Annotations[OwnerAnnotation] = "another-workload" }, "system:kube-controller-manager"},
		{"copied-namespace", func(w *Webhook, p *core.Pod) { p.Namespace = "other" }, "system:kube-controller-manager"},
		{"recreated-owner", func(w *Webhook, p *core.Pod) { p.OwnerReferences[0].UID = "deleted-replicaset" }, "system:kube-controller-manager"},
		{"forged-api-group", func(w *Webhook, p *core.Pod) { p.OwnerReferences[0].APIVersion = "other/v1" }, "system:kube-controller-manager"},
		{"malformed-configuration", func(w *Webhook, p *core.Pod) { p.Annotations[ConfigAnnotation] = "{" }, "system:kube-controller-manager"},
		{"missing-configuration", func(w *Webhook, p *core.Pod) { delete(p.Annotations, ConfigAnnotation) }, "system:kube-controller-manager"},
		{"unsigned-state", func(w *Webhook, p *core.Pod) {
			d, _ := w.Kube.AppsV1().Deployments("app").Get(context.Background(), "web", meta.GetOptions{})
			delete(d.Annotations, StateSignatureAnnotation)
			_, _ = w.Kube.AppsV1().Deployments("app").Update(context.Background(), d, meta.UpdateOptions{})
		}, "system:kube-controller-manager"},
		{"forged-state-and-template", func(w *Webhook, p *core.Pod) {
			d, _ := w.Kube.AppsV1().Deployments("app").Get(context.Background(), "web", meta.GetOptions{})
			c := cfg
			c.Image = "forged"
			b, _ := json.Marshal(c)
			p.Annotations[ConfigAnnotation] = string(b)
			p.Annotations[RevisionAnnotation] = c.Revision()
			d.Spec.Template.Annotations = p.Annotations
			b, _ = json.Marshal(State{Current: c, Stable: c})
			d.Annotations[StateAnnotation] = string(b)
			_, _ = w.Kube.AppsV1().Deployments("app").Update(context.Background(), d, meta.UpdateOptions{})
		}, "system:kube-controller-manager"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, p, _ := snapshotFixture(t, "Deployment", cfg)
			tc.change(w, p)
			r := admissionResult(t, w, p, tc.user)
			skip := tc.name == "copied-namespace" || tc.name == "recreated-owner" || tc.name == "forged-api-group"
			if skip {
				if !r.Allowed || r.Patch != nil {
					t.Fatalf("unavailable owner must skip injection: %+v", r)
				}
			} else if r.Allowed {
				t.Fatalf("forged snapshot accepted: %s", r.Patch)
			}
		})
	}
}

func TestDirectAndUnmanagedPodsUseCentralPolicy(t *testing.T) {
	for _, selected := range []bool{true, false} {
		for _, owner := range []string{"Pod", "ReplicaSet", "Job", "CronJob", "CustomController"} {
			t.Run(fmt.Sprintf("%s-selected-%v", owner, selected), func(t *testing.T) {
				p := testPolicy()
				if !selected {
					p.Spec.Rules = nil
				}
				store := &Store{}
				store.Set(Snapshot{Policy: p, Synced: true})
				pod := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "web", Namespace: "app"}, Spec: core.PodSpec{Containers: []core.Container{{Name: "app"}}}}
				kube := fake.NewClientset()
				switch owner {
				case "CustomController":
					pod.OwnerReferences = []meta.OwnerReference{ownerRef("CustomController", "custom", "custom")}
				case "ReplicaSet":
					pod.OwnerReferences = []meta.OwnerReference{ownerRef("ReplicaSet", "rs", "rs")}
					_, _ = kube.AppsV1().ReplicaSets("app").Create(context.Background(), &apps.ReplicaSet{ObjectMeta: meta.ObjectMeta{Name: "rs", UID: "rs"}}, meta.CreateOptions{})
				case "Job", "CronJob":
					pod.OwnerReferences = []meta.OwnerReference{ownerRef("Job", "job", "job")}
					job := &batch.Job{ObjectMeta: meta.ObjectMeta{Name: "job", UID: "job"}}
					if owner == "CronJob" {
						job.OwnerReferences = []meta.OwnerReference{ownerRef("CronJob", "cron", "cron")}
						_, _ = kube.BatchV1().CronJobs("app").Create(context.Background(), &batch.CronJob{ObjectMeta: meta.ObjectMeta{Name: "cron", UID: "cron"}}, meta.CreateOptions{})
					}
					_, _ = kube.BatchV1().Jobs("app").Create(context.Background(), job, meta.CreateOptions{})
				}
				w := &Webhook{Store: store, Kube: kube, Namespace: "operator"}
				r := admissionResult(t, w, pod, "system:kube-controller-manager")
				if !r.Allowed || bytes.Contains(r.Patch, []byte(`"name":"rio-payload"`)) != selected {
					t.Fatalf("central policy: %+v %s", r.Result, r.Patch)
				}
				forged := Config{Enabled: true, Image: "forged"}
				encoded, _ := json.Marshal(forged)
				pod.Annotations = map[string]string{ConfigAnnotation: string(encoded), RevisionAnnotation: forged.Revision(), OwnerAnnotation: "fake"}
				forgedResponse := admissionResult(t, w, pod, "system:kube-controller-manager")
				if selected && forgedResponse.Allowed {
					t.Fatal("selected unmanaged pin accepted")
				}
				if !selected && (!forgedResponse.Allowed || forgedResponse.Patch != nil) {
					t.Fatal("unselected annotations must not select injection or block admission")
				}
			})
		}
	}
}

func TestAuthorityPersistsAndBindsStateToWorkload(t *testing.T) {
	kube := fake.NewClientset()
	a, err := LoadAuthority(context.Background(), kube, "operator")
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadAuthority(context.Background(), kube, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.key, b.key) {
		t.Fatal("key changed on restart")
	}
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "web", "namespace": "app", "uid": "uid"}}}
	cfg := testPolicy().Resolve("app", "Deployment", "web")
	if err := a.writeState(obj, State{Current: cfg, Stable: cfg}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := b.readState(obj); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"namespace", "name", "uid", "kind", "state"} {
		t.Run(field, func(t *testing.T) {
			copy := obj.DeepCopy()
			switch field {
			case "namespace":
				copy.SetNamespace("elsewhere")
			case "name":
				copy.SetName("other")
			case "uid":
				copy.SetUID("other")
			case "kind":
				copy.SetKind("DaemonSet")
			case "state":
				ann := copy.GetAnnotations()
				ann[StateAnnotation] = strings.Replace(ann[StateAnnotation], "payload:v1", "forged", 1)
				copy.SetAnnotations(ann)
			}
			if _, err := b.readState(copy); err == nil {
				t.Fatal("copied or modified state authenticated")
			}
		})
	}
}

func TestFirstInstallRecoveryRemainsUninstrumented(t *testing.T) {
	off := Config{TimeoutSeconds: 300}
	w, pod, _ := snapshotFixture(t, "StatefulSet", off)
	response := admissionResult(t, w, pod, "system:serviceaccount:kube-system:statefulset-controller")
	if !response.Allowed || bytes.Contains(response.Patch, []byte(`"name":"rio-payload"`)) {
		t.Fatalf("uninstrumented recovery overridden by policy: %v %s", response.Result, response.Patch)
	}
}

func TestForgedOwnerCannotSelectWorkloadPolicy(t *testing.T) {
	cfg := testPolicy().Resolve("app", "Deployment", "web")
	w, pod, _ := snapshotFixture(t, "Deployment", cfg)
	pod.Annotations = nil
	p := testPolicy()
	p.Spec.Rules[0].Selector.Workloads = []WorkloadSelector{{Kind: "Deployment", Name: "web"}}
	w.Store.Set(Snapshot{Policy: p, Synced: true})
	response := admissionResult(t, w, pod, "tenant")
	if !response.Allowed || bytes.Contains(response.Patch, []byte(`"name":"rio-payload"`)) {
		t.Fatalf("forged owner selected Deployment policy: %v %s", response.Result, response.Patch)
	}
}

func TestInvalidWorkloadStateDoesNotBlockOtherWorkloads(t *testing.T) {
	p := testPolicy()
	p.APIVersion = Group + "/v1alpha1"
	p.Kind = "Instrumentation"
	raw, _ := json.Marshal(p)
	var policy map[string]any
	_ = json.Unmarshal(raw, &policy)
	makeWorkload := func(name string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": name, "namespace": "app", "uid": name}, "spec": map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"name": "app", "image": "application"}}}}}}}
	}
	valid := makeWorkload("valid")
	forged := makeWorkload("forged")
	forged.SetAnnotations(map[string]string{StateAnnotation: `{"current":{"enabled":true,"image":"forged"}}`})
	lists := map[schema.GroupVersionResource]string{PolicyResource: "InstrumentationList"}
	for name, kind := range map[string]string{"deployments": "DeploymentList", "statefulsets": "StatefulSetList", "daemonsets": "DaemonSetList"} {
		lists[schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: name}] = kind
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), lists, &unstructured.Unstructured{Object: policy}, valid, forged)
	c := &Controller{Authority: testAuthority(), Kube: fake.NewClientset(), Dynamic: dyn, Namespace: "operator", Store: &Store{}}
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !c.Store.Get().Synced {
		t.Fatal("invalid workload blocked central policy admission")
	}
	deployments := dyn.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace("app")
	good, err := deployments.Get(context.Background(), "valid", meta.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if state, err := c.Authority.readState(good); err != nil || state == nil || state.Current.Image != p.Spec.Image {
		t.Fatalf("healthy workload blocked: %+v %v", state, err)
	}
	bad, err := deployments.Get(context.Background(), "forged", meta.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bad.GetAnnotations(), forged.GetAnnotations()) {
		t.Fatal("forged state adopted or rewritten")
	}
	cr, err := dyn.Resource(PolicyResource).Get(context.Background(), PolicyName, meta.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	status, _, _ := unstructured.NestedSlice(cr.Object, "status", "workloads")
	found := false
	for _, entry := range status {
		row := entry.(map[string]any)
		if row["name"] == "forged" && row["phase"] == "Error" {
			found = true
		}
	}
	if !found {
		t.Fatalf("invalid state not reported: %+v", status)
	}
}

func TestAuthenticatedRecoverySurvivesPolicyRemoval(t *testing.T) {
	stable := testPolicy().Resolve("app", "Deployment", "web")
	for _, mode := range []string{"removed", "unselected", "skip", "deleting"} {
		t.Run(mode, func(t *testing.T) {
			w, pod, _ := snapshotFixture(t, "Deployment", stable)
			policy := testPolicy()
			switch mode {
			case "removed":
				policy = nil
			case "unselected":
				policy.Spec.Rules = nil
			case "skip":
				policy.Spec.Rules[0].Action = "Skip"
			case "deleting":
				now := meta.Now()
				policy.DeletionTimestamp = &now
			}
			w.Store.Set(Snapshot{Policy: policy, Synced: true})
			response := admissionResult(t, w, pod, "system:kube-controller-manager")
			if !response.Allowed || !bytes.Contains(response.Patch, []byte(`"image":"payload:v1"`)) {
				t.Fatalf("authenticated recovery lost after %s policy: %v %s", mode, response.Result, response.Patch)
			}
			// The same bytes and owner references cannot authorize a Pod creator.
			response = admissionResult(t, w, pod, "tenant")
			if !response.Allowed || response.Patch != nil {
				t.Fatal("ordinary creator selected injection outside policy")
			}
			// A controller can carry application-supplied hints, so creator
			// authentication alone must never authorize an unselected payload.
			forged := pod.DeepCopy()
			cfg := stable
			cfg.Image = "forged"
			raw, _ := json.Marshal(cfg)
			forged.Annotations[ConfigAnnotation] = string(raw)
			forged.Annotations[RevisionAnnotation] = cfg.Revision()
			response = admissionResult(t, w, forged, "system:kube-controller-manager")
			if !response.Allowed || response.Patch != nil {
				t.Fatal("unselected forged snapshot must remain unchanged")
			}
			deployment, _ := w.Kube.AppsV1().Deployments("app").Get(context.Background(), "web", meta.GetOptions{})
			delete(deployment.Annotations, StateSignatureAnnotation)
			_, err := w.Kube.AppsV1().Deployments("app").Update(context.Background(), deployment, meta.UpdateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			response = admissionResult(t, w, pod, "system:kube-controller-manager")
			if !response.Allowed || response.Patch != nil {
				t.Fatal("unsigned state authorized injection or blocked unselected admission")
			}
		})
	}
}
