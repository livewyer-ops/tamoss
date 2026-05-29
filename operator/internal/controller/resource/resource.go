package resource

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	FieldOwner = "tamoss-operator"

	AppName   = "tamoss"
	ManagedBy = "tamoss-operator"

	LabelName      = "app.kubernetes.io/name"
	LabelInstance  = "app.kubernetes.io/instance"
	LabelComponent = "app.kubernetes.io/component"
	LabelManagedBy = "app.kubernetes.io/managed-by"
)

func TamossLabels(tamoss *tamossv1alpha1.Tamoss, component string) map[string]string {
	return Labels(AppName, tamoss.Name, component)
}

func Labels(appName, instance, component string) map[string]string {
	labels := map[string]string{
		LabelName:      appName,
		LabelInstance:  instance,
		LabelManagedBy: ManagedBy,
	}
	if component != "" {
		labels[LabelComponent] = component
	}
	return labels
}

func MergeLabels(obj metav1.Object, desired map[string]string) bool {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	changed := false
	for key, value := range desired {
		if labels[key] != value {
			labels[key] = value
			changed = true
		}
	}
	if changed {
		obj.SetLabels(labels)
	}
	return changed
}

func TamossOwnerReferences(tamoss *tamossv1alpha1.Tamoss) []metav1.OwnerReference {
	return []metav1.OwnerReference{TamossOwnerReference(tamoss)}
}

func TamossOwnerReference(tamoss *tamossv1alpha1.Tamoss) metav1.OwnerReference {
	controller := true
	blockOwnerDeletion := true
	return metav1.OwnerReference{
		APIVersion:         tamossv1alpha1.GroupVersion.String(),
		Kind:               "Tamoss",
		Name:               tamoss.Name,
		UID:                tamoss.UID,
		Controller:         &controller,
		BlockOwnerDeletion: &blockOwnerDeletion,
	}
}

func SetOwnerReferences(obj metav1.Object, desired []metav1.OwnerReference) bool {
	existing := obj.GetOwnerReferences()
	if len(existing) == len(desired) {
		matches := true
		for i := range desired {
			if !OwnerReferenceEqual(existing[i], desired[i]) {
				matches = false
				break
			}
		}
		if matches {
			return false
		}
	}
	obj.SetOwnerReferences(desired)
	return true
}

func OwnerReferenceEqual(a, b metav1.OwnerReference) bool {
	return a.APIVersion == b.APIVersion &&
		a.Kind == b.Kind &&
		a.Name == b.Name &&
		a.UID == b.UID &&
		boolPtrValue(a.Controller) == boolPtrValue(b.Controller) &&
		boolPtrValue(a.BlockOwnerDeletion) == boolPtrValue(b.BlockOwnerDeletion)
}

func boolPtrValue(value *bool) bool {
	return value != nil && *value
}
