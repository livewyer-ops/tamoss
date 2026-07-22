package controller

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatormetrics "github.com/livewyer-ops/tamoss/operator/internal/metrics"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

type storageBackendReconcileResult struct {
	Ready    bool
	Reason   string
	Message  string
	Degraded bool
}

type storageBackendStatusInput struct {
	Ready           bool
	BucketReady     bool
	DatabaseReady   bool
	Reason          string
	Message         string
	DatabaseReason  string
	DatabaseMessage string
	Degraded        bool
	BackendID       string
	BucketName      string
	Diagnostic      *storageBackendDiagnosticResult
}

// storageBackendStageStatusInput fills the fields that are constant for a
// not-yet-ready stage so call sites only describe bucket readiness and the
// stage outcome.
func storageBackendStageStatusInput(spec tamossv1alpha1.StorageBackendSpec, bucketReady bool, result storageBackendReconcileResult) storageBackendStatusInput {
	return storageBackendStatusInput{
		BucketReady: bucketReady,
		Reason:      result.Reason,
		Message:     result.Message,
		Degraded:    result.Degraded,
		BackendID:   spec.ID,
		BucketName:  spec.BucketName,
	}
}

func (r *StorageBackendReconciler) updateStorageBackendStatus(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend, input storageBackendStatusInput) (ctrl.Result, error) {
	original := storageBackend.DeepCopy()
	storageBackend.Status.ObservedGeneration = storageBackend.Generation
	storageBackend.Status.BackendID = input.BackendID
	storageBackend.Status.BucketName = input.BucketName
	storageBackend.Status.Resolved = resolvedStorageBackendStatus(storageBackend)
	storageBackend.Status.Phase = operatorstatus.PhaseProgressing
	if input.Ready {
		storageBackend.Status.Phase = operatorstatus.PhaseReady
	}
	if input.Degraded {
		storageBackend.Status.Phase = operatorstatus.PhaseDegraded
	}
	operatorstatus.SetConditionBool(&storageBackend.Status.Conditions, storageBackend.Generation, operatorstatus.ConditionBucketReady, input.BucketReady, bucketReadyReason(input), bucketReadyMessage(input))
	operatorstatus.SetConditionBool(&storageBackend.Status.Conditions, storageBackend.Generation, operatorstatus.ConditionDatabaseReady, input.DatabaseReady, databaseReadyReason(input), databaseReadyMessage(input))
	if input.Diagnostic != nil {
		operatorstatus.SetConditionStatus(&storageBackend.Status.Conditions, storageBackend.Generation, operatorstatus.ConditionExternalS3DiagnosticReady, input.Diagnostic.Status, input.Diagnostic.Reason, input.Diagnostic.Message)
	}
	operatorstatus.SetConditionBool(&storageBackend.Status.Conditions, storageBackend.Generation, operatorstatus.ConditionReady, input.Ready, input.Reason, input.Message)
	if err := r.patchStorageBackendStatus(ctx, storageBackend, original); err != nil {
		return ctrl.Result{}, err
	}
	if input.Ready || input.Degraded {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: defaultProviderReadinessProbeInterval}, nil
}

func (r *StorageBackendReconciler) patchStorageBackendStatus(ctx context.Context, storageBackend, original *tamossv1alpha1.StorageBackend) error {
	if storageBackendStatusSemanticEqual(original.Status, storageBackend.Status) {
		operatormetrics.RecordStorageBackendStatus(storageBackend)
		return nil
	}
	if err := r.Client.Status().Patch(ctx, storageBackend, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return err
	}
	r.recordStorageBackendEvents(original, storageBackend)
	operatormetrics.RecordStorageBackendStatus(storageBackend)
	return nil
}

func (r *StorageBackendReconciler) recordStorageBackendEvents(original, storageBackend *tamossv1alpha1.StorageBackend) {
	if operatorstatus.ConditionBecameTrue(original.Status.Conditions, storageBackend.Status.Conditions, operatorstatus.ConditionReady) {
		operatorstatus.EmitNormalEvent(r.Recorder, storageBackend, operatorstatus.ReasonStorageBackendReady, "StorageBackend is ready")
	}
	if operatorstatus.ConditionBecameTrue(original.Status.Conditions, storageBackend.Status.Conditions, operatorstatus.ConditionBucketReady) {
		operatorstatus.EmitNormalEvent(r.Recorder, storageBackend, operatorstatus.ReasonStorageBackendBucketCreated, "StorageBackend bucket has been created")
	}
	if reason, message, changed := operatorstatus.ChangedConditionReason(original.Status.Conditions, storageBackend.Status.Conditions, operatorstatus.ConditionReady); changed && storageBackendWarningReason(reason) {
		operatorstatus.EmitWarningEvent(r.Recorder, &r.WarningEvents, storageBackend, reason, message)
	}
	if reason, message, changed := operatorstatus.ChangedConditionReason(original.Status.Conditions, storageBackend.Status.Conditions, operatorstatus.ConditionExternalS3DiagnosticReady); changed && storageBackendWarningReason(reason) {
		operatorstatus.EmitWarningEvent(r.Recorder, &r.WarningEvents, storageBackend, reason, message)
	}
}

func resolvedStorageBackendStatus(storageBackend *tamossv1alpha1.StorageBackend) tamossv1alpha1.StorageBackendResolvedStatus {
	spec := storageBackend.Spec
	spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
	return tamossv1alpha1.StorageBackendResolvedStatus{
		BackendID:         spec.ID,
		BucketName:        spec.BucketName,
		Provider:          spec.Provider,
		Usage:             spec.Usage,
		EndpointURL:       spec.Endpoint.Default.URL,
		PublicEndpointURL: spec.Endpoint.Public.URL,
		CredentialsSecret: spec.Credentials.ExistingSecret,
	}
}

func bucketReadyReason(input storageBackendStatusInput) string {
	if input.BucketReady {
		return operatorstatus.ReasonBucketReady
	}
	return input.Reason
}

func bucketReadyMessage(input storageBackendStatusInput) string {
	if input.BucketReady {
		return "StorageBackend bucket is ready"
	}
	return input.Message
}

func databaseReadyReason(input storageBackendStatusInput) string {
	if input.DatabaseReady {
		if input.DatabaseReason != "" {
			return input.DatabaseReason
		}
		return operatorstatus.ReasonDatabaseRegistered
	}
	return input.Reason
}

func databaseReadyMessage(input storageBackendStatusInput) string {
	if input.DatabaseReady {
		if input.DatabaseMessage != "" {
			return input.DatabaseMessage
		}
		return "TAMS storage backend row has been registered"
	}
	return input.Message
}
