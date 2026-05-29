package workload_renderer

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func renderAdvancedExtraResources(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	objects := make([]client.Object, 0, len(tamoss.Spec.Advanced.ExtraResources))
	for _, raw := range tamoss.Spec.Advanced.ExtraResources {
		if len(raw.Raw) == 0 {
			continue
		}
		var body map[string]interface{}
		if err := json.Unmarshal(raw.Raw, &body); err != nil || len(body) == 0 {
			continue
		}
		obj := &unstructured.Unstructured{Object: body}
		if obj.GetNamespace() == "" {
			obj.SetNamespace(tamoss.Namespace)
		}
		objects = append(objects, obj)
	}
	return objects
}
