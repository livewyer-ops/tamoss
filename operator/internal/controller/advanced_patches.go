package controller

import (
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const advancedPatchNameField = "name"

func applyAdvancedResourcePatches(tamoss *tamossv1alpha1.Tamoss, obj client.Object) error {
	if len(tamoss.Spec.Advanced.ResourcePatches) == 0 {
		return nil
	}
	ensureTypeMeta(obj)
	for _, patch := range tamoss.Spec.Advanced.ResourcePatches {
		if err := validateAdvancedResourcePatchTarget(patch.Target); err != nil {
			return err
		}
		if !advancedResourcePatchMatches(patch.Target, obj) {
			continue
		}
		if len(patch.Patch.Raw) == 0 {
			continue
		}
		if err := applyAdvancedResourcePatch(obj, patch.Patch.Raw); err != nil {
			return fmt.Errorf("apply advanced resource patch to %s/%s: %w", canonicalObjectKind(obj), obj.GetName(), err)
		}
	}
	return nil
}

func validateAdvancedResourcePatchTarget(target tamossv1alpha1.AdvancedResourcePatchTarget) error {
	if strings.TrimSpace(target.Kind) == "" {
		return fmt.Errorf("advanced resource patch target kind is required")
	}
	if strings.TrimSpace(target.Name) == "" {
		return fmt.Errorf("advanced resource patch target name is required")
	}
	return nil
}

func advancedResourcePatchMatches(target tamossv1alpha1.AdvancedResourcePatchTarget, obj client.Object) bool {
	if target.Kind != canonicalObjectKind(obj) || target.Name != obj.GetName() {
		return false
	}
	if target.APIVersion == "" {
		return true
	}
	return target.APIVersion == obj.GetObjectKind().GroupVersionKind().GroupVersion().String()
}

func applyAdvancedResourcePatch(obj client.Object, raw []byte) error {
	var patch map[string]interface{}
	if err := json.Unmarshal(raw, &patch); err != nil {
		return fmt.Errorf("decode JSON merge patch: %w", err)
	}
	if len(patch) == 0 {
		return nil
	}
	if err := validateAdvancedResourcePatch(patch); err != nil {
		return err
	}
	if typed, ok := obj.(*unstructured.Unstructured); ok {
		mergeJSONMap(typed.Object, patch)
		return nil
	}
	current, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return err
	}
	mergeJSONMap(current, patch)
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(current, obj); err != nil {
		return err
	}
	ensureTypeMeta(obj)
	return nil
}

func validateAdvancedResourcePatch(patch map[string]interface{}) error {
	for _, field := range []string{"apiVersion", "kind"} {
		if _, ok := patch[field]; ok {
			return fmt.Errorf("advanced resource patch must not set %s", field)
		}
	}
	metadata, ok := patch["metadata"].(map[string]interface{})
	if !ok {
		return nil
	}
	for _, field := range []string{advancedPatchNameField, "namespace", "ownerReferences"} {
		if _, ok := metadata[field]; ok {
			return fmt.Errorf("advanced resource patch must not set metadata.%s", field)
		}
	}
	return nil
}

func mergeJSONMap(target map[string]interface{}, patch map[string]interface{}) {
	for key, value := range patch {
		if value == nil {
			delete(target, key)
			continue
		}
		patchMap, patchIsMap := value.(map[string]interface{})
		targetMap, targetIsMap := target[key].(map[string]interface{})
		if patchIsMap && targetIsMap {
			mergeJSONMap(targetMap, patchMap)
			continue
		}
		target[key] = value
	}
}
