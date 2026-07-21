package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
	"github.com/livewyer-ops/tamoss/operator/internal/webhook/deleteprotection"
)

const hibernationCycleLabel = "tamoss.livewyer.io/hibernation-cycle"

// reconcileHibernationSpec converts the declarative spec.hibernation state
// into lifecycle operations. It materialises a TamossHibernate for each
// hibernation cycle, and aborts an in-flight cycle when hibernation is
// disabled again. The materialised operation then drives the shared lifecycle
// status exactly as a user-created one would.
func (r *TamossReconciler) reconcileHibernationSpec(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	phase := tamossv1alpha1.TamossLifecyclePhase(tamoss.Status.Lifecycle.Phase)
	if tamoss.Spec.Hibernation.Enabled {
		switch phase {
		case "", tamossv1alpha1.TamossLifecyclePhaseRunning:
			return r.ensureHibernationCycleOperation(ctx, tamoss)
		default:
			// An operation is in flight, completed, or failed; the operation
			// and its retry annotation own the lifecycle from here.
			return nil
		}
	}
	if phase == tamossv1alpha1.TamossLifecyclePhaseHibernating {
		return r.abortSpecHibernation(ctx, tamoss)
	}
	return nil
}

// ensureHibernationCycleOperation creates the TamossHibernate for the next
// hibernation cycle. Operations are numbered so repeated hibernate cycles of
// the same instance produce distinct artifacts, and carry the deletion
// confirmation annotation so the operator can abort its own operations.
func (r *TamossReconciler) ensureHibernationCycleOperation(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	if tamoss.Spec.Hibernation.Destination == nil {
		// CEL requires a destination while enabled; tolerate objects created
		// before that rule and report through the gated status instead.
		return nil
	}
	if current := tamoss.Status.Lifecycle.HibernationCycle; current > 0 {
		// The recorded cycle's operation may not have acquired the lifecycle
		// yet; do not start another cycle while it is still in flight.
		inFlight := &tamossv1alpha1.TamossHibernate{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: hibernationCycleOperationName(tamoss, current), Namespace: tamoss.Namespace}, inFlight)
		switch {
		case err == nil && !hibernationOperationTerminal(inFlight):
			return nil
		case err != nil && !apierrors.IsNotFound(err):
			return err
		}
	}
	cycle := tamoss.Status.Lifecycle.HibernationCycle + 1
	name := hibernationCycleOperationName(tamoss, cycle)
	existing := &tamossv1alpha1.TamossHibernate{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: tamoss.Namespace}, existing)
	switch {
	case err == nil:
		// Crash recovery: the operation exists but the cycle counter was not
		// recorded yet.
		return r.recordHibernationCycle(ctx, tamoss, cycle)
	case !apierrors.IsNotFound(err):
		return err
	}
	operation := &tamossv1alpha1.TamossHibernate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tamoss.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       tamossAppName,
				appInstanceLabel:               tamoss.Name,
				"app.kubernetes.io/managed-by": "tamoss-operator",
				hibernationCycleLabel:          fmt.Sprintf("%d", cycle),
			},
			Annotations: map[string]string{
				deleteprotection.ConfirmationAnnotation: "true",
			},
		},
		Spec: tamossv1alpha1.TamossHibernateSpec{
			TamossRef:   tamossv1alpha1.TamossReferenceSpec{Name: tamoss.Name},
			Destination: *tamoss.Spec.Hibernation.Destination,
		},
	}
	if err := controllerutil.SetControllerReference(tamoss, operation, r.Scheme); err != nil {
		return err
	}
	if err := r.Client.Create(ctx, operation); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	log.FromContext(ctx).Info("materialised hibernation operation from spec",
		"tamoss", tamoss.Name, "operation", name, "cycle", cycle)
	operatorstatus.EmitNormalEvent(r.Recorder, tamoss, operatorstatus.ReasonTamossHibernating,
		fmt.Sprintf("Hibernation requested by spec; created TamossHibernate %s", name))
	return r.recordHibernationCycle(ctx, tamoss, cycle)
}

func (r *TamossReconciler) recordHibernationCycle(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, cycle int32) error {
	if tamoss.Status.Lifecycle.HibernationCycle >= cycle {
		return nil
	}
	return patchTamossLifecycleStatus(ctx, r.Client, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
		lifecycle.HibernationCycle = cycle
	})
}

// abortSpecHibernation deletes the operator-materialised operation for the
// current cycle when hibernation is disabled mid-flight. Finalization marks
// the lifecycle Failed, and normal reconciliation then restores the instance.
// User-created operations are deliberately left alone.
func (r *TamossReconciler) abortSpecHibernation(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	ref := tamoss.Status.Lifecycle.ActiveOperationRef
	if ref == nil || ref.Kind != "TamossHibernate" {
		return nil
	}
	operation := &tamossv1alpha1.TamossHibernate{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: tamoss.Namespace}, operation); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(operation, tamoss) {
		return nil
	}
	if !operation.DeletionTimestamp.IsZero() {
		return nil
	}
	log.FromContext(ctx).Info("aborting spec-driven hibernation", "tamoss", tamoss.Name, "operation", operation.Name)
	operatorstatus.EmitNormalEvent(r.Recorder, tamoss, operatorstatus.ReasonLifecycleOperationDeleted,
		fmt.Sprintf("Hibernation disabled by spec; aborting TamossHibernate %s", operation.Name))
	return client.IgnoreNotFound(r.Client.Delete(ctx, operation))
}

func hibernationCycleOperationName(tamoss *tamossv1alpha1.Tamoss, cycle int32) string {
	return fmt.Sprintf("%s-hibernation-%d", tamoss.Name, cycle)
}

func hibernationOperationTerminal(operation *tamossv1alpha1.TamossHibernate) bool {
	switch operation.Status.Phase {
	case string(tamossv1alpha1.TamossOperationPhaseCompleted), string(tamossv1alpha1.TamossOperationPhaseFailed):
		return true
	default:
		return false
	}
}
