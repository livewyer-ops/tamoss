package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func operationObjectReference(obj client.Object, kind string) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		APIVersion: tamossv1alpha1.SchemeGroupVersion.String(),
		Kind:       kind,
		Namespace:  obj.GetNamespace(),
		Name:       obj.GetName(),
		UID:        obj.GetUID(),
	}
}

func operationRefMatches(ref *corev1.ObjectReference, obj client.Object, kind string) bool {
	if ref == nil {
		return false
	}
	if ref.Kind != kind || ref.Namespace != obj.GetNamespace() || ref.Name != obj.GetName() {
		return false
	}
	return ref.UID == "" || obj.GetUID() == "" || ref.UID == obj.GetUID()
}

func patchTamossLifecycleStatus(ctx context.Context, c client.Client, tamoss *tamossv1alpha1.Tamoss, lifecycle tamossv1alpha1.TamossLifecycleStatus) error {
	original := tamoss.DeepCopy()
	tamoss.Status.Lifecycle = lifecycle
	if tamossStatusSemanticEqual(original.Status, tamoss.Status) {
		return nil
	}
	return c.Status().Patch(ctx, tamoss, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}
