package cnpg

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func TestSecretReaderMissing(t *testing.T) {
	reader := SecretReader{Client: fake.NewClientBuilder().WithScheme(testScheme(t)).Build()}

	secrets, readiness, err := reader.Read(context.Background(), cnpgTamossFixture())
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if readiness.Ready {
		t.Fatalf("expected secrets to be missing")
	}
	if readiness.Reason != operatorstatus.ReasonWaitingForCNPGSecret {
		t.Fatalf("expected waiting reason, got %#v", readiness)
	}
	if secrets.App != nil || secrets.Superuser != nil {
		t.Fatalf("expected no secrets, got %#v", secrets)
	}
}

func TestSecretReaderAppOnly(t *testing.T) {
	tamoss := cnpgTamossFixture()
	reader := SecretReader{Client: fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(secret(tamoss.Namespace, AppSecretName(tamoss))).
		Build()}

	secrets, readiness, err := reader.Read(context.Background(), tamoss)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if readiness.Ready {
		t.Fatalf("expected superuser secret to be missing")
	}
	if secrets.App == nil || secrets.Superuser != nil {
		t.Fatalf("expected app-only state, got %#v", secrets)
	}
}

func TestSecretReaderMissingKey(t *testing.T) {
	tamoss := cnpgTamossFixture()
	app := secret(tamoss.Namespace, AppSecretName(tamoss))
	delete(app.Data, "password")
	reader := SecretReader{Client: fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(app).
		Build()}

	secrets, readiness, err := reader.Read(context.Background(), tamoss)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if readiness.Ready {
		t.Fatalf("expected app secret key to be missing")
	}
	if readiness.Reason != operatorstatus.ReasonCNPGSecretKeyMissing || readiness.Message == "" {
		t.Fatalf("expected missing key readiness, got %#v", readiness)
	}
	if secrets.App == nil {
		t.Fatalf("expected app secret to be returned")
	}
}

func TestSecretReaderBothPresent(t *testing.T) {
	tamoss := cnpgTamossFixture()
	reader := SecretReader{Client: fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(
			secret(tamoss.Namespace, AppSecretName(tamoss)),
			secret(tamoss.Namespace, SuperuserSecretName(tamoss)),
		).
		Build()}

	secrets, readiness, err := reader.Read(context.Background(), tamoss)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !readiness.Ready {
		t.Fatalf("expected both secrets to be ready")
	}
	if secrets.App == nil || secrets.Superuser == nil {
		t.Fatalf("expected both secrets, got %#v", secrets)
	}
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func secret(namespace, name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"username": []byte("tams"),
			"password": []byte("tams"),
		},
	}
}
