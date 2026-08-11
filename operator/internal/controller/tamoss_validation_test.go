package controller

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

var _ = Describe("Tamoss API validation", func() {
	Context("CRD validation and defaulting", func() {
		ctx := context.Background()

		It("accepts a minimal CR and applies CRD defaults", func() {
			resource := minimalTamossUnstructured("minimal-defaults")

			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, resource)
			})

			created := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resource.GetName(), Namespace: resource.GetNamespace()}, created)).To(Succeed())
			Expect(created.Spec.API.Enabled).To(BeNil())
			Expect(created.Spec.API.Image.Repository).To(Equal("livewyer/tamoss-api"))
			Expect(created.Spec.Worker.Enabled).To(BeNil())
			Expect(created.Spec.UI.Enabled).To(BeNil())
			Expect(created.Spec.Auth.Required).To(BeTrue())
			Expect(created.Spec.Service.Enabled).To(BeTrue())
			Expect(created.Spec.Secrets.APIToken.Generate).To(BeTrue())
		})

		It("rejects invalid replica counts", func() {
			resource := &tamossv1alpha1.Tamoss{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-replicas",
					Namespace: "default",
				},
				Spec: minimalTamossSpec(),
			}
			resource.Spec.API.ReplicaCount = ptr.To[int32](-1)

			err := k8sClient.Create(ctx, resource)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsInvalid(err)).To(BeTrue())
		})

		It("requires an explicit API token when generation is disabled", func() {
			resource := minimalTamossUnstructured("invalid-token")
			Expect(unstructured.SetNestedField(resource.Object, false, "spec", "secrets", "apiToken", "generate")).To(Succeed())

			err := k8sClient.Create(ctx, resource)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsInvalid(err)).To(BeTrue())
		})

		It("requires an IngestRun Tamoss reference name", func() {
			run := testIngestRun()
			run.Name = "missing-ingest-tamoss-ref"
			run.Namespace = "default"
			run.Spec.TamossRef.Name = ""

			err := k8sClient.Create(ctx, run)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsInvalid(err)).To(BeTrue())
		})

		// A minimal IngestRun omits spec.options entirely, and the nested
		// defaults do not materialise when the parent object is absent. The
		// options immutability rule must therefore tolerate its absence, or the
		// Console's only permitted mutation is rejected for the whole lifetime
		// of the run and it can never be cancelled.
		It("cancels a minimal IngestRun created without spec.options", func() {
			run := &tamossv1alpha1.IngestRun{
				ObjectMeta: metav1.ObjectMeta{Name: "minimal-ingest-run", Namespace: "default"},
				Spec: tamossv1alpha1.IngestRunSpec{
					TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "example"},
					InputRef:  tamossv1alpha1.IngestInputReference{Kind: "StagedObject", ID: "staged-123"},
				},
			}

			Expect(k8sClient.Create(ctx, run)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, run)
			})

			created := &tamossv1alpha1.IngestRun{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: run.Name, Namespace: run.Namespace}, created)).To(Succeed())
			Expect(created.Spec.Options.StorageBackendRef).To(BeNil())

			created.Spec.DesiredState = tamossv1alpha1.IngestRunDesiredStateCancelled
			Expect(k8sClient.Update(ctx, created)).To(Succeed())
		})

		It("keeps spec.options immutable once it is set", func() {
			run := &tamossv1alpha1.IngestRun{
				ObjectMeta: metav1.ObjectMeta{Name: "immutable-ingest-options", Namespace: "default"},
				Spec: tamossv1alpha1.IngestRunSpec{
					TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "example"},
					InputRef:  tamossv1alpha1.IngestInputReference{Kind: "StagedObject", ID: "staged-123"},
					Options:   tamossv1alpha1.IngestRunOptions{MaxInputs: 10},
				},
			}

			Expect(k8sClient.Create(ctx, run)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, run)
			})

			created := &tamossv1alpha1.IngestRun{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: run.Name, Namespace: run.Namespace}, created)).To(Succeed())
			created.Spec.Options.MaxInputs = 20

			err := k8sClient.Update(ctx, created)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsInvalid(err)).To(BeTrue())
		})

		It("requires an IngestRun spec", func() {
			run := &unstructured.Unstructured{}
			run.SetAPIVersion(tamossv1alpha1.SchemeGroupVersion.String())
			run.SetKind("IngestRun")
			run.SetName("missing-ingest-spec")
			run.SetNamespace("default")

			err := k8sClient.Create(ctx, run)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsInvalid(err)).To(BeTrue())
		})

		It("rejects StorageBackend durable identity updates", func() {
			tests := []struct {
				name   string
				mutate func(*tamossv1alpha1.StorageBackend)
			}{
				{
					name: "id",
					mutate: func(storageBackend *tamossv1alpha1.StorageBackend) {
						storageBackend.Spec.ID = "22222222-2222-5222-8222-222222222222"
					},
				},
				{
					name: "provider",
					mutate: func(storageBackend *tamossv1alpha1.StorageBackend) {
						storageBackend.Spec.Provider = tamossv1alpha1.StorageBackendProviderRustFS
					},
				},
				{
					name: "bucket",
					mutate: func(storageBackend *tamossv1alpha1.StorageBackend) {
						storageBackend.Spec.BucketName = "other-bucket"
					},
				},
				{
					name: "tamoss-ref",
					mutate: func(storageBackend *tamossv1alpha1.StorageBackend) {
						storageBackend.Spec.TamossRef.Name = "other-tamoss"
					},
				},
			}
			for _, tt := range tests {
				storageBackend := validStorageBackend("immutable-" + tt.name)
				Expect(k8sClient.Create(ctx, storageBackend)).To(Succeed())
				DeferCleanup(func(name string) {
					_ = k8sClient.Delete(ctx, &tamossv1alpha1.StorageBackend{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}})
				}, storageBackend.Name)

				current := &tamossv1alpha1.StorageBackend{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: storageBackend.Name, Namespace: storageBackend.Namespace}, current)).To(Succeed())
				tt.mutate(current)

				err := k8sClient.Update(ctx, current)
				Expect(err).To(HaveOccurred())
				Expect(errors.IsInvalid(err)).To(BeTrue())
			}
		})

		It("accepts unchanged fullname override and rejects changes", func() {
			name := types.NamespacedName{Name: "immutable-fullname", Namespace: "default"}
			resource := &tamossv1alpha1.Tamoss{
				ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
				Spec:       minimalTamossSpec(),
			}
			resource.Spec.FullnameOverride = "tamoss-fixed"
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, resource)
				cleanupTamossArtifacts(ctx, name.Name, name.Namespace)
			})

			current := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, name, current)).To(Succeed())
			current.Spec.Paused = true
			Expect(k8sClient.Update(ctx, current)).To(Succeed())

			Expect(k8sClient.Get(ctx, name, current)).To(Succeed())
			current.Spec.FullnameOverride = "tamoss-renamed"
			err := k8sClient.Update(ctx, current)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsInvalid(err)).To(BeTrue())
		})

		It("rejects missing external S3 endpoints at the API boundary", func() {
			storageBackend := validStorageBackend("missing-storage-endpoint")
			storageBackend.Spec.Endpoint.Default.URL = ""
			err := k8sClient.Create(ctx, storageBackend)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsInvalid(err)).To(BeTrue())

			tamoss := &tamossv1alpha1.Tamoss{
				ObjectMeta: metav1.ObjectMeta{Name: "missing-tamoss-endpoint", Namespace: "default"},
				Spec:       minimalTamossSpec(),
			}
			tamoss.Spec.Backends.S3.External.Endpoint.Default.URL = ""
			err = k8sClient.Create(ctx, tamoss)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsInvalid(err)).To(BeTrue())
		})

		It("accepts scalar and array storage backend tags without an artificial count limit", func() {
			tags := map[string]apiextensionsv1.JSON{
				"tier":   {Raw: []byte(`"hot"`)},
				"access": {Raw: []byte(`["programme","archive"]`)},
			}
			for index := 0; index < 65; index++ {
				tags[fmt.Sprintf("tag-%02d", index)] = apiextensionsv1.JSON{Raw: []byte(`"value"`)}
			}

			storageBackend := validStorageBackend("storage-tags-union")
			storageBackend.Spec.Tags = tags
			Expect(k8sClient.Create(ctx, storageBackend)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, storageBackend)
			})

			createdStorageBackend := &tamossv1alpha1.StorageBackend{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: storageBackend.Name, Namespace: storageBackend.Namespace}, createdStorageBackend)).To(Succeed())
			Expect(string(createdStorageBackend.Spec.Tags["tier"].Raw)).To(Equal(`"hot"`))
			Expect(string(createdStorageBackend.Spec.Tags["access"].Raw)).To(Equal(`["programme","archive"]`))

			tamoss := minimalTamossUnstructured("default-storage-tags-union")
			Expect(unstructured.SetNestedMap(tamoss.Object, map[string]interface{}{
				"tier":   "hot",
				"access": []interface{}{"programme", "archive"},
			}, "spec", "backends", "s3", "tags")).To(Succeed())
			Expect(k8sClient.Create(ctx, tamoss)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, tamoss)
			})

			createdTamoss := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: tamoss.GetName(), Namespace: tamoss.GetNamespace()}, createdTamoss)).To(Succeed())
			Expect(string(createdTamoss.Spec.Backends.S3.Tags["tier"].Raw)).To(Equal(`"hot"`))
			Expect(string(createdTamoss.Spec.Backends.S3.Tags["access"].Raw)).To(Equal(`["programme","archive"]`))
		})

		It("rejects ambiguous provider blocks at the API boundary", func() {
			tests := []struct {
				name   string
				mutate func(*unstructured.Unstructured)
			}{
				{
					name: "db-external-and-cnpg",
					mutate: func(resource *unstructured.Unstructured) {
						Expect(unstructured.SetNestedMap(resource.Object, map[string]interface{}{
							"instances": int64(1),
						}, "spec", "backends", "db", "cnpg")).To(Succeed())
					},
				},
				{
					name: "s3-external-and-rustfs",
					mutate: func(resource *unstructured.Unstructured) {
						Expect(unstructured.SetNestedMap(resource.Object, map[string]interface{}{
							"bucket": map[string]interface{}{"name": "tamoss"},
						}, "spec", "backends", "s3", "rustfsOperator")).To(Succeed())
					},
				},
				{
					name: "auth-none-with-external",
					mutate: func(resource *unstructured.Unstructured) {
						Expect(unstructured.SetNestedMap(resource.Object, map[string]interface{}{
							"providedBy": "none",
							"external": map[string]interface{}{
								"oauth2": map[string]interface{}{"enabled": true},
							},
						}, "spec", "auth")).To(Succeed())
					},
				},
			}

			for _, tt := range tests {
				resource := minimalTamossUnstructured(tt.name)
				tt.mutate(resource)

				err := k8sClient.Create(ctx, resource)
				Expect(err).To(HaveOccurred())
				Expect(errors.IsInvalid(err)).To(BeTrue())
			}
		})

		It("rejects incomplete CNPG restore configuration", func() {
			resource := minimalTamossUnstructured("invalid-cnpg-restore")
			Expect(unstructured.SetNestedMap(resource.Object, map[string]interface{}{
				"providedBy": "cnpg",
				"cnpg": map[string]interface{}{
					"restore": map[string]interface{}{
						"enabled": true,
						"source":  "source-db",
						"objectStore": map[string]interface{}{
							"bucket": "pg-backups",
						},
					},
				},
			}, "spec", "backends", "db")).To(Succeed())

			err := k8sClient.Create(ctx, resource)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsInvalid(err)).To(BeTrue())
		})
	})
})
