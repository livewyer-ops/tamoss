package controller

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/workload_renderer"
)

const (
	forwardAuthAPIChecksumAnnotation     = "checksum/forward-auth-api-secret"
	forwardAuthConsoleChecksumAnnotation = "checksum/forward-auth-console-secret"
)

type forwardAuthProofs struct {
	api     []byte
	console []byte
}

func (r *TamossReconciler) prepareForwardAuthProofSecret(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, objects []client.Object) error {
	desired := forwardAuthProofSecret(objects, tamoss)
	if desired == nil {
		return nil
	}

	proofs, err := r.resolveForwardAuthProofs(ctx, tamoss)
	if err != nil {
		return err
	}
	desired.Data = map[string][]byte{
		workload_renderer.ForwardAuthAPIProofSecretKey:     proofs.api,
		workload_renderer.ForwardAuthConsoleProofSecretKey: proofs.console,
	}
	desired.StringData = nil
	annotateForwardAuthProofChecksums(objects, proofs)
	return nil
}

func (r *TamossReconciler) resolveForwardAuthProofs(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (forwardAuthProofs, error) {
	existing := &corev1.Secret{}
	err := r.Client.Get(ctx, types.NamespacedName{
		Name:      workload_renderer.ForwardAuthProofSecretName(tamoss),
		Namespace: tamoss.Namespace,
	}, existing)
	if err != nil && !apierrors.IsNotFound(err) {
		return forwardAuthProofs{}, err
	}

	apiProof, err := existingOrGeneratedForwardAuthProof(existing.Data, workload_renderer.ForwardAuthAPIProofSecretKey)
	if err != nil {
		return forwardAuthProofs{}, err
	}
	consoleProof, err := existingOrGeneratedForwardAuthProof(existing.Data, workload_renderer.ForwardAuthConsoleProofSecretKey)
	if err != nil {
		return forwardAuthProofs{}, err
	}
	return forwardAuthProofs{api: apiProof, console: consoleProof}, nil
}

func existingOrGeneratedForwardAuthProof(existing map[string][]byte, key string) ([]byte, error) {
	if proof := existing[key]; validForwardAuthProof(proof) {
		return proof, nil
	}
	return generateForwardAuthProof()
}

func validForwardAuthProof(proof []byte) bool {
	if len(proof) != base64.RawURLEncoding.EncodedLen(32) {
		return false
	}
	decoded := make([]byte, 32)
	written, err := base64.RawURLEncoding.Decode(decoded, proof)
	return err == nil && written == len(decoded)
}

func generateForwardAuthProof() ([]byte, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, err
	}
	return []byte(base64.RawURLEncoding.EncodeToString(randomBytes)), nil
}

func forwardAuthProofSecret(objects []client.Object, tamoss *tamossv1alpha1.Tamoss) *corev1.Secret {
	name := workload_renderer.ForwardAuthProofSecretName(tamoss)
	for _, obj := range objects {
		secret, ok := obj.(*corev1.Secret)
		if ok && secret.Name == name && secret.Namespace == tamoss.Namespace {
			return secret
		}
	}
	return nil
}

func annotateForwardAuthProofChecksums(objects []client.Object, proofs forwardAuthProofs) {
	apiChecksum := forwardAuthProofChecksum(proofs.api)
	consoleChecksum := forwardAuthProofChecksum(proofs.console)
	consoleRendered := false
	for _, obj := range objects {
		if deployment, ok := obj.(*appsv1.Deployment); ok && deployment.Labels[appComponentLabel] == "console" {
			consoleRendered = true
			break
		}
	}
	for _, obj := range objects {
		deployment, ok := obj.(*appsv1.Deployment)
		if !ok {
			continue
		}
		component := deployment.Labels[appComponentLabel]
		if component != "api" && component != "ui" && component != "console" {
			continue
		}
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = map[string]string{}
		}
		if component == "api" || component == "ui" {
			deployment.Spec.Template.Annotations[forwardAuthAPIChecksumAnnotation] = apiChecksum
		}
		if component == "console" || (component == "ui" && consoleRendered) {
			deployment.Spec.Template.Annotations[forwardAuthConsoleChecksumAnnotation] = consoleChecksum
		}
	}
}

func forwardAuthProofChecksum(proof []byte) string {
	sum := sha256.Sum256(proof)
	return hex.EncodeToString(sum[:])
}
