package controller

import (
	"context"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var _ = Describe("S3 web identity API validation", func() {
	It("accepts an explicit keyless external backend and preserves immutable identity", func() {
		ctx := context.Background()
		backend := validStorageBackend("web-identity-valid")
		backend.Spec.ID = tamossv1alpha1.DefaultStorageBackendID
		backend.Spec.Provider = tamossv1alpha1.StorageBackendProviderExternalS3
		backend.Spec.Usage = tamossv1alpha1.StorageBackendUsageMedia
		backend.Spec.DefaultStorage = true
		backend.Spec.CredentialSource = tamossv1alpha1.S3CredentialSourceWebIdentity
		backend.Spec.Credentials = tamossv1alpha1.SecretReferenceSpec{}
		Expect(k8sClient.Create(ctx, backend)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, backend) })
		original := backend.DeepCopy()
		backend.Spec.ID = "11111111-1111-4111-8111-111111111111"
		Expect(errors.IsInvalid(k8sClient.Update(ctx, backend))).To(BeTrue())
		original.Spec.CredentialSource = tamossv1alpha1.S3CredentialSourceSecret
		original.Spec.Credentials.ExistingSecret = "retained"
		Expect(k8sClient.Update(ctx, original)).To(Succeed())
	})
	It("rejects unsupported web identity consumers and Secret conflicts", func() {
		for _, name := range []string{"additional", "hibernate", "rustfs", "secret", "unknown"} {
			backend := validStorageBackend("web-identity-" + name)
			backend.Spec.ID = tamossv1alpha1.DefaultStorageBackendID
			backend.Spec.Provider = tamossv1alpha1.StorageBackendProviderExternalS3
			backend.Spec.Usage = tamossv1alpha1.StorageBackendUsageMedia
			backend.Spec.DefaultStorage = true
			backend.Spec.CredentialSource = tamossv1alpha1.S3CredentialSourceWebIdentity
			backend.Spec.Credentials = tamossv1alpha1.SecretReferenceSpec{}
			switch name {
			case "additional":
				backend.Spec.DefaultStorage = false
			case "hibernate":
				backend.Spec.Usage = tamossv1alpha1.StorageBackendUsageHibernate
			case "rustfs":
				backend.Spec.Provider = tamossv1alpha1.StorageBackendProviderRustFS
			case "secret":
				backend.Spec.Credentials.ExistingSecret = "conflict"
			case "unknown":
				backend.Spec.CredentialSource = "unknown"
			}
			Expect(errors.IsInvalid(k8sClient.Create(context.Background(), backend))).To(BeTrue(), name)
		}
	})
	It("accepts Tamoss web identity and rejects a simultaneous Secret reference", func() {
		resource := minimalTamossUnstructured("web-identity-instance")
		unstructured.RemoveNestedField(resource.Object, "spec", "backends", "s3", "external", "auth")
		Expect(unstructured.SetNestedField(resource.Object, "web-identity", "spec", "backends", "s3", "external", "credentialSource")).To(Succeed())
		Expect(k8sClient.Create(context.Background(), resource)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), resource) })
		Expect(unstructured.SetNestedField(resource.Object, "conflict", "spec", "backends", "s3", "external", "auth", "existingSecret")).To(Succeed())
		Expect(errors.IsInvalid(k8sClient.Update(context.Background(), resource))).To(BeTrue())
	})
})
