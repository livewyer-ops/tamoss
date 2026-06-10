package controller

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/workload_renderer"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func (r *TamossReconciler) reconcileHTTPRouteInputGate(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, recordPhase func(string)) (reconcileControl, error) {
	invalid := workload_renderer.ValidateHTTPRouteFilters(tamoss)
	if len(invalid) == 0 {
		return continueReconcile(), nil
	}
	message := fmt.Sprintf("Unsupported or invalid HTTPRoute filter configuration: %s", strings.Join(invalid, ", "))
	if err := r.updateHTTPRouteInputStatus(ctx, tamoss, message); err != nil {
		return stopReconcileNow(), err
	}
	recordPhase(tamoss.Status.Phase)
	return stopReconcileNow(), nil
}

func (r *TamossReconciler) updateHTTPRouteInputStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, message string) error {
	return r.patchTamossStatusInput(ctx, tamoss, tamossStatusPatchInput{Apply: func(tamoss *tamossv1alpha1.Tamoss) error {
		tamoss.Status.Phase = operatorstatus.PhaseDegraded
		operatorstatus.SetConditionStatus(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionSchemaMigrated, metav1.ConditionUnknown, operatorstatus.ReasonUnsupportedHTTPRouteFilter, "Schema reconciliation is blocked by routing configuration")
		operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionBackendsReady, true, operatorstatus.ReasonBackendReferencesConfigured, "Backend secret references are configured")
		operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionRoutingReady, false, operatorstatus.ReasonUnsupportedHTTPRouteFilter, message)
		operatorstatus.SetConditionStatus(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionHostnamesReady, metav1.ConditionUnknown, operatorstatus.ReasonUnsupportedHTTPRouteFilter, message)
		setActiveBlockedConditions(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ReasonUnsupportedHTTPRouteFilter, message, "Reconciliation is blocked by routing configuration")
		return nil
	}})
}
