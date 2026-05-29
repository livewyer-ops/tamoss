package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatordiscovery "github.com/livewyer-ops/tamoss/operator/internal/discovery"
)

func TestStorageBackendCredentialSecretRequestsFilterReferencingBackends(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	storageBackend := storageBackendFixture()
	other := storageBackendFixture()
	other.Name = "other"
	other.Spec.Credentials.ExistingSecret = "other-secret"
	reconciler := StorageBackendReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(storageBackend, other).
			Build(),
		Scheme: scheme,
	}

	requests := reconciler.storageBackendCredentialSecretRequests(ctx, secretObject("archive-s3", "media"))

	if len(requests) != 1 || requests[0].Name != "archive" || requests[0].Namespace != "media" {
		t.Fatalf("expected archive StorageBackend request, got %#v", requests)
	}
}

func TestTamossOwnedObjectListUsesManagedLabels(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add appsv1 scheme: %v", err)
	}
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media", UID: types.UID("tamoss-uid")},
	}
	owned := deploymentObject("owned", "media", tamossManagedLabelSelector(tamoss), tamossOwnerReference(tamoss))
	ownerOnly := deploymentObject("owner-only", "media", nil, tamossOwnerReference(tamoss))
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(owned, ownerOnly).
		Build()

	list := &appsv1.DeploymentList{}
	if err := listTamossOwnedObjects(ctx, client, tamoss, list); err != nil {
		t.Fatalf("expected managed-label list to succeed: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != "owned" {
		t.Fatalf("expected managed-label list to include only labelled Deployment, got %#v", list.Items)
	}
}

func TestOptionalWatchPoliciesIncludeHTTPRouteManagedList(t *testing.T) {
	policies := optionalTamossWatchPolicies()
	found := false
	for _, policy := range policies {
		if policy.gvr == operatordiscovery.GatewayHTTPRoutesGVR {
			found = true
			if policy.object == nil || policy.list == nil {
				t.Fatal("expected HTTPRoute policy to carry object and list types")
			}
		}
	}
	if !found {
		t.Fatal("expected HTTPRoute to participate in optional managed-resource policy")
	}
}

func secretObject(name, namespace string) *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
}

func deploymentObject(name, namespace string, labels map[string]string, owner metav1.OwnerReference) *appsv1.Deployment {
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}}
	if owner.Name != "" {
		deployment.OwnerReferences = []metav1.OwnerReference{owner}
	}
	return deployment
}

func tamossOwnerReference(tamoss *tamossv1alpha1.Tamoss) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: tamossv1alpha1.GroupVersion.String(),
		Kind:       "Tamoss",
		Name:       tamoss.Name,
		UID:        tamoss.UID,
	}
}
