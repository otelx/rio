package operator

import (
	"encoding/json"
	"fmt"
	"time"

	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type State struct {
	Current        Config    `json:"current"`
	Stable         Config    `json:"stable"`
	Phase          string    `json:"phase"`
	Started        time.Time `json:"started"`
	FailedRevision string    `json:"failedRevision,omitempty"`
	Message        string    `json:"message,omitempty"`
}

type Health struct {
	InjectionMissing bool
	Instrumented     bool
	Ready            bool
	Failed           bool
	Message          string
}

// Advance contains only rollout decisions. Its caller atomically persists state
// and template changes with a resourceVersion precondition before admitting pods.
func Advance(state *State, wanted Config, health Health, now time.Time) (State, bool) {
	if state == nil {
		if !wanted.Enabled {
			return State{Current: wanted, Stable: wanted, Phase: "Disabled"}, false
		}
		return State{Current: wanted, Stable: Config{TimeoutSeconds: 300}, Phase: "RollingOut", Started: now}, true
	}
	s := *state
	// A settled, uninstrumented workload needs no further rollout when the CR
	// disappears or a skipped rule's retry/deadline settings change.
	if !wanted.Enabled && !s.Current.Enabled && (s.Phase == "Disabled" || s.Phase == "RolledBack") {
		s.Phase = "Disabled"
		s.Stable = s.Current
		s.Message = ""
		return s, false
	}
	if s.Phase == "RollbackInjectionMissing" {
		if !health.Instrumented {
			return s, false
		}
		s.Phase = "RollingBack"
		s.Started = now
		s.Message = ""
	}
	if s.Phase == "RollingBack" {
		if health.Ready && s.Current.Enabled && health.InjectionMissing {
			s.Phase = "RollbackInjectionMissing"
			s.Message = "healthy recovery pods did not receive the saved instrumentation; repair admission and recreate the affected pods"
			return s, false
		}
		if health.Ready {
			s.Phase = "RolledBack"
			s.Message = "previous instrumentation restored; change effective configuration or retryToken to retry"
			return s, false
		}
		if health.Failed || now.Sub(s.Started) >= deadline(s.Current) {
			s.Phase = "RecoveryFailed"
			s.Message = "rollback did not restore workload health: " + health.Message
		}
		return s, false
	}
	if wanted.Revision() != s.Current.Revision() && wanted.Revision() != s.FailedRevision {
		s.Current = wanted
		s.Phase = "RollingOut"
		s.Started = now
		s.Message = ""
		return s, true
	}
	if s.Phase == "InjectionMissing" {
		if !health.Instrumented {
			return s, false
		}
		// Start the health deadline when the candidate is actually observed,
		// rather than charging the previous admission outage to its health.
		s.Phase = "RollingOut"
		s.Started = now
		s.Message = ""
	}
	if s.Phase != "RollingOut" {
		return s, false
	}
	if health.Ready {
		if s.Current.Enabled && health.InjectionMissing {
			s.Phase = "InjectionMissing"
			s.Message = "healthy pods did not receive the requested instrumentation; repair admission and recreate pods or change retryToken"
			return s, false
		}
		s.Stable = s.Current
		s.Phase = "Ready"
		if !s.Current.Enabled {
			s.Phase = "Disabled"
		}
		s.Message = ""
		return s, false
	}
	if health.Failed || now.Sub(s.Started) >= deadline(s.Current) {
		s.FailedRevision = s.Current.Revision()
		s.Current = s.Stable
		s.Phase = "RollingBack"
		s.Started = now
		s.Message = "candidate failed workload health: " + health.Message
		return s, true
	}
	return s, false
}

func deadline(cfg Config) time.Duration {
	n := cfg.TimeoutSeconds
	if n < 10 {
		n = 300
	}
	return time.Duration(n) * time.Second
}

func readState(obj *unstructured.Unstructured) (*State, error) {
	raw := obj.GetAnnotations()[StateAnnotation]
	if raw == "" {
		return nil, nil
	}
	var state State
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, fmt.Errorf("invalid persisted rollout state: %w", err)
	}
	return &state, nil
}

func writeState(obj *unstructured.Unstructured, state State, changeTemplate bool) error {
	a := obj.GetAnnotations()
	if a == nil {
		a = map[string]string{}
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	a[StateAnnotation] = string(b)
	obj.SetAnnotations(a)
	if !changeTemplate {
		return nil
	}
	ta, _, err := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "annotations")
	if err != nil {
		return err
	}
	if ta == nil {
		ta = map[string]string{}
	}
	b, err = json.Marshal(state.Current)
	if err != nil {
		return err
	}
	ta[ConfigAnnotation] = string(b)
	ta[RevisionAnnotation] = state.Current.Revision()
	ta[OwnerAnnotation] = string(obj.GetUID())
	return unstructured.SetNestedStringMap(obj.Object, ta, "spec", "template", "metadata", "annotations")
}

func strategyProblem(obj *unstructured.Unstructured, kind string) string {
	if kind == "Deployment" {
		paused, _, _ := unstructured.NestedBool(obj.Object, "spec", "paused")
		if paused {
			return "Deployment is paused"
		}
		v, _, _ := unstructured.NestedString(obj.Object, "spec", "strategy", "type")
		if v != "" && v != "RollingUpdate" {
			return "Deployment must use RollingUpdate"
		}
	} else {
		v, _, _ := unstructured.NestedString(obj.Object, "spec", "updateStrategy", "type")
		if v != "" && v != "RollingUpdate" {
			return kind + " must use RollingUpdate"
		}
		if kind == "StatefulSet" {
			partition, _, _ := unstructured.NestedInt64(obj.Object, "spec", "updateStrategy", "rollingUpdate", "partition")
			if partition > 0 {
				return "StatefulSet has a partitioned rollout; set partition to zero"
			}
		}
	}
	return ""
}

func workloadHealth(obj *unstructured.Unstructured, kind string, pods []core.Pod, current Config) Health {
	get := func(path ...string) int64 { n, _, _ := unstructured.NestedInt64(obj.Object, path...); return n }
	if get("status", "observedGeneration") < obj.GetGeneration() {
		return Health{Message: "waiting for workload controller to observe the new template"}
	}
	var desired, updated, ready int64
	if kind == "DaemonSet" {
		desired = get("status", "desiredNumberScheduled")
		updated = get("status", "updatedNumberScheduled")
		ready = get("status", "numberAvailable")
	} else {
		desired = 1
		if n, found, _ := unstructured.NestedInt64(obj.Object, "spec", "replicas"); found {
			desired = n
		}
		updated = get("status", "updatedReplicas")
		ready = get("status", "readyReplicas")
		if kind == "Deployment" {
			ready = get("status", "availableReplicas")
		}
	}
	matching := int64(0)
	readyCandidates := int64(0)
	missing := false
	failure := ""
	minimumReady := time.Duration(get("spec", "minReadySeconds")) * time.Second
	for _, pod := range pods {
		if pod.Namespace != obj.GetNamespace() || pod.Annotations[OwnerAnnotation] != string(obj.GetUID()) || pod.DeletionTimestamp != nil {
			continue
		}
		if pod.Annotations[RevisionAnnotation] != current.Revision() {
			continue
		}
		matching++
		if current.Enabled {
			mode := pod.Annotations[Prefix+"delivery"]
			if (mode != "imageVolume" && mode != "initContainer") || verifyInjection(&pod, current, mode == "imageVolume", obj.GetName()) != nil {
				missing = true
			}
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == core.PodReady && condition.Status == core.ConditionTrue && time.Since(condition.LastTransitionTime.Time) >= minimumReady {
				readyCandidates++
			}
		}
		statuses := append(append([]core.ContainerStatus(nil), pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
		for _, cs := range statuses {
			if cs.RestartCount >= 3 && cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
				failure = "repeated crashes in " + pod.Name + "/" + cs.Name
			}
		}
	}
	health := Health{InjectionMissing: missing, Instrumented: matching >= desired && !missing}
	if failure != "" {
		health.Failed = true
		health.Message = failure
		return health
	}
	if updated == desired && ready >= desired && matching >= desired && readyCandidates >= desired {
		if kind == "Deployment" && get("status", "replicas") > desired {
			health.Message = "waiting for old replicas to retire"
			return health
		}
		health.Ready = true
		return health
	}
	health.Message = fmt.Sprintf("%d/%d updated and %d/%d ready", updated, desired, ready, desired)
	return health
}
