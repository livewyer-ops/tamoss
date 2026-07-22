package controller

import (
	"context"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
	"github.com/livewyer-ops/tamoss/operator/internal/webhook/deleteprotection"
)

func hibernationSpecTamossFixture() *tamossv1alpha1.Tamoss {
	tamoss := hibernateTamossFixture()
	tamoss.Spec.Hibernation = tamossv1alpha1.TamossHibernationSpec{
		Enabled: true,
		Destination: &tamossv1alpha1.HibernationDestinationSpec{
			StorageBackendRef: tamossv1alpha1.LocalObjectReference{Name: "archive"},
			Prefix:            "hibernate/example",
		},
	}
	return tamoss
}

func TestHibernationSpecMaterialisesOperation(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernationSpecTamossFixture()
	recorder := record.NewFakeRecorder(4)

	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
			WithObjects(tamoss).
			Build(),
		Scheme:   scheme,
		Recorder: recorder,
	}

	if err := reconciler.reconcileHibernationSpec(ctx, tamoss); err != nil {
		t.Fatalf("expected hibernation spec reconcile without error, got %v", err)
	}

	operation := &tamossv1alpha1.TamossHibernate{}
	key := types.NamespacedName{Name: "example-hibernation-1", Namespace: tamoss.Namespace}
	if err := reconciler.Client.Get(ctx, key, operation); err != nil {
		t.Fatalf("expected materialised TamossHibernate: %v", err)
	}
	if !metav1.IsControlledBy(operation, tamoss) {
		t.Fatalf("expected operation to be controller-owned by the Tamoss, got %#v", operation.OwnerReferences)
	}
	if operation.Annotations[deleteprotection.ConfirmationAnnotation] != "true" {
		t.Fatalf("expected deletion confirmation annotation, got %#v", operation.Annotations)
	}
	if operation.Spec.TamossRef.Name != tamoss.Name ||
		operation.Spec.Destination.StorageBackendRef.Name != "archive" ||
		operation.Spec.Destination.Prefix != "hibernate/example" {
		t.Fatalf("unexpected materialised spec: %#v", operation.Spec)
	}

	updated := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updated); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updated.Status.Lifecycle.HibernationCycle != 1 {
		t.Fatalf("expected hibernation cycle 1, got %d", updated.Status.Lifecycle.HibernationCycle)
	}

	// A second pass is idempotent.
	if err := reconciler.reconcileHibernationSpec(ctx, updated); err != nil {
		t.Fatalf("expected idempotent reconcile, got %v", err)
	}
	operations := &tamossv1alpha1.TamossHibernateList{}
	if err := reconciler.Client.List(ctx, operations); err != nil {
		t.Fatalf("list operations: %v", err)
	}
	if len(operations.Items) != 1 {
		t.Fatalf("expected exactly one materialised operation, got %d", len(operations.Items))
	}
	assertEventContains(t, drainRecorder(recorder), operatorstatus.ReasonTamossHibernating)
}

func TestHibernationSpecStartsNewCycleAfterWake(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernationSpecTamossFixture()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:            string(tamossv1alpha1.TamossLifecyclePhaseRunning),
		Reason:           operatorstatus.ReasonTamossReady,
		HibernationCycle: 1,
	}

	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
			WithObjects(tamoss).
			Build(),
		Scheme: scheme,
	}

	if err := reconciler.reconcileHibernationSpec(ctx, tamoss); err != nil {
		t.Fatalf("expected new cycle reconcile without error, got %v", err)
	}
	operation := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: "example-hibernation-2", Namespace: tamoss.Namespace}, operation); err != nil {
		t.Fatalf("expected second-cycle operation: %v", err)
	}
	updated := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updated); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updated.Status.Lifecycle.HibernationCycle != 2 {
		t.Fatalf("expected hibernation cycle 2, got %d", updated.Status.Lifecycle.HibernationCycle)
	}
}

func TestHibernationSpecAbortsMaterialisedOperationWhenDisabled(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernationSpecTamossFixture()
	tamoss.Spec.Hibernation.Enabled = false

	operation := &tamossv1alpha1.TamossHibernate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-hibernation-1",
			Namespace: tamoss.Namespace,
		},
		Spec: tamossv1alpha1.TamossHibernateSpec{
			TamossRef:   tamossv1alpha1.TamossReferenceSpec{Name: tamoss.Name},
			Destination: *hibernationSpecTamossFixture().Spec.Hibernation.Destination,
		},
	}
	controller := true
	operation.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: tamossv1alpha1.GroupVersion.String(),
		Kind:       "Tamoss",
		Name:       tamoss.Name,
		UID:        tamoss.UID,
		Controller: &controller,
	}}
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:              string(tamossv1alpha1.TamossLifecyclePhaseHibernating),
		Reason:             operatorstatus.ReasonTamossHibernating,
		HibernationCycle:   1,
		ActiveOperationRef: operationObjectReference(operation, "TamossHibernate"),
	}

	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
			WithObjects(tamoss, operation).
			Build(),
		Scheme: scheme,
	}

	if err := reconciler.reconcileHibernationSpec(ctx, tamoss); err != nil {
		t.Fatalf("expected abort reconcile without error, got %v", err)
	}
	err := reconciler.Client.Get(ctx, types.NamespacedName{Name: operation.Name, Namespace: operation.Namespace}, &tamossv1alpha1.TamossHibernate{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected materialised operation to be deleted, got err %v", err)
	}
}

func TestHibernationSpecLeavesUserOperationsAlone(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernationSpecTamossFixture()
	tamoss.Spec.Hibernation.Enabled = false
	userOperation := hibernateFixture()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:              string(tamossv1alpha1.TamossLifecyclePhaseHibernating),
		Reason:             operatorstatus.ReasonTamossHibernating,
		ActiveOperationRef: operationObjectReference(userOperation, "TamossHibernate"),
	}

	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
			WithObjects(tamoss, userOperation).
			Build(),
		Scheme: scheme,
	}

	if err := reconciler.reconcileHibernationSpec(ctx, tamoss); err != nil {
		t.Fatalf("expected reconcile without error, got %v", err)
	}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: userOperation.Name, Namespace: userOperation.Namespace}, &tamossv1alpha1.TamossHibernate{}); err != nil {
		t.Fatalf("expected user-created operation to survive, got %v", err)
	}
}

func TestHibernationSpecGatesReconcile(t *testing.T) {
	tamoss := hibernationSpecTamossFixture()
	if !tamossLifecycleBlocksReconcile(tamoss) {
		t.Fatal("expected declared hibernation to gate reconciliation")
	}
	tamoss.Status.Lifecycle.Phase = string(tamossv1alpha1.TamossLifecyclePhaseFailed)
	if !tamossLifecycleBlocksReconcile(tamoss) {
		t.Fatal("expected a failed cycle to stay gated while hibernation is declared")
	}
	tamoss.Spec.Hibernation.Enabled = false
	if tamossLifecycleBlocksReconcile(tamoss) {
		t.Fatal("expected failed lifecycle without declared hibernation to un-gate")
	}
}

func TestTamossHibernateRetryAnnotationReArmsFailedOperation(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	hibernate := hibernateFixture()
	hibernate.Annotations = map[string]string{AnnotationOperationRetry: "retry-1"}
	hibernate.Status.Phase = string(tamossv1alpha1.TamossOperationPhaseFailed)
	hibernate.Status.Reason = operatorstatus.ReasonBackupPolicyFailed

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.TamossHibernate{}).
			WithObjects(hibernate).
			Build(),
		Scheme: scheme,
	}

	request := types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}
	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(request))
	if err != nil {
		t.Fatalf("expected retry acceptance without error, got %v", err)
	}
	if result.RequeueAfter <= 0 {
		t.Fatalf("expected requeue after accepting retry, got %#v", result)
	}

	updated := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, request, updated); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if updated.Status.Phase == string(tamossv1alpha1.TamossOperationPhaseFailed) {
		t.Fatalf("expected failed phase to be cleared, got %#v", updated.Status)
	}
	if updated.Status.AcceptedRetry != "retry-1" {
		t.Fatalf("expected accepted retry to be recorded, got %#v", updated.Status)
	}

	// The same annotation value is honoured only once.
	updated.Status.Phase = string(tamossv1alpha1.TamossOperationPhaseFailed)
	if err := reconciler.Client.Status().Update(ctx, updated); err != nil {
		t.Fatalf("re-fail operation: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, reconcileRequestFor(request)); err != nil {
		t.Fatalf("expected reconcile without error, got %v", err)
	}
	final := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, request, final); err != nil {
		t.Fatalf("get final TamossHibernate: %v", err)
	}
	if final.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) {
		t.Fatalf("expected already-honoured retry value to stay Failed, got %#v", final.Status)
	}
}

func reconcileRequestFor(name types.NamespacedName) ctrl.Request {
	return ctrl.Request{NamespacedName: name}
}

func TestLifecycleGateFreezesMigratedSchemaCondition(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)

	for name, tc := range map[string]struct {
		prior          []metav1.Condition
		expectedStatus metav1.ConditionStatus
		expectedReason string
	}{
		"migrated schema stays true while gated": {
			prior: []metav1.Condition{{
				Type:               operatorstatus.ConditionSchemaMigrated,
				Status:             metav1.ConditionTrue,
				Reason:             "SchemaUpToDate",
				LastTransitionTime: metav1.Now(),
			}},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: "SchemaUpToDate",
		},
		"unobserved schema reports unknown": {
			expectedStatus: metav1.ConditionUnknown,
			expectedReason: operatorstatus.ReasonTamossHibernating,
		},
	} {
		t.Run(name, func(t *testing.T) {
			tamoss := hibernationSpecTamossFixture()
			tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
				Phase: string(tamossv1alpha1.TamossLifecyclePhaseHibernating),
			}
			tamoss.Status.Conditions = tc.prior

			reconciler := &TamossReconciler{
				Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
					WithScheme(scheme).
					WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
					WithObjects(tamoss).
					Build(),
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(8),
			}

			if err := reconciler.updateLifecycleGatedStatus(ctx, tamoss); err != nil {
				t.Fatalf("expected gated status update without error, got %v", err)
			}

			updated := &tamossv1alpha1.Tamoss{}
			if err := reconciler.Client.Get(ctx, client.ObjectKeyFromObject(tamoss), updated); err != nil {
				t.Fatalf("expected to fetch tamoss, got %v", err)
			}
			condition := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionSchemaMigrated)
			if condition == nil {
				t.Fatalf("expected a SchemaMigrated condition")
			}
			if condition.Status != tc.expectedStatus || condition.Reason != tc.expectedReason {
				t.Fatalf("expected SchemaMigrated %s/%s, got %s/%s", tc.expectedStatus, tc.expectedReason, condition.Status, condition.Reason)
			}
		})
	}
}

func TestPatchTamossLifecycleStatusRetriesOnConflict(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernationSpecTamossFixture()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase: string(tamossv1alpha1.TamossLifecyclePhaseResuming),
	}

	conflicts := 0
	funcs := fakeApplyInterceptor()
	funcs.SubResourcePatch = func(ctx context.Context, cl client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
		if conflicts == 0 {
			conflicts++
			return apierrors.NewConflict(k8sschema.GroupResource{Group: "tamoss.livewyer.io", Resource: "tamosses"}, obj.GetName(), fmt.Errorf("the object has been modified"))
		}
		return cl.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
	}
	fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(funcs).
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
		WithObjects(tamoss).
		Build()

	if err := patchTamossLifecycleStatus(ctx, fakeClient, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
		lifecycle.Phase = string(tamossv1alpha1.TamossLifecyclePhaseRunning)
		lifecycle.Reason = operatorstatus.ReasonTamossReady
	}); err != nil {
		t.Fatalf("expected conflict to be retried, got %v", err)
	}
	if conflicts != 1 {
		t.Fatalf("expected exactly one injected conflict, got %d", conflicts)
	}
	updated := &tamossv1alpha1.Tamoss{}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(tamoss), updated); err != nil {
		t.Fatalf("expected to fetch tamoss, got %v", err)
	}
	if updated.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseRunning) {
		t.Fatalf("expected lifecycle Running after retried patch, got %#v", updated.Status.Lifecycle)
	}
}
