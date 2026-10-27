package operator

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	registration "k8s.io/api/admissionregistration/v1"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Run only against a disposable cluster via scripts/admission-e2e.sh. The API
// server connects to this test's actual TLS webhook through its host gateway.
func TestAdmissionKubernetesAPI(t *testing.T) {
	host := os.Getenv("RIO_TEST_WEBHOOK_HOST")
	if host == "" {
		t.Skip("use scripts/admission-e2e.sh with a disposable kind cluster")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		t.Fatal("RIO_TEST_WEBHOOK_HOST must be an IP reachable from the API server")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Timeout = 5 * time.Second
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	selected, other := "rio-issue-1-selected", "rio-issue-1-other"
	for _, ns := range []string{selected, other} {
		if _, err := kube.CoreV1().Namespaces().Create(ctx, &core.Namespace{ObjectMeta: meta.ObjectMeta{Name: ns}}, meta.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	store := &Store{}
	webhook := &Webhook{Authority: testAuthority(), Store: store, Kube: kube, Namespace: "rio-system"}
	var calls atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		webhook.ServeHTTP(w, r)
	}))
	server.Listener.Close()
	server.Listener, err = net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	cert, ca := admissionTestCertificate(t, ip)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	url := "https://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/mutate"
	manifest, err := os.Open("../../config/install.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()
	decoder := yaml.NewYAMLOrJSONDecoder(manifest, 4096)
	var installed registration.MutatingWebhookConfiguration
	for {
		var obj unstructured.Unstructured
		if err := decoder.Decode(&obj); err != nil {
			if err == io.EOF {
				t.Fatal("installation has no MutatingWebhookConfiguration")
			}
			t.Fatal(err)
		}
		if obj.GetKind() == "MutatingWebhookConfiguration" {
			// The source template deliberately contains a non-base64 CA placeholder.
			webhooks := obj.Object["webhooks"].([]any)
			webhooks[0].(map[string]any)["clientConfig"].(map[string]any)["caBundle"] = ""
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &installed); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	installed.Name = "rio-issue-1-admission"
	installed.ResourceVersion = ""
	installed.Webhooks[0].ClientConfig = registration.WebhookClientConfig{URL: &url, CABundle: ca}
	installed.Webhooks[0].TimeoutSeconds = ptrInt32(2)
	created, err := kube.AdmissionregistrationV1().MutatingWebhookConfigurations().Create(ctx, &installed, meta.CreateOptions{})
	if err == nil {
		installed = *created
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = kube.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(context.Background(), installed.Name, meta.DeleteOptions{})
	}()
	if installed.Webhooks[0].FailurePolicy == nil || *installed.Webhooks[0].FailurePolicy != registration.Ignore {
		t.Fatal("fixture must exercise shipped failurePolicy: Ignore")
	}
	pod := func(name string) *core.Pod {
		return &core.Pod{ObjectMeta: meta.ObjectMeta{Name: name}, Spec: core.PodSpec{Containers: []core.Container{{Name: "app", Image: "registry.k8s.io/pause:3.10"}}}}
	}
	create := func(ns string, p *core.Pod) (*core.Pod, error) {
		return kube.CoreV1().Pods(ns).Create(ctx, p, meta.CreateOptions{DryRun: []string{meta.DryRunAll}})
	}
	unchanged := func(ns string, p *core.Pod) {
		t.Helper()
		got, err := create(ns, p)
		if err != nil {
			t.Fatal(err)
		}
		if got.Annotations[AppliedAnnotation] != "" {
			t.Fatalf("unexpected injection: %v", got.Annotations)
		}
	}
	// Admission configuration propagates asynchronously. Require a real call
	// before asserting startup behavior so Ignore cannot hide a broken fixture.
	eventuallyAPI(t, ctx, func() bool {
		before := calls.Load()
		unchanged(other, pod("startup"))
		return calls.Load() > before
	})
	t.Log("startup: reachable unsynchronized webhook allowed Pod without injection")
	p := testPolicy()
	p.Spec.Rules[0].Selector.Namespaces = []string{selected}
	p.Spec.Image = "ghcr.io/otelx/rio/instrumentation:issue-1-api"
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(p)
	if err != nil {
		t.Fatal(err)
	}
	obj["apiVersion"], obj["kind"] = Group+"/v1alpha1", "Instrumentation"
	cr, err := dyn.Resource(PolicyResource).Create(ctx, &unstructured.Unstructured{Object: obj}, meta.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	controller := &Controller{Authority: webhook.Authority, Kube: kube, Dynamic: dyn, Namespace: "rio-system", Store: store}
	// A broken workload in an unselected namespace and a healthy selected
	// workload exercise state isolation using persisted API objects.
	for _, ns := range []string{selected, other} {
		zero := int32(0)
		dep := &apps.Deployment{ObjectMeta: meta.ObjectMeta{Name: "fixture", Namespace: ns}, Spec: apps.DeploymentSpec{Replicas: &zero, Selector: &meta.LabelSelector{MatchLabels: map[string]string{"fixture": "isolation"}}, Template: core.PodTemplateSpec{ObjectMeta: meta.ObjectMeta{Labels: map[string]string{"fixture": "isolation"}}, Spec: pod("fixture").Spec}}}
		if ns == other {
			dep.Annotations = map[string]string{StateAnnotation: "{"}
		}
		if _, err := kube.AppsV1().Deployments(ns).Create(ctx, dep, meta.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if !store.Get().Synced {
		t.Fatal("malformed state blocked policy synchronization")
	}
	// The Deployment controller can update status between our list and write.
	// A conflict is workload-local and retries on the next reconciliation; wait
	// for convergence rather than assuming the first write must win that race.
	eventuallyAPI(t, ctx, func() bool {
		if err := controller.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
		cr, err = dyn.Resource(PolicyResource).Get(ctx, PolicyName, meta.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		reports, _, _ := unstructured.NestedSlice(cr.Object, "status", "workloads")
		phases := map[string]string{}
		for _, entry := range reports {
			report := entry.(map[string]any)
			phases[report["namespace"].(string)] = report["phase"].(string)
		}
		if phases[other] != "Error" || phases[selected] != "Ready" {
			t.Logf("waiting for healthy workload alongside malformed state: %v", reports)
			return false
		}
		return true
	})
	t.Log("malformed persisted state reported; healthy selected workload reached Ready and policy published")
	got, err := create(selected, pod("selected"))
	if err != nil || got.Annotations[AppliedAnnotation] == "" {
		t.Fatalf("webhook did not inject selected Pod: %v %v", got, err)
	}
	// Exercise saved snapshots through the actual API server, including the
	// authenticated creator supplied to admission (not caller-owned metadata).
	deployment, err := kube.AppsV1().Deployments(selected).Get(ctx, "fixture", meta.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	zero := int32(0)
	replicaSet, err := kube.AppsV1().ReplicaSets(selected).Create(ctx, &apps.ReplicaSet{
		ObjectMeta: meta.ObjectMeta{Name: "snapshot", OwnerReferences: []meta.OwnerReference{ownerRef("Deployment", deployment.Name, string(deployment.UID))}},
		Spec:       apps.ReplicaSetSpec{Replicas: &zero, Selector: deployment.Spec.Selector.DeepCopy(), Template: *deployment.Spec.Template.DeepCopy()},
	}, meta.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	owned := pod("authorized-snapshot")
	owned.Annotations = replicaSet.Spec.Template.Annotations
	owned.OwnerReferences = []meta.OwnerReference{ownerRef("ReplicaSet", replicaSet.Name, string(replicaSet.UID))}
	controllerConfig := rest.CopyConfig(cfg)
	controllerConfig.Impersonate.UserName = "system:serviceaccount:kube-system:replicaset-controller"
	controllerKube, err := kubernetes.NewForConfig(controllerConfig)
	if err != nil {
		t.Fatal(err)
	}
	createOwned := func(p *core.Pod) (*core.Pod, error) {
		return controllerKube.CoreV1().Pods(selected).Create(ctx, p, meta.CreateOptions{DryRun: []string{meta.DryRunAll}})
	}
	injected, err := createOwned(owned)
	if err != nil || injected.Annotations[AppliedAnnotation] != owned.Annotations[RevisionAnnotation] {
		t.Fatalf("authorized snapshot not injected: %v %v", injected, err)
	}
	denied := func(name string, p *core.Pod, controller bool) {
		t.Helper()
		var err error
		if controller {
			_, err = createOwned(p)
		} else {
			_, err = create(selected, p)
		}
		if err == nil || !strings.Contains(err.Error(), "denied the request") {
			t.Fatalf("%s was not explicitly rejected: %v", name, err)
		}
	}
	// An ordinary Pod creator cannot borrow the controller's snapshot, even
	// with a real owner chain and matching revision hash.
	denied("copied snapshot from ordinary creator", owned, false)
	forged := owned.DeepCopy()
	var forgedConfig Config
	if err := json.Unmarshal([]byte(forged.Annotations[ConfigAnnotation]), &forgedConfig); err != nil {
		t.Fatal(err)
	}
	forgedConfig.Image = "ghcr.io/otelx/rio/instrumentation:issue-2-forged"
	encoded, err := json.Marshal(forgedConfig)
	if err != nil {
		t.Fatal(err)
	}
	forged.Annotations[ConfigAnnotation] = string(encoded)
	forged.Annotations[RevisionAnnotation] = forgedConfig.Revision()
	denied("forged configuration with matching hash", forged, true)
	stale := owned.DeepCopy()
	stale.Annotations[RevisionAnnotation] = "stale"
	denied("stale saved revision", stale, true)
	malformed := owned.DeepCopy()
	malformed.Annotations[ConfigAnnotation] = "{"
	denied("malformed snapshot", malformed, true)
	marker := pod("forged-applied")
	marker.Annotations = map[string]string{AppliedAnnotation: owned.Annotations[RevisionAnnotation]}
	denied("applied marker without injected structure", marker, false)
	copied := owned.DeepCopy()
	copied.Name = "copied-unselected"
	unchanged(other, copied)
	// Workload editors cannot authorize a configuration by editing its state.
	patchState := func(raw string) {
		t.Helper()
		patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]any{StateAnnotation: raw}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := kube.AppsV1().Deployments(selected).Patch(ctx, deployment.Name, types.MergePatchType, patch, meta.PatchOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	originalState := deployment.Annotations[StateAnnotation]
	patchState(`{"current":{"enabled":true,"image":"forged"}}`)
	denied("edited unsigned workload state", owned, true)
	patchState(originalState)
	t.Log("snapshot authority: authenticated controller accepted; forged/copied/stale/malformed snapshots, unsigned state, and forged applied markers rejected; unselected copy admitted unchanged")

	yes := true
	for _, ns := range []string{other, selected} {
		missing := pod("missing-owner")
		missing.OwnerReferences = []meta.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "absent", UID: "absent", Controller: &yes}}
		unchanged(ns, missing)
	}
	t.Log("unselected and indeterminate missing-owner Pods admitted without mutation")
	// A reachable webhook's explicit denial remains a denial with Ignore. This
	// is a control for the API semantics, not a test-only substitute webhook.
	conflict := pod("unsafe-mutation")
	conflict.Spec.Volumes = []core.Volume{{Name: VolumeName, VolumeSource: core.VolumeSource{EmptyDir: &core.EmptyDirVolumeSource{}}}}
	if _, err := create(selected, conflict); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("Ignore swallowed an explicit rejection: %v", err)
	}
	t.Log("failurePolicy Ignore preserved explicit selected-workload rejection")
	// System exclusions must happen at API matching time, before our handler.
	for _, ns := range []string{"kube-system", "kube-public", "kube-node-lease"} {
		before := calls.Load()
		unchanged(ns, pod("excluded"))
		if calls.Load() != before {
			t.Fatalf("system namespace %s reached webhook", ns)
		}
	}
	for _, priority := range []string{"system-node-critical", "system-cluster-critical"} {
		critical := pod("critical")
		critical.Spec.PriorityClassName = priority
		before := calls.Load()
		unchanged(selected, critical)
		if calls.Load() != before {
			t.Fatalf("critical priority %s reached webhook", priority)
		}
	}
	t.Log("shipped namespaceSelector and matchConditions excluded system workloads before webhook dispatch")
	// Duplicate names pass the structural schema but violate policy validation.
	cr, err = dyn.Resource(PolicyResource).Get(ctx, PolicyName, meta.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rules, _, _ := unstructured.NestedSlice(cr.Object, "spec", "rules")
	_ = unstructured.SetNestedSlice(cr.Object, append(rules, rules[0]), "spec", "rules")
	if _, err := dyn.Resource(PolicyResource).Update(ctx, cr, meta.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if store.Get().Synced {
		t.Fatal("invalid policy left admission enabled")
	}
	unchanged(other, pod("invalid-policy"))
	unchanged(selected, pod("invalid-policy"))
	cr, err = dyn.Resource(PolicyResource).Get(ctx, PolicyName, meta.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	phase, _, _ := unstructured.NestedString(cr.Object, "status", "phase")
	if phase != "Invalid" {
		t.Fatalf("invalid policy status: %v", cr.Object["status"])
	}
	t.Log("invalid policy reported; selected and unrelated Pods admitted without mutation")
	_ = unstructured.SetNestedSlice(cr.Object, rules, "spec", "rules")
	if _, err := dyn.Resource(PolicyResource).Update(ctx, cr, meta.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	// An unreachable policy API simulates synchronization failure after success.
	unavailable, err := dynamic.NewForConfig(&rest.Config{Host: "http://127.0.0.1:1", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	controller.Dynamic = unavailable
	if err := controller.Reconcile(ctx); err == nil {
		t.Fatal("expected synchronization failure")
	}
	if store.Get().Synced {
		t.Fatal("stale policy survived synchronization failure")
	}
	unchanged(other, pod("sync-failure"))
	unchanged(selected, pod("sync-failure"))
	t.Log("synchronization failure discarded stale policy and admitted Pods unchanged")
	// Finally stop the TLS endpoint. Ignore should bypass transport failure even
	// for a Pod that the reachable selected-workload webhook would reject.
	store.Set(Snapshot{Policy: p, Synced: true})
	server.Close()
	unchanged(selected, conflict)
	t.Log("failurePolicy Ignore allowed Pod when webhook transport was unavailable")
}

func ptrInt32(n int32) *int32 { return &n }

func eventuallyAPI(t *testing.T, ctx context.Context, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if check() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("Kubernetes API condition did not converge before the deadline")
}

func admissionTestCertificate(t *testing.T, ip net.IP) (tls.Certificate, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "rio-issue-1"}, IPAddresses: []net.IP{ip}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	cert, err := tls.X509KeyPair(ca, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if err != nil {
		t.Fatal(fmt.Errorf("webhook certificate: %w", err))
	}
	return cert, ca
}
