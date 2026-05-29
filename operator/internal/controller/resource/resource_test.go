package resource

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestTamossLabels(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Name: "media"}}

	labels := TamossLabels(tamoss, "api")

	if labels[LabelName] != AppName {
		t.Fatalf("expected app name label %q, got %q", AppName, labels[LabelName])
	}
	if labels[LabelInstance] != "media" {
		t.Fatalf("expected instance label media, got %q", labels[LabelInstance])
	}
	if labels[LabelComponent] != "api" {
		t.Fatalf("expected component label api, got %q", labels[LabelComponent])
	}
	if labels[LabelManagedBy] != ManagedBy {
		t.Fatalf("expected managed-by label %q, got %q", ManagedBy, labels[LabelManagedBy])
	}
}

func TestMergeLabelsPreservesExistingLabels(t *testing.T) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"existing": "keep"}}}

	if !MergeLabels(secret, map[string]string{LabelName: AppName}) {
		t.Fatal("expected labels to change")
	}
	if secret.Labels["existing"] != "keep" {
		t.Fatalf("expected existing label to be preserved, got %#v", secret.Labels)
	}
	if secret.Labels[LabelName] != AppName {
		t.Fatalf("expected app label to be set, got %#v", secret.Labels)
	}
	if MergeLabels(secret, map[string]string{LabelName: AppName}) {
		t.Fatal("expected second merge to be unchanged")
	}
}

func TestTamossOwnerReferences(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{
			Name: "media",
			UID:  types.UID("tamoss-uid"),
		},
	}
	secret := &corev1.Secret{}

	refs := TamossOwnerReferences(tamoss)
	if len(refs) != 1 {
		t.Fatalf("expected one owner reference, got %d", len(refs))
	}
	if refs[0].APIVersion != tamossv1alpha1.GroupVersion.String() || refs[0].Kind != "Tamoss" || refs[0].Name != "media" {
		t.Fatalf("unexpected owner reference: %#v", refs[0])
	}
	if refs[0].Controller == nil || !*refs[0].Controller {
		t.Fatalf("expected controller owner reference, got %#v", refs[0])
	}

	if !SetOwnerReferences(secret, refs) {
		t.Fatal("expected owner references to change")
	}
	if SetOwnerReferences(secret, refs) {
		t.Fatal("expected second owner reference update to be unchanged")
	}
}
