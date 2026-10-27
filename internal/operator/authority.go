package operator

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	core "k8s.io/api/core/v1"
	errors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
)

const StateSignatureAnnotation = Prefix + "state-signature"
const authoritySecret = "rio-snapshot-authority"

// Authority authenticates persisted state, including recovery configurations.
// Its key never leaves the operator namespace. Hashes alone are not authority.
type Authority struct{ key []byte }

func LoadAuthority(ctx context.Context, kube kubernetes.Interface, namespace string) (*Authority, error) {
	secrets := kube.CoreV1().Secrets(namespace)
	secret, err := secrets.Get(ctx, authoritySecret, meta.GetOptions{})
	if errors.IsNotFound(err) {
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		immutable := true
		secret, err = secrets.Create(ctx, &core.Secret{ObjectMeta: meta.ObjectMeta{Name: authoritySecret}, Immutable: &immutable, Data: map[string][]byte{"key": key}}, meta.CreateOptions{})
		if errors.IsAlreadyExists(err) {
			secret, err = secrets.Get(ctx, authoritySecret, meta.GetOptions{})
		}
	}
	if err != nil {
		return nil, fmt.Errorf("load snapshot authority: %w", err)
	}
	if len(secret.Data["key"]) != 32 {
		return nil, fmt.Errorf("snapshot authority key must contain 32 bytes")
	}
	return &Authority{key: append([]byte(nil), secret.Data["key"]...)}, nil
}

func (a *Authority) signature(obj *unstructured.Unstructured, raw string) string {
	if a == nil || len(a.key) != 32 || obj.GetUID() == "" {
		return ""
	}
	b, _ := json.Marshal([]string{"rio-state-v1", obj.GetAPIVersion(), obj.GetKind(), obj.GetNamespace(), obj.GetName(), string(obj.GetUID()), raw})
	mac := hmac.New(sha256.New, a.key)
	_, _ = mac.Write(b)
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *Authority) readState(obj *unstructured.Unstructured) (*State, error) {
	raw := obj.GetAnnotations()[StateAnnotation]
	if raw == "" {
		return nil, nil
	}
	expected := a.signature(obj, raw)
	if expected == "" || !hmac.Equal([]byte(expected), []byte(obj.GetAnnotations()[StateSignatureAnnotation])) {
		return nil, fmt.Errorf("unauthenticated rollout state on %s %s/%s; restore authenticated state or remove legacy state to start a new rollout", obj.GetKind(), obj.GetNamespace(), obj.GetName())
	}
	return readState(obj)
}

func (a *Authority) writeState(obj *unstructured.Unstructured, state State, changeTemplate bool) error {
	if err := writeState(obj, state, changeTemplate); err != nil {
		return err
	}
	signature := a.signature(obj, obj.GetAnnotations()[StateAnnotation])
	if signature == "" {
		return fmt.Errorf("snapshot authority and workload UID are required")
	}
	annotations := obj.GetAnnotations()
	annotations[StateSignatureAnnotation] = signature
	obj.SetAnnotations(annotations)
	return nil
}
