package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"

	admission "k8s.io/api/admission/v1"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Snapshot struct {
	Policy       *Policy
	NativeImages map[string]bool
	Synced       bool
}

type Store struct {
	mu       sync.RWMutex
	snapshot Snapshot
}

func (s *Store) Get() Snapshot  { s.mu.RLock(); defer s.mu.RUnlock(); return s.snapshot }
func (s *Store) Set(v Snapshot) { s.mu.Lock(); defer s.mu.Unlock(); s.snapshot = v }

type Webhook struct {
	Authority *Authority
	Store     *Store
	Kube      kubernetes.Interface
	Namespace string
}

func (w *Webhook) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	var review admission.AdmissionReview
	body, err := io.ReadAll(io.LimitReader(req.Body, 2<<20))
	if err != nil || json.Unmarshal(body, &review) != nil || review.Request == nil {
		http.Error(rw, "invalid AdmissionReview", 400)
		return
	}
	r := review.Request
	response := &admission.AdmissionResponse{UID: r.UID, Allowed: true}
	if r.Operation == admission.Create && r.Resource.Resource == "pods" && !excluded(r.Namespace, w.Namespace) {
		var pod core.Pod
		if err := json.Unmarshal(r.Object.Raw, &pod); err != nil {
			response.Allowed = false
			response.Result = &meta.Status{Message: err.Error()}
		} else {
			if pod.Namespace == "" {
				pod.Namespace = r.Namespace
			}
			patch, err := w.admit(req.Context(), &pod, r.UserInfo.Username)
			if err != nil {
				response.Allowed = false
				response.Result = &meta.Status{Message: err.Error()}
				slog.Warn("injection rejected", "namespace", pod.Namespace, "pod", pod.Name, "error", err)
			} else if patch != nil {
				pt := admission.PatchTypeJSONPatch
				response.PatchType = &pt
				response.Patch = patch
			}
		}
	}
	_ = json.NewEncoder(rw).Encode(admission.AdmissionReview{TypeMeta: meta.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"}, Response: response})
}

// admit returns no patch when instrumentation is irrelevant or policy/owner
// information is unavailable. Explicit rejections are reserved for unsafe
// mutation of a workload known to be selected by a synchronized policy.
func (w *Webhook) admit(ctx context.Context, pod *core.Pod, user string) ([]byte, error) {
	if criticalPriority(pod.Spec.PriorityClassName) {
		return nil, nil
	}
	snapshot := w.Store.Get()
	potential := pod
	if !controllerUser(user) && meta.GetControllerOf(pod) != nil {
		// Owner references supplied by another identity cannot select workload rules.
		potential = pod.DeepCopy()
		potential.OwnerReferences = nil
	}
	if !snapshot.Synced || (!snapshot.Policy.mayInjectPod(potential) && !savedCandidate(pod, user)) {
		return nil, nil
	}
	cfg, service, err := w.configuration(ctx, pod, user, snapshot.Policy)
	var lookup *ownerLookupError
	if errors.As(err, &lookup) {
		slog.Warn("skipping injection; owner unavailable", "namespace", pod.Namespace, "pod", pod.Name, "error", err)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, nil
	}
	if err := Mutate(pod, cfg, snapshot.NativeImages[cfg.Image], service); err != nil {
		return nil, err
	}
	return json.Marshal([]map[string]any{{"op": "add", "path": "/spec", "value": pod.Spec}, {"op": "add", "path": "/metadata/annotations", "value": pod.Annotations}})
}

func Mutate(pod *core.Pod, cfg Config, native bool, service string) error {
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	if !cfg.Enabled {
		if pod.Annotations[AppliedAnnotation] != "" {
			return fmt.Errorf("applied instrumentation is not authorized by the effective configuration")
		}
		return nil
	}
	if pod.Spec.OS != nil && pod.Spec.OS.Name == core.Windows {
		return fmt.Errorf("Injector payload supports Linux containers only")
	}
	if pod.Annotations[AppliedAnnotation] != "" {
		return verifyInjection(pod, cfg, native, service)
	}
	for _, vol := range pod.Spec.Volumes {
		if vol.Name == VolumeName {
			return fmt.Errorf("volume name %s is already in use", VolumeName)
		}
	}
	for _, container := range pod.Spec.InitContainers {
		if container.Name == VolumeName {
			return fmt.Errorf("init container name %s is already in use", VolumeName)
		}
	}
	for _, container := range pod.Spec.Containers {
		for _, mount := range container.VolumeMounts {
			if mount.MountPath == MountPath {
				return fmt.Errorf("container %s already uses the payload mount path", container.Name)
			}
		}
		for _, env := range container.Env {
			if env.Name == "LD_PRELOAD" && env.ValueFrom != nil {
				return fmt.Errorf("cannot safely combine LD_PRELOAD from valueFrom in %s", container.Name)
			}
		}
	}
	volume := core.Volume{Name: VolumeName}
	if native {
		volume.Image = &core.ImageVolumeSource{Reference: cfg.Image, PullPolicy: core.PullIfNotPresent}
	} else {
		volume.EmptyDir = &core.EmptyDirVolumeSource{}
		zero := int64(0)
		no := false
		yes := true
		// Root owns the emptyDir. The copy process has no capabilities and writes
		// files readable by application UIDs; it never changes app security settings.
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, core.Container{Name: VolumeName, Image: cfg.Image, ImagePullPolicy: core.PullIfNotPresent,
			Command: []string{"/bin/sh", "/copy-to-volume.sh"}, Env: []core.EnvVar{{Name: "MOUNT_PATH", Value: MountPath}},
			VolumeMounts:    []core.VolumeMount{{Name: VolumeName, MountPath: MountPath}},
			SecurityContext: &core.SecurityContext{RunAsUser: &zero, RunAsNonRoot: &no, AllowPrivilegeEscalation: &no, ReadOnlyRootFilesystem: &yes, Capabilities: &core.Capabilities{Drop: []core.Capability{"ALL"}}}})
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, volume)
	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		preload := MountPath + "/dist/injector/libotelinject.so"
		for _, env := range container.Env {
			if env.Name == "LD_PRELOAD" && env.Value != "" {
				preload += " " + env.Value
			}
		}
		vars := []core.EnvVar{
			{Name: "LD_PRELOAD", Value: preload},
			{Name: "OTEL_INJECTOR_CONFIG_FILE", Value: MountPath + "/dist/injector/otelinject.conf"},
			{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: cfg.Endpoint},
			{Name: "OTEL_EXPORTER_OTLP_PROTOCOL", Value: "http/protobuf"},
			{Name: "OTEL_TRACES_EXPORTER", Value: exporter(cfg.Signals.Traces)},
			{Name: "OTEL_METRICS_EXPORTER", Value: exporter(cfg.Signals.Metrics)},
			{Name: "OTEL_LOGS_EXPORTER", Value: exporter(cfg.Signals.Logs)},
			{Name: "OTEL_INJECTOR_SERVICE_NAME", Value: service},
			{Name: "OTEL_INJECTOR_SERVICE_NAMESPACE", Value: pod.Namespace},
			{Name: "OTEL_INJECTOR_K8S_NAMESPACE_NAME", Value: pod.Namespace},
			{Name: "OTEL_INJECTOR_K8S_CONTAINER_NAME", Value: container.Name},
			{Name: "OTEL_INJECTOR_K8S_POD_NAME", ValueFrom: &core.EnvVarSource{FieldRef: &core.ObjectFieldSelector{FieldPath: "metadata.name"}}},
			{Name: "OTEL_INJECTOR_K8S_POD_UID", ValueFrom: &core.EnvVarSource{FieldRef: &core.ObjectFieldSelector{FieldPath: "metadata.uid"}}},
		}
		vars = mergeEnv(vars, cfg.Env)
		container.Env = mergeEnv(container.Env, vars)
		container.VolumeMounts = append(container.VolumeMounts, core.VolumeMount{Name: VolumeName, MountPath: MountPath, ReadOnly: true})
	}
	pod.Annotations[AppliedAnnotation] = cfg.Revision()
	mode := "initContainer"
	if native {
		mode = "imageVolume"
	}
	pod.Annotations[Prefix+"delivery"] = mode
	return nil
}

func exporter(enabled bool) string {
	if enabled {
		return "otlp"
	}
	return "none"
}
