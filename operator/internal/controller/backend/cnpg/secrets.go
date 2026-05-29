package cnpg

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

type SecretReader struct {
	Client client.Client
}

type Secrets struct {
	App       *corev1.Secret
	Superuser *corev1.Secret
}

type SecretReadiness struct {
	Ready   bool
	Reason  string
	Message string
}

func (r SecretReader) Read(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (Secrets, SecretReadiness, error) {
	app, ok, err := r.read(ctx, tamoss, AppSecretName(tamoss))
	if err != nil || !ok {
		return Secrets{}, waitingForSecret(AppSecretName(tamoss)), err
	}
	if missingKey := firstMissingKey(app, "username", "password"); missingKey != "" {
		return Secrets{App: app}, missingSecretKey(AppSecretName(tamoss), missingKey), nil
	}
	superuser, ok, err := r.read(ctx, tamoss, SuperuserSecretName(tamoss))
	if err != nil || !ok {
		return Secrets{App: app}, waitingForSecret(SuperuserSecretName(tamoss)), err
	}
	if missingKey := firstMissingKey(superuser, "username", "password"); missingKey != "" {
		return Secrets{App: app, Superuser: superuser}, missingSecretKey(SuperuserSecretName(tamoss), missingKey), nil
	}
	return Secrets{App: app, Superuser: superuser}, SecretReadiness{
		Ready:   true,
		Reason:  operatorstatus.ReasonCNPGSecretsReady,
		Message: "CNPG app and superuser Secrets are ready",
	}, nil
}

func (r SecretReader) read(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, name string) (*corev1.Secret, bool, error) {
	secret := &corev1.Secret{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: tamoss.Namespace}, secret)
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return secret, true, nil
}

func AppSecretName(tamoss *tamossv1alpha1.Tamoss) string {
	return tamoss.ResourceName("db-app")
}

func SuperuserSecretName(tamoss *tamossv1alpha1.Tamoss) string {
	return tamoss.ResourceName("db-superuser")
}

func firstMissingKey(secret *corev1.Secret, keys ...string) string {
	for _, key := range keys {
		if len(secret.Data[key]) == 0 {
			return key
		}
	}
	return ""
}

func waitingForSecret(name string) SecretReadiness {
	return SecretReadiness{
		Ready:   false,
		Reason:  operatorstatus.ReasonWaitingForCNPGSecret,
		Message: fmt.Sprintf("Waiting for CNPG Secret %s", name),
	}
}

func missingSecretKey(secretName, key string) SecretReadiness {
	return SecretReadiness{
		Ready:   false,
		Reason:  operatorstatus.ReasonCNPGSecretKeyMissing,
		Message: fmt.Sprintf("Required key %s is missing from CNPG Secret %s", key, secretName),
	}
}
