package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func observationFixture() tamossStatusObservation {
	return tamossStatusObservation{
		Phase:            operatorstatus.PhaseProgressing,
		SchemaState:      unknownCondition(operatorstatus.ReasonGatewayAPIUnavailable, "schema unchanged"),
		BackendsFallback: unknownCondition(operatorstatus.ReasonGatewayAPIUnavailable, "backends not observed"),
		Ready:            boolCondition(false, operatorstatus.ReasonGatewayAPIUnavailable, "not ready"),
		Progressing:      boolCondition(true, operatorstatus.ReasonGatewayAPIUnavailable, "progressing"),
		Degraded:         boolCondition(false, operatorstatus.ReasonNoError, "no error"),
	}
}

func TestStatusObservationWithoutBackendObservationFallsBackToUnknown(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Generation: 3}}

	applyTamossStatusObservation(tamoss, observationFixture())

	condition := meta.FindStatusCondition(tamoss.Status.Conditions, operatorstatus.ConditionBackendsReady)
	if condition == nil {
		t.Fatal("expected BackendsReady condition to be set")
	}
	if condition.Status != metav1.ConditionUnknown {
		t.Fatalf("expected unobserved backend state to be Unknown, got %s", condition.Status)
	}
	if condition.Reason != operatorstatus.ReasonGatewayAPIUnavailable {
		t.Fatalf("expected fallback reason, got %s", condition.Reason)
	}
}

func TestStatusObservationWithoutBackendObservationPreservesLastObservedState(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Generation: 3}}
	operatorstatus.SetConditionBool(&tamoss.Status.Conditions, 2, operatorstatus.ConditionBackendsReady, false, operatorstatus.ReasonMissingSecret, "Required secret app-backends was not found")

	applyTamossStatusObservation(tamoss, observationFixture())

	condition := meta.FindStatusCondition(tamoss.Status.Conditions, operatorstatus.ConditionBackendsReady)
	if condition == nil {
		t.Fatal("expected BackendsReady condition to be set")
	}
	if condition.Status != metav1.ConditionFalse || condition.Reason != operatorstatus.ReasonMissingSecret {
		t.Fatalf("expected last observed backend state to be preserved, got %s/%s", condition.Status, condition.Reason)
	}
	if condition.ObservedGeneration != 3 {
		t.Fatalf("expected preserved condition to be re-stamped at the current generation, got %d", condition.ObservedGeneration)
	}
}

func TestStatusObservationRecordsObservedBackendState(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
	observation := observationFixture()
	backends := boolCondition(false, operatorstatus.ReasonClusterNotReady, "CNPG Cluster app-db is not ready")
	observation.Backends = &backends

	applyTamossStatusObservation(tamoss, observation)

	condition := meta.FindStatusCondition(tamoss.Status.Conditions, operatorstatus.ConditionBackendsReady)
	if condition == nil {
		t.Fatal("expected BackendsReady condition to be set")
	}
	if condition.Status != metav1.ConditionFalse || condition.Reason != operatorstatus.ReasonClusterNotReady {
		t.Fatalf("expected observed backend state to be recorded, got %s/%s", condition.Status, condition.Reason)
	}
}
