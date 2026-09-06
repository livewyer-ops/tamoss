package controller

import (
	"context"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
)

func (r *StorageBackendReconciler) finalizeStorageBackend(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(storageBackend, storageBackendFinalizer) {
		return ctrl.Result{}, nil
	}

	spec := storageBackend.Spec
	spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
	shouldDeleteBucket, err := r.storageBackendBucketDeletionRequired(ctx, storageBackend, spec)
	if err != nil {
		return ctrl.Result{}, err
	}
	tamoss, found, err := r.storageBackendTamoss(ctx, storageBackend, spec)
	if err != nil {
		return ctrl.Result{}, err
	}
	if found && tamoss.DeletionTimestamp.IsZero() && !spec.IsHibernateDestination() && !r.schemaStateReady(ctx, tamoss) {
		return ctrl.Result{RequeueAfter: defaultProviderReadinessProbeInterval}, nil
	}
	if shouldDeleteBucket {
		result, err := r.reconcileStorageBackendBucketDeletion(ctx, storageBackend, tamoss, spec)
		if err != nil || !result.Ready {
			if err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: defaultFinalizerPollInterval}, nil
		}
	}
	if found && !spec.IsHibernateDestination() && r.schemaStateReady(ctx, tamoss) {
		result, err := r.reconcileStorageBackendDatabaseDeletion(ctx, storageBackend, tamoss, spec)
		if err != nil || !result.Ready {
			if err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: defaultFinalizerPollInterval}, nil
		}
	}

	if err := r.deleteStorageBackendCleanupObjects(ctx, storageBackend); err != nil {
		return ctrl.Result{}, err
	}
	if found && !spec.IsHibernateDestination() && tamoss.DeletionTimestamp.IsZero() {
		if err := r.reconcileRuntimeCredentialsSecret(ctx, tamoss); err != nil {
			return ctrl.Result{}, err
		}
	}
	original := storageBackend.DeepCopy()
	controllerutil.RemoveFinalizer(storageBackend, storageBackendFinalizer)
	return ctrl.Result{}, r.Client.Patch(ctx, storageBackend, client.MergeFrom(original))
}

func (r *StorageBackendReconciler) storageBackendBucketDeletionRequired(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend, spec tamossv1alpha1.StorageBackendSpec) (bool, error) {
	if spec.Provider != tamossv1alpha1.StorageBackendProviderRustFS || !storageBackendBucketDeletionPossible(spec) {
		return false, nil
	}
	state := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: storageBackendResourceName(storageBackend, "bucket-state"), Namespace: storageBackend.Namespace}
	if err := r.Client.Get(ctx, key, state); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return storageBackendManagedBucketReady(state, spec), nil
}

func (r *StorageBackendReconciler) storageBackendTamoss(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend, spec tamossv1alpha1.StorageBackendSpec) (*tamossv1alpha1.Tamoss, bool, error) {
	if strings.TrimSpace(spec.TamossRef.Name) == "" {
		return nil, false, nil
	}
	tamoss := &tamossv1alpha1.Tamoss{}
	key := types.NamespacedName{Name: spec.TamossRef.Name, Namespace: storageBackend.Namespace}
	if err := r.Client.Get(ctx, key, tamoss); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	resolved := tamoss.DeepCopy()
	defaults.Apply(resolved)
	return resolved, true, nil
}

func (r *StorageBackendReconciler) reconcileStorageBackendCleanupJob(ctx context.Context, desiredJob *batchv1.Job, hash, reasonPrefix, messagePrefix string) (storageBackendReconcileResult, error) {
	liveJob := &batchv1.Job{}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(desiredJob), liveJob); err != nil {
		if !apierrors.IsNotFound(err) {
			return storageBackendReconcileResult{}, err
		}
		if _, err := applyManagedObject(ctx, r.Client, desiredJob); err != nil {
			return storageBackendReconcileResult{}, err
		}
		return storageBackendReconcileResult{Ready: false, Reason: reasonPrefix + "InProgress", Message: messagePrefix + " job was launched"}, nil
	}
	if liveJob.Annotations[storageBackendDesiredHashAnnotation] != hash && jobSucceeded(liveJob) {
		if err := r.Client.Delete(ctx, liveJob); err != nil && !apierrors.IsNotFound(err) {
			return storageBackendReconcileResult{}, err
		}
		return storageBackendReconcileResult{Ready: false, Reason: reasonPrefix + "InProgress", Message: messagePrefix + " job is being refreshed"}, nil
	}
	if jobFailed(liveJob) {
		if err := r.Client.Delete(ctx, liveJob); err != nil && !apierrors.IsNotFound(err) {
			return storageBackendReconcileResult{}, err
		}
		return storageBackendReconcileResult{Ready: false, Reason: reasonPrefix + "Retrying", Message: messagePrefix + " job " + liveJob.Name + " failed and is being retried"}, nil
	}
	if !jobSucceeded(liveJob) {
		return storageBackendReconcileResult{Ready: false, Reason: reasonPrefix + "InProgress", Message: messagePrefix + " job " + liveJob.Name + " is still running"}, nil
	}
	return storageBackendReconcileResult{Ready: true, Reason: reasonPrefix + "Complete", Message: messagePrefix + " job completed"}, nil
}

func (r *StorageBackendReconciler) deleteStorageBackendCleanupObjects(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend) error {
	propagation := metav1.DeletePropagationForeground
	selector := client.MatchingLabels{
		"tamoss.livewyer.io/storage-backend": storageBackend.Name,
	}
	jobs := &batchv1.JobList{}
	if err := r.Client.List(ctx, jobs, client.InNamespace(storageBackend.Namespace), selector); err != nil {
		return err
	}
	for i := range jobs.Items {
		job := &jobs.Items[i]
		if err := r.Client.Delete(ctx, job, client.PropagationPolicy(propagation)); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	for _, suffix := range []string{"bucket-state", "db-state"} {
		configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: storageBackendResourceName(storageBackend, suffix), Namespace: storageBackend.Namespace}}
		if err := r.Client.Delete(ctx, configMap); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	pods := &corev1.PodList{}
	if err := r.Client.List(ctx, pods, client.InNamespace(storageBackend.Namespace), selector); err != nil {
		return err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if err := r.Client.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
