package controller

import (
	"context"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *TamossReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ownTamossManagedResources(
		ctrl.NewControllerManagedBy(mgr).
			For(&tamossv1alpha1.Tamoss{}, builder.WithPredicates(tamossPrimaryPredicate())),
	).
		// Spec-materialised hibernation operations re-trigger the gated
		// instance as they progress; they are deliberately not part of the
		// managed-resource policies so the pruner never touches them.
		Owns(&tamossv1alpha1.TamossHibernate{})
	builder = builder.Watches(&tamossv1alpha1.StorageBackend{}, handler.EnqueueRequestsFromMapFunc(storageBackendTamossRequest))
	controller, err := builder.Build(r)
	if err != nil {
		return err
	}
	r.optionalWatches = newTamossOptionalWatchRegistrar(mgr, controller, r.Discovery)
	return r.RegisterOptionalWatches(context.Background())
}

func storageBackendTamossRequest(_ context.Context, obj client.Object) []reconcile.Request {
	backend, ok := obj.(*tamossv1alpha1.StorageBackend)
	if !ok || backend.Spec.TamossRef.Name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: backend.Namespace, Name: backend.Spec.TamossRef.Name}}}
}
