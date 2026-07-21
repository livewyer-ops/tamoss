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

// storageBackendCredentialsSecretIndex is a field index on the resolved
// credentials Secret name so Secret events map to StorageBackends with an
// indexed lookup instead of listing and filtering every StorageBackend.
const storageBackendCredentialsSecretIndex = ".spec.credentials.existingSecret"

func storageBackendCredentialsSecretIndexValue(obj client.Object) []string {
	storageBackend, ok := obj.(*tamossv1alpha1.StorageBackend)
	if !ok {
		return nil
	}
	spec := storageBackend.Spec
	spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
	if spec.Credentials.ExistingSecret == "" {
		return nil
	}
	return []string{spec.Credentials.ExistingSecret}
}

const appInstanceLabel = "app.kubernetes.io/instance"

func tamossManagedLabelSelector(tamoss *tamossv1alpha1.Tamoss) client.MatchingLabels {
	return client.MatchingLabels{
		appInstanceLabel:               tamoss.Name,
		"app.kubernetes.io/managed-by": "tamoss-operator",
	}
}

func storageBackendRequest(storageBackend tamossv1alpha1.StorageBackend) types.NamespacedName {
	return types.NamespacedName{
		Name:      storageBackend.Name,
		Namespace: storageBackend.Namespace,
	}
}
