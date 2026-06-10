package controller

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func TestTamossLifecycleEventsFromConditionTransitions(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	reconciler := &TamossReconciler{Recorder: recorder}
	original := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"},
	}
	updated := original.DeepCopy()
	operatorstatus.SetConditionBool(&updated.Status.Conditions, updated.Generation, operatorstatus.ConditionReady, true, operatorstatus.ReasonAllComponentsReady, "ready")
	operatorstatus.SetConditionBool(&updated.Status.Conditions, updated.Generation, operatorstatus.ConditionSchemaMigrated, true, operatorstatus.ReasonSchemaApplied, "schema ready")
	operatorstatus.SetConditionBool(&updated.Status.Conditions, updated.Generation, operatorstatus.ConditionIdentityBlueprintSubmitted, true, operatorstatus.ReasonManagedBlueprintApplied, "blueprint applied")

	reconciler.recordTamossLifecycleEvents(original, updated)

	events := drainRecorder(recorder)
	assertEventContains(t, events, operatorstatus.ReasonTamossReady)
	assertEventContains(t, events, operatorstatus.ReasonSchemaMigrationSucceeded)
	assertEventContains(t, events, operatorstatus.ReasonManagedBlueprintApplied)
}

func TestTamossWarningEventsFromConditionTransitions(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	reconciler := &TamossReconciler{Recorder: recorder}
	original := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"},
	}
	updated := original.DeepCopy()
	operatorstatus.SetConditionBool(&updated.Status.Conditions, updated.Generation, operatorstatus.ConditionBackendsReady, false, operatorstatus.ReasonMissingDependencyOperator, "CNPG is not installed")
	operatorstatus.SetConditionBool(&updated.Status.Conditions, updated.Generation, operatorstatus.ConditionRoutingReady, false, operatorstatus.ReasonRouteRejected, "HTTPRoute was rejected")
	operatorstatus.SetConditionBool(&updated.Status.Conditions, updated.Generation, operatorstatus.ConditionBackupPolicyReady, false, operatorstatus.ReasonBackupArchivingFailed, "CNPG archiving failed")

	reconciler.recordTamossLifecycleEvents(original, updated)

	events := drainRecorder(recorder)
	if got := len(events); got != 3 {
		t.Fatalf("expected three warning events, got %d: %#v", got, events)
	}
	assertEventContains(t, events, operatorstatus.ReasonMissingDependencyOperator)
	assertEventContains(t, events, operatorstatus.ReasonRouteRejected)
	assertEventContains(t, events, operatorstatus.ReasonBackupArchivingFailed)
}

func TestTamossWarningEventsIgnoreProgressReasons(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	reconciler := &TamossReconciler{Recorder: recorder}
	original := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"},
	}
	updated := original.DeepCopy()
	operatorstatus.SetConditionBool(&updated.Status.Conditions, updated.Generation, operatorstatus.ConditionReady, false, operatorstatus.ReasonComponentsProgressing, "Components are still rolling out")
	operatorstatus.SetConditionBool(&updated.Status.Conditions, updated.Generation, operatorstatus.ConditionSchemaMigrated, false, operatorstatus.ReasonWaitingForSchema, "Schema migration is still running")
	operatorstatus.SetConditionBool(&updated.Status.Conditions, updated.Generation, operatorstatus.ConditionRoutingReady, false, operatorstatus.ReasonRoutePending, "Route has not reported status yet")

	reconciler.recordTamossLifecycleEvents(original, updated)

	if events := drainRecorder(recorder); len(events) != 0 {
		t.Fatalf("expected no warning events for progress reasons, got %#v", events)
	}
}

func TestTamossWarningsAreDedupedByReasonAndMessage(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	reconciler := &TamossReconciler{Recorder: recorder}
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"},
	}

	reconciler.recordWarning(tamoss, operatorstatus.ReasonAuthentikManagedBlueprintApplyFailed, "Authentik managed Blueprint apply failed")
	reconciler.recordWarning(tamoss, operatorstatus.ReasonAuthentikManagedBlueprintApplyFailed, "Authentik managed Blueprint apply failed")
	reconciler.recordWarning(tamoss, operatorstatus.ReasonAuthentikManagedBlueprintApplyFailed, "Authentik proxy outpost apply failed")

	events := drainRecorder(recorder)
	if got := len(events); got != 2 {
		t.Fatalf("expected two deduped warning events, got %d: %#v", got, events)
	}
	assertEventContains(t, events, operatorstatus.ReasonAuthentikManagedBlueprintApplyFailed)
}

func TestDriftCorrectedSuppressesInitialConvergence(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	reconciler := &TamossReconciler{Recorder: recorder}
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"},
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "example-api", Namespace: "media"},
	}

	reconciler.recordDriftCorrected(tamoss, deployment)
	if events := drainRecorder(recorder); len(events) != 0 {
		t.Fatalf("expected no drift event before Ready=True, got %#v", events)
	}

	operatorstatus.SetConditionBool(&tamoss.Status.Conditions, tamoss.Generation, operatorstatus.ConditionReady, true, operatorstatus.ReasonAllComponentsReady, "ready")
	reconciler.recordDriftCorrected(tamoss, deployment)

	events := drainRecorder(recorder)
	if got := len(events); got != 1 {
		t.Fatalf("expected one drift event after Ready=True, got %d: %#v", got, events)
	}
	assertEventContains(t, events, operatorstatus.ReasonDriftCorrected)
}

func drainRecorder(recorder *record.FakeRecorder) []string {
	events := []string{}
	for {
		select {
		case event := <-recorder.Events:
			events = append(events, event)
		default:
			return events
		}
	}
}

func assertEventContains(t *testing.T, events []string, needle string) {
	t.Helper()
	for _, event := range events {
		if strings.Contains(event, needle) {
			return
		}
	}
	t.Fatalf("expected event containing %q, got %#v", needle, events)
}
