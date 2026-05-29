package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/backend/rustfs"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func (r *StorageBackendReconciler) reconcileStorageBackendBucket(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend, tamoss *tamossv1alpha1.Tamoss, spec tamossv1alpha1.StorageBackendSpec) (storageBackendReconcileResult, error) {
	if spec.IsExternalObjectStore() {
		return r.reconcileExternalStorageBackendReference(ctx, storageBackend, spec)
	}
	target := rustfsBucketTarget(tamoss, spec)
	hash := storageBackendBucketHash(target)
	state := &corev1.ConfigMap{}
	stateKey := types.NamespacedName{Name: storageBackendResourceName(storageBackend, "bucket-state"), Namespace: storageBackend.Namespace}
	if err := r.Client.Get(ctx, stateKey, state); err != nil && !apierrors.IsNotFound(err) {
		return storageBackendReconcileResult{}, err
	}
	if storageBackendBucketReady(state, spec, hash) {
		return storageBackendReconcileResult{Ready: true, Reason: operatorstatus.ReasonBucketReady, Message: "RustFS bucket has been created"}, nil
	}

	credentials, err := r.storageBackendBucketCredentials(ctx, storageBackend.Namespace, spec)
	if err != nil {
		return storageBackendReconcileResult{}, err
	}
	if err := r.bucketClient().Ensure(ctx, target, credentials); err != nil {
		return storageBackendReconcileResult{
			Ready:   false,
			Reason:  operatorstatus.ReasonBucketCreationFailed,
			Message: fmt.Sprintf("RustFS bucket %s is not ready: %v", spec.BucketName, err),
		}, nil
	}

	desiredState := storageBackendBucketStateConfigMap(storageBackend, spec, hash)
	if err := controllerutil.SetControllerReference(storageBackend, desiredState, r.Scheme); err != nil {
		return storageBackendReconcileResult{}, err
	}
	if _, err := applyCanonicalObject(ctx, r.Client, desiredState); err != nil {
		return storageBackendReconcileResult{}, err
	}
	return storageBackendReconcileResult{Ready: true, Reason: operatorstatus.ReasonBucketReady, Message: "RustFS bucket has been created"}, nil
}

func (r *StorageBackendReconciler) reconcileExternalStorageBackendReference(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend, spec tamossv1alpha1.StorageBackendSpec) (storageBackendReconcileResult, error) {
	hash := storageBackendExternalBucketHash(spec)
	state := &corev1.ConfigMap{}
	stateName := storageBackendResourceName(storageBackend, "bucket-state")
	stateKey := types.NamespacedName{Name: stateName, Namespace: storageBackend.Namespace}
	if err := r.Client.Get(ctx, stateKey, state); err != nil && !apierrors.IsNotFound(err) {
		return storageBackendReconcileResult{}, err
	}
	if storageBackendBucketReady(state, spec, hash) {
		return storageBackendReconcileResult{Ready: true, Reason: operatorstatus.ReasonBucketReady, Message: "External object-store metadata has been accepted"}, nil
	}

	desiredState := storageBackendBucketStateConfigMap(storageBackend, spec, hash)
	if err := controllerutil.SetControllerReference(storageBackend, desiredState, r.Scheme); err != nil {
		return storageBackendReconcileResult{}, err
	}
	if _, err := applyCanonicalObject(ctx, r.Client, desiredState); err != nil {
		return storageBackendReconcileResult{}, err
	}
	return storageBackendReconcileResult{Ready: true, Reason: operatorstatus.ReasonBucketReady, Message: "External object-store metadata has been accepted"}, nil
}

func (r *StorageBackendReconciler) reconcileStorageBackendBucketDeletion(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend, tamoss *tamossv1alpha1.Tamoss, spec tamossv1alpha1.StorageBackendSpec) (storageBackendReconcileResult, error) {
	target := storageBackendBucketDeleteTarget(spec)
	credentials, err := r.storageBackendBucketCredentials(ctx, storageBackend.Namespace, spec)
	if err != nil {
		return storageBackendReconcileResult{}, err
	}
	if err := r.bucketClient().Delete(ctx, target, credentials); err != nil {
		return storageBackendReconcileResult{Ready: false, Reason: operatorstatus.ReasonBucketDeletionRetrying, Message: fmt.Sprintf("RustFS bucket deletion failed and will be retried: %v", err)}, nil
	}
	return storageBackendReconcileResult{Ready: true, Reason: operatorstatus.ReasonBucketDeletionComplete, Message: "RustFS bucket deletion completed"}, nil
}

func rustfsBucketTarget(tamoss *tamossv1alpha1.Tamoss, spec tamossv1alpha1.StorageBackendSpec) rustfs.BucketTarget {
	return rustfs.BucketTarget{
		EndpointURL: spec.Endpoint.Default.URL,
		BucketName:  spec.BucketName,
		Region:      spec.Region,
		CORSOrigin:  storageBackendCORSOrigin(tamoss),
	}
}

func storageBackendBucketDeleteTarget(spec tamossv1alpha1.StorageBackendSpec) rustfs.BucketTarget {
	return rustfs.BucketTarget{
		EndpointURL: spec.Endpoint.Default.URL,
		BucketName:  spec.BucketName,
		Region:      spec.Region,
	}
}

func storageBackendBucketStateConfigMap(storageBackend *tamossv1alpha1.StorageBackend, spec tamossv1alpha1.StorageBackendSpec, hash string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      storageBackendResourceName(storageBackend, "bucket-state"),
			Namespace: storageBackend.Namespace,
			Labels:    storageBackendLabels(storageBackend, nil),
		},
		Data: map[string]string{
			storageBackendStateReadyKey:      "true",
			storageBackendStateBucketKey:     spec.BucketName,
			storageBackendStateEndpointKey:   spec.Endpoint.Default.URL,
			storageBackendStateDesiredHash:   hash,
			storageBackendStateUpdatedAtKey:  time.Now().UTC().Format(time.RFC3339),
			storageBackendStateGenerationKey: fmt.Sprintf("%d", storageBackend.Generation),
		},
	}
}

func storageBackendBucketReady(state *corev1.ConfigMap, spec tamossv1alpha1.StorageBackendSpec, hash string) bool {
	return state != nil &&
		state.Data[storageBackendStateReadyKey] == "true" &&
		state.Data[storageBackendStateBucketKey] == spec.BucketName &&
		state.Data[storageBackendStateEndpointKey] == spec.Endpoint.Default.URL &&
		state.Data[storageBackendStateDesiredHash] == hash
}

func storageBackendManagedBucketReady(state *corev1.ConfigMap, spec tamossv1alpha1.StorageBackendSpec) bool {
	return state != nil &&
		state.Data[storageBackendStateReadyKey] == "true" &&
		state.Data[storageBackendStateBucketKey] == spec.BucketName &&
		state.Data[storageBackendStateEndpointKey] == spec.Endpoint.Default.URL
}

func storageBackendBucketHash(target rustfs.BucketTarget) string {
	return storageBackendHash(
		target.EndpointURL,
		target.BucketName,
		target.Region,
		target.CORSOrigin,
	)
}

func storageBackendExternalBucketHash(spec tamossv1alpha1.StorageBackendSpec) string {
	return storageBackendHash(
		string(spec.Provider),
		spec.Region,
		spec.BucketName,
		spec.Endpoint.Default.URL,
		spec.Endpoint.Public.URL,
		spec.Credentials.ExistingSecret,
		storageBackendAccessKey(spec),
		storageBackendSecretKey(spec),
	)
}

func storageBackendCORSOrigin(tamoss *tamossv1alpha1.Tamoss) string {
	host := tamoss.Spec.Ingress.UI.Web.Host
	if host == "" {
		return "*"
	}
	scheme := "http"
	if len(tamoss.Spec.Ingress.TLS) > 0 {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func storageBackendBucketDeletionPossible(spec tamossv1alpha1.StorageBackendSpec) bool {
	return strings.TrimSpace(spec.BucketName) != "" &&
		strings.TrimSpace(spec.Endpoint.Default.URL) != "" &&
		strings.TrimSpace(spec.Credentials.ExistingSecret) != ""
}
