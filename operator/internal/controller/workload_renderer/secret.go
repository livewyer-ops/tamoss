package workload_renderer

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func renderSecrets(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	db := tamoss.DBConnection()
	s3 := tamoss.S3Connection()
	objects := []client.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tamoss.ResourceName("backends"),
				Namespace: tamoss.Namespace,
				Labels:    labels(tamoss, ""),
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"POSTGRES_HOST":             db.Host,
				"POSTGRES_PORT":             db.Port,
				"POSTGRES_DB":               db.Database,
				"TAMOSS_STORAGE_BACKEND_ID": tamossv1alpha1.DefaultStorageBackendID,
				"TAMOSS_STORAGE_LABEL":      fmt.Sprintf("tamoss.%s:s3:%s", s3.Region, s3.Bucket),
				"TAMOSS_S3_ENDPOINT":        s3.Endpoint.Default.URL,
				"TAMOSS_S3_BUCKET":          s3.Bucket,
				"TAMOSS_S3_REGION":          s3.Region,
			},
		},
	}
	if tamoss.Spec.Backends.S3.Provider() == tamossv1alpha1.S3BackendProvidedByRustFSOperator {
		objects[0].(*corev1.Secret).StringData["TAMOSS_STORAGE_PROVIDER"] = "rustfs"
	}

	if s3.Endpoint.Public.URL != "" {
		objects[0].(*corev1.Secret).StringData["TAMOSS_S3_PUBLIC_ENDPOINT"] = s3.Endpoint.Public.URL
	}

	if tamoss.Spec.Secrets.APIToken.Generate {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tamoss.ResourceName("api-token"),
				Namespace: tamoss.Namespace,
				Labels:    labels(tamoss, ""),
			},
			Type: corev1.SecretTypeOpaque,
		}
		if tamoss.Spec.Secrets.APIToken.Token != "" {
			secret.StringData = map[string]string{"TAMOSS_API_TOKEN": tamoss.Spec.Secrets.APIToken.Token}
		}
		objects = append(objects, secret)
	}

	return objects
}
