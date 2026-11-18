package controller

import (
	"context"
	"encoding/json"
	"testing"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func webIdentityFixtures() (*tamossv1alpha1.Tamoss, *tamossv1alpha1.StorageBackend) {
	instance := tamossFixture()
	instance.Spec.Backends.S3 = tamossv1alpha1.S3BackendSpec{ProvidedBy: tamossv1alpha1.S3BackendProvidedByExternal, External: &tamossv1alpha1.S3ExternalSpec{
		CredentialSource: tamossv1alpha1.S3CredentialSourceWebIdentity,
		Region:           "us-east-1", Bucket: "media", Endpoint: tamossv1alpha1.S3EndpointSpec{Default: tamossv1alpha1.EndpointURLSpec{URL: "https://s3.example.test"}},
	}}
	backend := storageBackendFixture()
	backend.Spec = tamossv1alpha1.StorageBackendSpec{
		ID: tamossv1alpha1.DefaultStorageBackendID, TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: instance.Name},
		Provider: tamossv1alpha1.StorageBackendProviderExternalS3, DefaultStorage: true,
		CredentialSource: tamossv1alpha1.S3CredentialSourceWebIdentity,
		BucketName:       "media", Endpoint: instance.S3Connection().Endpoint,
	}
	backend.Spec.ApplyDefaults(backend.Namespace, backend.Name)
	return instance, backend
}

func TestStorageWebIdentityValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*tamossv1alpha1.Tamoss, *tamossv1alpha1.StorageBackend)
	}{
		{"valid", func(*tamossv1alpha1.Tamoss, *tamossv1alpha1.StorageBackend) {}},
		{"secret-mode", func(i *tamossv1alpha1.Tamoss, b *tamossv1alpha1.StorageBackend) {
			i.Spec.Backends.S3.External.CredentialSource = ""
		}},
		{"backend-secret-mode", func(i *tamossv1alpha1.Tamoss, b *tamossv1alpha1.StorageBackend) { b.Spec.CredentialSource = "" }},
		{"backend-secret", func(i *tamossv1alpha1.Tamoss, b *tamossv1alpha1.StorageBackend) {
			b.Spec.Credentials.ExistingSecret = "old"
		}},
		{"instance-secret", func(i *tamossv1alpha1.Tamoss, b *tamossv1alpha1.StorageBackend) {
			i.Spec.Backends.S3.External.Auth.ExistingSecret = "old"
		}},
		{"bucket", func(i *tamossv1alpha1.Tamoss, b *tamossv1alpha1.StorageBackend) { b.Spec.BucketName = "other" }},
		{"region", func(i *tamossv1alpha1.Tamoss, b *tamossv1alpha1.StorageBackend) { b.Spec.Region = "eu-west-1" }},
		{"endpoint", func(i *tamossv1alpha1.Tamoss, b *tamossv1alpha1.StorageBackend) {
			b.Spec.Endpoint.Default.URL = "https://other.example.test"
		}},
		{"public-endpoint", func(i *tamossv1alpha1.Tamoss, b *tamossv1alpha1.StorageBackend) {
			b.Spec.Endpoint.Public.URL = "https://other.example.test"
		}},
		{"id", func(i *tamossv1alpha1.Tamoss, b *tamossv1alpha1.StorageBackend) {
			b.Spec.ID = "11111111-1111-4111-8111-111111111111"
		}},
		{"additional", func(i *tamossv1alpha1.Tamoss, b *tamossv1alpha1.StorageBackend) { b.Spec.DefaultStorage = false }},
		{"rustfs", func(i *tamossv1alpha1.Tamoss, b *tamossv1alpha1.StorageBackend) {
			b.Spec.Provider = tamossv1alpha1.StorageBackendProviderRustFS
		}},
		{"hibernate", func(i *tamossv1alpha1.Tamoss, b *tamossv1alpha1.StorageBackend) {
			b.Spec.Usage = tamossv1alpha1.StorageBackendUsageHibernate
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, b := webIdentityFixtures()
			tc.mutate(i, b)
			err := validateStorageWebIdentity(i, b.Spec)
			if (err == nil) != (tc.name == "valid") {
				t.Fatalf("validation error = %v", err)
			}
		})
	}
}

func TestStorageWebIdentityConfigurationAndCredentialsTransition(t *testing.T) {
	ctx := context.Background()
	instance, backend := webIdentityFixtures()
	scheme := storageBackendTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, backend).Build()
	if err := validateStorageWebIdentitySelection(instance, []tamossv1alpha1.StorageBackend{*backend}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TAMOSS_S3_ACCESS_KEY", "AWS_ACCESS_KEY_ID", "AWS_PROFILE"} {
		instance.Spec.Worker.Env = map[string]string{name: "conflict"}
		if err := validateStorageWebIdentitySelection(instance, []tamossv1alpha1.StorageBackend{*backend}); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	instance.Spec.Worker.Env = nil
	if fields := missingStorageBackendFields(backend.Spec); len(fields) != 0 {
		t.Fatalf("unexpected requirements %v", fields)
	}
	// A retained Secret must never be copied into the keyless default entry.
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "retained", Namespace: instance.Namespace}, Data: map[string][]byte{"accessKey": []byte("access"), "secretKey": []byte("secret")}}
	if err := c.Create(ctx, secret); err != nil {
		t.Fatal(err)
	}
	for _, source := range []tamossv1alpha1.S3CredentialSource{tamossv1alpha1.S3CredentialSourceSecret, tamossv1alpha1.S3CredentialSourceWebIdentity, tamossv1alpha1.S3CredentialSourceSecret} {
		backend.Spec.CredentialSource = source
		backend.Spec.Credentials.ExistingSecret = "retained"
		backend.Spec.Credentials.SecretKeys = tamossv1alpha1.SecretKeySpec{AccessKey: "accessKey", SecretKey: "secretKey"}
		if err := c.Update(ctx, backend); err != nil {
			t.Fatal(err)
		}
		rendered, err := storageBackendRuntimeCredentialsSecret(ctx, c, instance)
		if err != nil {
			t.Fatal(err)
		}
		var payload runtimeStorageBackendCredentialsPayload
		if err := json.Unmarshal(rendered.Data["credentials.json"], &payload); err != nil {
			t.Fatal(err)
		}
		want := 1
		if source == tamossv1alpha1.S3CredentialSourceWebIdentity {
			want = 0
		}
		if payload.Credentials == nil || len(payload.Credentials) != want {
			t.Fatalf("credential count for %s = %d", source, len(payload.Credentials))
		}
	}
	if requests := (&StorageBackendReconciler{Client: c}).storageBackendTamossRequests(ctx, instance); len(requests) != 1 || requests[0].Name != backend.Name {
		t.Fatalf("incorrect instance watch mapping: %v", requests)
	}
	if requests := storageBackendTamossRequest(ctx, backend); len(requests) != 1 || requests[0].Name != instance.Name {
		t.Fatalf("incorrect backend watch mapping: %v", requests)
	}
}

func TestStorageWebIdentityRejectsMissingAndDuplicateDefaults(t *testing.T) {
	instance, backend := webIdentityFixtures()
	if err := validateStorageWebIdentitySelection(instance, nil); err == nil {
		t.Fatal("accepted missing default")
	}
	duplicate := backend.DeepCopy()
	duplicate.Name = "duplicate"
	if err := validateStorageWebIdentitySelection(instance, []tamossv1alpha1.StorageBackend{*backend, *duplicate}); err == nil {
		t.Fatal("accepted duplicate default")
	}
}
