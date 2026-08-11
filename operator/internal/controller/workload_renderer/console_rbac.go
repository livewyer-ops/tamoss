package workload_renderer

import (
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/consoleapi"
)

func renderConsoleRBAC(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	if !tamoss.Spec.ConsoleEnabled() {
		return nil
	}
	name := consoleServiceAccountName(tamoss)
	automount := true
	return []client.Object{
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: tamoss.Namespace,
				Labels:    labels(tamoss, "console"),
			},
			AutomountServiceAccountToken: &automount,
		},
		&rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: tamoss.Namespace,
				Labels:    labels(tamoss, "console"),
			},
			Rules: consoleapi.ReadOnlyPolicyRules(tamoss.Name),
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: tamoss.Namespace,
				Labels:    labels(tamoss, "console"),
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "Role",
				Name:     name,
			},
			Subjects: []rbacv1.Subject{{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      name,
				Namespace: tamoss.Namespace,
			}},
		},
	}
}

func consoleServiceAccountName(tamoss *tamossv1alpha1.Tamoss) string {
	return tamoss.ResourceName("console")
}
