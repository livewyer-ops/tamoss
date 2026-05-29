package controller

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestApplyAdvancedResourcePatchesUpdatesTypedResource(t *testing.T) {
	tamoss := advancedPatchTamossFixture()
	tamoss.Spec.Advanced.ResourcePatches = []tamossv1alpha1.AdvancedResourcePatch{{
		Target: tamossv1alpha1.AdvancedResourcePatchTarget{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "example-api",
		},
		Patch: apiextensionsv1.JSON{
			Raw: []byte(`{"metadata":{"labels":{"platform.example.com/tier":"edge"}},"spec":{"template":{"metadata":{"annotations":{"platform.example.com/profile":"high-throughput"}}}}}`),
		},
	}}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "example-api", Namespace: "tams"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "example"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "api", Image: "example:dev"}},
				},
			},
		},
	}

	if err := applyAdvancedResourcePatches(tamoss, deployment); err != nil {
		t.Fatalf("apply advanced patch: %v", err)
	}
	if got := deployment.Labels["platform.example.com/tier"]; got != "edge" {
		t.Fatalf("expected deployment label from advanced patch, got %q", got)
	}
	if got := deployment.Spec.Template.Annotations["platform.example.com/profile"]; got != "high-throughput" {
		t.Fatalf("expected pod annotation from advanced patch, got %q", got)
	}
}

func TestApplyAdvancedResourcePatchesUpdatesUnstructuredResource(t *testing.T) {
	tamoss := advancedPatchTamossFixture()
	tamoss.Spec.Advanced.ResourcePatches = []tamossv1alpha1.AdvancedResourcePatch{{
		Target: tamossv1alpha1.AdvancedResourcePatchTarget{
			APIVersion: "rustfs.com/v1alpha1",
			Kind:       "Tenant",
			Name:       "example-s3",
		},
		Patch: apiextensionsv1.JSON{Raw: []byte(`{"spec":{"image":"rustfs.example.com/rustfs:custom"}}`)},
	}}
	tenant := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "rustfs.com/v1alpha1",
		"kind":       "Tenant",
		"metadata": map[string]interface{}{
			"name":      "example-s3",
			"namespace": "tams",
		},
		"spec": map[string]interface{}{"pools": []interface{}{}},
	}}

	if err := applyAdvancedResourcePatches(tamoss, tenant); err != nil {
		t.Fatalf("apply advanced patch: %v", err)
	}
	if got, _, _ := unstructured.NestedString(tenant.Object, "spec", "image"); got != "rustfs.example.com/rustfs:custom" {
		t.Fatalf("expected patched Tenant image, got %q", got)
	}
}

func TestApplyAdvancedResourcePatchesRejectsIdentityChanges(t *testing.T) {
	tamoss := advancedPatchTamossFixture()
	tamoss.Spec.Advanced.ResourcePatches = []tamossv1alpha1.AdvancedResourcePatch{{
		Target: tamossv1alpha1.AdvancedResourcePatchTarget{Kind: "Deployment", Name: "example-api"},
		Patch:  apiextensionsv1.JSON{Raw: []byte(`{"metadata":{"name":"other"}}`)},
	}}
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "example-api", Namespace: "tams"}}

	if err := applyAdvancedResourcePatches(tamoss, deployment); err == nil {
		t.Fatal("expected identity patch to be rejected")
	}
}

func advancedPatchTamossFixture() *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "tams",
		},
	}
}
