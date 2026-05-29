package authentik

import (
	"bytes"
	"os"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestRenderBlueprintIsDeterministic(t *testing.T) {
	tamoss := blueprintTamoss()
	credentials := Credentials{
		ClientID:     []byte("client-id"),
		ClientSecret: []byte("client-secret"),
	}

	first, err := RenderBlueprint(tamoss, credentials)
	if err != nil {
		t.Fatalf("RenderBlueprint failed: %v", err)
	}
	second, err := RenderBlueprint(tamoss, credentials)
	if err != nil {
		t.Fatalf("second RenderBlueprint failed: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("expected byte-identical blueprint\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestRenderBlueprintGolden(t *testing.T) {
	tamoss := blueprintTamoss()
	got, err := RenderBlueprint(tamoss, Credentials{
		ClientID:     []byte("client-id"),
		ClientSecret: []byte("client-secret"),
	})
	if err != nil {
		t.Fatalf("RenderBlueprint failed: %v", err)
	}

	want, err := os.ReadFile("testdata/blueprint_kind.golden.yaml")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("blueprint mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestRedirectURIs(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*tamossv1alpha1.Tamoss)
		expected []string
	}{
		{
			name: "single ingress",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.Auth.AuthentikBlueprints.RedirectURIs = nil
				tamoss.Spec.Ingress.UI.Web.Host = "ingress.example.com"
			},
			expected: []string{"https://ingress.example.com/auth/callback"},
		},
		{
			name: "multiple HTTPRoute hostnames",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.Auth.AuthentikBlueprints.RedirectURIs = nil
				tamoss.Spec.HTTPRoute.UI.Hostnames = []string{"route-b.example.com", "route-a.example.com"}
			},
			expected: []string{
				"https://route-a.example.com/auth/callback",
				"https://route-b.example.com/auth/callback",
			},
		},
		{
			name: "mixed ingress and HTTPRoute",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.Auth.AuthentikBlueprints.RedirectURIs = nil
				tamoss.Spec.Ingress.UI.Web.Host = "ingress.example.com"
				tamoss.Spec.HTTPRoute.UI.Hostnames = []string{"route-b.example.com", "route-a.example.com"}
			},
			expected: []string{
				"https://ingress.example.com/auth/callback",
				"https://route-a.example.com/auth/callback",
				"https://route-b.example.com/auth/callback",
			},
		},
		{
			name: "explicit override",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.Auth.AuthentikBlueprints.RedirectURIs = []string{"https://x.example.com/cb", "https://y.example.com/cb"}
				tamoss.Spec.Ingress.UI.Web.Host = "ingress.example.com"
			},
			expected: []string{"https://x.example.com/cb", "https://y.example.com/cb"},
		},
		{
			name: "none",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.Auth.AuthentikBlueprints.RedirectURIs = nil
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tamoss := blueprintTamoss()
			tamoss.Spec.Auth.AuthentikBlueprints.RedirectURIs = nil
			tt.mutate(tamoss)

			got := RedirectURIs(tamoss)
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d URIs, got %d: %#v", len(tt.expected), len(got), got)
			}
			for i := range tt.expected {
				if got[i] != tt.expected[i] {
					t.Fatalf("URI %d: expected %q, got %q", i, tt.expected[i], got[i])
				}
			}
		})
	}
}

func blueprintTamoss() *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "tams",
		},
		Spec: tamossv1alpha1.TamossSpec{
			Auth: tamossv1alpha1.AuthSpec{
				ProvidedBy: tamossv1alpha1.AuthProvidedByAuthentikBlueprints,
				AuthentikBlueprints: &tamossv1alpha1.AuthentikBlueprintsSpec{
					PlatformNamespace: "authentik",
					RedirectURIs:      []string{"https://app.example.com/auth/callback"},
					GroupBindings: []tamossv1alpha1.AuthentikGroupBindingSpec{{
						GroupName:   "tamoss-admins",
						Permissions: []string{"admin"},
					}},
				},
			},
		},
	}
}
