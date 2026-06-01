package authentik

import (
	"context"
	"net/http"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestNewHTTPClientIsBounded(t *testing.T) {
	client := NewHTTPClient()
	if client.Timeout <= 0 || client.Timeout > defaultAPIOperationTimeout {
		t.Fatalf("expected bounded timeout no greater than API operation timeout, got %s", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected http.Transport, got %T", client.Transport)
	}
	if transport.MaxIdleConns == 0 {
		t.Fatal("expected MaxIdleConns to be configured")
	}
	if transport.IdleConnTimeout == 0 {
		t.Fatal("expected IdleConnTimeout to be configured")
	}
}

func TestClientsUseInjectedHTTPClientAndInternalBaseURL(t *testing.T) {
	httpClient := &http.Client{Timeout: time.Second}
	tamoss := authentikTamoss()

	blueprintClient := NewManagedBlueprintClient(tamoss, "token", httpClient)
	if blueprintClient.HTTPClient != httpClient {
		t.Fatal("expected managed Blueprint client to use injected HTTP client")
	}
	proxyClient := NewProxyOutpostClient(tamoss, "token", httpClient)
	if proxyClient.HTTPClient != httpClient {
		t.Fatal("expected proxy outpost client to use injected HTTP client")
	}
	if blueprintClient.BaseURL != "http://authentik-server.auth.svc.cluster.local" ||
		proxyClient.BaseURL != "http://authentik-server.auth.svc.cluster.local" {
		t.Fatalf("expected internal Authentik API base URL, got blueprint=%q proxy=%q", blueprintClient.BaseURL, proxyClient.BaseURL)
	}
}

func TestResolveAPITokenReadsConfiguredSecret(t *testing.T) {
	ctx := context.Background()
	tamoss := authentikTamoss()
	reader := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: DefaultAPITokenSecretName, Namespace: "auth"},
			Data:       map[string][]byte{DefaultAPITokenSecretKey: []byte(" test-token ")},
		}).
		Build()

	resolution, err := ResolveAPIToken(ctx, reader, tamoss)
	if err != nil {
		t.Fatalf("ResolveAPIToken failed: %v", err)
	}
	if resolution.Token != "test-token" || resolution.Message != "" {
		t.Fatalf("unexpected token resolution: %#v", resolution)
	}
}

func TestResolveAPITokenReportsMissingSecret(t *testing.T) {
	ctx := context.Background()
	tamoss := authentikTamoss()
	reader := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()

	resolution, err := ResolveAPIToken(ctx, reader, tamoss)
	if err != nil {
		t.Fatalf("ResolveAPIToken failed: %v", err)
	}
	if resolution.Token != "" || resolution.Message != "Authentik API token secret auth/authentik-api-token is required" {
		t.Fatalf("unexpected token resolution: %#v", resolution)
	}
}

func TestCheckPlatformNamespace(t *testing.T) {
	tamoss := authentikTamoss()
	policy := NewPlatformNamespacePolicy("allowed")

	decision := CheckPlatformNamespace(tamoss, policy)

	if decision.Allowed {
		t.Fatal("expected namespace to be rejected")
	}
	if decision.Message != `Authentik platform namespace "auth" is outside configured allow-list "allowed"` {
		t.Fatalf("unexpected rejection message %q", decision.Message)
	}
}

func authentikTamoss() *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"},
		Spec: tamossv1alpha1.TamossSpec{
			Auth: tamossv1alpha1.AuthSpec{
				ProvidedBy: tamossv1alpha1.AuthProvidedByAuthentikBlueprints,
				AuthentikBlueprints: &tamossv1alpha1.AuthentikBlueprintsSpec{
					PlatformNamespace: "auth",
					InternalURL:       "http://authentik-server.auth.svc.cluster.local",
					IssuerURL:         "https://auth.example.com",
				},
			},
		},
	}
}
