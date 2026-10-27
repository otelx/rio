package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
)

// Only authenticated Kubernetes controllers may use owner-based policy selection
// and saved snapshots. A Pod creator can otherwise forge an ownerReference.
func controllerUser(user string) bool {
	switch user {
	case "system:kube-controller-manager",
		"system:serviceaccount:kube-system:replicaset-controller",
		"system:serviceaccount:kube-system:statefulset-controller",
		"system:serviceaccount:kube-system:daemon-set-controller",
		"system:serviceaccount:kube-system:job-controller":
		return true
	}
	return false
}

func savedCandidate(pod *core.Pod, user string) bool {
	return controllerUser(user) && pod.Annotations[ConfigAnnotation] != "" && pod.Annotations[RevisionAnnotation] != "" && pod.Annotations[OwnerAnnotation] != ""
}

type ownerLookupError struct{ err error }

func (e *ownerLookupError) Error() string { return e.err.Error() }
func (e *ownerLookupError) Unwrap() error { return e.err }

func (w *Webhook) configuration(ctx context.Context, pod *core.Pod, user string, policy *Policy) (resolved Config, service string, configurationErr error) {
	kind, name := "Pod", pod.Name
	if name == "" {
		name = strings.TrimSuffix(pod.GenerateName, "-")
	}
	var workload *unstructured.Unstructured
	if controllerUser(user) || meta.GetControllerOf(pod) != nil {
		ownerKind, ownerName, ownerWorkload, err := w.owner(ctx, pod)
		if err != nil {
			return Config{}, name, &ownerLookupError{err: err}
		}
		if controllerUser(user) {
			kind, name, workload = ownerKind, ownerName, ownerWorkload
		}
	}
	cfg := policy.Resolve(pod.Namespace, kind, name)
	// Invalid hints must not block an unselected Pod. Only fully verified
	// active history can authorize injection independently of current selection.
	// This is essential when removal or a Skip revision itself needs rollback.
	if !cfg.Enabled {
		defer func() {
			if configurationErr != nil {
				resolved, service, configurationErr = cfg, name, nil
			}
		}()
		if workload == nil || !savedCandidate(pod, user) {
			return cfg, name, nil
		}
	}
	hasSnapshot := false
	for _, key := range []string{ConfigAnnotation, OwnerAnnotation, RevisionAnnotation} {
		if _, ok := pod.Annotations[key]; ok {
			hasSnapshot = true
		}
	}
	if workload == nil {
		if hasSnapshot {
			return Config{}, name, fmt.Errorf("saved configuration requires an authenticated managed workload controller")
		}
		return cfg, name, nil
	}
	state, err := w.Authority.readState(workload)
	if err != nil {
		return Config{}, name, err
	}
	if state == nil {
		if hasSnapshot {
			return Config{}, name, fmt.Errorf("saved configuration has no authenticated rollout state")
		}
		return cfg, name, nil
	}
	// Check the live workload, not a retained ReplicaSet or a copied Pod annotation.
	// Only the active revision is admitted; recovery makes Stable active atomically.
	template, _, err := unstructured.NestedStringMap(workload.Object, "spec", "template", "metadata", "annotations")
	if err != nil {
		return Config{}, name, err
	}
	for _, key := range []string{ConfigAnnotation, OwnerAnnotation, RevisionAnnotation} {
		if pod.Annotations[key] == "" || pod.Annotations[key] != template[key] {
			return Config{}, name, fmt.Errorf("saved configuration does not match the active workload template")
		}
	}
	if pod.Annotations[OwnerAnnotation] != string(workload.GetUID()) || pod.Annotations[RevisionAnnotation] != state.Current.Revision() {
		return Config{}, name, fmt.Errorf("saved configuration does not match the active workload identity and revision")
	}
	// Compare canonical configuration, including fields omitted by old revisions.
	// Decode only after authority is established, never into the latest policy.
	var saved Config
	if err := json.Unmarshal([]byte(pod.Annotations[ConfigAnnotation]), &saved); err != nil {
		return Config{}, name, fmt.Errorf("invalid saved configuration: %w", err)
	}
	if !reflect.DeepEqual(saved, state.Current) {
		return Config{}, name, fmt.Errorf("saved configuration differs from authenticated rollout state")
	}
	return saved, name, nil
}

func (w *Webhook) owner(ctx context.Context, pod *core.Pod) (string, string, *unstructured.Unstructured, error) {
	ref := meta.GetControllerOf(pod)
	if ref == nil {
		name := pod.Name
		if name == "" {
			name = strings.TrimSuffix(pod.GenerateName, "-")
		}
		return "Pod", name, nil, nil
	}
	switch ref.Kind {
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob":
		// These built-in identities can select a supported workload rule.
	default:
		name := pod.Name
		if name == "" {
			name = strings.TrimSuffix(pod.GenerateName, "-")
		}
		return "Pod", name, nil, nil
	}
	obj, err := getOwner(ctx, w.Kube, pod.Namespace, *ref)
	if err != nil {
		return "", "", nil, err
	}
	if ref.Kind == "ReplicaSet" || ref.Kind == "Job" {
		parent := meta.GetControllerOf(obj)
		if parent != nil && ((ref.Kind == "ReplicaSet" && parent.Kind == "Deployment") || (ref.Kind == "Job" && parent.Kind == "CronJob")) {
			ref = parent
			obj, err = getOwner(ctx, w.Kube, pod.Namespace, *ref)
			if err != nil {
				return "", "", nil, err
			}
		}
	}
	if ref.Kind != "Deployment" && ref.Kind != "StatefulSet" && ref.Kind != "DaemonSet" {
		return ref.Kind, ref.Name, nil, nil
	}
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return "", "", nil, err
	}
	root := &unstructured.Unstructured{Object: raw}
	root.SetAPIVersion("apps/v1")
	root.SetKind(ref.Kind)
	return ref.Kind, ref.Name, root, nil
}

type ownerObject interface {
	runtime.Object
	meta.Object
}

func getOwner(ctx context.Context, kube kubernetes.Interface, namespace string, ref meta.OwnerReference) (ownerObject, error) {
	var obj ownerObject
	var err error
	if kube == nil {
		return nil, fmt.Errorf("owner lookup unavailable")
	}
	switch {
	case ref.APIVersion == "apps/v1" && ref.Kind == "ReplicaSet":
		obj, err = kube.AppsV1().ReplicaSets(namespace).Get(ctx, ref.Name, meta.GetOptions{})
	case ref.APIVersion == "apps/v1" && ref.Kind == "Deployment":
		obj, err = kube.AppsV1().Deployments(namespace).Get(ctx, ref.Name, meta.GetOptions{})
	case ref.APIVersion == "apps/v1" && ref.Kind == "StatefulSet":
		obj, err = kube.AppsV1().StatefulSets(namespace).Get(ctx, ref.Name, meta.GetOptions{})
	case ref.APIVersion == "apps/v1" && ref.Kind == "DaemonSet":
		obj, err = kube.AppsV1().DaemonSets(namespace).Get(ctx, ref.Name, meta.GetOptions{})
	case ref.APIVersion == "batch/v1" && ref.Kind == "Job":
		obj, err = kube.BatchV1().Jobs(namespace).Get(ctx, ref.Name, meta.GetOptions{})
	case ref.APIVersion == "batch/v1" && ref.Kind == "CronJob":
		obj, err = kube.BatchV1().CronJobs(namespace).Get(ctx, ref.Name, meta.GetOptions{})
	default:
		return nil, fmt.Errorf("unsupported controller identity %s %s", ref.APIVersion, ref.Kind)
	}
	if err != nil {
		return nil, err
	}
	if ref.UID == "" || obj.GetUID() != ref.UID {
		return nil, fmt.Errorf("controller UID does not match live %s %s", ref.Kind, ref.Name)
	}
	return obj, nil
}
