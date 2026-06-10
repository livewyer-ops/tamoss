package controller

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatormetrics "github.com/livewyer-ops/tamoss/operator/internal/metrics"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

type tamossStatusPatchInput struct {
	RefreshBackupPolicy bool
	Apply               func(*tamossv1alpha1.Tamoss) error
}

type backendBlockedStatusInput struct {
	Reason             string
	Message            string
	SchemaMessage      string
	ProgressingMessage string
}

func (r *TamossReconciler) updateStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, schemaResult SchemaResult, identityResult identityReconcileResult) error {
	return r.patchTamossStatusInput(ctx, tamoss, tamossStatusPatchInput{
		RefreshBackupPolicy: true,
		Apply: func(tamoss *tamossv1alpha1.Tamoss) error {
			tamoss.Status.SchemaVersion = schemaResult.Version
			setUpgradeStatusFromSchema(tamoss, schemaResult)
			tamoss.Status.Replicas.API = r.componentReplicaStatus(ctx, tamoss, "api", tamoss.Spec.API.IsEnabled(), desiredReplicaCount(tamoss.Spec.API.WorkloadCommonSpec))
			tamoss.Status.Replicas.UI = r.componentReplicaStatus(ctx, tamoss, "ui", tamoss.Spec.UI.IsEnabled(), desiredReplicaCount(tamoss.Spec.UI.WorkloadCommonSpec))
			tamoss.Status.Replicas.Worker = r.componentReplicaStatus(ctx, tamoss, "worker", tamoss.Spec.Worker.IsEnabled(), desiredReplicaCount(tamoss.Spec.Worker.WorkloadCommonSpec))
			routingResult, err := r.routingStatus(ctx, tamoss)
			if err != nil {
				return err
			}

			ready := !schemaResult.Degraded && schemaResult.Ready && identityResult.Ready && routingResult.Ready && replicasReady(tamoss.Status.Replicas.API) && replicasReady(tamoss.Status.Replicas.UI) && replicasReady(tamoss.Status.Replicas.Worker)
			tamoss.Status.Phase = operatorstatus.PhaseProgressing
			if ready {
				tamoss.Status.Phase = operatorstatus.PhaseReady
			}
			if schemaResult.Degraded {
				tamoss.Status.Phase = operatorstatus.PhaseDegraded
			}
			operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionSchemaMigrated, schemaResult.Ready, schemaReason(schemaResult), schemaMessage(schemaResult))
			operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionBackendsReady, true, operatorstatus.ReasonBackendReferencesConfigured, "Backend secret references are configured")
			operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionIdentityBlueprintSubmitted, identityResult.BlueprintSubmitted, identityBlueprintReason(identityResult), identityBlueprintMessage(identityResult))
			operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionIdentityReady, identityResult.Ready, identityResult.Reason, identityResult.Message)
			setRoutingConditions(&tamoss.Status.Conditions, tamoss.Generation, routingResult)
			operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionDegraded, schemaResult.Degraded, degradedReason(schemaResult), degradedMessage(schemaResult))
			operatorstatus.SetReconciliationActive(&tamoss.Status.Conditions, tamoss.Generation)
			operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionProgressing, !ready && !schemaResult.Degraded, operatorstatus.ReasonReconciling, "Waiting for managed workloads to become available")
			operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionReady, ready, readyReason(ready, schemaResult, identityResult, routingResult), readyMessage(ready, schemaResult, identityResult, routingResult))
			return nil
		},
	})
}

func (r *TamossReconciler) updateBlockedBackendStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, input backendBlockedStatusInput) error {
	return r.patchTamossStatusInput(ctx, tamoss, tamossStatusPatchInput{Apply: func(tamoss *tamossv1alpha1.Tamoss) error {
		tamoss.Status.Phase = operatorstatus.PhaseDegraded
		operatorstatus.SetConditionStatus(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionSchemaMigrated, metav1.ConditionUnknown, input.Reason, input.SchemaMessage)
		operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionBackendsReady, false, input.Reason, input.Message)
		setActiveBlockedConditions(&tamoss.Status.Conditions, tamoss.Generation, input.Reason, input.Message, input.ProgressingMessage)
		return nil
	}})
}

func (r *TamossReconciler) updateProviderBackendStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, schemaResult *SchemaResult, backendResult providerBackendResult) error {
	return r.patchTamossStatusInput(ctx, tamoss, tamossStatusPatchInput{
		RefreshBackupPolicy: true,
		Apply: func(tamoss *tamossv1alpha1.Tamoss) error {
			if schemaResult != nil {
				tamoss.Status.SchemaVersion = schemaResult.Version
				setUpgradeStatusFromSchema(tamoss, *schemaResult)
			}
			tamoss.Status.Phase = operatorstatus.PhaseProgressing
			if backendResult.Degraded {
				tamoss.Status.Phase = operatorstatus.PhaseDegraded
			}
			if schemaResult == nil {
				operatorstatus.SetConditionStatus(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionSchemaMigrated, metav1.ConditionUnknown, backendResult.Reason, "Schema reconciliation is blocked by backend readiness")
			} else {
				operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionSchemaMigrated, schemaResult.Ready, schemaReason(*schemaResult), schemaMessage(*schemaResult))
			}
			operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionBackendsReady, false, backendResult.Reason, backendResult.Message)
			setProviderBackendConditions(&tamoss.Status.Conditions, tamoss.Generation, backendResult)
			return nil
		},
	})
}

func (r *TamossReconciler) updateGatewayAPIUnavailableStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	return r.patchTamossStatusInput(ctx, tamoss, tamossStatusPatchInput{Apply: func(tamoss *tamossv1alpha1.Tamoss) error {
		routingResult := gatewayAPIUnavailableResult()
		tamoss.Status.Phase = operatorstatus.PhaseProgressing
		operatorstatus.SetConditionStatus(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionSchemaMigrated, metav1.ConditionUnknown, routingResult.Reason, "Schema migration status is unchanged while Gateway API routing is unavailable")
		operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionBackendsReady, true, operatorstatus.ReasonBackendReferencesConfigured, "Backend secret references are configured")
		setRoutingConditions(&tamoss.Status.Conditions, tamoss.Generation, routingResult)
		operatorstatus.SetReconciliationActive(&tamoss.Status.Conditions, tamoss.Generation)
		operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionReady, false, routingResult.Reason, routingResult.Message)
		operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionProgressing, true, routingResult.Reason, routingResult.Message)
		operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionDegraded, false, operatorstatus.ReasonNoError, "No terminal reconcile error has been observed")
		return nil
	}})
}

func (r *TamossReconciler) updatePausedStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	return r.patchTamossStatusInput(ctx, tamoss, tamossStatusPatchInput{Apply: func(tamoss *tamossv1alpha1.Tamoss) error {
		tamoss.Status.Phase = operatorstatus.PhasePaused
		operatorstatus.SetConditionStatus(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionSchemaMigrated, metav1.ConditionUnknown, operatorstatus.ReasonPaused, "Schema reconciliation is paused")
		operatorstatus.SetConditionStatus(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionBackendsReady, metav1.ConditionUnknown, operatorstatus.ReasonPaused, "Backend checks are paused")
		operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionPaused, true, operatorstatus.ReasonReconciliationPaused, "Reconciliation is paused by spec.paused")
		operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionReady, false, operatorstatus.ReasonPaused, "Reconciliation is paused")
		operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionProgressing, false, operatorstatus.ReasonPaused, "Reconciliation is paused")
		operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionDegraded, false, operatorstatus.ReasonNoError, "No terminal reconcile error has been observed")
		return nil
	}})
}

func setActiveBlockedConditions(conditions *[]metav1.Condition, generation int64, reason, message, progressingMessage string) {
	operatorstatus.SetReconciliationActive(conditions, generation)
	operatorstatus.SetConditionBool(conditions, generation, operatorstatus.ConditionReady, false, reason, message)
	operatorstatus.SetConditionBool(conditions, generation, operatorstatus.ConditionProgressing, false, reason, progressingMessage)
	operatorstatus.SetConditionBool(conditions, generation, operatorstatus.ConditionDegraded, true, reason, message)
}

func setProviderBackendConditions(conditions *[]metav1.Condition, generation int64, backendResult providerBackendResult) {
	operatorstatus.SetReconciliationActive(conditions, generation)
	operatorstatus.SetConditionBool(conditions, generation, operatorstatus.ConditionReady, false, backendResult.Reason, backendResult.Message)
	operatorstatus.SetConditionBool(conditions, generation, operatorstatus.ConditionProgressing, !backendResult.Degraded, backendResult.Reason, backendResult.Message)
	operatorstatus.SetConditionBool(conditions, generation, operatorstatus.ConditionDegraded, backendResult.Degraded, backendResult.Reason, backendResult.Message)
}

func (r *TamossReconciler) patchTamossStatusInput(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, input tamossStatusPatchInput) error {
	original := tamoss.DeepCopy()
	setCommonTamossStatus(tamoss)
	if input.RefreshBackupPolicy {
		if err := r.refreshObservedBackupPolicyCondition(ctx, tamoss); err != nil {
			return err
		}
	}
	if input.Apply != nil {
		if err := input.Apply(tamoss); err != nil {
			return err
		}
	}
	return r.patchTamossStatus(ctx, tamoss, original)
}

func (r *TamossReconciler) patchTamossStatus(ctx context.Context, tamoss, original *tamossv1alpha1.Tamoss) error {
	if tamossStatusSemanticEqual(original.Status, tamoss.Status) {
		operatormetrics.RecordTamossStatus(tamoss)
		return nil
	}
	if err := r.Client.Status().Patch(ctx, tamoss, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return err
	}
	r.recordTamossLifecycleEvents(original, tamoss)
	operatormetrics.RecordTamossStatus(tamoss)
	return nil
}

func (r *TamossReconciler) recordTamossLifecycleEvents(original, tamoss *tamossv1alpha1.Tamoss) {
	if operatorstatus.ConditionBecameTrue(original.Status.Conditions, tamoss.Status.Conditions, operatorstatus.ConditionReady) {
		operatorstatus.EmitNormalEvent(r.Recorder, tamoss, operatorstatus.ReasonTamossReady, "Tamoss is ready")
	}
	if operatorstatus.ConditionBecameTrue(original.Status.Conditions, tamoss.Status.Conditions, operatorstatus.ConditionSchemaMigrated) {
		operatorstatus.EmitNormalEvent(r.Recorder, tamoss, operatorstatus.ReasonSchemaMigrationSucceeded, "Schema migration has completed")
	}
	if operatorstatus.ConditionBecameTrue(original.Status.Conditions, tamoss.Status.Conditions, operatorstatus.ConditionIdentityBlueprintSubmitted) {
		operatorstatus.EmitNormalEvent(r.Recorder, tamoss, operatorstatus.ReasonManagedBlueprintApplied, "Managed identity blueprint has been applied")
	}
	for _, conditionType := range tamossWarningConditionTypes() {
		reason, message, changed := operatorstatus.ChangedConditionReason(original.Status.Conditions, tamoss.Status.Conditions, conditionType)
		if changed && tamossWarningReason(reason) {
			operatorstatus.EmitWarningEvent(r.Recorder, &r.WarningEvents, tamoss, reason, message)
		}
	}
}

func setCommonTamossStatus(tamoss *tamossv1alpha1.Tamoss) {
	tamoss.Status.ObservedGeneration = tamoss.Generation
	tamoss.Status.Backends.DB.Provider = tamoss.Spec.Backends.DB.Provider()
	tamoss.Status.Backends.S3.Provider = tamoss.Spec.Backends.S3.Provider()
	tamoss.Status.Auth = authStatus(tamoss)
	tamoss.Status.Endpoints = endpointStatus(tamoss)
	tamoss.Status.Providers = providerStatus(tamoss)
	tamoss.Status.Resolved = resolvedTamossStatus(tamoss)
	setBackupPolicyCondition(&tamoss.Status.Conditions, tamoss)
	setUpgradeUnknown(tamoss, operatorstatus.ReasonUpgradeNotEvaluated, "Upgrade readiness has not been evaluated in this reconcile")
}
