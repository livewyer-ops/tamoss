package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	fakediscovery "k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	authentikbackend "github.com/livewyer-ops/tamoss/operator/internal/controller/auth/authentik"
	cnpgbackend "github.com/livewyer-ops/tamoss/operator/internal/controller/backend/cnpg"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/backend/rustfs"
	operatordiscovery "github.com/livewyer-ops/tamoss/operator/internal/discovery"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

var _ = Describe("Tamoss Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		tamoss := &tamossv1alpha1.Tamoss{}

		BeforeEach(func() {
			cleanupTamossArtifacts(ctx, resourceName, "default")
			cleanupAuthentikBlueprintArtifacts(ctx, "default", resourceName, "auth", "other", "any-auth")
			ensureBackendSecrets(ctx, "default")
			By("creating the custom resource for the Kind Tamoss")
			err := k8sClient.Get(ctx, typeNamespacedName, tamoss)
			if err != nil && errors.IsNotFound(err) {
				resource := &tamossv1alpha1.Tamoss{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: minimalTamossSpec(),
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &tamossv1alpha1.Tamoss{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if errors.IsNotFound(err) {
				return
			}
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Tamoss")
			if len(resource.Finalizers) > 0 {
				resource.Finalizers = nil
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())
			}
			err = k8sClient.Delete(ctx, resource)
			if !errors.IsNotFound(err) {
				Expect(err).NotTo(HaveOccurred())
			}
			cleanupTamossArtifacts(ctx, resourceName, "default")
			cleanupAuthentikBlueprintArtifacts(ctx, "default", resourceName, "auth", "other", "any-auth")
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &TamossReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			apiDeployment := &appsv1.Deployment{}
			Expect(errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-api", Namespace: "default"}, apiDeployment))).To(BeTrue())
			uiDeployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-ui", Namespace: "default"}, uiDeployment)).To(Succeed())
			schemaJob := &batchv1.Job{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-schema-migrate-" + schemaVersionForName(), Namespace: "default"}, schemaJob)).To(Succeed())
			Expect(schemaJob.Spec.Template.Spec.Containers[0].Command).To(Equal([]string{"uv"}))
			Expect(schemaJob.Spec.Template.Spec.Containers[0].Args).To(ContainElements("run", "tamoss-db", "migrate"))
			completeSchemaMigration(ctx, controllerReconciler, typeNamespacedName, schemaJob)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-api", Namespace: "default"}, apiDeployment)).To(Succeed())
			Expect(hasTamossOwner(apiDeployment.OwnerReferences, resourceName)).To(BeTrue())

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(updated.Status.ObservedGeneration).To(Equal(updated.Generation))
			for _, conditionType := range []string{
				operatorstatus.ConditionReady,
				operatorstatus.ConditionProgressing,
				operatorstatus.ConditionDegraded,
				operatorstatus.ConditionSchemaMigrated,
				operatorstatus.ConditionBackendsReady,
				operatorstatus.ConditionPaused,
			} {
				Expect(meta.FindStatusCondition(updated.Status.Conditions, conditionType)).NotTo(BeNil())
			}
			Expect(meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionPaused).Status).To(Equal(metav1.ConditionFalse))
		})

		It("should ignore CRs outside WATCH_NAMESPACES scope", func() {
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}}
			err := k8sClient.Create(ctx, namespace)
			if err != nil {
				Expect(errors.IsAlreadyExists(err)).To(BeTrue())
			}
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, namespace)
			})

			scopedName := types.NamespacedName{Name: "scoped-out", Namespace: "other"}
			resource := &tamossv1alpha1.Tamoss{
				ObjectMeta: metav1.ObjectMeta{Name: scopedName.Name, Namespace: scopedName.Namespace},
				Spec:       minimalTamossSpec(),
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, resource)
			})

			controllerReconciler := &TamossReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				WatchNamespaces: map[string]struct{}{"default": {}},
			}
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: scopedName})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: scopedName.Name + "-ui", Namespace: scopedName.Namespace}, deployment)
			Expect(errors.IsNotFound(err)).To(BeTrue())
			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, scopedName, updated)).To(Succeed())
			Expect(updated.Status.ObservedGeneration).To(BeZero())
		})

		It("should block reconciliation when backend Secrets are missing", func() {
			missing := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "tams-postgresql-auth", Namespace: "default"}}
			Expect(k8sClient.Delete(ctx, missing)).To(Succeed())

			recorder := record.NewFakeRecorder(10)
			controllerReconciler := &TamossReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: recorder,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			ready := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(operatorstatus.ReasonMissingSecret))
			backendsReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionBackendsReady)
			Expect(backendsReady).NotTo(BeNil())
			Expect(backendsReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(backendsReady.Reason).To(Equal(operatorstatus.ReasonMissingSecret))

			job := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-schema-migrate-" + schemaVersionForName(), Namespace: "default"}, job)
			Expect(errors.IsNotFound(err)).To(BeTrue())
			Eventually(recorder.Events).Should(Receive(ContainSubstring(operatorstatus.ReasonMissingSecret)))
		})

		It("should reject external backend updates when required static configuration is missing", func() {
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Backends.S3.External.Endpoint.Default.URL = ""
			err := k8sClient.Update(ctx, instance)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsInvalid(err)).To(BeTrue())
		})

		It("should block external OAuth2 when required metadata is missing", func() {
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Auth = tamossv1alpha1.AuthSpec{
				ProvidedBy: tamossv1alpha1.AuthProvidedByExternal,
				Required:   true,
				External: &tamossv1alpha1.AuthExternalSpec{
					OAuth2: tamossv1alpha1.OAuth2Spec{
						Enabled:    true,
						Issuer:     "https://auth.example.com/application/o/tamoss/",
						Algorithms: []string{"RS256"},
					},
				},
			}
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			recorder := record.NewFakeRecorder(10)
			controllerReconciler := &TamossReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: recorder,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			identityReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionIdentityReady)
			Expect(identityReady).NotTo(BeNil())
			Expect(identityReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(identityReady.Reason).To(Equal(operatorstatus.ReasonMissingProviderConfiguration))

			job := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-schema-migrate-" + schemaVersionForName(), Namespace: "default"}, job)
			Expect(errors.IsNotFound(err)).To(BeTrue())
			Eventually(recorder.Events).Should(Receive(ContainSubstring(operatorstatus.ReasonMissingProviderConfiguration)))
		})

		It("should default managed Authentik to the first platform namespace", func() {
			ensureNamespace(ctx, "auth")
			ensureNamespace(ctx, "other")
			ensureAuthentikTokenSecret(ctx, "auth")
			server := authentikManagedServer()
			defer server.Close()

			instance := configureAuthentikIdentity(ctx, typeNamespacedName, "auth", server.URL, []string{"https://app.example.com/auth/callback"})
			controllerReconciler := &TamossReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(server.applied()).To(Equal(1))
			Expect(server.content()).To(ContainSubstring(instance.Spec.Auth.ApplicationSlug(instance.Namespace, instance.Name)))

			secondName := types.NamespacedName{Name: "second-auth", Namespace: "default"}
			second := &tamossv1alpha1.Tamoss{
				ObjectMeta: metav1.ObjectMeta{Name: secondName.Name, Namespace: secondName.Namespace},
				Spec:       minimalTamossSpec(),
			}
			Expect(k8sClient.Create(ctx, second)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, second)
				cleanupTamossArtifacts(ctx, secondName.Name, secondName.Namespace)
			})
			configureAuthentikIdentity(ctx, secondName, "other", server.URL, []string{"https://other.example.com/auth/callback"})

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: secondName})
			Expect(err).NotTo(HaveOccurred())

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, secondName, updated)).To(Succeed())
			identityReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionIdentityReady)
			Expect(identityReady).NotTo(BeNil())
			Expect(identityReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(identityReady.Reason).To(Equal("PlatformNamespaceNotAllowed"))
		})

		It("should reject Authentik platform namespaces outside a configured allow-list", func() {
			ensureNamespace(ctx, "other")
			instance := configureAuthentikIdentity(ctx, typeNamespacedName, "other", "http://authentik.auth.svc:9000", []string{"https://app.example.com/auth/callback"})
			recorder := record.NewFakeRecorder(10)
			controllerReconciler := &TamossReconciler{
				Client:                      k8sClient,
				Scheme:                      k8sClient.Scheme(),
				Recorder:                    recorder,
				AuthentikPlatformNamespaces: authentikbackend.NewPlatformNamespacePolicy("auth"),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			identityReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionIdentityReady)
			Expect(identityReady).NotTo(BeNil())
			Expect(identityReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(identityReady.Reason).To(Equal("PlatformNamespaceNotAllowed"))
			Expect(instance.Spec.Auth.AuthentikBlueprints.PlatformNamespace).To(Equal("other"))
			Eventually(recorder.Events).Should(Receive(ContainSubstring("PlatformNamespaceNotAllowed")))
		})

		It("should allow any Authentik platform namespace in cluster-wide mode", func() {
			ensureNamespace(ctx, "any-auth")
			ensureAuthentikTokenSecret(ctx, "any-auth")
			server := authentikManagedServer()
			defer server.Close()

			instance := configureAuthentikIdentity(ctx, typeNamespacedName, "any-auth", server.URL, []string{"https://app.example.com/auth/callback"})
			controllerReconciler := &TamossReconciler{
				Client:                      k8sClient,
				Scheme:                      k8sClient.Scheme(),
				AuthentikPlatformNamespaces: authentikbackend.NewPlatformNamespacePolicy("*"),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(server.applied()).To(Equal(1))
			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			identityReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionIdentityReady)
			Expect(identityReady).NotTo(BeNil())
			Expect(identityReady.Status).To(Equal(metav1.ConditionTrue))
			Expect(identityReady.Reason).To(Equal("IssuerReachable"))
			Expect(updated.Status.Auth.ManagedBlueprint).To(Equal(authentikbackend.ManagedBlueprintName(instance)))
		})

		It("should not apply a managed Authentik Blueprint when no redirect URI can be derived", func() {
			ensureNamespace(ctx, "auth")
			server := authentikManagedServer()
			defer server.Close()
			instance := configureAuthentikIdentity(ctx, typeNamespacedName, "auth", "http://authentik.auth.svc:9000", nil)
			recorder := record.NewFakeRecorder(10)
			controllerReconciler := &TamossReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: recorder,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			identityReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionIdentityReady)
			Expect(identityReady).NotTo(BeNil())
			Expect(identityReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(identityReady.Reason).To(Equal(operatorstatus.ReasonNoRedirectURIDerivable))
			Expect(instance.Spec.Auth.AuthentikBlueprints.PlatformNamespace).To(Equal("auth"))
			Expect(server.applied()).To(Equal(0))
			Eventually(recorder.Events).Should(Receive(ContainSubstring(operatorstatus.ReasonNoRedirectURIDerivable)))
		})

		It("should block managed Authentik when the API token Secret is missing", func() {
			ensureNamespace(ctx, "auth")
			server := authentikManagedServer()
			defer server.Close()
			configureAuthentikIdentity(ctx, typeNamespacedName, "auth", server.URL, []string{"https://app.example.com/auth/callback"})
			recorder := record.NewFakeRecorder(10)
			controllerReconciler := &TamossReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: recorder,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			identityReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionIdentityReady)
			Expect(identityReady).NotTo(BeNil())
			Expect(identityReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(identityReady.Reason).To(Equal(operatorstatus.ReasonAuthentikAPITokenMissing))
			Expect(server.applied()).To(Equal(0))
			Eventually(recorder.Events).Should(Receive(ContainSubstring(operatorstatus.ReasonAuthentikAPITokenMissing)))
		})

		It("should report Gateway API unavailable when HTTPRoute CRDs are missing", func() {
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.HTTPRoute.Enabled = true
			instance.Spec.HTTPRoute.API.Hostnames = []string{"api.example.com"}
			instance.Spec.HTTPRoute.UI.Hostnames = []string{"app.example.com"}
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			controllerReconciler := &TamossReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			routingReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionRoutingReady)
			Expect(routingReady).NotTo(BeNil())
			Expect(routingReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(routingReady.Reason).To(Equal(operatorstatus.ReasonGatewayAPIUnavailable))
			hostnamesReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionHostnamesReady)
			Expect(hostnamesReady).NotTo(BeNil())
			Expect(hostnamesReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(hostnamesReady.Reason).To(Equal(operatorstatus.ReasonGatewayAPIUnavailable))
		})

		It("should report unsupported HTTPRoute filters before rendering routes", func() {
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.HTTPRoute.Enabled = true
			instance.Spec.HTTPRoute.API.Hostnames = []string{"api.example.com"}
			instance.Spec.HTTPRoute.API.Filters = []apiextensionsv1.JSON{
				{Raw: []byte(`{"type":"RequestRedirect","requestRedirect":{"scheme":"https"}}`)},
				{Raw: []byte(`{"type":"URLRewrite","urlRewrite":{"hostname":"api.internal.example.com"}}`)},
			}
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			controllerReconciler := &TamossReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			routingReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionRoutingReady)
			Expect(routingReady).NotTo(BeNil())
			Expect(routingReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(routingReady.Reason).To(Equal(operatorstatus.ReasonUnsupportedHTTPRouteFilter))
			Expect(routingReady.Message).To(ContainSubstring("RequestRedirect+URLRewrite"))
		})

		It("should block CNPG reconciliation when the dependency operator is missing", func() {
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Backends.DB = cnpgDBSpec()
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			discoveryManager, _ := fakeDependencyDiscovery(false)
			recorder := record.NewFakeRecorder(10)
			controllerReconciler := &TamossReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				Recorder:                recorder,
				Discovery:               discoveryManager,
				DependencyProbeInterval: time.Second,
			}
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Second))

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			backendsReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionBackendsReady)
			Expect(backendsReady).NotTo(BeNil())
			Expect(backendsReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(backendsReady.Reason).To(Equal(operatorstatus.ReasonMissingDependencyOperator))
			degraded := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionDegraded)
			Expect(degraded).NotTo(BeNil())
			Expect(degraded.Status).To(Equal(metav1.ConditionTrue))
			Expect(updated.Status.Backends.DB.Provider).To(Equal(tamossv1alpha1.BackendProvidedByCNPG))
			Expect(updated.Status.Providers.DB.Provider).To(Equal(string(tamossv1alpha1.BackendProvidedByCNPG)))
			Expect(updated.Status.Providers.DB.Ownership).To(Equal(tamossv1alpha1.ProviderOwnershipManaged))

			job := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-schema-migrate-" + schemaVersionForName(), Namespace: "default"}, job)
			Expect(errors.IsNotFound(err)).To(BeTrue())
			Eventually(recorder.Events).Should(Receive(ContainSubstring(operatorstatus.ReasonMissingDependencyOperator)))
		})

		It("should requeue CNPG reconciliation while dependency discovery is unknown", func() {
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Backends.DB = cnpgDBSpec()
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			fakeClient := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}
			discoveryManager := operatordiscovery.NewManager(fakeClient, []schema.GroupVersionResource{operatordiscovery.CNPGClustersGVR})
			recorder := record.NewFakeRecorder(10)
			controllerReconciler := &TamossReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				Recorder:                recorder,
				Discovery:               discoveryManager,
				DependencyProbeInterval: time.Second,
			}
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Second))

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			backendsReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionBackendsReady)
			if backendsReady != nil {
				Expect(backendsReady.Reason).NotTo(Equal(operatorstatus.ReasonMissingDependencyOperator))
			}
			Consistently(recorder.Events, 100*time.Millisecond).ShouldNot(Receive(ContainSubstring(operatorstatus.ReasonMissingDependencyOperator)))
		})

		It("should resume CNPG reconciliation after discovery sees the dependency CRD", func() {
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Backends.DB = cnpgDBSpec()
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			discoveryManager, fakeClient := fakeDependencyDiscovery(false)
			controllerReconciler := &TamossReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				Discovery:               discoveryManager,
				DependencyProbeInterval: time.Second,
			}
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Second))

			ensureCNPGClusterCRD(ctx)
			fakeClient.Resources = cnpgAPIResources()
			Expect(discoveryManager.Refresh(ctx)).To(Succeed())
			result, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(10 * time.Second))

			cluster := &cnpgv1.Cluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-db", Namespace: "default"}, cluster)).To(Succeed())
			Expect(hasTamossOwner(cluster.OwnerReferences, resourceName)).To(BeTrue())

			job := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-schema-migrate-" + schemaVersionForName(), Namespace: "default"}, job)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			setCNPGClusterStatus(ctx, resourceName+"-db", "default", []metav1.Condition{{
				Type:    string(cnpgv1.ConditionClusterReady),
				Status:  metav1.ConditionTrue,
				Reason:  string(cnpgv1.ClusterReady),
				Message: "Cluster is ready",
			}})
			createCNPGSecrets(ctx, instance)

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-schema-migrate-" + schemaVersionForName(), Namespace: "default"}, job)).To(Succeed())
		})

		It("should render a managed CNPG backup policy", func() {
			ensureCNPGClusterCRD(ctx)
			ensureCNPGScheduledBackupCRD(ctx)
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Backends.DB = cnpgDBSpec()
			instance.Spec.Backends.DB.CNPG.Backup = tamossv1alpha1.DBCNPGBackupSpec{
				Enabled:         true,
				Schedule:        "0 0 2 * * *",
				RetentionPolicy: "30d",
				ObjectStore: tamossv1alpha1.DBCNPGObjectStoreSpec{
					EndpointURL:    "https://s3.example.com",
					Bucket:         "pg-backups",
					ExistingSecret: "backup-creds",
				},
			}
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			discoveryManager, _ := fakeDependencyDiscovery(true)
			controllerReconciler := &TamossReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Discovery: discoveryManager,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			cluster := &cnpgv1.Cluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-db", Namespace: "default"}, cluster)).To(Succeed())
			Expect(cluster.Spec.Backup).NotTo(BeNil())
			Expect(cluster.Spec.Backup.RetentionPolicy).To(Equal("30d"))
			scheduledBackup := &cnpgv1.ScheduledBackup{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-db-backup", Namespace: "default"}, scheduledBackup)).To(Succeed())
			Expect(scheduledBackup.Spec.Schedule).To(Equal("0 0 2 * * *"))
			Expect(scheduledBackup.Spec.Cluster.Name).To(Equal(resourceName + "-db"))
			Expect(scheduledBackup.Spec.BackupOwnerReference).To(Equal("cluster"))

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			backupReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionBackupPolicyReady)
			Expect(backupReady).NotTo(BeNil())
			Expect(backupReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(backupReady.Reason).To(Equal(operatorstatus.ReasonBackupArchivingUnknown))
		})

		It("should report misconfigured managed CNPG backup policy", func() {
			ensureCNPGClusterCRD(ctx)
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Backends.DB = cnpgDBSpec()
			instance.Spec.Backends.DB.CNPG.Backup = tamossv1alpha1.DBCNPGBackupSpec{
				Enabled: true,
				ObjectStore: tamossv1alpha1.DBCNPGObjectStoreSpec{
					Bucket: "pg-backups",
				},
			}
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			discoveryManager, _ := fakeDependencyDiscovery(true)
			controllerReconciler := &TamossReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Discovery: discoveryManager,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			backupReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionBackupPolicyReady)
			Expect(backupReady).NotTo(BeNil())
			Expect(backupReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(backupReady.Reason).To(Equal("BackupPolicyIncomplete"))
			Expect(backupReady.Message).To(ContainSubstring("schedule"))
			Expect(backupReady.Message).To(ContainSubstring("retentionPolicy"))
			backendsReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionBackendsReady)
			Expect(backendsReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(backendsReady.Reason).To(Equal("BackupPolicyIncomplete"))
			cluster := &cnpgv1.Cluster{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-db", Namespace: "default"}, cluster)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should wait for CNPG Secrets after the Cluster is ready", func() {
			ensureCNPGClusterCRD(ctx)
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Backends.DB = cnpgDBSpec()
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			discoveryManager, _ := fakeDependencyDiscovery(true)
			controllerReconciler := &TamossReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Discovery: discoveryManager,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			setCNPGClusterStatus(ctx, resourceName+"-db", "default", []metav1.Condition{{
				Type:    string(cnpgv1.ConditionClusterReady),
				Status:  metav1.ConditionTrue,
				Reason:  string(cnpgv1.ClusterReady),
				Message: "Cluster is ready",
			}})

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			backendsReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionBackendsReady)
			Expect(backendsReady).NotTo(BeNil())
			Expect(backendsReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(backendsReady.Reason).To(Equal("WaitingForCNPGSecret"))
			job := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-schema-migrate-" + schemaVersionForName(), Namespace: "default"}, job)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should launch schema migration only after CNPG is ready and Secrets exist", func() {
			ensureCNPGClusterCRD(ctx)
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Backends.DB = cnpgDBSpec()
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			discoveryManager, _ := fakeDependencyDiscovery(true)
			controllerReconciler := &TamossReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Discovery: discoveryManager,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			setCNPGClusterStatus(ctx, resourceName+"-db", "default", []metav1.Condition{{
				Type:    string(cnpgv1.ConditionClusterReady),
				Status:  metav1.ConditionTrue,
				Reason:  string(cnpgv1.ClusterReady),
				Message: "Cluster is ready",
			}})
			createCNPGSecrets(ctx, instance)

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			job := &batchv1.Job{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-schema-migrate-" + schemaVersionForName(), Namespace: "default"}, job)).To(Succeed())
			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(SatisfyAll(
				HaveField("Name", "POSTGRES_USER"),
				HaveField("ValueFrom.SecretKeyRef.LocalObjectReference.Name", cnpgbackend.SuperuserSecretName(instance)),
				HaveField("ValueFrom.SecretKeyRef.Key", "username"),
			)))
			Expect(container.Env).To(ContainElement(SatisfyAll(
				HaveField("Name", "POSTGRES_PASSWORD"),
				HaveField("ValueFrom.SecretKeyRef.LocalObjectReference.Name", cnpgbackend.SuperuserSecretName(instance)),
				HaveField("ValueFrom.SecretKeyRef.Key", "password"),
			)))
			apiDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-api", Namespace: "default"}, apiDeployment)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should surface CNPG status failures as warning Events", func() {
			ensureCNPGClusterCRD(ctx)
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Backends.DB = cnpgDBSpec()
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			discoveryManager, _ := fakeDependencyDiscovery(true)
			recorder := record.NewFakeRecorder(10)
			controllerReconciler := &TamossReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Recorder:  recorder,
				Discovery: discoveryManager,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			setCNPGClusterStatus(ctx, resourceName+"-db", "default", []metav1.Condition{
				{
					Type:    string(cnpgv1.ConditionClusterReady),
					Status:  metav1.ConditionFalse,
					Reason:  string(cnpgv1.ClusterIsNotReady),
					Message: "initializing",
				},
				{
					Type:    string(cnpgv1.ConditionContinuousArchiving),
					Status:  metav1.ConditionFalse,
					Reason:  "WALArchiveFailing",
					Message: "archive command failed",
				},
			})

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(operatorstatus.PhaseDegraded))
			Eventually(recorder.Events).Should(Receive(ContainSubstring(operatorstatus.ReasonBackupArchivingFailed)))
		})

		It("should create a RustFS Tenant and wait while it is not Ready", func() {
			ensureRustFSTenantCRD(ctx)
			instance := configureRustFSOperatorBackend(ctx, typeNamespacedName)

			discoveryManager, _ := fakeRustFSDependencyDiscovery()
			controllerReconciler := &TamossReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Discovery: discoveryManager,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			tenant := rustfs.NewTenant()
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: instance.ResourceName("s3"), Namespace: instance.Namespace}, tenant)).To(Succeed())
			Expect(hasTamossOwner(tenant.GetOwnerReferences(), resourceName)).To(BeTrue())
			job := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-s3-bucket-init", Namespace: "default"}, job)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			backendsReady := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionBackendsReady)
			Expect(backendsReady).NotTo(BeNil())
			Expect(backendsReady.Status).To(Equal(metav1.ConditionFalse))
			Expect(backendsReady.Reason).To(Equal("TenantNotReady"))
		})

		It("should emit the default StorageBackend after the Tenant is Ready", func() {
			ensureRustFSTenantCRD(ctx)
			instance := configureRustFSOperatorBackend(ctx, typeNamespacedName)

			discoveryManager, _ := fakeRustFSDependencyDiscovery()
			controllerReconciler := &TamossReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Discovery: discoveryManager,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			setRustFSTenantStatus(ctx, instance.ResourceName("s3"), instance.Namespace, []interface{}{
				map[string]interface{}{"type": "Ready", "status": "True", "reason": "Ready", "message": "Tenant is ready"},
			})

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			storageBackend := &tamossv1alpha1.StorageBackend{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-storage-default", Namespace: "default"}, storageBackend)).To(Succeed())
			Expect(hasTamossOwner(storageBackend.OwnerReferences, resourceName)).To(BeTrue())
			Expect(storageBackend.Spec.TamossRef.Name).To(Equal(resourceName))
			Expect(storageBackend.Spec.BucketName).To(Equal("tamoss"))
			Expect(storageBackend.Spec.Credentials.ExistingSecret).To(Equal(resourceName + "-s3-creds"))
		})

		It("should continue schema reconciliation when the default StorageBackend bucket is ready", func() {
			ensureRustFSTenantCRD(ctx)
			instance := configureRustFSOperatorBackend(ctx, typeNamespacedName)

			discoveryManager, _ := fakeRustFSDependencyDiscovery()
			controllerReconciler := &TamossReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Discovery: discoveryManager,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			setRustFSTenantStatus(ctx, instance.ResourceName("s3"), instance.Namespace, []interface{}{
				map[string]interface{}{"type": "Ready", "status": "True", "reason": "Ready", "message": "Tenant is ready"},
			})
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			storageBackend := &tamossv1alpha1.StorageBackend{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-storage-default", Namespace: "default"}, storageBackend)).To(Succeed())
			meta.SetStatusCondition(&storageBackend.Status.Conditions, metav1.Condition{
				Type:    operatorstatus.ConditionBucketReady,
				Status:  metav1.ConditionTrue,
				Reason:  operatorstatus.ReasonBucketReady,
				Message: "RustFS bucket has been created",
			})
			Expect(k8sClient.Status().Update(ctx, storageBackend)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			job := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-s3-bucket-init", Namespace: "default"}, job)
			Expect(errors.IsNotFound(err)).To(BeTrue())
			schemaJob := &batchv1.Job{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-schema-migrate-" + schemaVersionForName(), Namespace: "default"}, schemaJob)).To(Succeed())
		})

		It("should surface RustFS Tenant failures as warning Events", func() {
			ensureRustFSTenantCRD(ctx)
			instance := configureRustFSOperatorBackend(ctx, typeNamespacedName)

			discoveryManager, _ := fakeRustFSDependencyDiscovery()
			recorder := record.NewFakeRecorder(10)
			controllerReconciler := &TamossReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Recorder:  recorder,
				Discovery: discoveryManager,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			setRustFSTenantStatus(ctx, instance.ResourceName("s3"), instance.Namespace, []interface{}{
				map[string]interface{}{"type": "Ready", "status": "False", "reason": "TenantNotReady", "message": "Tenant is not ready"},
				map[string]interface{}{"type": "Degraded", "status": "True", "reason": "PoolFailed", "message": "pool failed"},
			})

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(operatorstatus.PhaseDegraded))
			Eventually(recorder.Events).Should(Receive(ContainSubstring(operatorstatus.ReasonTenantFailed)))
		})

		It("should correct Deployment drift and emit an event", func() {
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.API.Resources.Limits = corev1.ResourceList{
				corev1.ResourceMemory: resource2Gi("512Mi"),
			}
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			recorder := record.NewFakeRecorder(10)
			controllerReconciler := &TamossReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: recorder,
			}

			makeSchemaReady(ctx, controllerReconciler, typeNamespacedName, resourceName)
			instance = &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			operatorstatus.SetConditionBool(&instance.Status.Conditions, instance.Generation, operatorstatus.ConditionReady, true, "AllComponentsReady", "All components are ready")
			Expect(k8sClient.Status().Update(ctx, instance)).To(Succeed())

			apiDeployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-api", Namespace: "default"}, apiDeployment)).To(Succeed())
			apiDeployment.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
				corev1.ResourceMemory: resource2Gi("2Gi"),
			}
			Expect(k8sClient.Update(ctx, apiDeployment)).To(Succeed())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			corrected := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-api", Namespace: "default"}, corrected)).To(Succeed())
			Expect(corrected.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory]).To(Equal(resource2Gi("512Mi")))
			Eventually(recorder.Events).Should(Receive(ContainSubstring("DriftCorrected")))
		})

		It("should preserve a generated API token across reconciles", func() {
			controllerReconciler := &TamossReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			existing := &corev1.Secret{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-api-token", Namespace: "default"}, existing)
			if err == nil {
				Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
			} else {
				Expect(errors.IsNotFound(err)).To(BeTrue())
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-api-token", Namespace: "default"}, secret)).To(Succeed())
			firstToken := string(secret.Data[apiTokenKey])
			Expect(len(firstToken)).To(BeNumerically(">=", 32))

			makeSchemaReady(ctx, controllerReconciler, typeNamespacedName, resourceName)

			updated := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-api-token", Namespace: "default"}, updated)).To(Succeed())
			Expect(string(updated.Data[apiTokenKey])).To(Equal(firstToken))
		})

		It("should replace the generated API token when an explicit token is supplied", func() {
			controllerReconciler := &TamossReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			existing := &corev1.Secret{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-api-token", Namespace: "default"}, existing)
			if err == nil {
				Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
			} else {
				Expect(errors.IsNotFound(err)).To(BeTrue())
			}

			makeSchemaReady(ctx, controllerReconciler, typeNamespacedName, resourceName)

			before := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-api", Namespace: "default"}, before)).To(Succeed())
			previousChecksum := before.Spec.Template.Annotations["checksum/api-token-secret"]

			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Secrets.APIToken.Token = "user-supplied-token"
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-api-token", Namespace: "default"}, secret)).To(Succeed())
			Expect(string(secret.Data[apiTokenKey])).To(Equal("user-supplied-token"))

			after := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-api", Namespace: "default"}, after)).To(Succeed())
			Expect(after.Spec.Template.Annotations["checksum/api-token-secret"]).NotTo(Equal(previousChecksum))
		})

		It("should skip workload writes while paused", func() {
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.API.Resources.Limits = corev1.ResourceList{
				corev1.ResourceMemory: resource2Gi("512Mi"),
			}
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			controllerReconciler := &TamossReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			makeSchemaReady(ctx, controllerReconciler, typeNamespacedName, resourceName)

			apiDeployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-api", Namespace: "default"}, apiDeployment)).To(Succeed())
			apiDeployment.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
				corev1.ResourceMemory: resource2Gi("2Gi"),
			}
			Expect(k8sClient.Update(ctx, apiDeployment)).To(Succeed())

			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Paused = true
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			pausedDeployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-api", Namespace: "default"}, pausedDeployment)).To(Succeed())
			Expect(pausedDeployment.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory]).To(Equal(resource2Gi("2Gi")))

			paused := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, paused)).To(Succeed())
			condition := meta.FindStatusCondition(paused.Status.Conditions, operatorstatus.ConditionPaused)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(meta.FindStatusCondition(paused.Status.Conditions, operatorstatus.ConditionSchemaMigrated).Status).To(Equal(metav1.ConditionUnknown))
		})

		It("should write schema state after the migration Job succeeds", func() {
			controllerReconciler := &TamossReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			jobName := resourceName + "-schema-migrate-" + schemaVersionForName()
			job := &batchv1.Job{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, job)).To(Succeed())
			running := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, running)).To(Succeed())
			Expect(running.Status.SchemaMigration.Phase).To(Equal(operatorstatus.PhaseRunning))
			Expect(running.Status.SchemaMigration.Attempts).To(Equal(int32(1)))
			Expect(meta.FindStatusCondition(running.Status.Conditions, operatorstatus.ConditionUpgradeable).Status).To(Equal(metav1.ConditionUnknown))
			markJobStatusSucceeded(job)
			Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			state := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-schema-state", Namespace: "default"}, state)).To(Succeed())
			Expect(state.Data[schemaStateAppliedVersionKey]).To(Equal(schemabundle.SchemaVersion))
			Expect(state.Data[schemaStateJobUIDKey]).To(Equal(string(job.UID)))

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionSchemaMigrated).Status).To(Equal(metav1.ConditionTrue))
			Expect(meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionUpgradeable).Status).To(Equal(metav1.ConditionTrue))
			Expect(updated.Status.Upgrade.Phase).To(Equal(operatorstatus.PhaseUpgradeable))
			Expect(updated.Status.SchemaMigration.Phase).To(Equal(operatorstatus.PhaseSucceeded))
			Expect(updated.Status.SchemaMigration.LastAttemptResult).To(Equal(operatorstatus.PhaseSucceeded))
		})

		It("should degrade after three schema migration failures", func() {
			recorder := record.NewFakeRecorder(10)
			controllerReconciler := &TamossReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: recorder,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			jobName := resourceName + "-schema-migrate-" + schemaVersionForName()
			job := &batchv1.Job{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, job)).To(Succeed())
			markJobStatusFailed(job)
			Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Eventually(recorder.Events).Should(Receive(ContainSubstring("observed failed attempt 1 of 3")))

			for i := 0; i < 2; i++ {
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			}

			state := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-schema-state", Namespace: "default"}, state)).To(Succeed())
			Expect(state.Data[schemaStateFailureCountKey]).To(Equal("3"))

			for i := 0; i < 3; i++ {
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-schema-state", Namespace: "default"}, state)).To(Succeed())
			Expect(state.Data[schemaStateFailureCountKey]).To(Equal("3"))

			updated := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			degraded := meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionDegraded)
			Expect(degraded).NotTo(BeNil())
			Expect(degraded.Status).To(Equal(metav1.ConditionTrue))
			Expect(degraded.Reason).To(Equal("SchemaMigrationFailed"))
			Expect(updated.Status.Phase).To(Equal(operatorstatus.PhaseDegraded))
			Expect(meta.FindStatusCondition(updated.Status.Conditions, operatorstatus.ConditionUpgradeable).Status).To(Equal(metav1.ConditionFalse))
			Expect(updated.Status.Upgrade.Phase).To(Equal(operatorstatus.PhaseBlocked))
			Expect(updated.Status.SchemaMigration.Phase).To(Equal(operatorstatus.PhaseFailed))
			Expect(updated.Status.SchemaMigration.LastAttemptResult).To(Equal(operatorstatus.PhaseFailed))
			Expect(updated.Status.SchemaMigration.Attempts).To(Equal(int32(3)))
		})

		It("should apply fixtures only during first schema bootstrap", func() {
			instance := &tamossv1alpha1.Tamoss{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, instance)).To(Succeed())
			instance.Spec.Backends.DB.ApplyFixtures = ptr.To(true)
			Expect(k8sClient.Update(ctx, instance)).To(Succeed())

			controllerReconciler := &TamossReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			jobName := resourceName + "-schema-migrate-" + schemaVersionForName()
			job := &batchv1.Job{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, job)).To(Succeed())
			Expect(job.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--apply-fixtures"))
			completeSchemaMigration(ctx, controllerReconciler, typeNamespacedName, job)

			state := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-schema-state", Namespace: "default"}, state)).To(Succeed())
			Expect(state.Data[schemaStateFixturesKey]).To(Equal("true"))
		})
	})
})

func minimalTamossSpec() tamossv1alpha1.TamossSpec {
	return tamossv1alpha1.TamossSpec{
		Backends: tamossv1alpha1.BackendsSpec{
			DB: tamossv1alpha1.DBBackendSpec{
				ProvidedBy: tamossv1alpha1.BackendProvidedByExternal,
				External: &tamossv1alpha1.DBExternalSpec{
					Host:     "postgresql",
					Port:     "5432",
					Database: "tams",
					Auth: tamossv1alpha1.SecretReferenceSpec{
						ExistingSecret: "tams-postgresql-auth",
						SecretKeys: tamossv1alpha1.SecretKeySpec{
							Username: "username",
							Password: "password",
						},
					},
				},
			},
			S3: tamossv1alpha1.S3BackendSpec{
				ProvidedBy: tamossv1alpha1.S3BackendProvidedByExternal,
				External: &tamossv1alpha1.S3ExternalSpec{
					Endpoint: tamossv1alpha1.S3EndpointSpec{
						Default: tamossv1alpha1.EndpointURLSpec{
							URL: "http://rustfs-svc:9000",
						},
					},
					Bucket: "tamoss",
					Auth: tamossv1alpha1.SecretReferenceSpec{
						ExistingSecret: "tams-rustfs-auth",
						SecretKeys: tamossv1alpha1.SecretKeySpec{
							AccessKey: "RUSTFS_ACCESS_KEY",
							SecretKey: "RUSTFS_SECRET_KEY",
						},
					},
				},
			},
		},
	}
}

func validStorageBackend(name string) *tamossv1alpha1.StorageBackend {
	return &tamossv1alpha1.StorageBackend{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: tamossv1alpha1.StorageBackendSpec{
			ID:         "11111111-1111-5111-8111-111111111111",
			TamossRef:  tamossv1alpha1.TamossReferenceSpec{Name: "tamoss-kind"},
			Provider:   tamossv1alpha1.StorageBackendProviderExternalS3,
			BucketName: "archive",
			Endpoint: tamossv1alpha1.S3EndpointSpec{
				Default: tamossv1alpha1.EndpointURLSpec{URL: "https://s3.example.com"},
			},
			Credentials: tamossv1alpha1.SecretReferenceSpec{
				ExistingSecret: "archive-s3-creds",
				SecretKeys: tamossv1alpha1.SecretKeySpec{
					AccessKey: "accessKeyID",
					SecretKey: "secretAccessKey",
				},
			},
		},
	}
}

func cnpgDBSpec() tamossv1alpha1.DBBackendSpec {
	return tamossv1alpha1.DBBackendSpec{
		ProvidedBy: tamossv1alpha1.BackendProvidedByCNPG,
		CNPG: &tamossv1alpha1.DBCNPGSpec{
			Instances:       3,
			PostgresVersion: "16",
			Storage: tamossv1alpha1.BackendStorageSpec{
				Size: "50Gi",
			},
		},
	}
}

func fakeDependencyDiscovery(withCNPG bool) (*operatordiscovery.Manager, *fakediscovery.FakeDiscovery) {
	client := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}
	if withCNPG {
		client.Resources = cnpgAPIResources()
	}
	manager := operatordiscovery.NewManager(client, []schema.GroupVersionResource{operatordiscovery.CNPGClustersGVR})
	_ = manager.Refresh(context.Background())
	return manager, client
}

func cnpgAPIResources() []*metav1.APIResourceList {
	return []*metav1.APIResourceList{{
		GroupVersion: "postgresql.cnpg.io/v1",
		APIResources: []metav1.APIResource{{
			Name: "clusters",
		}},
	}}
}

func fakeRustFSDependencyDiscovery() (*operatordiscovery.Manager, *fakediscovery.FakeDiscovery) {
	client := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}
	client.Resources = rustfsAPIResources()
	manager := operatordiscovery.NewManager(client, []schema.GroupVersionResource{operatordiscovery.RustFSTenantsGVR})
	_ = manager.Refresh(context.Background())
	return manager, client
}

func rustfsAPIResources() []*metav1.APIResourceList {
	return []*metav1.APIResourceList{{
		GroupVersion: "rustfs.com/v1alpha1",
		APIResources: []metav1.APIResource{{
			Name: "tenants",
		}},
	}}
}

func ensureBackendSecrets(ctx context.Context, namespace string) {
	secrets := []corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "tams-postgresql-auth", Namespace: namespace},
			Data: map[string][]byte{
				"username": []byte("tams"),
				"password": []byte("tams"),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "tams-rustfs-auth", Namespace: namespace},
			Data: map[string][]byte{
				"RUSTFS_ACCESS_KEY": []byte("access-key"),
				"RUSTFS_SECRET_KEY": []byte("secret-key"),
			},
		},
	}
	for i := range secrets {
		secret := secrets[i]
		existing := &corev1.Secret{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, existing)
		if errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, &secret)).To(Succeed())
			continue
		}
		Expect(err).NotTo(HaveOccurred())
		existing.Data = secret.Data
		Expect(k8sClient.Update(ctx, existing)).To(Succeed())
	}
}

func ensureNamespace(ctx context.Context, namespace string) {
	err := k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
	if err != nil {
		Expect(errors.IsAlreadyExists(err)).To(BeTrue())
	}
}

type authentikManagedTestServer struct {
	*httptest.Server
	mu               sync.Mutex
	blueprint        authentikbackend.ManagedBlueprint
	appliedBlueprint int
}

func authentikManagedServer() *authentikManagedTestServer {
	server := &authentikManagedTestServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v3/managed/blueprints/") {
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			server.handleBlueprintAPI(w, r)
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/.well-known/openid-configuration") {
			baseURL := fmt.Sprintf("http://%s", r.Host)
			if _, err := fmt.Fprintf(
				w,
				`{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`,
				baseURL+r.URL.Path,
				baseURL+"/authorize",
				baseURL+"/token",
				baseURL+"/jwks",
			); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		http.NotFound(w, r)
	}))
	return server
}

func (s *authentikManagedTestServer) handleBlueprintAPI(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v3/managed/blueprints/":
		results := []authentikbackend.ManagedBlueprint{}
		if s.blueprint.PK != "" {
			results = append(results, s.blueprint)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v3/managed/blueprints/":
		var request struct {
			Name    string `json:"name"`
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		Expect(json.NewDecoder(r.Body).Decode(&request)).To(Succeed())
		s.blueprint = authentikbackend.ManagedBlueprint{
			PK:      "blueprint-id",
			Name:    request.Name,
			Path:    request.Path,
			Content: request.Content,
			Status:  "unknown",
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(s.blueprint)
	case r.Method == http.MethodPut && r.URL.Path == "/api/v3/managed/blueprints/blueprint-id/":
		var request struct {
			Content string `json:"content"`
		}
		Expect(json.NewDecoder(r.Body).Decode(&request)).To(Succeed())
		s.blueprint.Content = request.Content
		_ = json.NewEncoder(w).Encode(s.blueprint)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v3/managed/blueprints/blueprint-id/apply/":
		s.appliedBlueprint++
		s.blueprint.Status = "successful"
		_ = json.NewEncoder(w).Encode(s.blueprint)
	case r.Method == http.MethodDelete && r.URL.Path == "/api/v3/managed/blueprints/blueprint-id/":
		s.blueprint = authentikbackend.ManagedBlueprint{}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (s *authentikManagedTestServer) applied() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appliedBlueprint
}

func (s *authentikManagedTestServer) content() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blueprint.Content
}

func ensureAuthentikTokenSecret(ctx context.Context, namespace string) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      authentikbackend.DefaultAPITokenSecretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			authentikbackend.DefaultAPITokenSecretKey: []byte("test-token"),
		},
	}
	existing := &corev1.Secret{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: namespace}, existing)
	if errors.IsNotFound(err) {
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		return
	}
	Expect(err).NotTo(HaveOccurred())
	existing.Data = secret.Data
	Expect(k8sClient.Update(ctx, existing)).To(Succeed())
}

func configureAuthentikIdentity(ctx context.Context, name types.NamespacedName, platformNamespace, issuerURL string, redirectURIs []string) *tamossv1alpha1.Tamoss {
	instance := &tamossv1alpha1.Tamoss{}
	Expect(k8sClient.Get(ctx, name, instance)).To(Succeed())
	instance.Spec.Auth = tamossv1alpha1.AuthSpec{
		ProvidedBy: tamossv1alpha1.AuthProvidedByAuthentikBlueprints,
		Required:   true,
		AuthentikBlueprints: &tamossv1alpha1.AuthentikBlueprintsSpec{
			PlatformNamespace: platformNamespace,
			IssuerURL:         issuerURL,
			RedirectURIs:      append([]string(nil), redirectURIs...),
		},
	}
	instance.Spec.Ingress.UI.Web.Host = ""
	instance.Spec.HTTPRoute.UI.Hostnames = nil
	Expect(k8sClient.Update(ctx, instance)).To(Succeed())
	Expect(k8sClient.Get(ctx, name, instance)).To(Succeed())
	return instance
}

func hasTamossOwner(refs []metav1.OwnerReference, name string) bool {
	for _, ref := range refs {
		if ref.APIVersion == tamossv1alpha1.GroupVersion.String() && ref.Kind == "Tamoss" && ref.Name == name {
			return true
		}
	}
	return false
}

func resource2Gi(value string) resource.Quantity {
	return resource.MustParse(value)
}

func cleanupTamossArtifacts(ctx context.Context, name, namespace string) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	options := []client.DeleteAllOfOption{
		client.InNamespace(namespace),
		client.MatchingLabels{"app.kubernetes.io/instance": name},
	}
	for _, obj := range []client.Object{
		&appsv1.Deployment{},
		&batchv1.Job{},
		&corev1.ConfigMap{},
		&corev1.Secret{},
		&corev1.Service{},
		&corev1.ServiceAccount{},
		&networkingv1.Ingress{},
		&networkingv1.NetworkPolicy{},
		&policyv1.PodDisruptionBudget{},
		&autoscalingv2.HorizontalPodAutoscaler{},
		&tamossv1alpha1.StorageBackend{},
	} {
		_ = k8sClient.DeleteAllOf(ctx, obj, options...)
	}
	httpRoute := &unstructured.Unstructured{}
	httpRoute.SetGroupVersionKind(httpRouteGVK)
	_ = k8sClient.DeleteAllOf(ctx, httpRoute, options...)
	tenant := rustfs.NewTenant()
	tenant.SetName(name + "-s3")
	tenant.SetNamespace(namespace)
	_ = k8sClient.Delete(ctx, tenant)
	_ = k8sClient.Delete(ctx, &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: name + "-db", Namespace: namespace}})
	_ = k8sClient.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: cnpgbackend.AppSecretName(tamoss), Namespace: namespace}})
	_ = k8sClient.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: cnpgbackend.SuperuserSecretName(tamoss), Namespace: namespace}})
	Eventually(func() bool {
		probe := rustfs.NewTenant()
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name + "-s3", Namespace: namespace}, probe)
		return errors.IsNotFound(err) || meta.IsNoMatchError(err)
	}, "5s").Should(BeTrue())
	for _, cmName := range []string{name + "-schema-state"} {
		_ = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: namespace}})
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: namespace}, &corev1.ConfigMap{})
			return errors.IsNotFound(err)
		}).Should(BeTrue())
	}
	for _, jobName := range []string{name + "-schema-migrate-" + schemaVersionForName(), name + "-s3-bucket-init"} {
		job := &batchv1.Job{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: namespace}, job); err == nil {
			if len(job.Finalizers) > 0 {
				job.Finalizers = nil
				_ = k8sClient.Update(ctx, job)
			}
		}
		propagation := metav1.DeletePropagationBackground
		_ = k8sClient.Delete(ctx, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: namespace}}, client.PropagationPolicy(propagation))
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: namespace}, &batchv1.Job{})
			return errors.IsNotFound(err)
		}, "5s").Should(BeTrue())
	}
}

func cleanupAuthentikBlueprintArtifacts(ctx context.Context, tamossNamespace, tamossName string, platformNamespaces ...string) {
	name := fmt.Sprintf("tamoss-%s-%s-blueprint", tamossNamespace, tamossName)
	for _, platformNamespace := range platformNamespaces {
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: platformNamespace}}
		_ = k8sClient.Delete(ctx, secret)
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: platformNamespace}, &corev1.Secret{})
			return errors.IsNotFound(err)
		}, "5s").Should(BeTrue())
		tokenSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: authentikbackend.DefaultAPITokenSecretName, Namespace: platformNamespace}}
		_ = k8sClient.Delete(ctx, tokenSecret)
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: authentikbackend.DefaultAPITokenSecretName, Namespace: platformNamespace}, &corev1.Secret{})
			return errors.IsNotFound(err)
		}, "5s").Should(BeTrue())
	}
}

func makeSchemaReady(ctx context.Context, reconciler *TamossReconciler, name types.NamespacedName, resourceName string) {
	_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
	Expect(err).NotTo(HaveOccurred())

	job := &batchv1.Job{}
	err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-schema-migrate-" + schemaVersionForName(), Namespace: name.Namespace}, job)
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	completeSchemaMigration(ctx, reconciler, name, job)
}

// markJobStatusSucceeded stamps the full status a finished Job carries on a
// real cluster. Since Kubernetes 1.32 the apiserver validates Job status
// transitions strictly: Complete=True requires SuccessCriteriaMet plus
// startTime and completionTime.
func markJobStatusSucceeded(job *batchv1.Job) {
	now := metav1.Now()
	if job.Status.StartTime == nil {
		job.Status.StartTime = &now
	}
	job.Status.Succeeded = 1
	job.Status.CompletionTime = &now
	job.Status.Conditions = []batchv1.JobCondition{
		{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue, Reason: "CompletionsReached"},
		{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, Reason: "CompletionsReached"},
	}
}

// markJobStatusFailed mirrors markJobStatusSucceeded for failed Jobs:
// Failed=True requires a FailureTarget condition and a startTime.
func markJobStatusFailed(job *batchv1.Job) {
	now := metav1.Now()
	if job.Status.StartTime == nil {
		job.Status.StartTime = &now
	}
	job.Status.Failed = 1
	job.Status.Conditions = []batchv1.JobCondition{
		{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded"},
		{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded"},
	}
}

func completeSchemaMigration(ctx context.Context, reconciler *TamossReconciler, name types.NamespacedName, job *batchv1.Job) {
	markJobStatusSucceeded(job)
	Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())

	_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
	Expect(err).NotTo(HaveOccurred())
}

func configureRustFSOperatorBackend(ctx context.Context, name types.NamespacedName) *tamossv1alpha1.Tamoss {
	instance := &tamossv1alpha1.Tamoss{}
	Expect(k8sClient.Get(ctx, name, instance)).To(Succeed())
	instance.Spec.Backends.S3 = tamossv1alpha1.S3BackendSpec{
		ProvidedBy: tamossv1alpha1.S3BackendProvidedByRustFSOperator,
		RustFSOperator: &tamossv1alpha1.S3RustFSOperatorSpec{
			Bucket: tamossv1alpha1.S3RustFSOperatorBucketSpec{
				Name:            "tamoss",
				CreateIfMissing: true,
			},
		},
	}
	Expect(k8sClient.Update(ctx, instance)).To(Succeed())
	Expect(k8sClient.Get(ctx, name, instance)).To(Succeed())
	return instance
}

func createCNPGSecrets(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) {
	secrets := []corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{Name: cnpgbackend.AppSecretName(tamoss), Namespace: tamoss.Namespace},
			Data: map[string][]byte{
				"username": []byte("tams"),
				"password": []byte("tams"),
				"host":     []byte(tamoss.ResourceName("db-rw")),
				"port":     []byte("5432"),
				"dbname":   []byte("tams"),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: cnpgbackend.SuperuserSecretName(tamoss), Namespace: tamoss.Namespace},
			Data: map[string][]byte{
				"username": []byte("postgres"),
				"password": []byte("postgres"),
			},
		},
	}
	for i := range secrets {
		secret := secrets[i]
		err := k8sClient.Create(ctx, &secret)
		if err != nil {
			Expect(errors.IsAlreadyExists(err)).To(BeTrue())
		}
	}
}

func ensureCNPGClusterCRD(ctx context.Context) {
	ensureTestCRD(ctx, testCRD{
		Name:     "clusters.postgresql.cnpg.io",
		Group:    "postgresql.cnpg.io",
		Plural:   "clusters",
		Singular: "cluster",
		Kind:     "Cluster",
		ListKind: "ClusterList",
	})
}

func ensureCNPGScheduledBackupCRD(ctx context.Context) {
	ensureTestCRD(ctx, testCRD{
		Name:     "scheduledbackups.postgresql.cnpg.io",
		Group:    "postgresql.cnpg.io",
		Plural:   "scheduledbackups",
		Singular: "scheduledbackup",
		Kind:     "ScheduledBackup",
		ListKind: "ScheduledBackupList",
	})
}

type testCRD struct {
	Name     string
	Group    string
	Plural   string
	Singular string
	Kind     string
	ListKind string
}

func ensureTestCRD(ctx context.Context, spec testCRD) {
	preserveUnknownFields := true
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: spec.Group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   spec.Plural,
				Singular: spec.Singular,
				Kind:     spec.Kind,
				ListKind: spec.ListKind,
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type:                   "object",
						XPreserveUnknownFields: &preserveUnknownFields,
					},
				},
				Subresources: &apiextensionsv1.CustomResourceSubresources{
					Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
				},
			}},
		},
	}
	err := k8sClient.Create(ctx, crd)
	if err != nil {
		Expect(errors.IsAlreadyExists(err)).To(BeTrue())
	}
	Eventually(func() bool {
		current := &apiextensionsv1.CustomResourceDefinition{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: spec.Name}, current); err != nil {
			return false
		}
		for _, condition := range current.Status.Conditions {
			if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
				return true
			}
		}
		return false
	}, "5s").Should(BeTrue())
}

func setCNPGClusterStatus(ctx context.Context, name, namespace string, conditions []metav1.Condition) {
	cluster := &cnpgv1.Cluster{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, cluster)).To(Succeed())
	for i := range conditions {
		if conditions[i].LastTransitionTime.IsZero() {
			conditions[i].LastTransitionTime = metav1.Now()
		}
	}
	cluster.Status.Conditions = conditions
	Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())
}

func ensureRustFSTenantCRD(ctx context.Context) {
	preserveUnknownFields := true
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "tenants.rustfs.com"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "rustfs.com",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "tenants",
				Singular: "tenant",
				Kind:     "Tenant",
				ListKind: "TenantList",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1alpha1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type:                   "object",
						XPreserveUnknownFields: &preserveUnknownFields,
					},
				},
				Subresources: &apiextensionsv1.CustomResourceSubresources{
					Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
				},
			}},
		},
	}
	err := k8sClient.Create(ctx, crd)
	if err != nil {
		Expect(errors.IsAlreadyExists(err)).To(BeTrue())
	}
	Eventually(func() bool {
		current := &apiextensionsv1.CustomResourceDefinition{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: "tenants.rustfs.com"}, current); err != nil {
			return false
		}
		for _, condition := range current.Status.Conditions {
			if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
				return true
			}
		}
		return false
	}, "5s").Should(BeTrue())
}

func setRustFSTenantStatus(ctx context.Context, name, namespace string, conditions []interface{}) {
	tenant := rustfs.NewTenant()
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, tenant)).To(Succeed())
	Expect(unstructured.SetNestedField(tenant.Object, int64(1), "status", "availableReplicas")).To(Succeed())
	Expect(unstructured.SetNestedField(tenant.Object, "Ready", "status", "currentState")).To(Succeed())
	Expect(unstructured.SetNestedSlice(tenant.Object, []interface{}{}, "status", "pools")).To(Succeed())
	Expect(unstructured.SetNestedSlice(tenant.Object, conditions, "status", "conditions")).To(Succeed())
	Expect(k8sClient.Status().Update(ctx, tenant)).To(Succeed())
}

func minimalTamossUnstructured(name string) *unstructured.Unstructured {
	resource := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"backends": map[string]interface{}{
					"db": map[string]interface{}{
						"providedBy": "external",
						"external": map[string]interface{}{
							"host":     "postgresql",
							"port":     "5432",
							"database": "tams",
							"auth": map[string]interface{}{
								"existingSecret": "tams-postgresql-auth",
								"secretKeys": map[string]interface{}{
									"username": "username",
									"password": "password",
								},
							},
						},
					},
					"s3": map[string]interface{}{
						"providedBy": "external",
						"external": map[string]interface{}{
							"endpoint": map[string]interface{}{
								"default": map[string]interface{}{
									"url": "http://rustfs-svc:9000",
								},
							},
							"bucket": "tamoss",
							"auth": map[string]interface{}{
								"existingSecret": "tams-rustfs-auth",
								"secretKeys": map[string]interface{}{
									"accessKey": "RUSTFS_ACCESS_KEY",
									"secretKey": "RUSTFS_SECRET_KEY",
								},
							},
						},
					},
				},
			},
		},
	}
	resource.SetAPIVersion("tamoss.livewyer.io/v1alpha1")
	resource.SetKind("Tamoss")
	resource.SetName(name)
	resource.SetNamespace("default")
	return resource
}
