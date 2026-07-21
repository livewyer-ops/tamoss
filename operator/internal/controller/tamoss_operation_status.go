package controller

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

// operationWaitResult keeps non-terminal lifecycle operations polling. The
// hibernate and resume controllers only watch their own resources, so an
// operation waiting on a Tamoss, StorageBackend, or CNPG object that is not
// ready yet must requeue itself or it would stall until an operator restart.
func operationWaitResult(phase string, poll time.Duration) ctrl.Result {
	switch phase {
	case string(tamossv1alpha1.TamossOperationPhaseCompleted), string(tamossv1alpha1.TamossOperationPhaseFailed):
		return ctrl.Result{}
	default:
		return ctrl.Result{RequeueAfter: poll}
	}
}

func setOperationStatus(status *tamossv1alpha1.TamossOperationStatus, generation int64, phase tamossv1alpha1.TamossOperationPhase, reason, message string, artifact tamossv1alpha1.HibernationArtifactStatus) {
	now := metav1.Now()
	status.ObservedGeneration = generation
	status.Phase = string(phase)
	status.Reason = reason
	status.Message = message
	if status.StartedAt == nil && phase != tamossv1alpha1.TamossOperationPhasePending {
		status.StartedAt = &now
	}
	if status.CompletedAt == nil && (phase == tamossv1alpha1.TamossOperationPhaseCompleted || phase == tamossv1alpha1.TamossOperationPhaseFailed) {
		status.CompletedAt = &now
	}
	if artifact.Driver != "" || artifact.ManifestURI != "" || artifact.ManifestKey != "" || artifact.CNPGBackup.Name != "" || artifact.Cleanup.Phase != "" {
		status.Artifact = artifact
	}
	operatorstatus.SetConditionBool(
		&status.Conditions,
		generation,
		operatorstatus.ConditionReady,
		phase == tamossv1alpha1.TamossOperationPhaseCompleted,
		reason,
		message,
	)
	operatorstatus.SetConditionBool(
		&status.Conditions,
		generation,
		operatorstatus.ConditionProgressing,
		phase != tamossv1alpha1.TamossOperationPhaseCompleted && phase != tamossv1alpha1.TamossOperationPhaseFailed,
		reason,
		message,
	)
	operatorstatus.SetConditionBool(
		&status.Conditions,
		generation,
		operatorstatus.ConditionDegraded,
		phase == tamossv1alpha1.TamossOperationPhaseFailed,
		reason,
		message,
	)
}

func (r *TamossHibernateReconciler) updateHibernateStatus(ctx context.Context, hibernate *tamossv1alpha1.TamossHibernate, phase tamossv1alpha1.TamossOperationPhase, reason, message string, artifact tamossv1alpha1.HibernationArtifactStatus) error {
	original := hibernate.DeepCopy()
	setOperationStatus(&hibernate.Status, hibernate.Generation, phase, reason, message, artifact)
	if operationStatusSemanticEqual(original.Status, hibernate.Status) {
		return nil
	}
	if err := r.Client.Status().Patch(ctx, hibernate, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return err
	}
	recordOperationEvents(r.Recorder, hibernate, original.Status, hibernate.Status)
	return nil
}

func recordOperationEvents(recorder record.EventRecorder, obj operatorstatus.EventObject, original, updated tamossv1alpha1.TamossOperationStatus) {
	if original.Phase == updated.Phase && original.Reason == updated.Reason && original.Message == updated.Message {
		return
	}
	switch tamossv1alpha1.TamossOperationPhase(updated.Phase) {
	case tamossv1alpha1.TamossOperationPhaseCompleted:
		operatorstatus.EmitNormalEvent(recorder, obj, updated.Reason, updated.Message)
	case tamossv1alpha1.TamossOperationPhaseFailed:
		operatorstatus.EmitWarningEvent(recorder, nil, obj, updated.Reason, updated.Message)
	default:
		if original.Phase != updated.Phase {
			operatorstatus.EmitNormalEvent(recorder, obj, updated.Reason, updated.Message)
		}
	}
}
