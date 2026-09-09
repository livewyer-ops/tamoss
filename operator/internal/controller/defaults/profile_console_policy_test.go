package defaults

import (
	"testing"

	"k8s.io/utils/ptr"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

// The rendered Console policy always declares policyTypes Ingress, so a Console
// enabled outside multi-server used to be denied all inbound traffic and left
// without the egress rules the multi-server profile renders.
func TestConsoleNetworkPolicyDefaultsOutsideMultiServer(t *testing.T) {
	for _, profile := range []tamossv1alpha1.TamossProfile{
		tamossv1alpha1.TamossProfileSingleServer,
		tamossv1alpha1.TamossProfileEdge,
		tamossv1alpha1.TamossProfileLocalKind,
	} {
		t.Run(string(profile), func(t *testing.T) {
			tamoss := &tamossv1alpha1.Tamoss{Spec: tamossv1alpha1.TamossSpec{Profile: profile}}
			tamoss.Name = "example"
			tamoss.Spec.Console.Enabled = ptr.To(true)
			tamoss.Spec.NetworkPolicy.Enabled = ptr.To(true)

			Apply(tamoss)

			rules := tamoss.Spec.NetworkPolicy.Console.Egress
			if len(rules) != 2 {
				t.Fatalf("expected port-scoped DNS and Kubernetes API Console egress, got %#v", rules)
			}
			assertPortScopedDNSRule(t, rules[0])
			assertPortScopedTCPRule(t, rules[1], 443, 6443)
			if len(tamoss.Spec.NetworkPolicy.Console.Ingress) == 0 {
				t.Fatal("expected Console ingress rules")
			}
		})
	}
}

// NetworkPolicy must stay off unless the profile or the user enables it.
func TestConsoleNetworkPolicyDefaultsStayOffWhenPolicyDisabled(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{Spec: tamossv1alpha1.TamossSpec{Profile: tamossv1alpha1.TamossProfileSingleServer}}
	tamoss.Name = "example"
	tamoss.Spec.Console.Enabled = ptr.To(true)

	Apply(tamoss)

	if tamoss.Spec.NetworkPolicy.IsEnabled() {
		t.Fatal("Console defaults must not enable NetworkPolicy on their own")
	}
	if len(tamoss.Spec.NetworkPolicy.Console.Egress) != 0 {
		t.Fatalf("expected no rules while NetworkPolicy is disabled, got %#v", tamoss.Spec.NetworkPolicy.Console.Egress)
	}
}
