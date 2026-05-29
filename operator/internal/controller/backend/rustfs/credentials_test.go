package rustfs

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCredsSecretManagerGeneratesCredentials(t *testing.T) {
	ctx := context.Background()
	tamoss := rustfsTamossFixture()
	client := fakeClient()
	manager := CredsSecretManager{Client: client}

	name, err := manager.Ensure(ctx, tamoss)
	if err != nil {
		t.Fatalf("ensure failed: %v", err)
	}
	if name != "example-s3-creds" {
		t.Fatalf("expected generated secret name example-s3-creds, got %q", name)
	}

	secret := &corev1.Secret{}
	if err := client.Get(ctx, types.NamespacedName{Name: name, Namespace: tamoss.Namespace}, secret); err != nil {
		t.Fatalf("expected generated secret: %v", err)
	}
	assertCredentialAliases(t, secret)
	if len(secret.OwnerReferences) != 1 || secret.OwnerReferences[0].Name != "example" {
		t.Fatalf("expected Tamoss owner reference, got %#v", secret.OwnerReferences)
	}
}

func TestCredsSecretManagerPreservesGeneratedCredentials(t *testing.T) {
	ctx := context.Background()
	tamoss := rustfsTamossFixture()
	client := fakeClient()
	manager := CredsSecretManager{Client: client}

	name, err := manager.Ensure(ctx, tamoss)
	if err != nil {
		t.Fatalf("first ensure failed: %v", err)
	}
	first := &corev1.Secret{}
	if err := client.Get(ctx, types.NamespacedName{Name: name, Namespace: tamoss.Namespace}, first); err != nil {
		t.Fatalf("expected generated secret: %v", err)
	}
	firstAccess := string(first.Data[AccessKeyEnvKey])
	firstSecret := string(first.Data[SecretKeyEnvKey])

	if _, err := manager.Ensure(ctx, tamoss); err != nil {
		t.Fatalf("second ensure failed: %v", err)
	}
	second := &corev1.Secret{}
	if err := client.Get(ctx, types.NamespacedName{Name: name, Namespace: tamoss.Namespace}, second); err != nil {
		t.Fatalf("expected preserved secret: %v", err)
	}
	if string(second.Data[AccessKeyEnvKey]) != firstAccess || string(second.Data[SecretKeyEnvKey]) != firstSecret {
		t.Fatalf("expected credentials to be preserved across reconciles")
	}
}

func TestCredsSecretManagerAddsAliasesToExistingGeneratedSecret(t *testing.T) {
	ctx := context.Background()
	tamoss := rustfsTamossFixture()
	secret := &corev1.Secret{}
	secret.Name = "example-s3-creds"
	secret.Namespace = tamoss.Namespace
	secret.Data = map[string][]byte{
		AccessKeyEnvKey: []byte("existing-access"),
		SecretKeyEnvKey: []byte("existing-secret"),
	}
	client := fakeClient(secret)
	manager := CredsSecretManager{Client: client}

	if _, err := manager.Ensure(ctx, tamoss); err != nil {
		t.Fatalf("ensure failed: %v", err)
	}
	updated := &corev1.Secret{}
	if err := client.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, updated); err != nil {
		t.Fatalf("expected updated secret: %v", err)
	}
	if string(updated.Data[AccessKeyKey]) != "existing-access" || string(updated.Data[SecretKeyKey]) != "existing-secret" {
		t.Fatalf("expected upstream aliases to reuse existing credential values, got %#v", updated.Data)
	}
}

func TestCredsSecretManagerDoesNotMutateUserSuppliedSecret(t *testing.T) {
	ctx := context.Background()
	tamoss := rustfsTamossFixture()
	tamoss.Spec.Backends.S3.RustFSOperator.CredsSecret.ExistingSecret = "user-s3-creds"
	secret := &corev1.Secret{}
	secret.Name = "user-s3-creds"
	secret.Namespace = tamoss.Namespace
	secret.Data = map[string][]byte{
		AccessKeyKey: []byte("user-access"),
		SecretKeyKey: []byte("user-secret"),
	}
	client := fakeClient(secret)
	manager := CredsSecretManager{Client: client}

	name, err := manager.Ensure(ctx, tamoss)
	if err != nil {
		t.Fatalf("ensure failed: %v", err)
	}
	if name != "user-s3-creds" {
		t.Fatalf("expected user secret name, got %q", name)
	}
	updated := &corev1.Secret{}
	if err := client.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, updated); err != nil {
		t.Fatalf("expected user secret: %v", err)
	}
	if _, ok := updated.Data[AccessKeyEnvKey]; ok {
		t.Fatalf("did not expect user secret to be mutated with TAMOSS env key")
	}
}

func assertCredentialAliases(t *testing.T, secret *corev1.Secret) {
	t.Helper()
	access := secret.Data[AccessKeyEnvKey]
	secretKey := secret.Data[SecretKeyEnvKey]
	if len(access) < 32 || len(secretKey) < 32 {
		t.Fatalf("expected generated credentials to have at least 32 bytes each")
	}
	if string(secret.Data[AccessKeyKey]) != string(access) {
		t.Fatalf("expected accesskey alias to match RUSTFS_ACCESS_KEY")
	}
	if string(secret.Data[SecretKeyKey]) != string(secretKey) {
		t.Fatalf("expected secretkey alias to match RUSTFS_SECRET_KEY")
	}
	if string(access) == string(secretKey) {
		t.Fatalf("expected independent access and secret values")
	}
}

func fakeClient(objects ...runtime.Object) client.Client {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	return clientfake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objects...).
		Build()
}
