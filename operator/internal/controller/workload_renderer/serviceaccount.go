package workload_renderer

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func renderServiceAccount(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	if !tamoss.Spec.ServiceAccount.Create {
		return nil
	}
	return []client.Object{
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:        serviceAccountName(tamoss),
				Namespace:   tamoss.Namespace,
				Labels:      labels(tamoss, ""),
				Annotations: tamoss.Spec.ServiceAccount.Annotations,
			},
			AutomountServiceAccountToken: &tamoss.Spec.ServiceAccount.Automount,
		},
	}
}
