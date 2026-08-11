package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func (r *StorageBackendReconciler) schemaStateReady(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) bool {
	state := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: tamossResourceName(tamoss, "schema-state"), Namespace: tamoss.Namespace}
	if err := r.Client.Get(ctx, key, state); err != nil {
		return false
	}
	return state.Data[schemaStateAppliedVersionKey] == schemabundle.SchemaVersion
}

func (r *StorageBackendReconciler) reconcileStorageBackendDatabase(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend, tamoss *tamossv1alpha1.Tamoss, spec tamossv1alpha1.StorageBackendSpec) (storageBackendReconcileResult, error) {
	state := &corev1.ConfigMap{}
	stateName := storageBackendResourceName(storageBackend, "db-state")
	hash := storageBackendRegistrationHash(spec)
	stateKey := types.NamespacedName{Name: stateName, Namespace: storageBackend.Namespace}
	if err := r.Client.Get(ctx, stateKey, state); err != nil && !apierrors.IsNotFound(err) {
		return storageBackendReconcileResult{}, err
	}
	if storageBackendDBRegistered(state, spec, hash) {
		return storageBackendReconcileResult{Ready: true, Reason: operatorstatus.ReasonDatabaseRegistered, Message: "TAMS storage backend row has been registered"}, nil
	}

	desiredJob := storageBackendRegistrationJob(storageBackend, tamoss, spec, hash)
	if err := controllerutil.SetControllerReference(storageBackend, desiredJob, r.Scheme); err != nil {
		return storageBackendReconcileResult{}, err
	}
	liveJob := &batchv1.Job{}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(desiredJob), liveJob); err != nil {
		if !apierrors.IsNotFound(err) {
			return storageBackendReconcileResult{}, err
		}
		if _, err := applyManagedObject(ctx, r.Client, desiredJob); err != nil {
			return storageBackendReconcileResult{}, err
		}
		return storageBackendReconcileResult{Ready: false, Reason: operatorstatus.ReasonDatabaseRegistrationInProgress, Message: "TAMS storage backend registration job was launched"}, nil
	}
	if liveJob.Annotations[storageBackendDesiredHashAnnotation] != hash && jobSucceeded(liveJob) {
		if err := r.Client.Delete(ctx, liveJob); err != nil && !apierrors.IsNotFound(err) {
			return storageBackendReconcileResult{}, err
		}
		return storageBackendReconcileResult{Ready: false, Reason: operatorstatus.ReasonDatabaseRegistrationInProgress, Message: "TAMS storage backend registration job is being refreshed"}, nil
	}
	if jobFailed(liveJob) {
		if err := r.Client.Delete(ctx, liveJob); err != nil && !apierrors.IsNotFound(err) {
			return storageBackendReconcileResult{}, err
		}
		return storageBackendReconcileResult{Ready: false, Reason: operatorstatus.ReasonDatabaseRegistrationRetrying, Message: fmt.Sprintf("TAMS storage backend registration job %s failed and is being retried", liveJob.Name)}, nil
	}
	if !jobSucceeded(liveJob) {
		return storageBackendReconcileResult{Ready: false, Reason: operatorstatus.ReasonDatabaseRegistrationInProgress, Message: fmt.Sprintf("TAMS storage backend registration job %s is still running", liveJob.Name)}, nil
	}

	desiredState := storageBackendDBStateConfigMap(storageBackend, spec, hash, liveJob)
	if err := controllerutil.SetControllerReference(storageBackend, desiredState, r.Scheme); err != nil {
		return storageBackendReconcileResult{}, err
	}
	if _, err := applyManagedObject(ctx, r.Client, desiredState); err != nil {
		return storageBackendReconcileResult{}, err
	}
	return storageBackendReconcileResult{Ready: true, Reason: operatorstatus.ReasonDatabaseRegistered, Message: "TAMS storage backend row has been registered"}, nil
}

func (r *StorageBackendReconciler) reconcileStorageBackendDatabaseDeletion(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend, tamoss *tamossv1alpha1.Tamoss, spec tamossv1alpha1.StorageBackendSpec) (storageBackendReconcileResult, error) {
	hash := storageBackendDeregistrationHash(spec)
	desiredJob := storageBackendDeregistrationJob(storageBackend, tamoss, spec, hash)
	return r.reconcileStorageBackendCleanupJob(ctx, desiredJob, hash, "DatabaseDeregistration", "TAMS storage backend deregistration")
}

func storageBackendRegistrationJob(storageBackend *tamossv1alpha1.StorageBackend, tamoss *tamossv1alpha1.Tamoss, spec tamossv1alpha1.StorageBackendSpec, hash string) *batchv1.Job {
	backoffLimit := int32(3)
	labels := storageBackendLabels(storageBackend, tamoss)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      storageBackendResourceName(storageBackend, "db-register"),
			Namespace: storageBackend.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				storageBackendDesiredHashAnnotation: hash,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:            "storage-backend-register",
						Image:           schemaMigrationPostgresClientImage(tamoss),
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"/bin/sh", "-ec", storageBackendRegistrationScript()},
						Env:             storageBackendRegistrationEnv(tamoss, spec),
					}},
				},
			},
		},
	}
}

func storageBackendDeregistrationJob(storageBackend *tamossv1alpha1.StorageBackend, tamoss *tamossv1alpha1.Tamoss, spec tamossv1alpha1.StorageBackendSpec, hash string) *batchv1.Job {
	backoffLimit := int32(3)
	labels := storageBackendLabels(storageBackend, tamoss)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      storageBackendResourceName(storageBackend, "db-deregister"),
			Namespace: storageBackend.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				storageBackendDesiredHashAnnotation: hash,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:            "storage-backend-deregister",
						Image:           schemaMigrationPostgresClientImage(tamoss),
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"/bin/sh", "-ec", storageBackendDeregistrationScript()},
						Env:             storageBackendDeregistrationEnv(tamoss, spec),
					}},
				},
			},
		},
	}
}

func storageBackendRegistrationEnv(tamoss *tamossv1alpha1.Tamoss, spec tamossv1alpha1.StorageBackendSpec) []corev1.EnvVar {
	db := tamoss.DBConnection()
	tagsJSON := storageBackendTagsJSON(spec.Tags)
	return []corev1.EnvVar{
		{Name: "POSTGRES_HOST", Value: db.Host},
		{Name: "POSTGRES_PORT", Value: db.Port},
		{Name: "POSTGRES_DB", Value: db.Database},
		{
			Name: "POSTGRES_USER",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: db.Auth.ExistingSecret},
				Key:                  db.Auth.SecretKeys.Username,
			}},
		},
		{
			Name: "POSTGRES_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: db.Auth.ExistingSecret},
				Key:                  db.Auth.SecretKeys.Password,
			}},
		},
		{Name: "TAMOSS_STORAGE_BACKEND_ID", Value: spec.ID},
		{Name: "TAMOSS_STORAGE_BACKEND_LABEL", Value: spec.Label},
		{Name: "TAMOSS_STORAGE_PROVIDER", Value: string(spec.Provider)},
		{Name: "TAMOSS_STORAGE_REGION", Value: spec.Region},
		{Name: "TAMOSS_STORAGE_PRODUCT", Value: spec.StoreProduct},
		{Name: "TAMOSS_STORAGE_TYPE", Value: spec.StoreType},
		{Name: "TAMOSS_STORAGE_BACKEND_TAGS", Value: tagsJSON},
		{Name: "TAMOSS_STORAGE_DEFAULT", Value: fmt.Sprintf("%t", spec.DefaultStorage)},
		{Name: "TAMOSS_STORAGE_BUCKET", Value: spec.BucketName},
		{Name: "TAMOSS_STORAGE_ENDPOINT", Value: spec.Endpoint.Default.URL},
		{Name: "TAMOSS_STORAGE_PUBLIC_ENDPOINT", Value: spec.Endpoint.Public.URL},
	}
}

func storageBackendDeregistrationEnv(tamoss *tamossv1alpha1.Tamoss, spec tamossv1alpha1.StorageBackendSpec) []corev1.EnvVar {
	db := tamoss.DBConnection()
	return []corev1.EnvVar{
		{Name: "POSTGRES_HOST", Value: db.Host},
		{Name: "POSTGRES_PORT", Value: db.Port},
		{Name: "POSTGRES_DB", Value: db.Database},
		{
			Name: "POSTGRES_USER",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: db.Auth.ExistingSecret},
				Key:                  db.Auth.SecretKeys.Username,
			}},
		},
		{
			Name: "POSTGRES_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: db.Auth.ExistingSecret},
				Key:                  db.Auth.SecretKeys.Password,
			}},
		},
		{Name: "TAMOSS_STORAGE_BACKEND_ID", Value: spec.ID},
	}
}

func storageBackendDBStateConfigMap(storageBackend *tamossv1alpha1.StorageBackend, spec tamossv1alpha1.StorageBackendSpec, hash string, job *batchv1.Job) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      storageBackendResourceName(storageBackend, "db-state"),
			Namespace: storageBackend.Namespace,
			Labels:    storageBackendLabels(storageBackend, nil),
		},
		Data: map[string]string{
			storageBackendStateReadyKey:      "true",
			storageBackendStateBackendIDKey:  spec.ID,
			storageBackendStateBucketKey:     spec.BucketName,
			storageBackendStateEndpointKey:   spec.Endpoint.Default.URL,
			storageBackendStateDesiredHash:   hash,
			storageBackendStateJobUIDKey:     string(job.UID),
			storageBackendStateUpdatedAtKey:  time.Now().UTC().Format(time.RFC3339),
			storageBackendStateGenerationKey: fmt.Sprintf("%d", storageBackend.Generation),
		},
	}
}

func storageBackendDBRegistered(state *corev1.ConfigMap, spec tamossv1alpha1.StorageBackendSpec, hash string) bool {
	return state != nil &&
		state.Data[storageBackendStateReadyKey] == "true" &&
		state.Data[storageBackendStateBackendIDKey] == spec.ID &&
		state.Data[storageBackendStateBucketKey] == spec.BucketName &&
		state.Data[storageBackendStateEndpointKey] == spec.Endpoint.Default.URL &&
		state.Data[storageBackendStateDesiredHash] == hash
}

func storageBackendRegistrationHash(spec tamossv1alpha1.StorageBackendSpec) string {
	tagsJSON := storageBackendTagsJSON(spec.Tags)
	return storageBackendHash(
		spec.ID,
		spec.Label,
		string(spec.Provider),
		spec.Region,
		spec.StoreProduct,
		spec.StoreType,
		tagsJSON,
		fmt.Sprintf("%t", spec.DefaultStorage),
		spec.BucketName,
		spec.Endpoint.Default.URL,
		spec.Endpoint.Public.URL,
	)
}

func storageBackendTagsJSON(tags map[string]apiextensionsv1.JSON) string {
	if tags == nil {
		tags = map[string]apiextensionsv1.JSON{}
	}
	encoded, err := json.Marshal(tags)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func storageBackendDeregistrationHash(spec tamossv1alpha1.StorageBackendSpec) string {
	return storageBackendHash(
		"delete",
		spec.ID,
		spec.BucketName,
	)
}
