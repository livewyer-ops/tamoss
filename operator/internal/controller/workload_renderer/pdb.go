package workload_renderer

import (
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func renderPDBs(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	var objects []client.Object
	if tamoss.Spec.API.IsEnabled() && tamoss.Spec.API.PDB.IsEnabled() {
		objects = append(objects, pdbFor(tamoss, "api", tamoss.Spec.API.PDB))
	}
	if tamoss.Spec.UI.IsEnabled() && tamoss.Spec.UI.PDB.IsEnabled() {
		objects = append(objects, pdbFor(tamoss, "ui", tamoss.Spec.UI.PDB))
	}
	if tamoss.Spec.Worker.IsEnabled() && tamoss.Spec.Worker.PDB.IsEnabled() {
		objects = append(objects, pdbFor(tamoss, "worker", tamoss.Spec.Worker.PDB))
	}
	if tamoss.Spec.ConsoleEnabled() && tamoss.Spec.Console.PDB.IsEnabled() {
		objects = append(objects, pdbFor(tamoss, "console", tamoss.Spec.Console.PDB))
	}
	return objects
}

func pdbFor(tamoss *tamossv1alpha1.Tamoss, component string, spec tamossv1alpha1.PDBSpec) client.Object {
	maxUnavailable := spec.MaxUnavailable
	if spec.MinAvailable == nil && maxUnavailable == nil {
		defaultMaxUnavailable := intstr.FromInt(1)
		maxUnavailable = &defaultMaxUnavailable
	}
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tamoss.ResourceName(component),
			Namespace: tamoss.Namespace,
			Labels:    labels(tamoss, component),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable:   spec.MinAvailable,
			MaxUnavailable: maxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels(tamoss, component),
			},
		},
	}
}
