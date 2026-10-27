package operator

import (
	"fmt"
	"reflect"
	"strings"

	core "k8s.io/api/core/v1"
)

// verifyInjection checks the delivery contract on webhook reinvocation. The
// applied annotation is a hint to validate, never proof that injection happened.
func verifyInjection(pod *core.Pod, cfg Config, native bool, service string) error {
	reject := func() error { return fmt.Errorf("applied instrumentation does not match the authorized configuration") }
	if !cfg.Enabled || pod.Annotations[AppliedAnnotation] != cfg.Revision() {
		return reject()
	}
	// Build the expected contract through the same injector with the application's
	// existing preload suffix. Other application fields do not affect the contract.
	expected := &core.Pod{ObjectMeta: pod.ObjectMeta, Spec: core.PodSpec{}}
	expected.Annotations = nil
	for _, container := range pod.Spec.Containers {
		c := core.Container{Name: container.Name}
		for _, env := range container.Env {
			if env.Name == "LD_PRELOAD" {
				prefix := MountPath + "/dist/injector/libotelinject.so"
				if env.ValueFrom != nil || (env.Value != prefix && !strings.HasPrefix(env.Value, prefix+" ")) {
					return reject()
				}
				if suffix := strings.TrimPrefix(env.Value, prefix); suffix != "" {
					c.Env = []core.EnvVar{{Name: "LD_PRELOAD", Value: strings.TrimPrefix(suffix, " ")}}
				}
			}
		}
		expected.Spec.Containers = append(expected.Spec.Containers, c)
	}
	if err := Mutate(expected, cfg, native, service); err != nil {
		return err
	}
	if pod.Annotations[Prefix+"delivery"] != expected.Annotations[Prefix+"delivery"] {
		return reject()
	}
	volumes := 0
	for _, vol := range pod.Spec.Volumes {
		if vol.Name == VolumeName {
			volumes++
			if !reflect.DeepEqual(vol, expected.Spec.Volumes[0]) {
				return reject()
			}
		}
	}
	if volumes != 1 {
		return reject()
	}
	inits := 0
	for _, init := range pod.Spec.InitContainers {
		if init.Name != VolumeName {
			continue
		}
		inits++
		if native {
			return reject()
		}
		want := expected.Spec.InitContainers[0]
		if init.Image != want.Image || init.ImagePullPolicy != want.ImagePullPolicy || !reflect.DeepEqual(init.Command, want.Command) || len(init.Args) != 0 || init.WorkingDir != "" || !reflect.DeepEqual(init.SecurityContext, want.SecurityContext) || !reflect.DeepEqual(init.Env, want.Env) || len(init.EnvFrom) != 0 || !reflect.DeepEqual(init.VolumeMounts, want.VolumeMounts) || init.RestartPolicy != nil {
			return reject()
		}
	}
	if (!native && inits != 1) || (native && inits != 0) {
		return reject()
	}
	for i, container := range pod.Spec.Containers {
		mounts := 0
		for _, mount := range container.VolumeMounts {
			if mount.Name == VolumeName || mount.MountPath == MountPath {
				mounts++
				if !reflect.DeepEqual(mount, expected.Spec.Containers[i].VolumeMounts[0]) {
					return reject()
				}
			}
		}
		if mounts != 1 {
			return reject()
		}
		for _, want := range expected.Spec.Containers[i].Env {
			count := 0
			for _, env := range container.Env {
				if env.Name == want.Name {
					count++
					// Kubernetes defaults downward API selectors between admission rounds.
					normalize := func(v core.EnvVar) core.EnvVar {
						if v.ValueFrom != nil && v.ValueFrom.FieldRef != nil {
							v.ValueFrom = v.ValueFrom.DeepCopy()
							if v.ValueFrom.FieldRef.APIVersion == "" {
								v.ValueFrom.FieldRef.APIVersion = "v1"
							}
						}
						return v
					}
					if !reflect.DeepEqual(normalize(env), normalize(want)) {
						return reject()
					}
				}
			}
			if count != 1 {
				return reject()
			}
		}
	}
	return nil
}
