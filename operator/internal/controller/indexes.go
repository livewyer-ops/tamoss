package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func listTamossOwnedObjects(ctx context.Context, c client.Client, tamoss *tamossv1alpha1.Tamoss, list client.ObjectList) error {
	return c.List(ctx, list, client.InNamespace(tamoss.Namespace), tamossManagedLabelSelector(tamoss))
}

func listStorageBackendsForTamoss(ctx context.Context, c client.Client, namespace, name string, list *tamossv1alpha1.StorageBackendList) error {
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return err
	}
	filtered := list.Items[:0]
	for _, storageBackend := range list.Items {
		if storageBackend.Spec.TamossRef.Name == name {
			filtered = append(filtered, storageBackend)
		}
	}
	list.Items = filtered
	return nil
}

func listStorageBackendsByCredentialSecret(ctx context.Context, c client.Client, namespace, secretName string, list *tamossv1alpha1.StorageBackendList) error {
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return err
	}
	filtered := list.Items[:0]
	for _, storageBackend := range list.Items {
		spec := storageBackend.Spec
		spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
		if spec.Credentials.ExistingSecret == secretName {
			filtered = append(filtered, storageBackend)
		}
	}
	list.Items = filtered
	return nil
}

func tamossManagedLabelSelector(tamoss *tamossv1alpha1.Tamoss) client.MatchingLabels {
	return client.MatchingLabels{
		"app.kubernetes.io/instance":   tamoss.Name,
		"app.kubernetes.io/managed-by": "tamoss-operator",
	}
}

func storageBackendRequest(storageBackend tamossv1alpha1.StorageBackend) types.NamespacedName {
	return types.NamespacedName{
		Name:      storageBackend.Name,
		Namespace: storageBackend.Namespace,
	}
}
