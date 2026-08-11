package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/workload_renderer"
)

func TestForwardAuthProofIsStableAndAnnotatesConsumers(t *testing.T) {
	tamoss := forwardAuthTamoss()
	apiProof := testForwardAuthProof('a')
	consoleProof := testForwardAuthProof('c')
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workload_renderer.ForwardAuthProofSecretName(tamoss),
			Namespace: tamoss.Namespace,
		},
		Data: map[string][]byte{
			workload_renderer.ForwardAuthAPIProofSecretKey:     apiProof,
			workload_renderer.ForwardAuthConsoleProofSecretKey: consoleProof,
		},
	}
	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithScheme(storageBackendTestScheme(t)).WithObjects(existing).Build(),
	}
	objects := forwardAuthObjects(tamoss)

	if err := reconciler.prepareForwardAuthProofSecret(context.Background(), tamoss, objects); err != nil {
		t.Fatalf("prepare forward-auth proof: %v", err)
	}
	desired := objects[0].(*corev1.Secret)
	if got := string(desired.Data[workload_renderer.ForwardAuthAPIProofSecretKey]); got != string(apiProof) {
		t.Fatalf("API proof changed across reconcile: %q", got)
	}
	if got := string(desired.Data[workload_renderer.ForwardAuthConsoleProofSecretKey]); got != string(consoleProof) {
		t.Fatalf("Console proof changed across reconcile: %q", got)
	}
	api := objects[1].(*appsv1.Deployment)
	ui := objects[2].(*appsv1.Deployment)
	console := objects[3].(*appsv1.Deployment)
	if got := api.Spec.Template.Annotations[forwardAuthAPIChecksumAnnotation]; got != forwardAuthProofChecksum(apiProof) {
		t.Fatalf("API checksum = %q", got)
	}
	if got := api.Spec.Template.Annotations[forwardAuthConsoleChecksumAnnotation]; got != "" {
		t.Fatalf("API received Console checksum %q", got)
	}
	if got := console.Spec.Template.Annotations[forwardAuthConsoleChecksumAnnotation]; got != forwardAuthProofChecksum(consoleProof) {
		t.Fatalf("Console checksum = %q", got)
	}
	if got := console.Spec.Template.Annotations[forwardAuthAPIChecksumAnnotation]; got != "" {
		t.Fatalf("Console received API checksum %q", got)
	}
	if got := ui.Spec.Template.Annotations[forwardAuthAPIChecksumAnnotation]; got != forwardAuthProofChecksum(apiProof) {
		t.Fatalf("UI API checksum = %q", got)
	}
	if got := ui.Spec.Template.Annotations[forwardAuthConsoleChecksumAnnotation]; got != forwardAuthProofChecksum(consoleProof) {
		t.Fatalf("UI Console checksum = %q", got)
	}
}

func TestForwardAuthProofIsGeneratedAfterSecretDeletion(t *testing.T) {
	tamoss := forwardAuthTamoss()
	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithScheme(storageBackendTestScheme(t)).Build(),
	}
	objects := forwardAuthObjects(tamoss)

	if err := reconciler.prepareForwardAuthProofSecret(context.Background(), tamoss, objects); err != nil {
		t.Fatalf("prepare missing forward-auth proof: %v", err)
	}
	secret := objects[0].(*corev1.Secret)
	apiProof := secret.Data[workload_renderer.ForwardAuthAPIProofSecretKey]
	consoleProof := secret.Data[workload_renderer.ForwardAuthConsoleProofSecretKey]
	if len(apiProof) < 32 || len(consoleProof) < 32 {
		t.Fatalf("generated proofs are too short: API=%d Console=%d", len(apiProof), len(consoleProof))
	}
	if string(apiProof) == string(consoleProof) {
		t.Fatal("API and Console proofs must be independently generated")
	}
}

func TestForwardAuthProofRegeneratesOnlyMissingKey(t *testing.T) {
	tamoss := forwardAuthTamoss()
	apiProof := testForwardAuthProof('a')
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workload_renderer.ForwardAuthProofSecretName(tamoss),
			Namespace: tamoss.Namespace,
		},
		Data: map[string][]byte{workload_renderer.ForwardAuthAPIProofSecretKey: apiProof},
	}
	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithScheme(storageBackendTestScheme(t)).WithObjects(existing).Build(),
	}
	objects := forwardAuthObjects(tamoss)

	if err := reconciler.prepareForwardAuthProofSecret(context.Background(), tamoss, objects); err != nil {
		t.Fatalf("prepare partial forward-auth proof: %v", err)
	}
	secret := objects[0].(*corev1.Secret)
	if got := secret.Data[workload_renderer.ForwardAuthAPIProofSecretKey]; string(got) != string(apiProof) {
		t.Fatalf("existing API proof changed: %q", got)
	}
	if got := secret.Data[workload_renderer.ForwardAuthConsoleProofSecretKey]; len(got) < 32 {
		t.Fatalf("missing Console proof was not generated: %d bytes", len(got))
	}
}

func TestForwardAuthProofRegeneratesMalformedValues(t *testing.T) {
	tamoss := forwardAuthTamoss()
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workload_renderer.ForwardAuthProofSecretName(tamoss),
			Namespace: tamoss.Namespace,
		},
		Data: map[string][]byte{
			workload_renderer.ForwardAuthAPIProofSecretKey:     []byte("this is long enough but is not raw base64url proof material"),
			workload_renderer.ForwardAuthConsoleProofSecretKey: bytes.Repeat([]byte("a"), 4097),
		},
	}
	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithScheme(storageBackendTestScheme(t)).WithObjects(existing).Build(),
	}
	objects := forwardAuthObjects(tamoss)

	if err := reconciler.prepareForwardAuthProofSecret(context.Background(), tamoss, objects); err != nil {
		t.Fatalf("prepare malformed forward-auth proofs: %v", err)
	}
	secret := objects[0].(*corev1.Secret)
	for _, key := range []string{workload_renderer.ForwardAuthAPIProofSecretKey, workload_renderer.ForwardAuthConsoleProofSecretKey} {
		if proof := secret.Data[key]; !validForwardAuthProof(proof) {
			t.Fatalf("%s was not repaired with generated proof material: %q", key, proof)
		}
	}
}

func TestForwardAuthConsoleChecksumIsOmittedWithoutConsoleConsumer(t *testing.T) {
	tamoss := forwardAuthTamoss()
	objects := forwardAuthObjects(tamoss)[:3]
	proofs := forwardAuthProofs{api: testForwardAuthProof('a'), console: testForwardAuthProof('c')}

	annotateForwardAuthProofChecksums(objects, proofs)

	ui := objects[2].(*appsv1.Deployment)
	if got := ui.Spec.Template.Annotations[forwardAuthAPIChecksumAnnotation]; got != forwardAuthProofChecksum(proofs.api) {
		t.Fatalf("UI API checksum = %q", got)
	}
	if got := ui.Spec.Template.Annotations[forwardAuthConsoleChecksumAnnotation]; got != "" {
		t.Fatalf("UI received unused Console checksum %q", got)
	}
}

func TestForwardAuthPreparationIsNoopWithoutRenderedSecret(t *testing.T) {
	tamoss := forwardAuthTamoss()
	reconciler := &TamossReconciler{}

	if err := reconciler.prepareForwardAuthProofSecret(context.Background(), tamoss, nil); err != nil {
		t.Fatalf("expected no-op without a rendered proof Secret: %v", err)
	}
}

func forwardAuthTamoss() *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "tams"}}
}

func forwardAuthObjects(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	objects := []client.Object{&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name:      workload_renderer.ForwardAuthProofSecretName(tamoss),
		Namespace: tamoss.Namespace,
	}}}
	for _, component := range []string{"api", "ui", "console"} {
		objects = append(objects, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tamoss.ResourceName(component),
				Namespace: tamoss.Namespace,
				Labels:    map[string]string{"app.kubernetes.io/component": component},
			},
		})
	}
	return objects
}

func testForwardAuthProof(fill byte) []byte {
	return []byte(base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32)))
}
