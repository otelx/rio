package operator

import (
	"encoding/json"
	"testing"
	"time"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestMissingInjectionNeverPromotesCandidateToStable(t *testing.T) {
	stable := testPolicy().Resolve("app", "Deployment", "web")
	candidate := stable
	candidate.Image = "candidate"
	now := time.Now()
	s := State{Current: stable, Stable: stable, Phase: "Ready"}
	s, _ = Advance(&s, candidate, Health{}, now)
	s, changed := Advance(&s, candidate, Health{Ready: true, InjectionMissing: true}, now.Add(time.Second))
	if changed || s.Phase != "InjectionMissing" || s.Stable.Revision() != stable.Revision() {
		t.Fatalf("unapplied candidate became stable: %+v", s)
	}
	raw, _ := json.Marshal(s)
	var restored State
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	s, changed = Advance(&restored, candidate, Health{Ready: true, InjectionMissing: true}, now.Add(time.Hour))
	if changed || s.Phase != "InjectionMissing" || s.Stable.Revision() != stable.Revision() {
		t.Fatal("admission outage retried or promoted candidate")
	}
	// When injection resumes, actually unhealthy candidates still restore the
	// last successfully instrumented configuration, even across restarts.
	s, _ = Advance(&s, candidate, Health{Instrumented: true}, now.Add(2*time.Hour))
	if s.Phase != "RollingOut" || !s.Started.Equal(now.Add(2*time.Hour)) {
		t.Fatal("health deadline was not restarted when injection resumed")
	}
	s, changed = Advance(&s, candidate, Health{Failed: true, Instrumented: true, Message: "crashing"}, now.Add(2*time.Hour+time.Second))
	if !changed || s.Phase != "RollingBack" || s.Current.Revision() != stable.Revision() {
		t.Fatalf("restored unapplied policy: %+v", s)
	}
}

func TestHealthValidatesStructureAndNamespace(t *testing.T) {
	cfg := testPolicy().Resolve("app", "Deployment", "web")
	obj := &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "web", "namespace": "app", "uid": "workload"}, "spec": map[string]any{"replicas": int64(1)}, "status": map[string]any{"updatedReplicas": int64(1), "availableReplicas": int64(1), "replicas": int64(1)}}}
	pod := core.Pod{ObjectMeta: meta.ObjectMeta{Namespace: "app", Annotations: map[string]string{OwnerAnnotation: "workload", RevisionAnnotation: cfg.Revision(), AppliedAnnotation: cfg.Revision()}}, Spec: core.PodSpec{Containers: []core.Container{{Name: "app"}}}, Status: core.PodStatus{Conditions: []core.PodCondition{{Type: core.PodReady, Status: core.ConditionTrue}}}}
	h := workloadHealth(obj, "Deployment", []core.Pod{pod}, cfg)
	if !h.Ready || !h.InjectionMissing || h.Instrumented {
		t.Fatalf("forged marker was trusted: %+v", h)
	}
	delete(pod.Annotations, AppliedAnnotation)
	if err := Mutate(&pod, cfg, false, "web"); err != nil {
		t.Fatal(err)
	}
	h = workloadHealth(obj, "Deployment", []core.Pod{pod}, cfg)
	if !h.Ready || h.InjectionMissing || !h.Instrumented {
		t.Fatalf("legitimate injection was not observed: %+v", h)
	}
	pod.Namespace = "other"
	if workloadHealth(obj, "Deployment", []core.Pod{pod}, cfg).Ready {
		t.Fatal("copied annotations in another namespace established workload health")
	}
	pod.Namespace = "app"
	pod.Spec.Containers[0].VolumeMounts = nil
	h = workloadHealth(obj, "Deployment", []core.Pod{pod}, cfg)
	if !h.InjectionMissing || h.Instrumented {
		t.Fatal("incomplete injected structure was trusted")
	}
}

func TestRollbackWaitsForSavedInjectionWithoutExpiringItsHealthDeadline(t *testing.T) {
	stable := testPolicy().Resolve("app", "StatefulSet", "web")
	candidate := stable
	candidate.Image = "broken"
	now := time.Now()
	s := State{Current: stable, Stable: stable, Phase: "RollingBack", Started: now, FailedRevision: candidate.Revision()}
	s, _ = Advance(&s, candidate, Health{Ready: true, InjectionMissing: true}, now.Add(time.Second))
	if s.Phase != "RollbackInjectionMissing" || s.Current.Revision() != stable.Revision() {
		t.Fatal("uninstrumented recovery was accepted")
	}
	s, _ = Advance(&s, candidate, Health{Instrumented: true}, now.Add(time.Hour))
	if s.Phase != "RollingBack" || !s.Started.Equal(now.Add(time.Hour)) {
		t.Fatal("outage expired recovery deadline")
	}
	s, _ = Advance(&s, candidate, Health{Ready: true, Instrumented: true}, now.Add(time.Hour+time.Second))
	if s.Phase != "RolledBack" || s.FailedRevision != candidate.Revision() {
		t.Fatal("recovery lost failed revision or saved configuration")
	}
}
