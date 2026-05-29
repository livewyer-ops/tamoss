package authentik

import "testing"

func TestPlatformNamespacePolicyDefaultsToFirstNamespace(t *testing.T) {
	policy := NewPlatformNamespacePolicy("")
	if !policy.Allow("auth") {
		t.Fatalf("expected first namespace to be allowed")
	}
	if policy.Allow("other") {
		t.Fatalf("expected second namespace to be rejected in default mode")
	}
	if policy.Description() != "auth" {
		t.Fatalf("unexpected description %q", policy.Description())
	}
}

func TestPlatformNamespacePolicyConfiguredList(t *testing.T) {
	policy := NewPlatformNamespacePolicy("auth,auth2")
	if !policy.Allow("auth2") {
		t.Fatalf("expected configured namespace to be allowed")
	}
	if policy.Allow("other") {
		t.Fatalf("expected unconfigured namespace to be rejected")
	}
	if policy.Description() != "auth,auth2" {
		t.Fatalf("unexpected description %q", policy.Description())
	}
}

func TestPlatformNamespacePolicyWildcard(t *testing.T) {
	policy := NewPlatformNamespacePolicy("*")
	for _, namespace := range []string{"auth", "other"} {
		if !policy.Allow(namespace) {
			t.Fatalf("expected wildcard to allow %s", namespace)
		}
	}
	if policy.Description() != "*" {
		t.Fatalf("unexpected description %q", policy.Description())
	}
}
