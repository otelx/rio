package operator

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	core "k8s.io/api/core/v1"
	errors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
)

type Capabilities struct {
	Kube      kubernetes.Interface
	Namespace string
}

func runtimeSupportsImageVolume(node core.Node) bool {
	parts := strings.SplitN(node.Status.NodeInfo.ContainerRuntimeVersion, "://", 2)
	if len(parts) != 2 {
		return false
	}
	minimum := ""
	switch parts[0] {
	case "containerd":
		minimum = "2.1.0"
	case "cri-o":
		minimum = "1.31.0"
	default:
		return false
	}
	runtime, err := version.ParseSemantic(parts[1])
	if err != nil {
		return false
	}
	kubelet, err := version.ParseSemantic(node.Status.NodeInfo.KubeletVersion)
	if err != nil {
		return false
	}
	return runtime.AtLeast(version.MustParseSemantic(minimum)) && kubelet.AtLeast(version.MustParseSemantic("1.31.0"))
}

// Check confirms capabilities on every registered Linux node. This is deliberately
// conservative in mixed clusters; it does not modify workload scheduling. The
// probe verifies actual image mounting and executable library mapping, including
// feature gates that cannot be inferred from version strings.
func (c *Capabilities) Check(ctx context.Context, image string) (native, pending bool, err error) {
	server, err := c.Kube.Discovery().ServerVersion()
	if err != nil {
		return false, false, err
	}
	v, err := version.ParseSemantic(server.GitVersion)
	if err != nil || !v.AtLeast(version.MustParseSemantic("1.31.0")) {
		return false, false, nil
	}
	nodes, err := c.Kube.CoreV1().Nodes().List(ctx, meta.ListOptions{})
	if err != nil {
		return false, false, err
	}
	eligible := []core.Node{}
	for _, node := range nodes.Items {
		if node.Status.NodeInfo.OperatingSystem != "linux" {
			continue
		}
		if !runtimeSupportsImageVolume(node) {
			return false, false, nil
		}
		eligible = append(eligible, node)
	}
	if len(eligible) == 0 {
		return false, false, nil
	}
	all := true
	for _, node := range eligible {
		hash := sha256.Sum256([]byte(string(node.UID) + node.Status.NodeInfo.ContainerRuntimeVersion + node.Status.NodeInfo.KubeletVersion + image))
		name := fmt.Sprintf("image-volume-probe-%x", hash[:10])
		pod, e := c.Kube.CoreV1().Pods(c.Namespace).Get(ctx, name, meta.GetOptions{})
		if errors.IsNotFound(e) {
			seconds := int64(60)
			no := false
			yes := true
			uid := int64(65532)
			probe := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: name, Namespace: c.Namespace, Labels: map[string]string{Prefix + "component": "image-volume-probe"}}, Spec: core.PodSpec{
				NodeName: node.Name, RestartPolicy: core.RestartPolicyNever, ActiveDeadlineSeconds: &seconds, AutomountServiceAccountToken: &no,
				Tolerations:     []core.Toleration{{Operator: core.TolerationOpExists}},
				SecurityContext: &core.PodSecurityContext{RunAsUser: &uid, RunAsNonRoot: &yes, SeccompProfile: &core.SeccompProfile{Type: core.SeccompProfileTypeRuntimeDefault}},
				Containers: []core.Container{{Name: "check", Image: image, Command: []string{"/bin/sh", "-ec", `test -r ` + MountPath + `/dist/injector/libotelinject.so; output="$(LD_PRELOAD=` + MountPath + `/dist/injector/libotelinject.so /bin/sh -c true 2>&1)"; test -z "$output"`},
					SecurityContext: &core.SecurityContext{AllowPrivilegeEscalation: &no, ReadOnlyRootFilesystem: &yes, Capabilities: &core.Capabilities{Drop: []core.Capability{"ALL"}}},
					VolumeMounts:    []core.VolumeMount{{Name: "payload", MountPath: MountPath, ReadOnly: true}}}},
				Volumes: []core.Volume{{Name: "payload", VolumeSource: core.VolumeSource{Image: &core.ImageVolumeSource{Reference: image, PullPolicy: core.PullIfNotPresent}}}},
			}}
			_, e = c.Kube.CoreV1().Pods(c.Namespace).Create(ctx, probe, meta.CreateOptions{})
			if e != nil && !errors.IsAlreadyExists(e) {
				// API feature-gate rejection is evidence to use the portable path.
				if errors.IsInvalid(e) || errors.IsForbidden(e) {
					return false, false, nil
				}
				return false, false, e
			}
			all = false
			pending = true
			continue
		}
		if e != nil {
			return false, false, e
		}
		if pod.Status.Phase == core.PodSucceeded {
			continue
		}
		if pod.Status.Phase == core.PodFailed || time.Since(pod.CreationTimestamp.Time) > 75*time.Second {
			return false, false, nil
		}
		all = false
		pending = true
	}
	return all, pending, nil
}
