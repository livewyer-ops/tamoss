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
	reason := operatorstatus.ReasonUnsupportedHTTPRouteFilter
	routing := routingStatusResult{
		Ready:           false,
		Reason:          reason,
		Message:         message,
		HostnameStatus:  metav1.ConditionUnknown,
		HostnameReason:  reason,
		HostnameMessage: message,
	}
	// This gate runs before any backend stage, so the backend state for this
	// pass is whatever was last observed.
	return r.patchTamossStatusObservation(ctx, tamoss, tamossStatusObservation{
		Phase:            operatorstatus.PhaseDegraded,
		SchemaState:      unknownCondition(reason, "Schema reconciliation is blocked by routing configuration"),
		BackendsFallback: unknownCondition(reason, "Backend state has not been evaluated while routing configuration is invalid"),
		Routing:          &routing,
		Ready:            boolCondition(false, reason, message),
		Progressing:      boolCondition(false, reason, "Reconciliation is blocked by routing configuration"),
		Degraded:         boolCondition(true, reason, message),
	})
}
