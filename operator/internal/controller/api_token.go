package controller

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

const (
	apiTokenKey = "TAMOSS_API_TOKEN"
)

type apiTokenResolution struct {
	token            []byte
	rotationConsumed string
}

func (r *TamossReconciler) prepareAPITokenSecret(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, objects []client.Object) error {
	secret := apiTokenSecret(objects, tamoss)
	if secret == nil {
		r.rejectAPITokenRotationWithoutGeneratedSecret(tamoss)
		return nil
	}

	resolution, err := r.resolveAPIToken(ctx, tamoss)
	if err != nil {
		return err
	}
	secret.Data = map[string][]byte{apiTokenKey: resolution.token}
	secret.StringData = nil
	if resolution.rotationConsumed != "" {
		if secret.Annotations == nil {
			secret.Annotations = map[string]string{}
		}
		secret.Annotations[annotationAPITokenDone] = resolution.rotationConsumed
	}
	annotateAPITokenChecksum(objects, resolution.token)
	return nil
}

func (r *TamossReconciler) resolveAPIToken(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (apiTokenResolution, error) {
	if tamoss.Spec.Secrets.APIToken.Token != "" {
		r.rejectAPITokenRotation(tamoss, "API token rotation is not available because spec.secrets.apiToken.token is supplied directly")
		return apiTokenResolution{token: []byte(tamoss.Spec.Secrets.APIToken.Token)}, nil
	}
	if !tamoss.Spec.Secrets.APIToken.Generate {
		r.rejectAPITokenRotation(tamoss, "API token rotation is not available because spec.secrets.apiToken.generate is false and no token is supplied")
		return apiTokenResolution{}, fmt.Errorf("spec.secrets.apiToken.token is required when generate is false")
	}

	rotationValue := strings.TrimSpace(tamoss.Annotations[AnnotationAPITokenRotate])
	existing := &corev1.Secret{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: tamossResourceName(tamoss, "api-token"), Namespace: tamoss.Namespace}, existing)
	if err == nil {
		consumed := existing.Annotations[annotationAPITokenDone]
		if rotationValue != "" && consumed != rotationValue {
			token, err := generateAPIToken()
			if err != nil {
				return apiTokenResolution{}, err
			}
			operatorstatus.EmitNormalEvent(r.Recorder, tamoss, operatorstatus.ReasonAPITokenRotationAccepted, "Generated API token rotation annotation accepted")
			return apiTokenResolution{token: token, rotationConsumed: rotationValue}, nil
		}
		if token := existing.Data[apiTokenKey]; len(token) > 0 {
			return apiTokenResolution{token: token, rotationConsumed: consumed}, nil
		}
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return apiTokenResolution{}, err
	}
	token, err := generateAPIToken()
	if err != nil {
		return apiTokenResolution{}, err
	}
	if rotationValue != "" {
		operatorstatus.EmitNormalEvent(r.Recorder, tamoss, operatorstatus.ReasonAPITokenRotationAccepted, "Generated API token rotation annotation accepted")
		return apiTokenResolution{token: token, rotationConsumed: rotationValue}, nil
	}
	return apiTokenResolution{token: token}, nil
}

func (r *TamossReconciler) rejectAPITokenRotationWithoutGeneratedSecret(tamoss *tamossv1alpha1.Tamoss) {
	if strings.TrimSpace(tamoss.Annotations[AnnotationAPITokenRotate]) == "" {
		return
	}
	if tamoss.Spec.Secrets.APIToken.Token != "" {
		r.rejectAPITokenRotation(tamoss, "API token rotation is not available because spec.secrets.apiToken.token is supplied directly")
		return
	}
	if !tamoss.Spec.Secrets.APIToken.Generate {
		r.rejectAPITokenRotation(tamoss, "API token rotation is not available because spec.secrets.apiToken.generate is false and no token is supplied")
	}
}

func (r *TamossReconciler) rejectAPITokenRotation(tamoss *tamossv1alpha1.Tamoss, message string) {
	if strings.TrimSpace(tamoss.Annotations[AnnotationAPITokenRotate]) == "" {
		return
	}
	r.recordWarning(tamoss, operatorstatus.ReasonAPITokenRotationRejected, message)
}

func generateAPIToken() ([]byte, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes)
	return []byte(token), nil
}

func apiTokenSecret(objects []client.Object, tamoss *tamossv1alpha1.Tamoss) *corev1.Secret {
	name := tamossResourceName(tamoss, "api-token")
	for _, obj := range objects {
		secret, ok := obj.(*corev1.Secret)
		if ok && secret.Name == name && secret.Namespace == tamoss.Namespace {
			return secret
		}
	}
	return nil
}

func annotateAPITokenChecksum(objects []client.Object, token []byte) {
	sum := sha256.Sum256(token)
	checksum := hex.EncodeToString(sum[:])
	for _, obj := range objects {
		deployment, ok := obj.(*appsv1.Deployment)
		if !ok {
			continue
		}
		component := deployment.Labels["app.kubernetes.io/component"]
		if component != "api" && component != "ui" {
			continue
		}
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = map[string]string{}
		}
		deployment.Spec.Template.Annotations["checksum/api-token-secret"] = checksum
	}
}
