package controller

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func TestTamossPrimaryPredicateSuppressesStatusOnlyUpdate(t *testing.T) {
	predicate := tamossPrimaryPredicate()
	oldObj := predicateTamoss("example")
	newObj := oldObj.DeepCopy()
	newObj.Status.Phase = operatorstatus.PhaseReady

	if predicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatal("expected status-only update with unchanged generation to be suppressed")
	}
}

func TestTamossPrimaryPredicateAllowsLifecycleStatusUpdate(t *testing.T) {
	predicate := tamossPrimaryPredicate()
	oldObj := predicateTamoss("example")
	newObj := oldObj.DeepCopy()
	newObj.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:  string(tamossv1alpha1.TamossLifecyclePhaseHibernating),
		Reason: operatorstatus.ReasonTamossHibernating,
	}

	if !predicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatal("expected lifecycle status change to be allowed")
	}
}

func TestTamossPrimaryPredicateAllowsSpecUpdate(t *testing.T) {
	predicate := tamossPrimaryPredicate()
	oldObj := predicateTamoss("example")
	newObj := oldObj.DeepCopy()
	newObj.Generation = oldObj.Generation + 1

	if !predicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatal("expected generation change to be allowed")
	}
}

func TestTamossPrimaryPredicateAllowsActionAnnotationUpdate(t *testing.T) {
	predicate := tamossPrimaryPredicate()
	oldObj := predicateTamoss("example")
	newObj := oldObj.DeepCopy()
	newObj.Annotations = map[string]string{AnnotationSchemaRetry: "retry-1"}

	if !predicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatal("expected schema retry annotation change to be allowed")
	}
	oldObj = newObj.DeepCopy()
	newObj = oldObj.DeepCopy()
	newObj.Annotations = map[string]string{AnnotationSchemaRetry: "retry-1", AnnotationAPITokenRotate: "rotate-1"}
	if !predicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatal("expected API token rotation annotation change to be allowed")
	}
}

func TestPrimaryPredicateAllowsDeletionTimestampAndFinalizerChanges(t *testing.T) {
	predicate := storageBackendPrimaryPredicate()
	oldObj := predicateStorageBackend("archive")
	newObj := oldObj.DeepCopy()
	now := metav1.NewTime(time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC))
	newObj.DeletionTimestamp = &now

	if !predicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatal("expected deletion timestamp change to be allowed")
	}
	oldObj = predicateStorageBackend("archive")
	newObj = oldObj.DeepCopy()
	newObj.Finalizers = []string{storageBackendFinalizer}
	if !predicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatal("expected finalizer change to be allowed")
	}
}

func TestStorageBackendPrimaryPredicateSuppressesStatusOnlyUpdate(t *testing.T) {
	predicate := storageBackendPrimaryPredicate()
	oldObj := predicateStorageBackend("archive")
	newObj := oldObj.DeepCopy()
	newObj.Status.Phase = operatorstatus.PhaseReady

	if predicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatal("expected StorageBackend status-only update with unchanged generation to be suppressed")
	}
}

func TestStatusSemanticEqualityIgnoresGeneratedTimestamps(t *testing.T) {
	first := tamossv1alpha1.TamossStatus{
		Phase: operatorstatus.PhaseReady,
		Conditions: []metav1.Condition{{
			Type:               operatorstatus.ConditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             operatorstatus.ReasonAllComponentsReady,
			Message:            "ready",
			LastTransitionTime: metav1.NewTime(time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)),
		}},
		SchemaMigration: tamossv1alpha1.SchemaMigrationStatus{
			Phase:             operatorstatus.PhaseSucceeded,
			LastAttemptTime:   &metav1.Time{Time: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)},
			LastAttemptResult: operatorstatus.PhaseSucceeded,
			Attempts:          1,
		},
	}
	second := first
	second.Conditions = append([]metav1.Condition(nil), first.Conditions...)
	second.Conditions[0].LastTransitionTime = metav1.NewTime(time.Date(2026, 5, 22, 12, 1, 0, 0, time.UTC))
	second.SchemaMigration.LastAttemptTime = &metav1.Time{Time: time.Date(2026, 5, 22, 12, 1, 0, 0, time.UTC)}

	if !tamossStatusSemanticEqual(first, second) {
		t.Fatal("expected timestamp-only status differences to be semantic no-ops")
	}

	firstOperation := tamossv1alpha1.TamossOperationStatus{
		Phase: string(tamossv1alpha1.TamossOperationPhaseCompleted),
		Conditions: []metav1.Condition{{
			Type:               operatorstatus.ConditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             operatorstatus.ReasonTamossReady,
			Message:            "ready",
			LastTransitionTime: metav1.NewTime(time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)),
		}},
	}
	secondOperation := firstOperation
	secondOperation.Conditions = append([]metav1.Condition(nil), firstOperation.Conditions...)
	secondOperation.Conditions[0].LastTransitionTime = metav1.NewTime(time.Date(2026, 5, 22, 12, 1, 0, 0, time.UTC))

	if !operationStatusSemanticEqual(firstOperation, secondOperation) {
		t.Fatal("expected operation status timestamp-only differences to be semantic no-ops")
	}
}

func TestPatchStatusSkipsUnchangedStatusWithoutClientWrite(t *testing.T) {
	tamoss := predicateTamoss("example")
	tamoss.Status.Phase = operatorstatus.PhaseReady
	reconciler := &TamossReconciler{}
	if err := reconciler.patchTamossStatus(context.TODO(), tamoss, tamoss.DeepCopy()); err != nil {
		t.Fatalf("expected unchanged Tamoss status to skip client write: %v", err)
	}
	lifecycle := tamossv1alpha1.TamossLifecycleStatus{
		Phase:   string(tamossv1alpha1.TamossLifecyclePhaseRunning),
		Reason:  operatorstatus.ReasonTamossReady,
		Message: "TAMOSS lifecycle is running",
	}
	tamoss.Status.Lifecycle = lifecycle
	if err := patchTamossLifecycleStatus(context.TODO(), nil, tamoss, lifecycle); err != nil {
		t.Fatalf("expected unchanged Tamoss lifecycle status to skip client write: %v", err)
	}
	storageBackend := predicateStorageBackend("archive")
	storageBackend.Status.Phase = operatorstatus.PhaseReady
	storageReconciler := &StorageBackendReconciler{}
	if err := storageReconciler.patchStorageBackendStatus(context.TODO(), storageBackend, storageBackend.DeepCopy()); err != nil {
		t.Fatalf("expected unchanged StorageBackend status to skip client write: %v", err)
	}
	hibernate := &tamossv1alpha1.TamossHibernate{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 1,
			Name:       "snapshot",
			Namespace:  "media",
		},
	}
	artifact := tamossv1alpha1.HibernationArtifactStatus{ManifestKey: "hibernations/example/snapshot/manifest.json"}
	setOperationStatus(&hibernate.Status, hibernate.Generation, tamossv1alpha1.TamossOperationPhaseCompleted, operatorstatus.ReasonTamossHibernated, "complete", artifact)
	hibernateReconciler := &TamossHibernateReconciler{}
	if err := hibernateReconciler.updateHibernateStatus(context.TODO(), hibernate, tamossv1alpha1.TamossOperationPhaseCompleted, operatorstatus.ReasonTamossHibernated, "complete", artifact); err != nil {
		t.Fatalf("expected unchanged TamossHibernate status to skip client write: %v", err)
	}
}

func predicateTamoss(name string) *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "media", Generation: 1},
	}
}

func predicateStorageBackend(name string) *tamossv1alpha1.StorageBackend {
	return &tamossv1alpha1.StorageBackend{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "media", Generation: 1},
	}
}
