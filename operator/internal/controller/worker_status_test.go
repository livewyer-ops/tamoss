package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func TestUpdateStatusSurfacesUnavailableWorker(t *testing.T) {
	ctx := context.Background()
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media", Generation: 3},
		Spec: tamossv1alpha1.TamossSpec{
			API:    tamossv1alpha1.APIComponentSpec{WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{ReplicaCount: ptr.To[int32](1)}},
			UI:     tamossv1alpha1.UIComponentSpec{WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{ReplicaCount: ptr.To[int32](1)}},
			Worker: tamossv1alpha1.WorkerComponentSpec{Enabled: ptr.To(true), WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{ReplicaCount: ptr.To[int32](2)}},
		},
	}
	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(workerStatusScheme(t)).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
			WithObjects(
				tamoss,
				availableDeployment(tamoss, "api", 1),
				availableDeployment(tamoss, "ui", 1),
				availableDeployment(tamoss, "worker", 1),
			).
			Build(),
	}

	err := reconciler.updateStatus(ctx, tamoss, SchemaResult{Ready: true, Version: "2026.05.24"}, identityReconcileResult{Ready: true})
	if err != nil {
		t.Fatalf("update status: %v", err)
	}

	refreshed := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: "example", Namespace: "media"}, refreshed); err != nil {
		t.Fatalf("get refreshed Tamoss: %v", err)
	}
	if refreshed.Status.Replicas.Worker.Desired != 2 || refreshed.Status.Replicas.Worker.Available != 1 {
		t.Fatalf("expected worker replica gap, got %#v", refreshed.Status.Replicas.Worker)
	}
	ready := findCondition(t, refreshed.Status.Conditions, operatorstatus.ConditionReady)
	if ready.Status != metav1.ConditionFalse {
		t.Fatalf("expected Ready=False while worker is unavailable, got %#v", ready)
	}
}

func TestUpdateStatusSurfacesUnavailableConsole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example-console", Namespace: "media", Generation: 3},
		Spec: tamossv1alpha1.TamossSpec{
			API:     tamossv1alpha1.APIComponentSpec{WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{ReplicaCount: ptr.To[int32](1)}},
			UI:      tamossv1alpha1.UIComponentSpec{WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{ReplicaCount: ptr.To[int32](1)}},
			Console: tamossv1alpha1.ConsoleComponentSpec{Enabled: ptr.To(true), WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{ReplicaCount: ptr.To[int32](1)}},
		},
	}
	reconciler := &TamossReconciler{Client: fake.NewClientBuilder().
		WithScheme(workerStatusScheme(t)).
		WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
		WithObjects(
			tamoss,
			availableDeployment(tamoss, "api", 1),
			availableDeployment(tamoss, "ui", 1),
			availableDeployment(tamoss, "console", 0),
		).
		Build()}

	if err := reconciler.updateStatus(ctx, tamoss, SchemaResult{Ready: true, Version: "2026.05.24"}, identityReconcileResult{Ready: true}); err != nil {
		t.Fatalf("update status: %v", err)
	}
	refreshed := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, refreshed); err != nil {
		t.Fatalf("get refreshed Tamoss: %v", err)
	}
	if refreshed.Status.Replicas.Console.Desired != 1 || refreshed.Status.Replicas.Console.Available != 0 {
		t.Fatalf("expected Console replica gap, got %#v", refreshed.Status.Replicas.Console)
	}
	ready := findCondition(t, refreshed.Status.Conditions, operatorstatus.ConditionReady)
	if ready.Status != metav1.ConditionFalse {
		t.Fatalf("expected Ready=False while Console is unavailable, got %#v", ready)
	}
}

func TestUpdateStatusSurfacesUnavailableBrowserAuthentication(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example-browser-auth", Namespace: "media", Generation: 3},
		Spec: tamossv1alpha1.TamossSpec{
			API: tamossv1alpha1.APIComponentSpec{WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{ReplicaCount: ptr.To[int32](1)}},
			UI:  tamossv1alpha1.UIComponentSpec{WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{ReplicaCount: ptr.To[int32](1)}},
			Auth: tamossv1alpha1.AuthSpec{
				ProvidedBy: tamossv1alpha1.AuthProvidedByExternal,
				Required:   true,
			},
		},
	}
	reconciler := &TamossReconciler{Client: fake.NewClientBuilder().
		WithScheme(workerStatusScheme(t)).
		WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
		WithObjects(
			tamoss,
			availableDeployment(tamoss, "api", 1),
			availableDeployment(tamoss, "ui", 1),
		).
		Build()}

	if err := reconciler.updateStatus(ctx, tamoss, SchemaResult{Ready: true, Version: "2026.05.24"}, identityReconcileResult{Ready: true}); err != nil {
		t.Fatalf("update status: %v", err)
	}
	refreshed := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, refreshed); err != nil {
		t.Fatalf("get refreshed Tamoss: %v", err)
	}
	browserAuth := findCondition(t, refreshed.Status.Conditions, operatorstatus.ConditionBrowserAuthReady)
	if browserAuth.Status != metav1.ConditionFalse || browserAuth.Reason != operatorstatus.ReasonBrowserAuthenticationUnavailable {
		t.Fatalf("unsupported browser authentication was not surfaced: %#v", browserAuth)
	}

	// The browser surfaces refuse API requests without a trusted identity path,
	// so the state is reported on its own condition. Folding it into Degraded
	// flipped healthy instances on upgrade and blocked unrelated features.
	degraded := findCondition(t, refreshed.Status.Conditions, operatorstatus.ConditionDegraded)
	if degraded.Status != metav1.ConditionFalse {
		t.Fatalf("an otherwise healthy instance must not be Degraded for browser authentication alone: %#v", degraded)
	}
	if refreshed.Status.Phase == operatorstatus.PhaseDegraded {
		t.Fatalf("phase = %s, want a non-degraded phase", refreshed.Status.Phase)
	}
	ready := findCondition(t, refreshed.Status.Conditions, operatorstatus.ConditionReady)
	if ready.Reason == operatorstatus.ReasonBrowserAuthenticationUnavailable {
		t.Fatalf("Ready must not be attributed to browser authentication: %#v", ready)
	}
}

func TestUpdateStatusReportsConfiguredBrowserAuthentication(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example-no-browser", Namespace: "media", Generation: 1},
		Spec: tamossv1alpha1.TamossSpec{
			API: tamossv1alpha1.APIComponentSpec{WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{ReplicaCount: ptr.To[int32](1)}},
			UI:  tamossv1alpha1.UIComponentSpec{Enabled: ptr.To(false)},
		},
	}
	reconciler := &TamossReconciler{Client: fake.NewClientBuilder().
		WithScheme(workerStatusScheme(t)).
		WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
		WithObjects(tamoss, availableDeployment(tamoss, "api", 1)).
		Build()}

	if err := reconciler.updateStatus(ctx, tamoss, SchemaResult{Ready: true, Version: "2026.05.24"}, identityReconcileResult{Ready: true}); err != nil {
		t.Fatalf("update status: %v", err)
	}
	refreshed := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, refreshed); err != nil {
		t.Fatalf("get refreshed Tamoss: %v", err)
	}
	browserAuth := findCondition(t, refreshed.Status.Conditions, operatorstatus.ConditionBrowserAuthReady)
	if browserAuth.Status != metav1.ConditionTrue || browserAuth.Reason != operatorstatus.ReasonBrowserAuthenticationNotRequired {
		t.Fatalf("an instance with no browser surface must report browser authentication as satisfied: %#v", browserAuth)
	}
}

func workerStatusScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add apps scheme: %v", err)
	}
	if err := tamossv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add tamoss scheme: %v", err)
	}
	return scheme
}

func availableDeployment(tamoss client.Object, component string, available int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: tamoss.GetName() + "-" + component, Namespace: tamoss.GetNamespace()},
		Status:     appsv1.DeploymentStatus{AvailableReplicas: available},
	}
}
