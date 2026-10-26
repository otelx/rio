package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
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
			snapshot := w.Store.Get()
			if !snapshot.Synced {
				response.Allowed = false
				response.Result = &meta.Status{Message: "instrumentation policy has not synchronized"}
			} else {
				kind, name, err := w.owner(req.Context(), &pod)
				if err == nil {
					cfg := snapshot.Policy.Resolve(pod.Namespace, kind, name)
					if pinned := pod.Annotations[ConfigAnnotation]; pinned != "" {
						// Unmarshal into a fresh value: omitted fields in a saved
						// revision must not inherit env/retry settings from the new CR.
						cfg = Config{}
						err = json.Unmarshal([]byte(pinned), &cfg)
						if err == nil && pod.Annotations[RevisionAnnotation] != cfg.Revision() {
							err = fmt.Errorf("instrumentation revision does not match its configuration")
						}
					}
					if err == nil {
						err = Mutate(&pod, cfg, snapshot.NativeImages[cfg.Image], name)
					}
				}
				if err != nil {
					response.Allowed = false
					response.Result = &meta.Status{Message: err.Error()}
					slog.Warn("injection rejected", "namespace", pod.Namespace, "pod", pod.Name, "error", err)
				} else {
					patch, _ := json.Marshal([]map[string]any{{"op": "add", "path": "/spec", "value": pod.Spec}, {"op": "add", "path": "/metadata/annotations", "value": pod.Annotations}})
					pt := admission.PatchTypeJSONPatch
					response.PatchType = &pt
					response.Patch = patch
				}
			}
		}
	}
	_ = json.NewEncoder(rw).Encode(admission.AdmissionReview{TypeMeta: meta.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"}, Response: response})
}

func (w *Webhook) owner(ctx context.Context, pod *core.Pod) (string, string, error) {
	for _, ref := range pod.OwnerReferences {
		if ref.Controller == nil || !*ref.Controller {
			continue
		}
		if ref.Kind == "ReplicaSet" {
			rs, err := w.Kube.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, ref.Name, meta.GetOptions{})
			if err != nil {
				return "", "", err
			}
			for _, parent := range rs.OwnerReferences {
				if parent.Kind == "Deployment" && parent.Controller != nil && *parent.Controller {
					return parent.Kind, parent.Name, nil
				}
			}
		}
		if ref.Kind == "Job" {
			job, err := w.Kube.BatchV1().Jobs(pod.Namespace).Get(ctx, ref.Name, meta.GetOptions{})
			if err != nil {
				return "", "", err
			}
			for _, parent := range job.OwnerReferences {
				if parent.Kind == "CronJob" && parent.Controller != nil && *parent.Controller {
					return parent.Kind, parent.Name, nil
				}
			}
		}
		return ref.Kind, ref.Name, nil
	}
	name := pod.Name
	if name == "" {
		name = strings.TrimSuffix(pod.GenerateName, "-")
	}
	return "Pod", name, nil
}

func Mutate(pod *core.Pod, cfg Config, native bool, service string) error {
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	if !cfg.Enabled {
		return nil
	}
	if pod.Spec.OS != nil && pod.Spec.OS.Name == core.Windows {
		return fmt.Errorf("Injector payload supports Linux containers only")
	}
	if pod.Annotations[AppliedAnnotation] == cfg.Revision() {
		return nil
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
