package authentik

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	defaultHTTPTimeout         = 30 * time.Second
	defaultMaxIdleConns        = 100
	defaultMaxIdleConnsPerHost = 10
	defaultIdleConnTimeout     = 90 * time.Second
)

// defaultHTTPClient intentionally binds proxy environment handling at process
// start. Kubernetes pod environment is immutable after startup.
var defaultHTTPClient = NewHTTPClient()

type APITokenResolution struct {
	Token   string
	Message string
}

type PlatformNamespaceDecision struct {
	Allowed bool
	Message string
}

func ResolveAPIToken(ctx context.Context, reader client.Reader, tamoss *tamossv1alpha1.Tamoss) (APITokenResolution, error) {
	spec := tamoss.Spec.Auth.AuthentikBlueprints
	if spec == nil {
		return APITokenResolution{Message: "auth.authentikBlueprints is required"}, nil
	}
	name := APITokenSecretName(tamoss)
	key := APITokenSecretKey(tamoss)
	secret := &corev1.Secret{}
	if err := reader.Get(ctx, types.NamespacedName{Name: name, Namespace: spec.PlatformNamespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return APITokenResolution{Message: fmt.Sprintf("Authentik API token secret %s/%s is required", spec.PlatformNamespace, name)}, nil
		}
		return APITokenResolution{}, err
	}
	token := strings.TrimSpace(string(secret.Data[key]))
	if token == "" {
		return APITokenResolution{Message: fmt.Sprintf("Authentik API token key %q is missing or empty in secret %s/%s", key, spec.PlatformNamespace, name)}, nil
	}
	return APITokenResolution{Token: token}, nil
}

func CheckPlatformNamespace(tamoss *tamossv1alpha1.Tamoss, policy *PlatformNamespacePolicy) PlatformNamespaceDecision {
	if tamoss.Spec.Auth.Provider() != tamossv1alpha1.AuthProvidedByAuthentikBlueprints {
		return PlatformNamespaceDecision{Allowed: true}
	}
	if tamoss.Spec.Auth.AuthentikBlueprints == nil || tamoss.Spec.Auth.AuthentikBlueprints.PlatformNamespace == "" {
		return PlatformNamespaceDecision{Message: "auth.authentikBlueprints.platformNamespace is required"}
	}
	namespace := tamoss.Spec.Auth.AuthentikBlueprints.PlatformNamespace
	if policy.Allow(namespace) {
		return PlatformNamespaceDecision{Allowed: true}
	}
	return PlatformNamespaceDecision{
		Message: fmt.Sprintf("Authentik platform namespace %q is outside configured allow-list %q", namespace, policy.Description()),
	}
}

func APIBaseURL(tamoss *tamossv1alpha1.Tamoss) string {
	if tamoss.Spec.Auth.AuthentikBlueprints == nil {
		return ""
	}
	if tamoss.Spec.Auth.AuthentikBlueprints.InternalURL != "" {
		return tamoss.Spec.Auth.AuthentikBlueprints.InternalURL
	}
	return tamoss.Spec.Auth.AuthentikBlueprints.IssuerURL
}

func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout: defaultHTTPTimeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          defaultMaxIdleConns,
			MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
			IdleConnTimeout:       defaultIdleConnTimeout,
			TLSHandshakeTimeout:   defaultHTTPTimeout,
			ExpectContinueTimeout: time.Second,
		},
	}
}

func HTTPClientOrDefault(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return defaultHTTPClient
}

func NewManagedBlueprintClient(tamoss *tamossv1alpha1.Tamoss, token string, httpClient *http.Client) ManagedBlueprintClient {
	return ManagedBlueprintClient{
		BaseURL:    APIBaseURL(tamoss),
		Token:      token,
		HTTPClient: HTTPClientOrDefault(httpClient),
	}
}

func NewProxyOutpostClient(tamoss *tamossv1alpha1.Tamoss, token string, httpClient *http.Client) ProxyOutpostClient {
	return ProxyOutpostClient{
		BaseURL:    APIBaseURL(tamoss),
		Token:      token,
		HTTPClient: HTTPClientOrDefault(httpClient),
	}
}
