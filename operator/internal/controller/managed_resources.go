package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

type tamossManagedResourcePolicy struct {
	object client.Object
	list   client.ObjectList
}

func tamossManagedResourcePolicies() []tamossManagedResourcePolicy {
	return []tamossManagedResourcePolicy{
		{object: &appsv1.Deployment{}, list: &appsv1.DeploymentList{}},
		{object: &batchv1.Job{}, list: &batchv1.JobList{}},
		{object: &corev1.ConfigMap{}, list: &corev1.ConfigMapList{}},
		{object: &corev1.Service{}, list: &corev1.ServiceList{}},
		{object: &corev1.Secret{}, list: &corev1.SecretList{}},
		{object: &corev1.ServiceAccount{}, list: &corev1.ServiceAccountList{}},
		{object: &networkingv1.Ingress{}, list: &networkingv1.IngressList{}},
		{object: &networkingv1.NetworkPolicy{}, list: &networkingv1.NetworkPolicyList{}},
		{object: &policyv1.PodDisruptionBudget{}, list: &policyv1.PodDisruptionBudgetList{}},
		{object: &rbacv1.Role{}, list: &rbacv1.RoleList{}},
		{object: &rbacv1.RoleBinding{}, list: &rbacv1.RoleBindingList{}},
		{object: &autoscalingv2.HorizontalPodAutoscaler{}, list: &autoscalingv2.HorizontalPodAutoscalerList{}},
		{object: &tamossv1alpha1.StorageBackend{}, list: &tamossv1alpha1.StorageBackendList{}},
	}
}

func ownTamossManagedResources(b *builder.Builder) *builder.Builder {
	for _, policy := range tamossManagedResourcePolicies() {
		b = b.Owns(policy.object)
	}
	return b
}
