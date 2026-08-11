package consoleapi

import rbacv1 "k8s.io/api/rbac/v1"

const (
	rbacVerbGet   = "get"
	rbacVerbList  = "list"
	rbacVerbWatch = "watch"
	rbacVerbPatch = "patch"
)

// ReadOnlyPolicyRules is the complete read permission contract for one Console
// API instance. It intentionally excludes Secrets, logs, exec, proxy, and
// subresources. The HTTP projection separately omits EndpointSlice addresses,
// target references, input IDs, and artifact locators.
func ReadOnlyPolicyRules(instance string) []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			// The cache includes an exact metadata.name field selector on its
			// ListWatch, which is required for resourceNames-scoped RBAC.
			APIGroups:     []string{"tamoss.livewyer.io"},
			Resources:     []string{"tamosses"},
			ResourceNames: []string{instance},
			Verbs:         []string{rbacVerbGet, rbacVerbList, rbacVerbWatch},
		},
		{
			APIGroups: []string{"tamoss.livewyer.io"},
			Resources: []string{"storagebackends"},
			Verbs:     []string{rbacVerbGet, rbacVerbList, rbacVerbWatch},
		},
		{
			// IngestRun history uses bounded live reads and is never informer-cached.
			APIGroups: []string{"tamoss.livewyer.io"},
			Resources: []string{"ingestruns"},
			Verbs:     []string{rbacVerbGet, rbacVerbList},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"events", "pods", "services"},
			Verbs:     []string{rbacVerbGet, rbacVerbList, rbacVerbWatch},
		},
		{
			APIGroups: []string{"apps"},
			Resources: []string{"deployments", "replicasets"},
			Verbs:     []string{rbacVerbGet, rbacVerbList, rbacVerbWatch},
		},
		{
			APIGroups: []string{"batch"},
			Resources: []string{"jobs"},
			Verbs:     []string{rbacVerbGet, rbacVerbList, rbacVerbWatch},
		},
		{
			APIGroups: []string{"discovery.k8s.io"},
			Resources: []string{"endpointslices"},
			Verbs:     []string{rbacVerbGet, rbacVerbList, rbacVerbWatch},
		},
	}
}

// PolicyRules adds the sole mutating permission used by Console commands.
// IngestRun admission makes all spec fields except spec.desiredState immutable,
// and the HTTP command layer further limits this patch to one-way cancellation
// of a run whose spec.tamossRef.name is this instance.
//
// Residual risk, which Kubernetes RBAC cannot remove: the rule is
// namespace-wide. resourceNames is not evaluated for the list verb the history
// browser needs, and IngestRun names are created on demand, so the Console
// cannot be given a fixed set of names to patch. A patch also reaches metadata,
// not only spec. A compromised Console pod therefore holds a token that can,
// inside its own namespace, cancel an IngestRun belonging to a different Tamoss
// instance or add a finalizer that wedges IngestRun deletion. It cannot create,
// update, or delete any object, cannot read Secrets, and cannot leave the
// namespace.
//
// Closing that gap needs admission rather than RBAC: a ValidatingAdmissionPolicy
// bound to this ServiceAccount could require that a patched IngestRun keeps its
// spec.tamossRef.name and its metadata.finalizers. Such a policy is
// cluster-scoped and cannot be owned by a namespaced operand, so it is not
// rendered here. Until it exists, give each Tamoss instance its own namespace
// when instances do not trust one another.
func PolicyRules(instance string) []rbacv1.PolicyRule {
	rules := ReadOnlyPolicyRules(instance)
	return append(rules, rbacv1.PolicyRule{
		APIGroups: []string{"tamoss.livewyer.io"},
		Resources: []string{"ingestruns"},
		Verbs:     []string{rbacVerbPatch},
	})
}
