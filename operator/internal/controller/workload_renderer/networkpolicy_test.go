package workload_renderer

import (
	"slices"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

// Declaring policyTypes Egress with an empty egress list denies all outbound
// traffic including DNS, so a component without egress rules must not have the
// type declared for it.
func TestRenderNetworkPolicyDeclaresEgressOnlyWithEgressRules(t *testing.T) {
	t.Parallel()
	tamoss := rendererFixture()
	tamoss.Spec.NetworkPolicy.Enabled = ptr.To(true)
	tamoss.Spec.NetworkPolicy.API.Ingress = []networkingv1.NetworkPolicyIngressRule{{
		Ports: []networkingv1.NetworkPolicyPort{{Port: ptr.To(intstr.FromInt32(8000))}},
	}}
	tamoss.Spec.NetworkPolicy.UI.Egress = []networkingv1.NetworkPolicyEgressRule{{
		Ports: []networkingv1.NetworkPolicyPort{{Port: ptr.To(intstr.FromInt32(8000))}},
	}}

	objects := Render(tamoss)

	apiPolicy := networkPolicyByName(t, objects, "example-api")
	if !slices.Equal(apiPolicy.Spec.PolicyTypes, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}) {
		t.Fatalf("expected ingress-only policy types without egress rules, got %#v", apiPolicy.Spec.PolicyTypes)
	}

	uiPolicy := networkPolicyByName(t, objects, "example-ui")
	if !slices.Equal(uiPolicy.Spec.PolicyTypes, []networkingv1.PolicyType{
		networkingv1.PolicyTypeIngress,
		networkingv1.PolicyTypeEgress,
	}) {
		t.Fatalf("expected ingress and egress policy types with egress rules, got %#v", uiPolicy.Spec.PolicyTypes)
	}
}

// An empty ingress list denying inbound traffic is the intended default, so the
// Ingress type stays declared even when a component has no ingress rules.
func TestRenderNetworkPolicyAlwaysDeclaresIngress(t *testing.T) {
	t.Parallel()
	tamoss := rendererFixture()
	tamoss.Spec.NetworkPolicy.Enabled = ptr.To(true)

	objects := Render(tamoss)

	for _, name := range []string{"example-api", "example-ui", "example-console"} {
		policy := networkPolicyByName(t, objects, name)
		if !slices.Equal(policy.Spec.PolicyTypes, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}) {
			t.Fatalf("expected %s to declare policyTypes Ingress alone, got %#v", name, policy.Spec.PolicyTypes)
		}
		if len(policy.Spec.Ingress) != 0 || len(policy.Spec.Egress) != 0 {
			t.Fatalf("expected %s to render the spec rules verbatim, got %#v", name, policy.Spec)
		}
	}
}

// Rules supplied on the spec are rendered as given; the renderer neither adds
// destinations nor drops user-declared peers.
func TestRenderNetworkPolicyPreservesExplicitEgressRules(t *testing.T) {
	t.Parallel()
	tamoss := rendererFixture()
	tamoss.Spec.NetworkPolicy.Enabled = ptr.To(true)
	tamoss.Spec.NetworkPolicy.Console.Egress = []networkingv1.NetworkPolicyEgressRule{{
		To:    []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "10.96.0.1/32"}}},
		Ports: []networkingv1.NetworkPolicyPort{{Port: ptr.To(intstr.FromInt32(443))}},
	}}

	objects := Render(tamoss)

	policy := networkPolicyByName(t, objects, "example-console")
	if !slices.Contains(policy.Spec.PolicyTypes, networkingv1.PolicyTypeEgress) {
		t.Fatalf("expected explicit Console egress to declare the Egress policy type, got %#v", policy.Spec.PolicyTypes)
	}
	if len(policy.Spec.Egress) != 1 || len(policy.Spec.Egress[0].To) != 1 ||
		policy.Spec.Egress[0].To[0].IPBlock == nil ||
		policy.Spec.Egress[0].To[0].IPBlock.CIDR != "10.96.0.1/32" {
		t.Fatalf("expected the explicit Console egress rule to be rendered verbatim, got %#v", policy.Spec.Egress)
	}
}
