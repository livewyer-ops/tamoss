package workload_renderer

import (
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func renderNetworkPolicies(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	if !tamoss.Spec.NetworkPolicy.IsEnabled() {
		return nil
	}
	var objects []client.Object
	if tamoss.Spec.API.IsEnabled() {
		objects = append(objects, networkPolicyFor(tamoss, "api", tamoss.Spec.NetworkPolicy.API))
	}
	if tamoss.Spec.UI.IsEnabled() {
		objects = append(objects, networkPolicyFor(tamoss, "ui", tamoss.Spec.NetworkPolicy.UI))
	}
	if tamoss.Spec.Worker.IsEnabled() {
		objects = append(objects, networkPolicyFor(tamoss, "worker", tamoss.Spec.NetworkPolicy.Worker))
	}
	if tamoss.Spec.ConsoleEnabled() {
		objects = append(objects, networkPolicyFor(tamoss, "console", tamoss.Spec.NetworkPolicy.Console))
	}
	return objects
}

func networkPolicyFor(tamoss *tamossv1alpha1.Tamoss, component string, rules tamossv1alpha1.NetworkPolicyRulesSpec) client.Object {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tamoss.ResourceName(component),
			Namespace: tamoss.Namespace,
			Labels:    labels(tamoss, component),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: selectorLabels(tamoss, component),
			},
			PolicyTypes: policyTypesFor(rules),
			Ingress:     rules.Ingress,
			Egress:      rules.Egress,
		},
	}
}

// policyTypesFor declares Egress only for a component that has egress rules.
// Declaring the type with an empty rule list denies all outbound traffic
// including DNS, which is never the intent of a component that simply has no
// egress rules of its own. Ingress is unconditional: an empty ingress list
// denying inbound traffic is the intended default for a selected component.
func policyTypesFor(rules tamossv1alpha1.NetworkPolicyRulesSpec) []networkingv1.PolicyType {
	policyTypes := []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
	if len(rules.Egress) > 0 {
		policyTypes = append(policyTypes, networkingv1.PolicyTypeEgress)
	}
	return policyTypes
}
