package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"time"

	core "k8s.io/api/core/v1"
	errors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type Controller struct {
	Kube         kubernetes.Interface
	Dynamic      dynamic.Interface
	Namespace    string
	Store        *Store
	Capabilities *Capabilities
}

type workload struct {
	object   *unstructured.Unstructured
	kind     string
	resource schema.GroupVersionResource
}

func (c *Controller) Run(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		if err := c.Reconcile(ctx); err != nil {
			slog.Error("reconcile", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Controller) Reconcile(ctx context.Context) error {
	cr, err := c.Dynamic.Resource(PolicyResource).Get(ctx, PolicyName, meta.GetOptions{})
	var policy *Policy
	if errors.IsNotFound(err) {
		cr = nil
	} else if err != nil {
		return err
	} else {
		bytes, _ := json.Marshal(cr.Object)
		policy = &Policy{}
		if err = json.Unmarshal(bytes, policy); err != nil {
			return err
		}
		if err = policy.Validate(); err != nil {
			c.Store.Set(Snapshot{})
			return c.status(ctx, cr, "Invalid", nil, err.Error())
		}
		if policy.DeletionTimestamp == nil && !slices.Contains(cr.GetFinalizers(), Finalizer) {
			cr.SetFinalizers(append(cr.GetFinalizers(), Finalizer))
			cr, err = c.Dynamic.Resource(PolicyResource).Update(ctx, cr, meta.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	}
	workloads := []workload{}
	for _, typ := range []struct{ kind, resource string }{{"Deployment", "deployments"}, {"StatefulSet", "statefulsets"}, {"DaemonSet", "daemonsets"}} {
		gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: typ.resource}
		list, e := c.Dynamic.Resource(gvr).Namespace("").List(ctx, meta.ListOptions{})
		if e != nil {
			return e
		}
		for i := range list.Items {
			if !excluded(list.Items[i].GetNamespace(), c.Namespace) {
				workloads = append(workloads, workload{&list.Items[i], typ.kind, gvr})
			}
		}
	}
	slices.SortFunc(workloads, func(a, b workload) int {
		return compare(a.kind+"/"+a.object.GetNamespace()+"/"+a.object.GetName(), b.kind+"/"+b.object.GetNamespace()+"/"+b.object.GetName())
	})
	images := map[string]bool{}
	if policy != nil && policy.DeletionTimestamp == nil {
		images[policy.Spec.Image] = true
		for _, rule := range policy.Spec.Rules {
			if rule.Image != "" {
				images[rule.Image] = true
			}
		}
	}
	for _, w := range workloads {
		s, e := readState(w.object)
		if e != nil {
			return e
		}
		if s != nil {
			if s.Current.Enabled {
				images[s.Current.Image] = true
			}
			if s.Stable.Enabled {
				images[s.Stable.Image] = true
			}
		}
	}
	native := map[string]bool{}
	pending := map[string]bool{}
	for image := range images {
		if c.Capabilities == nil {
			continue
		}
		yes, wait, e := c.Capabilities.Check(ctx, image)
		if e != nil {
			slog.Warn("image volume detection; using init container", "image", image, "error", e)
		}
		native[image] = yes
		pending[image] = wait
	}
	c.Store.Set(Snapshot{Policy: policy, NativeImages: native, Synced: true})
	pods, err := c.Kube.CoreV1().Pods("").List(ctx, meta.ListOptions{})
	if err != nil {
		return err
	}
	reports := []any{}
	phase := "Ready"
	cleanupComplete := true
	for _, w := range workloads {
		obj := w.object
		wanted := policy.Resolve(obj.GetNamespace(), w.kind, obj.GetName())
		state, e := readState(obj)
		if e != nil {
			return e
		}
		if state == nil && !wanted.Enabled {
			continue
		}
		report := map[string]any{"namespace": obj.GetNamespace(), "kind": w.kind, "name": obj.GetName()}
		problem := strategyProblem(obj, w.kind)
		if problem != "" {
			report["phase"] = "UnsupportedStrategy"
			report["message"] = problem
			reports = append(reports, report)
			phase = "Degraded"
			cleanupComplete = false
			continue
		}
		if state == nil && pending[wanted.Image] {
			report["phase"] = "DetectingCapabilities"
			reports = append(reports, report)
			if phase == "Ready" {
				phase = "Progressing"
			}
			cleanupComplete = false
			continue
		}
		health := Health{}
		if state != nil {
			health = workloadHealth(obj, w.kind, pods.Items, state.Current)
		}
		next, templateChanged := Advance(state, wanted, health, time.Now().UTC())
		if state == nil || !reflect.DeepEqual(*state, next) {
			if err = writeState(obj, next, templateChanged); err != nil {
				return err
			}
			obj, err = c.Dynamic.Resource(w.resource).Namespace(obj.GetNamespace()).Update(ctx, obj, meta.UpdateOptions{})
			if err != nil {
				return err
			}
			slog.Info("instrumentation state", "kind", w.kind, "namespace", obj.GetNamespace(), "name", obj.GetName(), "phase", next.Phase, "revision", next.Current.Revision())
		}
		if w.kind == "StatefulSet" && next.Phase == "RollingBack" {
			// OrderedReady may otherwise wait forever for the failed candidate.
			// Delete only unready Pods from the failed revision, after persisting
			// the restored template, with UID preconditions and normal grace periods.
			if err = c.recoverStatefulSet(ctx, obj, pods.Items, next); err != nil {
				return err
			}
		}
		report["phase"] = next.Phase
		report["revision"] = next.Current.Revision()
		report["message"] = next.Message
		if next.FailedRevision != "" {
			report["failedRevision"] = next.FailedRevision
		}
		if next.Phase == "Ready" && next.Current.Enabled {
			for _, pod := range pods.Items {
				if pod.Annotations[OwnerAnnotation] == string(obj.GetUID()) && pod.Annotations[RevisionAnnotation] == next.Current.Revision() && pod.DeletionTimestamp == nil && pod.Annotations[AppliedAnnotation] != next.Current.Revision() {
					report["phase"] = "InjectionMissing"
					report["message"] = "healthy pod did not receive the requested injection; inspect webhook events"
					phase = "Degraded"
				}
			}
		}
		reports = append(reports, report)
		if next.Phase == "RollingOut" || next.Phase == "RollingBack" {
			if phase == "Ready" {
				phase = "Progressing"
			}
		}
		if next.Phase == "RolledBack" || next.Phase == "RecoveryFailed" {
			phase = "Degraded"
		}
		if next.Current.Enabled || next.Phase != "Disabled" {
			cleanupComplete = false
		}
	}
	if cr == nil {
		return nil
	}
	if err = c.status(ctx, cr, phase, reports, ""); err != nil {
		return err
	}
	if cr.GetDeletionTimestamp() != nil && cleanupComplete && slices.Contains(cr.GetFinalizers(), Finalizer) {
		latest, e := c.Dynamic.Resource(PolicyResource).Get(ctx, PolicyName, meta.GetOptions{})
		if e != nil {
			return e
		}
		latest.SetFinalizers(slices.DeleteFunc(latest.GetFinalizers(), func(s string) bool { return s == Finalizer }))
		_, err = c.Dynamic.Resource(PolicyResource).Update(ctx, latest, meta.UpdateOptions{})
		return err
	}
	return nil
}

func (c *Controller) status(ctx context.Context, cr *unstructured.Unstructured, phase string, workloads []any, message string) error {
	if workloads == nil {
		workloads = []any{}
	}
	status := map[string]any{"observedGeneration": cr.GetGeneration(), "phase": phase, "message": message, "workloads": workloads}
	if reflect.DeepEqual(cr.Object["status"], status) {
		return nil
	}
	cr.Object["status"] = status
	_, err := c.Dynamic.Resource(PolicyResource).UpdateStatus(ctx, cr, meta.UpdateOptions{})
	return err
}

func (c *Controller) recoverStatefulSet(ctx context.Context, obj *unstructured.Unstructured, pods []core.Pod, s State) error {
	for _, pod := range pods {
		if pod.Namespace != obj.GetNamespace() || pod.Annotations[OwnerAnnotation] != string(obj.GetUID()) || pod.Annotations[RevisionAnnotation] != s.FailedRevision || pod.DeletionTimestamp != nil {
			continue
		}
		owned := false
		for _, ref := range pod.OwnerReferences {
			if ref.UID == obj.GetUID() && ref.Kind == "StatefulSet" && ref.Controller != nil && *ref.Controller {
				owned = true
			}
		}
		if !owned {
			continue
		}
		ready := false
		for _, cond := range pod.Status.Conditions {
			if cond.Type == core.PodReady && cond.Status == core.ConditionTrue {
				ready = true
			}
		}
		if ready {
			continue
		}
		uid := types.UID(pod.UID)
		if err := c.Kube.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, meta.DeleteOptions{Preconditions: &meta.Preconditions{UID: &uid}}); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("recover StatefulSet: %w", err)
		}
	}
	return nil
}

func compare(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
