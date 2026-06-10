package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("applyManagedObject", func() {
	ctx := context.Background()

	desiredService := func(name string) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				Labels:    map[string]string{"app.kubernetes.io/name": "tamoss"},
			},
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeClusterIP,
				Selector: map[string]string{"app.kubernetes.io/component": "api"},
				Ports: []corev1.ServicePort{{
					Name:       "http",
					Port:       8000,
					TargetPort: intstr.FromString("http"),
				}},
			},
		}
	}

	desiredDeployment := func(name string) *appsv1.Deployment {
		replicas := int32(1)
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				Labels:    map[string]string{"app.kubernetes.io/name": "tamoss"},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "api",
							Image: "example:latest",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{corev1.ResourceMemory: resource2Gi("512Mi")},
							},
						}},
					},
				},
			},
		}
	}

	It("creates absent objects and reports unchanged re-applies", func() {
		result, err := applyManagedObject(ctx, k8sClient, desiredService("apply-create"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Created).To(BeTrue())
		Expect(result.Changed).To(BeTrue())

		result, err = applyManagedObject(ctx, k8sClient, desiredService("apply-create"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Created).To(BeFalse())
		Expect(result.Changed).To(BeFalse())

		result, err = applyManagedObject(ctx, k8sClient, desiredDeployment("apply-create"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Created).To(BeTrue())

		result, err = applyManagedObject(ctx, k8sClient, desiredDeployment("apply-create"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Changed).To(BeFalse())
	})

	It("forces conflicting field drift back to the desired state", func() {
		_, err := applyManagedObject(ctx, k8sClient, desiredDeployment("apply-conflict"))
		Expect(err).NotTo(HaveOccurred())

		live := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "apply-conflict", Namespace: "default"}, live)).To(Succeed())
		live.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{corev1.ResourceMemory: resource2Gi("2Gi")}
		Expect(k8sClient.Update(ctx, live)).To(Succeed())

		result, err := applyManagedObject(ctx, k8sClient, desiredDeployment("apply-conflict"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Changed).To(BeTrue())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "apply-conflict", Namespace: "default"}, live)).To(Succeed())
		Expect(live.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory]).To(Equal(resource2Gi("512Mi")))
	})

	It("prunes foreign list additions while preserving allocated Service fields", func() {
		_, err := applyManagedObject(ctx, k8sClient, desiredService("apply-prune"))
		Expect(err).NotTo(HaveOccurred())

		live := &corev1.Service{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "apply-prune", Namespace: "default"}, live)).To(Succeed())
		allocatedClusterIP := live.Spec.ClusterIP
		Expect(allocatedClusterIP).NotTo(BeEmpty())

		patch := []byte(`[{"op":"add","path":"/spec/ports/-","value":{"name":"drift","port":9999,"protocol":"TCP","targetPort":"http"}}]`)
		Expect(k8sClient.Patch(ctx, live, client.RawPatch(types.JSONPatchType, patch), client.FieldOwner("kubectl-patch"))).To(Succeed())

		result, err := applyManagedObject(ctx, k8sClient, desiredService("apply-prune"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Changed).To(BeTrue())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "apply-prune", Namespace: "default"}, live)).To(Succeed())
		Expect(live.Spec.Ports).To(HaveLen(1))
		Expect(live.Spec.Ports[0].Port).To(Equal(int32(8000)))
		Expect(live.Spec.ClusterIP).To(Equal(allocatedClusterIP))

		result, err = applyManagedObject(ctx, k8sClient, desiredService("apply-prune"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Changed).To(BeFalse())
	})

	It("leaves fields owned by other managers outside authoritative sections alone", func() {
		_, err := applyManagedObject(ctx, k8sClient, desiredDeployment("apply-unmanaged"))
		Expect(err).NotTo(HaveOccurred())

		live := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "apply-unmanaged", Namespace: "default"}, live)).To(Succeed())
		annotated := live.DeepCopy()
		annotated.Annotations = map[string]string{"example.com/keep": "true"}
		Expect(k8sClient.Patch(ctx, annotated, client.MergeFrom(live), client.FieldOwner("kubectl-annotate"))).To(Succeed())

		drift := []byte(`[{"op":"replace","path":"/spec/template/spec/containers/0/resources/limits/memory","value":"2Gi"}]`)
		Expect(k8sClient.Patch(ctx, live, client.RawPatch(types.JSONPatchType, drift), client.FieldOwner("kubectl-patch"))).To(Succeed())

		result, err := applyManagedObject(ctx, k8sClient, desiredDeployment("apply-unmanaged"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Changed).To(BeTrue())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "apply-unmanaged", Namespace: "default"}, live)).To(Succeed())
		Expect(live.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory]).To(Equal(resource2Gi("512Mi")))
		Expect(live.Annotations).To(HaveKeyWithValue("example.com/keep", "true"))
	})

	It("takes ownership of objects created by older operator versions without losing allocations", func() {
		// Simulate the pre-SSA operator: a Create attributes every field,
		// including the allocated clusterIP, to an update-operation manager.
		legacy := desiredService("apply-migrate")
		legacy.Spec.Ports = append(legacy.Spec.Ports, corev1.ServicePort{
			Name:       "stale",
			Port:       9000,
			TargetPort: intstr.FromString("http"),
		})
		Expect(k8sClient.Create(ctx, legacy, client.FieldOwner("manager"))).To(Succeed())

		live := &corev1.Service{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "apply-migrate", Namespace: "default"}, live)).To(Succeed())
		allocatedClusterIP := live.Spec.ClusterIP
		Expect(allocatedClusterIP).NotTo(BeEmpty())

		result, err := applyManagedObject(ctx, k8sClient, desiredService("apply-migrate"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Changed).To(BeTrue())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "apply-migrate", Namespace: "default"}, live)).To(Succeed())
		Expect(live.Spec.Ports).To(HaveLen(1))
		Expect(live.Spec.ClusterIP).To(Equal(allocatedClusterIP))
	})

	It("reverts out-of-band Secret data edits", func() {
		desiredSecret := func() *corev1.Secret {
			return &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "apply-secret", Namespace: "default"},
				Type:       corev1.SecretTypeOpaque,
				StringData: map[string]string{"POSTGRES_HOST": "db.internal"},
			}
		}
		_, err := applyManagedObject(ctx, k8sClient, desiredSecret())
		Expect(err).NotTo(HaveOccurred())

		live := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "apply-secret", Namespace: "default"}, live)).To(Succeed())
		drifted := live.DeepCopy()
		drifted.Data["POSTGRES_HOST"] = []byte("attacker.example.com")
		drifted.Data["EXTRA"] = []byte("drift")
		Expect(k8sClient.Patch(ctx, drifted, client.MergeFrom(live), client.FieldOwner("kubectl-edit"))).To(Succeed())

		result, err := applyManagedObject(ctx, k8sClient, desiredSecret())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Changed).To(BeTrue())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "apply-secret", Namespace: "default"}, live)).To(Succeed())
		Expect(live.Data).To(HaveKeyWithValue("POSTGRES_HOST", []byte("db.internal")))
		Expect(live.Data).NotTo(HaveKey("EXTRA"))
	})
})
