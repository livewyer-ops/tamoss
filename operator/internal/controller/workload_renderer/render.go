package workload_renderer

import (
	"fmt"
	"slices"
	"sort"

	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
)

const (
	managedBy      = "tamoss-operator"
	defaultAppName = "tamoss"
)

// Render returns the workload resources declared by the Tamoss spec.
func Render(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	return slices.Concat(
		renderServiceAccount(tamoss),
		renderSecrets(tamoss),
		renderServices(tamoss),
		renderAPIDeployment(tamoss),
		renderWorkerDeployment(tamoss),
		renderUIDeployment(tamoss),
		renderIngresses(tamoss),
		renderHTTPRoutes(tamoss),
		renderNetworkPolicies(tamoss),
		renderPDBs(tamoss),
		renderHPAs(tamoss),
		renderAdvancedExtraResources(tamoss),
	)
}

func appLabelName(tamoss *tamossv1alpha1.Tamoss) string {
	if tamoss.Spec.NameOverride != "" {
		return tamoss.Spec.NameOverride
	}
	return defaultAppName
}

func labels(tamoss *tamossv1alpha1.Tamoss, component string) map[string]string {
	labels := selectorLabels(tamoss, component)
	labels["app.kubernetes.io/managed-by"] = managedBy
	return labels
}

func selectorLabels(tamoss *tamossv1alpha1.Tamoss, component string) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":     appLabelName(tamoss),
		"app.kubernetes.io/instance": tamoss.Name,
	}
	if component != "" {
		labels["app.kubernetes.io/component"] = component
	}
	return labels
}

func mergeStringMaps(base map[string]string, overlays ...map[string]string) map[string]string {
	merged := map[string]string{}
	for key, value := range base {
		merged[key] = value
	}
	for _, overlay := range overlays {
		for key, value := range overlay {
			merged[key] = value
		}
	}
	return merged
}

func serviceAccountName(tamoss *tamossv1alpha1.Tamoss) string {
	if tamoss.Spec.ServiceAccount.Name != "" {
		return tamoss.Spec.ServiceAccount.Name
	}
	if tamoss.Spec.ServiceAccount.Create {
		return tamoss.ResourceName("workload")
	}
	return "default"
}

func image(repository, tag string) string {
	if repository == "" {
		repository = defaults.DefaultAPIRepository
	}
	if tag == "" {
		tag = defaults.DefaultOperandTag
	}
	return fmt.Sprintf("%s:%s", repository, tag)
}

func uiImage(repository, tag string) string {
	if repository == "" {
		repository = defaults.DefaultUIRepository
	}
	return image(repository, tag)
}

func sortedEnv(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
