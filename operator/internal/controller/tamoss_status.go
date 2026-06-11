package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatormetrics "github.com/livewyer-ops/tamoss/operator/internal/metrics"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

type backendBlockedStatusInput struct {
	Reason             string
	Message            string
	SchemaMessage      string
	ProgressingMessage string
}

// statusConditionValue is one observed condition outcome: status plus the
// reason and message that explain it.
type statusConditionValue struct {
	Status  metav1.ConditionStatus
	Reason  string
	Message string
}

func boolCondition(ok bool, reason, message string) statusConditionValue {
	status := metav1.ConditionFalse
	if ok {
		status = metav1.ConditionTrue
	}
	return statusConditionValue{Status: status, Reason: reason, Message: message}
}

func unknownCondition(reason, message string) statusConditionValue {
	return statusConditionValue{Status: metav1.ConditionUnknown, Reason: reason, Message: message}
}

// tamossStatusObservation captures everything a reconcile pass has learned by
// the time it writes status. applyTamossStatusObservation derives the phase
// and the full condition matrix from a single observation so that every
// status writer shares one implementation and the per-path condition sets
// cannot skew apart.
type tamossStatusObservation struct {
	RefreshBackupPolicy bool
	Phase               string
	Paused              bool

	// Schema carries a known schema outcome and also records the schema
	// version and upgrade status; SchemaState is the SchemaMigrated
	// condition used when the schema outcome is not known on this path.
	Schema      *SchemaResult
	SchemaState statusConditionValue

	// Backends is the observed backend state. A nil value means this pass
	// learned nothing new about the backends; the last observed state is
	// re-affirmed, falling back to BackendsFallback when none exists.
	Backends         *statusConditionValue
	BackendsFallback statusConditionValue

	IdentityBlueprint *statusConditionValue
	Identity          *statusConditionValue
	Routing           *routingStatusResult
	Replicas          *tamossv1alpha1.ReplicaStatus

	Ready       statusConditionValue
	Progressing statusConditionValue
	Degraded    statusConditionValue
}

func (r *TamossReconciler) updateStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, schemaResult SchemaResult, identityResult identityReconcileResult) error {
	replicas := tamossv1alpha1.ReplicaStatus{
		API:    r.componentReplicaStatus(ctx, tamoss, "api", tamoss.Spec.API.IsEnabled(), desiredReplicaCount(tamoss.Spec.API.WorkloadCommonSpec)),
		UI:     r.componentReplicaStatus(ctx, tamoss, "ui", tamoss.Spec.UI.IsEnabled(), desiredReplicaCount(tamoss.Spec.UI.WorkloadCommonSpec)),
		Worker: r.componentReplicaStatus(ctx, tamoss, "worker", tamoss.Spec.Worker.IsEnabled(), desiredReplicaCount(tamoss.Spec.Worker.WorkloadCommonSpec)),
	}
	routingResult, err := r.routingStatus(ctx, tamoss)
	if err != nil {
		return err
	}

	ready := !schemaResult.Degraded && schemaResult.Ready && identityResult.Ready && routingResult.Ready &&
		replicasReady(replicas.API) && replicasReady(replicas.UI) && replicasReady(replicas.Worker)
	phase := operatorstatus.PhaseProgressing
	if ready {
		phase = operatorstatus.PhaseReady
	}
	if schemaResult.Degraded {
		phase = operatorstatus.PhaseDegraded
	}

	identityBlueprint := boolCondition(identityResult.BlueprintSubmitted, identityBlueprintReason(identityResult), identityBlueprintMessage(identityResult))
	identity := boolCondition(identityResult.Ready, identityResult.Reason, identityResult.Message)
	return r.patchTamossStatusObservation(ctx, tamoss, tamossStatusObservation{
		RefreshBackupPolicy: true,
		Phase:               phase,
		Schema:              &schemaResult,
		Backends:            &statusConditionValue{Status: metav1.ConditionTrue, Reason: operatorstatus.ReasonBackendReferencesConfigured, Message: "Backend secret references are configured"},
		IdentityBlueprint:   &identityBlueprint,
		Identity:            &identity,
		Routing:             &routingResult,
		Replicas:            &replicas,
		Ready:               boolCondition(ready, readyReason(ready, schemaResult, identityResult, routingResult), readyMessage(ready, schemaResult, identityResult, routingResult)),
		Progressing:         boolCondition(!ready && !schemaResult.Degraded, operatorstatus.ReasonReconciling, "Waiting for managed workloads to become available"),
		Degraded:            boolCondition(schemaResult.Degraded, degradedReason(schemaResult), degradedMessage(schemaResult)),
	})
}

func (r *TamossReconciler) updateBlockedBackendStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, input backendBlockedStatusInput) error {
	backends := boolCondition(false, input.Reason, input.Message)
	return r.patchTamossStatusObservation(ctx, tamoss, tamossStatusObservation{
		Phase:       operatorstatus.PhaseDegraded,
		SchemaState: unknownCondition(input.Reason, input.SchemaMessage),
		Backends:    &backends,
		Ready:       boolCondition(false, input.Reason, input.Message),
		Progressing: boolCondition(false, input.Reason, input.ProgressingMessage),
		Degraded:    boolCondition(true, input.Reason, input.Message),
	})
}

func (r *TamossReconciler) updateProviderBackendStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, schemaResult *SchemaResult, backendResult providerBackendResult) error {
	phase := operatorstatus.PhaseProgressing
	if backendResult.Degraded {
		phase = operatorstatus.PhaseDegraded
	}
	backends := boolCondition(false, backendResult.Reason, backendResult.Message)
	return r.patchTamossStatusObservation(ctx, tamoss, tamossStatusObservation{
		RefreshBackupPolicy: true,
		Phase:               phase,
		Schema:              schemaResult,
		SchemaState:         unknownCondition(backendResult.Reason, "Schema reconciliation is blocked by backend readiness"),
		Backends:            &backends,
		Ready:               boolCondition(false, backendResult.Reason, backendResult.Message),
		Progressing:         boolCondition(!backendResult.Degraded, backendResult.Reason, backendResult.Message),
		Degraded:            boolCondition(backendResult.Degraded, backendResult.Reason, backendResult.Message),
	})
}

func (r *TamossReconciler) updateGatewayAPIUnavailableStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	routingResult := gatewayAPIUnavailableResult()
	return r.patchTamossStatusObservation(ctx, tamoss, tamossStatusObservation{
		Phase:            operatorstatus.PhaseProgressing,
		SchemaState:      unknownCondition(routingResult.Reason, "Schema migration status is unchanged while Gateway API routing is unavailable"),
		BackendsFallback: unknownCondition(routingResult.Reason, "Backend state has not been observed while Gateway API routing is unavailable"),
		Routing:          &routingResult,
		Ready:            boolCondition(false, routingResult.Reason, routingResult.Message),
		Progressing:      boolCondition(true, routingResult.Reason, routingResult.Message),
		Degraded:         boolCondition(false, operatorstatus.ReasonNoError, "No terminal reconcile error has been observed"),
	})
}

func (r *TamossReconciler) updatePausedStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	backends := unknownCondition(operatorstatus.ReasonPaused, "Backend checks are paused")
	return r.patchTamossStatusObservation(ctx, tamoss, tamossStatusObservation{
		Phase:       operatorstatus.PhasePaused,
		Paused:      true,
		SchemaState: unknownCondition(operatorstatus.ReasonPaused, "Schema reconciliation is paused"),
		Backends:    &backends,
		Ready:       boolCondition(false, operatorstatus.ReasonPaused, "Reconciliation is paused"),
		Progressing: boolCondition(false, operatorstatus.ReasonPaused, "Reconciliation is paused"),
		Degraded:    boolCondition(false, operatorstatus.ReasonNoError, "No terminal reconcile error has been observed"),
	})
}

func (r *TamossReconciler) patchTamossStatusObservation(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, observation tamossStatusObservation) error {
	original := tamoss.DeepCopy()
	setCommonTamossStatus(tamoss)
	if observation.RefreshBackupPolicy {
		if err := r.refreshObservedBackupPolicyCondition(ctx, tamoss); err != nil {
			return err
		}
	}
	applyTamossStatusObservation(tamoss, observation)
	return r.patchTamossStatus(ctx, tamoss, original)
}

// applyTamossStatusObservation is the single place the Tamoss phase and
// condition matrix are derived from what a reconcile pass observed.
func applyTamossStatusObservation(tamoss *tamossv1alpha1.Tamoss, observation tamossStatusObservation) {
	generation := tamoss.Generation
	conditions := &tamoss.Status.Conditions
	tamoss.Status.Phase = observation.Phase
	if observation.Replicas != nil {
		tamoss.Status.Replicas = *observation.Replicas
	}
	if observation.Schema != nil {
		tamoss.Status.SchemaVersion = observation.Schema.Version
		setUpgradeStatusFromSchema(tamoss, *observation.Schema)
		setStatusCondition(conditions, generation, operatorstatus.ConditionSchemaMigrated, boolCondition(observation.Schema.Ready, schemaReason(*observation.Schema), schemaMessage(*observation.Schema)))
	} else {
		setStatusCondition(conditions, generation, operatorstatus.ConditionSchemaMigrated, observation.SchemaState)
	}
	setBackendsReadyCondition(conditions, generation, observation)
	if observation.IdentityBlueprint != nil {
		setStatusCondition(conditions, generation, operatorstatus.ConditionIdentityBlueprintSubmitted, *observation.IdentityBlueprint)
	}
	if observation.Identity != nil {
		setStatusCondition(conditions, generation, operatorstatus.ConditionIdentityReady, *observation.Identity)
	}
	if observation.Routing != nil {
		setRoutingConditions(conditions, generation, *observation.Routing)
	}
	if observation.Paused {
		operatorstatus.SetConditionBool(conditions, generation, operatorstatus.ConditionPaused, true, operatorstatus.ReasonReconciliationPaused, "Reconciliation is paused by spec.paused")
	} else {
		operatorstatus.SetReconciliationActive(conditions, generation)
	}
	setStatusCondition(conditions, generation, operatorstatus.ConditionReady, observation.Ready)
	setStatusCondition(conditions, generation, operatorstatus.ConditionProgressing, observation.Progressing)
	setStatusCondition(conditions, generation, operatorstatus.ConditionDegraded, observation.Degraded)
}

// setBackendsReadyCondition keeps BackendsReady tied to what was actually
// observed. When a pass learned nothing new about the backends — for example
// the Gateway-API-unavailable path aborts before the backend stages report —
// the previously observed state is re-affirmed at the current generation
// instead of being overwritten with a hard-coded value.
func setBackendsReadyCondition(conditions *[]metav1.Condition, generation int64, observation tamossStatusObservation) {
	observed := observation.Backends
	if observed == nil {
		if existing := meta.FindStatusCondition(*conditions, operatorstatus.ConditionBackendsReady); existing != nil {
			observed = &statusConditionValue{Status: existing.Status, Reason: existing.Reason, Message: existing.Message}
		} else {
			observed = &observation.BackendsFallback
		}
	}
	setStatusCondition(conditions, generation, operatorstatus.ConditionBackendsReady, *observed)
}

func setStatusCondition(conditions *[]metav1.Condition, generation int64, conditionType string, value statusConditionValue) {
	operatorstatus.SetConditionStatus(conditions, generation, conditionType, value.Status, value.Reason, value.Message)
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
