package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

var _ = Describe("Tamoss Controller lifecycle", func() {
	const resourceName = "test-resource-lifecycle"

	ctx := context.Background()
	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	BeforeEach(func() {
		cleanupTamossArtifacts(ctx, resourceName, "default")
		cleanupAuthentikBlueprintArtifacts(ctx, "default", resourceName, "auth")
		ensureBackendSecrets(ctx, "default")
		resource := &tamossv1alpha1.Tamoss{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
			},
			Spec: minimalTamossSpec(),
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
	})

	AfterEach(func() {
		resource := &tamossv1alpha1.Tamoss{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		if err == nil {
			if len(resource.Finalizers) > 0 {
				resource.Finalizers = nil
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())
			}
			err = k8sClient.Delete(ctx, resource)
		}
		if err != nil && !errors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred())
		}
		cleanupTamossArtifacts(ctx, resourceName, "default")
		cleanupAuthentikBlueprintArtifacts(ctx, "default", resourceName, "auth")
	})

	It("removes the finalizer after owned resources are deleted", func() {
		controllerReconciler := &TamossReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		resource := &tamossv1alpha1.Tamoss{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
		Expect(resource.Finalizers).To(ContainElement(tamossFinalizer))
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

		Eventually(func() bool {
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			probe := &tamossv1alpha1.Tamoss{}
			err = k8sClient.Get(ctx, typeNamespacedName, probe)
			return errors.IsNotFound(err)
		}, "5s").Should(BeTrue())
	})

	It("removes the finalizer when Authentik cleanup fails", func() {
		ensureNamespace(ctx, "auth")
		ensureAuthentikTokenSecret(ctx, "auth")
		server := authentikManagedServer()
		configureAuthentikIdentity(ctx, typeNamespacedName, "auth", server.URL, []string{"https://app.example.com/auth/callback"})
		recorder := record.NewFakeRecorder(10)
		controllerReconciler := &TamossReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: recorder,
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(server.applied()).To(Equal(1))

		resource := &tamossv1alpha1.Tamoss{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
		Expect(resource.Finalizers).To(ContainElement(tamossFinalizer))
		server.Close()
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

		Eventually(func() bool {
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			probe := &tamossv1alpha1.Tamoss{}
			err = k8sClient.Get(ctx, typeNamespacedName, probe)
			return errors.IsNotFound(err)
		}, "5s").Should(BeTrue())
		Eventually(recorder.Events).Should(Receive(ContainSubstring(operatorstatus.ReasonAuthentikManagedBlueprintDeleteFailed)))
	})
})
