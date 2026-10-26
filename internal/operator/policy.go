package operator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	Group              = "rio.otelx.github.io"
	PolicyName         = "cluster"
	Prefix             = Group + "/"
	StateAnnotation    = Prefix + "state"
	ConfigAnnotation   = Prefix + "configuration"
	RevisionAnnotation = Prefix + "revision"
	AppliedAnnotation  = Prefix + "applied-revision"
	OwnerAnnotation    = Prefix + "workload-uid"
	Finalizer          = Prefix + "remove-instrumentation"
	MountPath          = "/__otel_sdk_auto_instrumentation__"
	VolumeName         = "rio-payload"
)

var PolicyResource = schema.GroupVersionResource{Group: Group, Version: "v1alpha1", Resource: "instrumentations"}

type Signals struct {
	Traces  bool `json:"traces"`
	Metrics bool `json:"metrics"`
	Logs    bool `json:"logs"`
}

type SignalOverrides struct {
	Traces  *bool `json:"traces,omitempty"`
	Metrics *bool `json:"metrics,omitempty"`
	Logs    *bool `json:"logs,omitempty"`
}

type WorkloadSelector struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type Selector struct {
	Namespaces []string           `json:"namespaces"`
	Workloads  []WorkloadSelector `json:"workloads,omitempty"`
}

type Rule struct {
	Name     string          `json:"name"`
	Selector Selector        `json:"selector"`
	Action   string          `json:"action"`
	Image    string          `json:"image,omitempty"`
	Endpoint string          `json:"endpoint,omitempty"`
	Signals  SignalOverrides `json:"signals,omitempty"`
	Env      []core.EnvVar   `json:"env,omitempty"`
}

type Spec struct {
	Image          string        `json:"image"`
	Endpoint       string        `json:"endpoint"`
	Signals        Signals       `json:"signals"`
	Env            []core.EnvVar `json:"env,omitempty"`
	TimeoutSeconds int           `json:"timeoutSeconds"`
	RetryToken     string        `json:"retryToken,omitempty"`
	Rules          []Rule        `json:"rules"`
}

type Policy struct {
	meta.TypeMeta   `json:",inline"`
	meta.ObjectMeta `json:"metadata"`
	Spec            Spec `json:"spec"`
}

// Config is a resolved, immutable instrumentation revision. It is carried in the
// workload's Pod template so admission and rollback never race a policy cache.
type Config struct {
	Enabled        bool          `json:"enabled"`
	Image          string        `json:"image,omitempty"`
	Endpoint       string        `json:"endpoint,omitempty"`
	Signals        Signals       `json:"signals"`
	Env            []core.EnvVar `json:"env,omitempty"`
	RetryToken     string        `json:"retryToken,omitempty"`
	TimeoutSeconds int           `json:"timeoutSeconds,omitempty"`
}

func (c Config) Revision() string {
	b, _ := json.Marshal(c)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])[:24]
}

func (p *Policy) Resolve(namespace, kind, name string) Config {
	off := Config{TimeoutSeconds: 300}
	if p == nil {
		return off
	}
	off.RetryToken = p.Spec.RetryToken
	off.TimeoutSeconds = p.Spec.TimeoutSeconds
	if p.DeletionTimestamp != nil {
		return off
	}
	for _, rule := range p.Spec.Rules {
		if !rule.Selector.Matches(namespace, kind, name) {
			continue
		}
		if rule.Action == "Skip" {
			return off
		}
		c := Config{Enabled: true, Image: p.Spec.Image, Endpoint: p.Spec.Endpoint, Signals: p.Spec.Signals,
			Env: append([]core.EnvVar(nil), p.Spec.Env...), RetryToken: p.Spec.RetryToken, TimeoutSeconds: p.Spec.TimeoutSeconds}
		if rule.Image != "" {
			c.Image = rule.Image
		}
		if rule.Endpoint != "" {
			c.Endpoint = rule.Endpoint
		}
		if rule.Signals.Traces != nil {
			c.Signals.Traces = *rule.Signals.Traces
		}
		if rule.Signals.Metrics != nil {
			c.Signals.Metrics = *rule.Signals.Metrics
		}
		if rule.Signals.Logs != nil {
			c.Signals.Logs = *rule.Signals.Logs
		}
		c.Env = mergeEnv(c.Env, rule.Env)
		return c
	}
	return off
}

func (s Selector) Matches(namespace, kind, name string) bool {
	match := false
	for _, ns := range s.Namespaces {
		if ns == "*" || ns == namespace {
			match = true
			break
		}
	}
	if !match {
		return false
	}
	if len(s.Workloads) == 0 {
		return true
	}
	for _, w := range s.Workloads {
		if w.Kind == kind && (w.Name == "*" || w.Name == name) {
			return true
		}
	}
	return false
}

func excluded(namespace, operatorNamespace string) bool {
	return namespace == operatorNamespace
}

func (p *Policy) Validate() error {
	if p.Name != PolicyName {
		return fmt.Errorf("the single Instrumentation resource must be named %q", PolicyName)
	}
	if p.Spec.TimeoutSeconds < 10 {
		return fmt.Errorf("timeoutSeconds must be at least 10")
	}
	if p.Spec.Image == "" {
		return fmt.Errorf("image is required")
	}
	if err := validateEndpoint(p.Spec.Endpoint); err != nil {
		return err
	}
	if err := validateEnv(p.Spec.Env); err != nil {
		return err
	}
	names := map[string]bool{}
	for _, r := range p.Spec.Rules {
		if r.Name == "" || names[r.Name] {
			return fmt.Errorf("rule names must be nonempty and unique")
		}
		names[r.Name] = true
		if r.Action != "Inject" && r.Action != "Skip" {
			return fmt.Errorf("invalid action %q", r.Action)
		}
		if len(r.Selector.Namespaces) == 0 {
			return fmt.Errorf("rule %s requires namespaces; use [\"*\"] for all", r.Name)
		}
		for _, ns := range r.Selector.Namespaces {
			if ns == "" || (ns != "*" && strings.ContainsAny(ns, "*?[]")) {
				return fmt.Errorf("use exact namespace names or *")
			}
		}
		for _, w := range r.Selector.Workloads {
			if w.Kind != "Deployment" && w.Kind != "StatefulSet" && w.Kind != "DaemonSet" && w.Kind != "Pod" && w.Kind != "Job" && w.Kind != "CronJob" && w.Kind != "ReplicaSet" {
				return fmt.Errorf("unsupported selector kind %s", w.Kind)
			}
			if w.Name == "" {
				return fmt.Errorf("workload name is required; use * for all")
			}
		}
		if r.Endpoint != "" {
			if err := validateEndpoint(r.Endpoint); err != nil {
				return err
			}
		}
		if err := validateEnv(r.Env); err != nil {
			return err
		}
	}
	return nil
}

func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("endpoint must be an absolute http(s) OTLP endpoint")
	}
	return nil
}

func validateEnv(env []core.EnvVar) error {
	seen := map[string]bool{}
	for _, e := range env {
		if !strings.HasPrefix(e.Name, "OTEL_") || seen[e.Name] {
			return fmt.Errorf("env entries must have unique OTEL_ names: %s", e.Name)
		}
		seen[e.Name] = true
		if e.Name == "OTEL_TRACES_EXPORTER" || e.Name == "OTEL_METRICS_EXPORTER" || e.Name == "OTEL_LOGS_EXPORTER" || strings.HasPrefix(e.Name, "OTEL_INJECTOR_") || e.Name == "OTEL_EXPORTER_OTLP_ENDPOINT" || e.Name == "OTEL_EXPORTER_OTLP_PROTOCOL" {
			return fmt.Errorf("%s is managed by typed configuration", e.Name)
		}
		if e.ValueFrom != nil && e.Value != "" {
			return fmt.Errorf("env %s has both value and valueFrom", e.Name)
		}
	}
	return nil
}

func mergeEnv(base, overrides []core.EnvVar) []core.EnvVar {
	result := append([]core.EnvVar(nil), base...)
	for _, v := range overrides {
		found := false
		for i := range result {
			if result[i].Name == v.Name {
				result[i] = v
				found = true
				break
			}
		}
		if !found {
			result = append(result, v)
		}
	}
	return result
}
