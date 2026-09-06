package v1alpha1

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// ValidateTAMSTags enforces the TAMS string-or-string-array tag union after
// Kubernetes has preserved each value as arbitrary JSON.
func ValidateTAMSTags(tags map[string]apiextensionsv1.JSON) error {
	for _, key := range slices.Sorted(maps.Keys(tags)) {
		var value any
		if err := json.Unmarshal(tags[key].Raw, &value); err != nil {
			return fmt.Errorf("tag %q contains invalid JSON: %w", key, err)
		}
		switch typedValue := value.(type) {
		case string:
			continue
		case []any:
			for _, item := range typedValue {
				if _, ok := item.(string); !ok {
					return fmt.Errorf("tag %q must contain only strings", key)
				}
			}
		default:
			return fmt.Errorf("tag %q must be a string or an array of strings", key)
		}
	}
	return nil
}

func ValidateStorageBackendTags(tags map[string]apiextensionsv1.JSON) error {
	return ValidateTAMSTags(tags)
}
