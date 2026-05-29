package controller

import (
	"context"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func TestDefaultStorageBackendReadyRequiresDatabaseRegistration(t *testing.T) {
	ctx := context.Background()
	tamoss := tamossFixture()
	tamoss.Spec.Backends.S3 = tamossv1alpha1.S3BackendSpec{
		ProvidedBy: tamossv1alpha1.S3BackendProvidedByRustFSOperator,
	}
	storageBackend := defaultStorageBackend(tamoss)
	operatorstatus.SetConditionBool(
		&storageBackend.Status.Conditions,
		operatorstatus.ConditionBucketReady,
		true,
		operatorstatus.ReasonBucketReady,
		"bucket is ready",
	)
	operatorstatus.SetConditionBool(
		&storageBackend.Status.Conditions,
		operatorstatus.ConditionDatabaseReady,
		false,
		operatorstatus.ReasonDatabaseRegistrationInProgress,
		"registration is pending",
	)
	operatorstatus.SetConditionBool(
		&storageBackend.Status.Conditions,
		operatorstatus.ConditionReady,
		false,
		operatorstatus.ReasonDatabaseRegistrationInProgress,
		"database registration is pending",
	)
	storageBackend.Status.Phase = operatorstatus.PhaseProgressing

	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(storageBackendTestScheme(t)).
			WithObjects(storageBackend).
			WithStatusSubresource(&tamossv1alpha1.StorageBackend{}).
			Build(),
	}

	result, err := reconciler.checkDefaultStorageBackendReady(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected default StorageBackend readiness check to succeed: %v", err)
	}
	if result.Ready {
		t.Fatalf("expected database registration to gate readiness")
	}
	if result.Reason != operatorstatus.ReasonDatabaseRegistrationInProgress {
		t.Fatalf("expected database registration reason, got %q", result.Reason)
	}
	if !strings.Contains(result.Message, "database registration") {
		t.Fatalf("expected database registration message, got %q", result.Message)
	}
}

func TestDefaultStorageBackendReadyAfterBucketAndDatabaseRegistration(t *testing.T) {
	ctx := context.Background()
	tamoss := tamossFixture()
	tamoss.Spec.Backends.S3 = tamossv1alpha1.S3BackendSpec{
		ProvidedBy: tamossv1alpha1.S3BackendProvidedByRustFSOperator,
	}
	storageBackend := defaultStorageBackend(tamoss)
	operatorstatus.SetConditionBool(
		&storageBackend.Status.Conditions,
		operatorstatus.ConditionBucketReady,
		true,
		operatorstatus.ReasonBucketReady,
		"bucket is ready",
	)
	operatorstatus.SetConditionBool(
		&storageBackend.Status.Conditions,
		operatorstatus.ConditionDatabaseReady,
		true,
		operatorstatus.ReasonDatabaseRegistered,
		"registration is complete",
	)
	operatorstatus.SetConditionBool(
		&storageBackend.Status.Conditions,
		operatorstatus.ConditionReady,
		true,
		operatorstatus.ReasonStorageBackendReady,
		"StorageBackend bucket and database registration are ready",
	)
	storageBackend.Status.Phase = operatorstatus.PhaseReady

	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(storageBackendTestScheme(t)).
			WithObjects(storageBackend).
			WithStatusSubresource(&tamossv1alpha1.StorageBackend{}).
			Build(),
	}

	result, err := reconciler.checkDefaultStorageBackendReady(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected default StorageBackend readiness check to succeed: %v", err)
	}
	if !result.Ready {
		t.Fatalf("expected bucket and database registration to be ready: %#v", result)
	}
	if result.Reason != operatorstatus.ReasonStorageBackendReady {
		t.Fatalf("expected ready reason, got %q", result.Reason)
	}
}
