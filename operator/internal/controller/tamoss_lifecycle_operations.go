package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
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

// patchTamossLifecycleStatus applies a targeted mutation to the lifecycle
// status. Callers mutate only the fields they own so bookkeeping fields such
// as hibernationCycle and resolvedRestore survive phase transitions, and the
// transition timestamp is stamped centrally. Optimistic-lock conflicts with
// the other status writers on the same Tamoss re-read the latest object and
// re-apply the mutation: a lost recording (for example the artifact cleanup
// outcome after the objects were already deleted) cannot be replayed.
func patchTamossLifecycleStatus(ctx context.Context, c client.Client, tamoss *tamossv1alpha1.Tamoss, mutate func(*tamossv1alpha1.TamossLifecycleStatus)) error {
	refetch := false
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if refetch {
			if err := c.Get(ctx, client.ObjectKeyFromObject(tamoss), tamoss); err != nil {
				return err
			}
		}
		refetch = true
		original := tamoss.DeepCopy()
		lifecycle := *tamoss.Status.Lifecycle.DeepCopy()
		mutate(&lifecycle)
		if lifecycle.Phase != tamoss.Status.Lifecycle.Phase {
			now := metav1.Now()
			lifecycle.LastTransitionTime = &now
		}
		tamoss.Status.Lifecycle = lifecycle
		if tamossStatusSemanticEqual(original.Status, tamoss.Status) {
			return nil
		}
		return c.Status().Patch(ctx, tamoss, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
	})
}

// setLifecycleOperationState resets the operation-facing lifecycle fields as a
// single unit while leaving bookkeeping fields untouched.
func setLifecycleOperationState(lifecycle *tamossv1alpha1.TamossLifecycleStatus, phase tamossv1alpha1.TamossLifecyclePhase, reason, message string, activeRef *corev1.ObjectReference) {
	lifecycle.Phase = string(phase)
	lifecycle.Reason = reason
	lifecycle.Message = message
	lifecycle.ActiveOperationRef = activeRef
}
