package consoleapi

import rbacv1 "k8s.io/api/rbac/v1"

// ReadOnlyPolicyRules is the complete Kubernetes permission contract for one
// Console API instance. It intentionally excludes Secrets, logs, exec, proxy,
// subresources, and every mutating verb. The HTTP projection separately omits
// EndpointSlice addresses and target references.
func ReadOnlyPolicyRules(instance string) []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			// The cache includes an exact metadata.name field selector on its
			// ListWatch, which is required for resourceNames-scoped RBAC.
			APIGroups:     []string{"tamoss.livewyer.io"},
			Resources:     []string{"tamosses"},
			ResourceNames: []string{instance},
			Verbs:         []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"events", "pods", "services"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"apps"},
			Resources: []string{"deployments"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"batch"},
			Resources: []string{"jobs"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"discovery.k8s.io"},
			Resources: []string{"endpointslices"},
			Verbs:     []string{"get", "list", "watch"},
		},
	}
}
