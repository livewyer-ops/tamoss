package controller

import (
	"crypto/sha256"
	"fmt"
	"strings"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func storageBackendLabels(storageBackend *tamossv1alpha1.StorageBackend, tamoss *tamossv1alpha1.Tamoss) map[string]string {
	instance := storageBackend.Spec.TamossRef.Name
	if tamoss != nil {
		instance = tamoss.Name
	}
	return map[string]string{
		"app.kubernetes.io/name":             tamossAppName,
		appInstanceLabel:                     instance,
		appComponentLabel:                    "storage-backend",
		"app.kubernetes.io/managed-by":       "tamoss-operator",
		"tamoss.livewyer.io/storage-backend": storageBackend.Name,
	}
}

func storageBackendResourceName(storageBackend *tamossv1alpha1.StorageBackend, suffix string) string {
	if suffix == "" {
		return storageBackend.Name
	}
	return fmt.Sprintf("%s-%s", storageBackend.Name, suffix)
}

func storageBackendHash(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return fmt.Sprintf("%x", sum[:])
}
