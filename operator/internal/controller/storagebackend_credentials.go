package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/backend/rustfs"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/workload_renderer"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

type runtimeStorageBackendCredentialsPayload struct {
	APIVersion  string                             `json:"apiVersion"`
	Kind        string                             `json:"kind"`
	Credentials []runtimeStorageBackendCredentials `json:"credentials"`
}

type runtimeStorageBackendCredentials struct {
	StorageBackendID string `json:"storageBackendId"`
	AccessKey        string `json:"accessKey"`
	SecretKey        string `json:"secretKey"`
}

func storageBackendRuntimeCredentialsSecret(ctx context.Context, c client.Client, tamoss *tamossv1alpha1.Tamoss) (*corev1.Secret, error) {
	credentials, err := storageBackendRuntimeCredentials(ctx, c, tamoss)
	if err != nil {
		return nil, err
	}
	payload := runtimeStorageBackendCredentialsPayload{
		APIVersion:  "tamoss.livewyer.io/v1",
		Kind:        "StorageBackendCredentials",
		Credentials: credentials,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workload_renderer.StorageBackendCredentialsSecretName(tamoss),
			Namespace: tamoss.Namespace,
			Labels:    storageBackendRuntimeCredentialsLabels(tamoss),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			workload_renderer.StorageBackendCredentialsFileName: data,
		},
	}, nil
}

func storageBackendRuntimeCredentials(ctx context.Context, c client.Client, tamoss *tamossv1alpha1.Tamoss) ([]runtimeStorageBackendCredentials, error) {
	list := &tamossv1alpha1.StorageBackendList{}
	if err := listStorageBackendsForTamoss(ctx, c, tamoss.Namespace, tamoss.Name, list); err != nil {
		return nil, err
	}
	credentials := make([]runtimeStorageBackendCredentials, 0, len(list.Items))
	for i := range list.Items {
		storageBackend := list.Items[i]
		if !storageBackend.DeletionTimestamp.IsZero() {
			continue
		}
		spec := storageBackend.Spec
		spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
		if spec.IsHibernateDestination() {
			continue
		}
		if spec.TamossRef.Name != tamoss.Name || spec.Credentials.ExistingSecret == "" {
			continue
		}
		secret := &corev1.Secret{}
		key := client.ObjectKey{Name: spec.Credentials.ExistingSecret, Namespace: storageBackend.Namespace}
		if err := c.Get(ctx, key, secret); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		accessKey := secret.Data[storageBackendAccessKey(spec)]
		secretKey := secret.Data[storageBackendSecretKey(spec)]
		if len(accessKey) == 0 || len(secretKey) == 0 {
			continue
		}
		credentials = append(credentials, runtimeStorageBackendCredentials{
			StorageBackendID: spec.ID,
			AccessKey:        string(accessKey),
			SecretKey:        string(secretKey),
		})
	}
	sort.Slice(credentials, func(i, j int) bool {
		return credentials[i].StorageBackendID < credentials[j].StorageBackendID
	})
	return credentials, nil
}

func storageBackendRuntimeCredentialsLabels(tamoss *tamossv1alpha1.Tamoss) map[string]string {
	appName := tamossAppName
	if tamoss.Spec.NameOverride != "" {
		appName = tamoss.Spec.NameOverride
	}
	return map[string]string{
		"app.kubernetes.io/name":       appName,
		appInstanceLabel:               tamoss.Name,
		"app.kubernetes.io/component":  "storage-backend-credentials",
		"app.kubernetes.io/managed-by": "tamoss-operator",
	}
}

func (r *StorageBackendReconciler) reconcileRuntimeCredentialsSecret(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	secret, err := storageBackendRuntimeCredentialsSecret(ctx, r.Client, tamoss)
	if err != nil {
		return err
	}
	if err := controllerutil.SetControllerReference(tamoss, secret, r.Scheme); err != nil {
		return err
	}
	_, err = applyManagedObject(ctx, r.Client, secret)
	return err
}

func (r *StorageBackendReconciler) storageBackendCredentials(ctx context.Context, namespace string, spec tamossv1alpha1.StorageBackendSpec) (bool, string, string, error) {
	secret := &corev1.Secret{}
	secretName := spec.Credentials.ExistingSecret
	if err := r.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return false, operatorstatus.ReasonMissingSecret, fmt.Sprintf("Required credentials Secret %s was not found", secretName), nil
		}
		return false, "", "", err
	}
	for _, key := range []string{storageBackendAccessKey(spec), storageBackendSecretKey(spec)} {
		if len(secret.Data[key]) == 0 {
			return false, operatorstatus.ReasonMissingSecret, fmt.Sprintf("Required key %s is missing from Secret %s", key, secretName), nil
		}
	}
	return true, operatorstatus.ReasonSecretReady, "StorageBackend credentials Secret is ready", nil
}

func (r *StorageBackendReconciler) storageBackendBucketCredentials(ctx context.Context, namespace string, spec tamossv1alpha1.StorageBackendSpec) (rustfs.BucketCredentials, error) {
	secret := &corev1.Secret{}
	secretName := spec.Credentials.ExistingSecret
	if err := r.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret); err != nil {
		return rustfs.BucketCredentials{}, err
	}
	return rustfs.BucketCredentials{
		AccessKey: string(secret.Data[storageBackendAccessKey(spec)]),
		SecretKey: string(secret.Data[storageBackendSecretKey(spec)]),
	}, nil
}

func (r *StorageBackendReconciler) bucketClient() rustfs.BucketClient {
	if r.BucketClient != nil {
		return r.BucketClient
	}
	return rustfs.S3BucketClient{}
}

func missingStorageBackendFields(spec tamossv1alpha1.StorageBackendSpec) []string {
	missing := []string{}
	checks := map[string]string{
		".spec.tamossRef.name":                   spec.TamossRef.Name,
		".spec.bucketName":                       spec.BucketName,
		".spec.endpoint.default.url":             spec.Endpoint.Default.URL,
		".spec.credentials.existingSecret":       spec.Credentials.ExistingSecret,
		".spec.credentials.secretKeys.accessKey": storageBackendAccessKey(spec),
		".spec.credentials.secretKeys.secretKey": storageBackendSecretKey(spec),
	}
	for field, value := range checks {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, field)
		}
	}
	sort.Strings(missing)
	return missing
}

func storageBackendAccessKey(spec tamossv1alpha1.StorageBackendSpec) string {
	if spec.Credentials.SecretKeys.AccessKey != "" {
		return spec.Credentials.SecretKeys.AccessKey
	}
	return rustfs.AccessKeyEnvKey
}

func storageBackendSecretKey(spec tamossv1alpha1.StorageBackendSpec) string {
	if spec.Credentials.SecretKeys.SecretKey != "" {
		return spec.Credentials.SecretKeys.SecretKey
	}
	return rustfs.SecretKeyEnvKey
}
