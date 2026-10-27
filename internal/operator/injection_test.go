package operator

import (
	"encoding/json"
	"reflect"
	"testing"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReinvocationVerifiesInjectedContract(t *testing.T) {
	cfg := testPolicy().Resolve("app", "Pod", "web")
	cfg.Env = []core.EnvVar{{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: "deployment.environment.name=test"}}
	for _, native := range []bool{false, true} {
		mode := "copy"
		if native {
			mode = "native"
		}
		t.Run(mode, func(t *testing.T) {
			original := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "web", Namespace: "app"}, Spec: core.PodSpec{Containers: []core.Container{{Name: "app", Env: []core.EnvVar{{Name: "LD_PRELOAD", Value: "/existing.so"}}}, {Name: "sidecar"}}}}
			if err := Mutate(original, cfg, native, "web"); err != nil {
				t.Fatal(err)
			}
			// Simulate API defaulting before the webhook runs again.
			for i := range original.Spec.Containers {
				for j := range original.Spec.Containers[i].Env {
					e := &original.Spec.Containers[i].Env[j]
					if e.ValueFrom != nil && e.ValueFrom.FieldRef != nil {
						e.ValueFrom.FieldRef.APIVersion = "v1"
					}
				}
			}
			before := original.DeepCopy()
			if err := Mutate(original, cfg, native, "web"); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, original) {
				t.Fatal("reinvocation changed a valid injected Pod")
			}
			edits := []struct {
				name string
				edit func(*core.Pod)
			}{
				{"annotation-only", func(p *core.Pod) {
					p.Spec.Volumes = nil
					p.Spec.InitContainers = nil
					p.Spec.Containers = []core.Container{{Name: "app"}}
				}},
				{"stale-applied", func(p *core.Pod) { p.Annotations[AppliedAnnotation] = "old" }},
				{"delivery", func(p *core.Pod) { p.Annotations[Prefix+"delivery"] = "wrong" }},
				{"missing-volume", func(p *core.Pod) { p.Spec.Volumes = nil }},
				{"wrong-source", func(p *core.Pod) {
					p.Spec.Volumes[0].VolumeSource = core.VolumeSource{Secret: &core.SecretVolumeSource{SecretName: "other"}}
				}},
				{"writable-mount", func(p *core.Pod) { p.Spec.Containers[0].VolumeMounts[0].ReadOnly = false }},
				{"subpath", func(p *core.Pod) { p.Spec.Containers[0].VolumeMounts[0].SubPath = "other" }},
				{"missing-sidecar-mount", func(p *core.Pod) { p.Spec.Containers[1].VolumeMounts = nil }},
				{"duplicate-volume", func(p *core.Pod) { p.Spec.Volumes = append(p.Spec.Volumes, p.Spec.Volumes[0]) }},
				{"duplicate-mount", func(p *core.Pod) {
					p.Spec.Containers[0].VolumeMounts = append(p.Spec.Containers[0].VolumeMounts, p.Spec.Containers[0].VolumeMounts[0])
				}},
				{"duplicate-env", func(p *core.Pod) {
					p.Spec.Containers[0].Env = append(p.Spec.Containers[0].Env, p.Spec.Containers[0].Env[0])
				}},
				{"missing-env", func(p *core.Pod) { p.Spec.Containers[0].Env = p.Spec.Containers[0].Env[1:] }},
				{"wrong-endpoint", func(p *core.Pod) {
					for i, e := range p.Spec.Containers[0].Env {
						if e.Name == "OTEL_EXPORTER_OTLP_ENDPOINT" {
							p.Spec.Containers[0].Env[i].Value = "http://forged"
						}
					}
				}},
				{"wrong-configured-env", func(p *core.Pod) {
					for i, e := range p.Spec.Containers[0].Env {
						if e.Name == "OTEL_RESOURCE_ATTRIBUTES" {
							p.Spec.Containers[0].Env[i].Value = "other"
						}
					}
				}},
				{"wrong-downward-api", func(p *core.Pod) {
					for i, e := range p.Spec.Containers[0].Env {
						if e.Name == "OTEL_INJECTOR_K8S_POD_UID" {
							p.Spec.Containers[0].Env[i].ValueFrom.FieldRef.FieldPath = "metadata.name"
						}
					}
				}},
			}
			if native {
				edits = append(edits, struct {
					name string
					edit func(*core.Pod)
				}{"wrong-payload", func(p *core.Pod) { p.Spec.Volumes[0].Image.Reference = "forged" }})
			} else {
				edits = append(edits, []struct {
					name string
					edit func(*core.Pod)
				}{
					{"missing-init", func(p *core.Pod) { p.Spec.InitContainers = nil }},
					{"wrong-init-image", func(p *core.Pod) { p.Spec.InitContainers[0].Image = "forged" }},
					{"wrong-init-command", func(p *core.Pod) { p.Spec.InitContainers[0].Command = []string{"true"} }},
					{"wrong-init-env", func(p *core.Pod) { p.Spec.InitContainers[0].Env = nil }},
					{"wrong-init-mount", func(p *core.Pod) { p.Spec.InitContainers[0].VolumeMounts = nil }},
					{"wrong-init-security", func(p *core.Pod) { p.Spec.InitContainers[0].SecurityContext = nil }},
				}...)
			}
			for _, tc := range edits {
				t.Run(tc.name, func(t *testing.T) {
					p := original.DeepCopy()
					tc.edit(p)
					if err := Mutate(p, cfg, native, "web"); err == nil {
						t.Fatal("incomplete or modified injection accepted")
					}
				})
			}
			off := Config{}
			if err := Mutate(original.DeepCopy(), off, native, "web"); err == nil {
				t.Fatal("applied annotation bypassed disabled policy")
			}
		})
	}
}

func TestWebhookReinvocation(t *testing.T) {
	cfg := testPolicy().Resolve("app", "Deployment", "web")
	w, pod, _ := snapshotFixture(t, "Deployment", cfg)
	first := admissionResult(t, w, pod, "system:kube-controller-manager")
	if !first.Allowed {
		t.Fatal(first.Result)
	}
	var patch []struct {
		Path  string          `json:"path"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(first.Patch, &patch); err != nil {
		t.Fatal(err)
	}
	for _, p := range patch {
		switch p.Path {
		case "/spec":
			if err := json.Unmarshal(p.Value, &pod.Spec); err != nil {
				t.Fatal(err)
			}
		case "/metadata/annotations":
			if err := json.Unmarshal(p.Value, &pod.Annotations); err != nil {
				t.Fatal(err)
			}
		}
	}
	second := admissionResult(t, w, pod, "system:kube-controller-manager")
	if !second.Allowed || string(first.Patch) != string(second.Patch) {
		t.Fatalf("reinvocation failed: %v %s", second.Result, second.Patch)
	}
	pod.Spec.Containers[0].VolumeMounts = nil
	if admissionResult(t, w, pod, "system:kube-controller-manager").Allowed {
		t.Fatal("tampered reinvocation accepted")
	}
}
