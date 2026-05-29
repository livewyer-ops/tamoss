package rustfs

import (
	"context"
	"crypto/rand"
	"encoding/base64"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/resource"
)

const (
	AccessKeyEnvKey = "RUSTFS_ACCESS_KEY"
	SecretKeyEnvKey = "RUSTFS_SECRET_KEY"
	AccessKeyKey    = "accesskey"
	SecretKeyKey    = "secretkey"
)

type CredsSecretManager struct {
	Client client.Client
}

func (m CredsSecretManager) Ensure(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (string, error) {
	rustfsSpec := rustfsOperatorSpec(tamoss)
	if rustfsSpec.CredsSecret.ExistingSecret != "" {
		return rustfsSpec.CredsSecret.ExistingSecret, nil
	}
	name := tamoss.S3Connection().Auth.ExistingSecret
	if name == "" {
		return "", nil
	}

	secret := &corev1.Secret{}
	key := types.NamespacedName{Name: name, Namespace: tamoss.Namespace}
	if err := m.Client.Get(ctx, key, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", err
		}
		created, err := generatedSecret(tamoss, name, nil)
		if err != nil {
			return "", err
		}
		return name, m.Client.Create(ctx, created)
	}

	data, changed, err := secretDataWithAliases(secret.Data)
	if err != nil {
		return "", err
	}
	labelsChanged := resource.MergeLabels(secret, resource.TamossLabels(tamoss, "s3"))
	ownersChanged := resource.SetOwnerReferences(secret, resource.TamossOwnerReferences(tamoss))
	if changed || labelsChanged || ownersChanged {
		secret.Data = data
		if err := m.Client.Update(ctx, secret); err != nil {
			return "", err
		}
	}
	return name, nil
}

func generatedSecret(tamoss *tamossv1alpha1.Tamoss, name string, data map[string][]byte) (*corev1.Secret, error) {
	next, _, err := secretDataWithAliases(data)
	if err != nil {
		return nil, err
	}
	return &corev1.Secret{
		ObjectMeta: metav1ObjectMeta(tamoss, name),
		Type:       corev1.SecretTypeOpaque,
		Data:       next,
	}, nil
}

func secretDataWithAliases(existing map[string][]byte) (map[string][]byte, bool, error) {
	next := map[string][]byte{}
	for key, value := range existing {
		next[key] = append([]byte(nil), value...)
	}
	accessKey := firstSecretValue(next, AccessKeyEnvKey, AccessKeyKey)
	secretKey := firstSecretValue(next, SecretKeyEnvKey, SecretKeyKey)
	var err error
	if len(accessKey) == 0 {
		accessKey, err = generateCredential()
		if err != nil {
			return nil, false, err
		}
	}
	if len(secretKey) == 0 {
		secretKey, err = generateCredential()
		if err != nil {
			return nil, false, err
		}
	}
	changed := setSecretValue(next, AccessKeyEnvKey, accessKey)
	changed = setSecretValue(next, AccessKeyKey, accessKey) || changed
	changed = setSecretValue(next, SecretKeyEnvKey, secretKey) || changed
	changed = setSecretValue(next, SecretKeyKey, secretKey) || changed
	return next, changed, nil
}

func firstSecretValue(data map[string][]byte, keys ...string) []byte {
	for _, key := range keys {
		if value := data[key]; len(value) > 0 {
			return append([]byte(nil), value...)
		}
	}
	return nil
}

func setSecretValue(data map[string][]byte, key string, value []byte) bool {
	existing := data[key]
	if string(existing) == string(value) {
		return false
	}
	data[key] = append([]byte(nil), value...)
	return true
}

func generateCredential() ([]byte, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, err
	}
	value := base64.RawURLEncoding.EncodeToString(randomBytes)
	return []byte(value), nil
}

func metav1ObjectMeta(tamoss *tamossv1alpha1.Tamoss, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:            name,
		Namespace:       tamoss.Namespace,
		Labels:          resource.TamossLabels(tamoss, "s3"),
		OwnerReferences: resource.TamossOwnerReferences(tamoss),
	}
}
