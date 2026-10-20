package controller

import (
	"fmt"
	"strings"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func validateStorageWebIdentity(tamoss *tamossv1alpha1.Tamoss, spec tamossv1alpha1.StorageBackendSpec) error {
	if !tamoss.UsesS3WebIdentity() || spec.CredentialSource != tamossv1alpha1.S3CredentialSourceWebIdentity {
		return fmt.Errorf("both Tamoss and default StorageBackend must select credentialSource web-identity")
	}
	s3 := tamoss.S3Connection()
	if !spec.IsExternalObjectStore() || spec.IsHibernateDestination() || !spec.DefaultStorage || spec.ID != tamossv1alpha1.DefaultStorageBackendID {
		return fmt.Errorf("web-identity requires the default external S3 media backend and its runtime ID")
	}
	publicEndpoint := s3.Endpoint.Public.URL
	if publicEndpoint == "" {
		publicEndpoint = s3.Endpoint.Default.URL
	}
	if spec.BucketName != s3.Bucket || spec.Region != s3.Region || spec.Endpoint.Default.URL != s3.Endpoint.Default.URL || spec.Endpoint.Public.URL != publicEndpoint {
		return fmt.Errorf("web-identity StorageBackend bucket, region and endpoints must match the Tamoss default S3 configuration")
	}
	if spec.Credentials.ExistingSecret != "" || s3.Auth.ExistingSecret != "" {
		return fmt.Errorf("web-identity must not reference a credentials Secret")
	}
	return nil
}

func validateStorageWebIdentitySelection(tamoss *tamossv1alpha1.Tamoss, backends []tamossv1alpha1.StorageBackend) error {
	for component, env := range map[string]map[string]string{"api": tamoss.Spec.API.Env, "worker": tamoss.Spec.Worker.Env} {
		for _, name := range []string{"TAMOSS_S3_ACCESS_KEY", "TAMOSS_S3_SECRET_KEY", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_PROFILE", "AWS_SHARED_CREDENTIALS_FILE"} {
			if strings.TrimSpace(env[name]) != "" {
				return fmt.Errorf("web-identity conflicts with %s environment variable %s", component, name)
			}
		}
		if id := env["TAMOSS_STORAGE_BACKEND_ID"]; id != "" && id != tamossv1alpha1.DefaultStorageBackendID {
			return fmt.Errorf("web-identity requires the default runtime storage backend ID")
		}
	}
	var selected *tamossv1alpha1.StorageBackendSpec
	for _, backend := range backends {
		if !backend.DeletionTimestamp.IsZero() {
			continue
		}
		spec := backend.Spec
		spec.ApplyDefaults(backend.Namespace, backend.Name)
		if spec.ID != tamossv1alpha1.DefaultStorageBackendID && !spec.DefaultStorage {
			continue
		}
		if selected != nil {
			return fmt.Errorf("web-identity requires exactly one default StorageBackend")
		}
		selected = &spec
	}
	if selected == nil {
		return fmt.Errorf("web-identity requires a matching default StorageBackend")
	}
	return validateStorageWebIdentity(tamoss, *selected)
}
