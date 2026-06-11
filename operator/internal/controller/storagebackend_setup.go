package controller

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func (r *StorageBackendReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&tamossv1alpha1.StorageBackend{},
		storageBackendCredentialsSecretIndex,
		storageBackendCredentialsSecretIndexValue,
	); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&tamossv1alpha1.StorageBackend{}, builder.WithPredicates(storageBackendPrimaryPredicate())).
		Owns(&batchv1.Job{}).
		Owns(&corev1.ConfigMap{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.storageBackendCredentialSecretRequests),
			builder.WithPredicates(credentialSecretPredicate()),
		).
		Complete(r)
}

func (r *StorageBackendReconciler) storageBackendCredentialSecretRequests(ctx context.Context, obj client.Object) []reconcile.Request {
	list := &tamossv1alpha1.StorageBackendList{}
	if err := r.Client.List(ctx, list,
		client.InNamespace(obj.GetNamespace()),
		client.MatchingFields{storageBackendCredentialsSecretIndex: obj.GetName()},
	); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: storageBackendRequest(list.Items[i]),
		})
	}
	return requests
}
