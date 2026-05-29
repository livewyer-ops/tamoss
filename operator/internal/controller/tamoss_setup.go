package controller

import (
	"context"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
)

func (r *TamossReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ownTamossManagedResources(
		ctrl.NewControllerManagedBy(mgr).
			For(&tamossv1alpha1.Tamoss{}, builder.WithPredicates(tamossPrimaryPredicate())),
	)
	controller, err := builder.Build(r)
	if err != nil {
		return err
	}
	r.optionalWatches = newTamossOptionalWatchRegistrar(mgr, controller, r.Discovery)
	return r.RegisterOptionalWatches(context.Background())
}
