package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func TestTamossCompletionResultRequeuesForManagedAuthentik(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		Spec: tamossv1alpha1.TamossSpec{
			Auth: tamossv1alpha1.AuthSpec{ProvidedBy: tamossv1alpha1.AuthProvidedByAuthentikBlueprints},
		},
	}

	result := tamossCompletionResult(tamoss, 45*time.Second, 5*time.Minute)
	if result.RequeueAfter != 45*time.Second {
		t.Fatalf("expected Authentik completion to requeue after probe interval, got %#v", result)
	}
}

func TestTamossCompletionResultDoesNotRequeueForExternalAuth(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		Spec: tamossv1alpha1.TamossSpec{
			Auth: tamossv1alpha1.AuthSpec{ProvidedBy: tamossv1alpha1.AuthProvidedByExternal},
		},
	}

	result := tamossCompletionResult(tamoss, 45*time.Second, 5*time.Minute)
	if result.Requeue || result.RequeueAfter != 0 {
		t.Fatalf("did not expect external-auth completion to requeue, got %#v", result)
	}
}

func TestTamossCompletionResultRequeuesForProviderManagedBackends(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		Spec: tamossv1alpha1.TamossSpec{
			Backends: tamossv1alpha1.BackendsSpec{
				DB: tamossv1alpha1.DBBackendSpec{ProvidedBy: tamossv1alpha1.BackendProvidedByCNPG},
			},
		},
	}

	result := tamossCompletionResult(tamoss, 45*time.Second, 5*time.Minute)
	if result.RequeueAfter != 5*time.Minute {
		t.Fatalf("expected provider-managed backend completion to requeue after dependency probe interval, got %#v", result)
	}
}

func TestTamossCompletionResultUsesShortestManagedProbeInterval(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		Spec: tamossv1alpha1.TamossSpec{
			Auth: tamossv1alpha1.AuthSpec{ProvidedBy: tamossv1alpha1.AuthProvidedByAuthentikBlueprints},
			Backends: tamossv1alpha1.BackendsSpec{
				S3: tamossv1alpha1.S3BackendSpec{ProvidedBy: tamossv1alpha1.S3BackendProvidedByRustFSOperator},
			},
		},
	}

	result := tamossCompletionResult(tamoss, 45*time.Second, 5*time.Minute)
	if result.RequeueAfter != 45*time.Second {
		t.Fatalf("expected shortest managed probe interval, got %#v", result)
	}
}

func TestObservedSchemaStateResultRejectsUnsupportedVersion(t *testing.T) {
	state := &corev1.ConfigMap{
		Data: map[string]string{schemaStateAppliedVersionKey: "unknown"},
	}

	result, done := observedSchemaStateResult(state, true, []client.Object{state})
	if !done {
		t.Fatal("expected unsupported schema state to finish the stage")
	}
	if !result.Degraded || result.Reason != operatorstatus.ReasonUnsupportedSchemaVersion {
		t.Fatalf("expected unsupported schema version result, got %#v", result)
	}
}

func TestPrepareStorageBackendLifecycleIgnoresDisallowedNamespace(t *testing.T) {
	reconciler := &StorageBackendReconciler{
		WatchNamespaces: map[string]struct{}{"allowed": {}},
	}
	storageBackend := &tamossv1alpha1.StorageBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "store", Namespace: "ignored"},
	}

	control, err := reconciler.prepareStorageBackendLifecycle(context.Background(), storageBackend)
	if err != nil {
		t.Fatal(err)
	}
	if !control.Stop || control.Result.Requeue || control.Result.RequeueAfter != 0 {
		t.Fatalf("expected disallowed namespace to finish without requeue, got control=%#v", control)
	}
}
