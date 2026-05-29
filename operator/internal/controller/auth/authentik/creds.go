package authentik

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
	ClientIDKey     = "client_id"
	ClientSecretKey = "client_secret"

	ClientIDEnvKey     = "TAMOSS_OAUTH_CLIENT_ID"
	ClientSecretEnvKey = "TAMOSS_OAUTH_CLIENT_SECRET"
)

type Credentials struct {
	ClientID     []byte
	ClientSecret []byte
}

type CredsManager struct {
	Client client.Client
}

func (m CredsManager) Ensure(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (string, Credentials, error) {
	name := tamoss.OAuth2CredentialsSecretName()
	secret := &corev1.Secret{}
	key := types.NamespacedName{Name: name, Namespace: tamoss.Namespace}
	if err := m.Client.Get(ctx, key, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", Credentials{}, err
		}
		created, credentials, err := generatedSecret(tamoss, name, nil)
		if err != nil {
			return "", Credentials{}, err
		}
		return name, credentials, m.Client.Create(ctx, created)
	}

	data, credentials, changed, err := secretData(secret.Data)
	if err != nil {
		return "", Credentials{}, err
	}
	labelsChanged := resource.MergeLabels(secret, resource.TamossLabels(tamoss, "auth"))
	ownersChanged := resource.SetOwnerReferences(secret, resource.TamossOwnerReferences(tamoss))
	if changed || labelsChanged || ownersChanged {
		secret.Data = data
		if err := m.Client.Update(ctx, secret); err != nil {
			return "", Credentials{}, err
		}
	}
	return name, credentials, nil
}

func generatedSecret(tamoss *tamossv1alpha1.Tamoss, name string, data map[string][]byte) (*corev1.Secret, Credentials, error) {
	next, credentials, _, err := secretData(data)
	if err != nil {
		return nil, Credentials{}, err
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       tamoss.Namespace,
			Labels:          resource.TamossLabels(tamoss, "auth"),
			OwnerReferences: resource.TamossOwnerReferences(tamoss),
		},
		Type: corev1.SecretTypeOpaque,
		Data: next,
	}, credentials, nil
}

func secretData(existing map[string][]byte) (map[string][]byte, Credentials, bool, error) {
	next := map[string][]byte{}
	for key, value := range existing {
		next[key] = append([]byte(nil), value...)
	}
	clientID := firstSecretValue(next, ClientIDKey, ClientIDEnvKey)
	clientSecret := firstSecretValue(next, ClientSecretKey, ClientSecretEnvKey)
	var err error
	if len(clientID) == 0 {
		clientID, err = randomCredential(16)
		if err != nil {
			return nil, Credentials{}, false, err
		}
	}
	if len(clientSecret) == 0 {
		clientSecret, err = randomCredential(32)
		if err != nil {
			return nil, Credentials{}, false, err
		}
	}
	changed := setSecretValue(next, ClientIDKey, clientID)
	changed = setSecretValue(next, ClientIDEnvKey, clientID) || changed
	changed = setSecretValue(next, ClientSecretKey, clientSecret) || changed
	changed = setSecretValue(next, ClientSecretEnvKey, clientSecret) || changed
	return next, Credentials{ClientID: clientID, ClientSecret: clientSecret}, changed, nil
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

func randomCredential(size int) ([]byte, error) {
	randomBytes := make([]byte, size)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, err
	}
	value := base64.RawURLEncoding.EncodeToString(randomBytes)
	return []byte(value), nil
}
