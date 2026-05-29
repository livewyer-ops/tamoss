package authentik

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestCredsManagerGeneratesCredentials(t *testing.T) {
	ctx := context.Background()
	tamoss := testTamoss()
	manager := CredsManager{Client: fake.NewClientBuilder().WithScheme(testScheme(t)).Build()}

	name, credentials, err := manager.Ensure(ctx, tamoss)
	if err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}
	if name != "example-oauth2-creds" {
		t.Fatalf("unexpected secret name %q", name)
	}
	if len(credentials.ClientID) == 0 || len(credentials.ClientSecret) == 0 {
		t.Fatalf("expected generated credentials, got id=%q secret=%q", credentials.ClientID, credentials.ClientSecret)
	}

	secret := &corev1.Secret{}
	if err := manager.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: tamoss.Namespace}, secret); err != nil {
		t.Fatalf("expected Secret to be created: %v", err)
	}
	if string(secret.Data[ClientIDKey]) != string(credentials.ClientID) ||
		string(secret.Data[ClientSecretKey]) != string(credentials.ClientSecret) {
		t.Fatalf("Secret data does not match returned credentials")
	}
	if string(secret.Data[ClientIDEnvKey]) != string(credentials.ClientID) ||
		string(secret.Data[ClientSecretEnvKey]) != string(credentials.ClientSecret) {
		t.Fatalf("Secret is missing env aliases")
	}
}

func TestCredsManagerPreservesCredentials(t *testing.T) {
	ctx := context.Background()
	tamoss := testTamoss()
	manager := CredsManager{Client: fake.NewClientBuilder().WithScheme(testScheme(t)).Build()}

	name, first, err := manager.Ensure(ctx, tamoss)
	if err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}
	_, second, err := manager.Ensure(ctx, tamoss)
	if err != nil {
		t.Fatalf("second Ensure failed: %v", err)
	}
	if string(first.ClientID) != string(second.ClientID) ||
		string(first.ClientSecret) != string(second.ClientSecret) {
		t.Fatalf("expected credentials to be preserved")
	}

	secret := &corev1.Secret{}
	if err := manager.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: tamoss.Namespace}, secret); err != nil {
		t.Fatalf("expected Secret to exist: %v", err)
	}
	if string(secret.Data[ClientIDKey]) != string(first.ClientID) {
		t.Fatalf("expected stored client_id to remain unchanged")
	}
}

func TestCredsManagerRegeneratesAfterDeletion(t *testing.T) {
	ctx := context.Background()
	tamoss := testTamoss()
	manager := CredsManager{Client: fake.NewClientBuilder().WithScheme(testScheme(t)).Build()}

	name, first, err := manager.Ensure(ctx, tamoss)
	if err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: tamoss.Namespace}}
	if err := manager.Client.Delete(ctx, secret); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	_, second, err := manager.Ensure(ctx, tamoss)
	if err != nil {
		t.Fatalf("second Ensure failed: %v", err)
	}
	if string(first.ClientID) == string(second.ClientID) ||
		string(first.ClientSecret) == string(second.ClientSecret) {
		t.Fatalf("expected deletion to regenerate credentials")
	}
}

func testTamoss() *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "default",
			UID:       "test-uid",
		},
	}
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}
	if err := tamossv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add tamoss scheme: %v", err)
	}
	return scheme
}
