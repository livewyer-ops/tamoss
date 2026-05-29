package controller

import (
	"fmt"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/auth/authentik"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/backend/cnpg"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/workload_renderer"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
)

func providerStatus(tamoss *tamossv1alpha1.Tamoss) tamossv1alpha1.ProviderStatus {
	dbProvider := tamoss.Spec.Backends.DB.Provider()
	s3Provider := tamoss.Spec.Backends.S3.Provider()
	authProvider := tamoss.Spec.Auth.Provider()
	return tamossv1alpha1.ProviderStatus{
		DB:      providerDomainStatus(string(dbProvider), backendOwnership(dbProvider)),
		S3:      providerDomainStatus(string(s3Provider), s3Ownership(s3Provider)),
		Auth:    providerDomainStatus(string(authProvider), authOwnership(authProvider)),
		Routing: routingProviderStatus(tamoss),
	}
}

func providerDomainStatus(provider string, ownership tamossv1alpha1.ProviderOwnership) tamossv1alpha1.ProviderDomainStatus {
	return tamossv1alpha1.ProviderDomainStatus{
		Provider:  provider,
		Ownership: ownership,
	}
}

func backendOwnership(provider tamossv1alpha1.BackendProvidedBy) tamossv1alpha1.ProviderOwnership {
	if provider == tamossv1alpha1.BackendProvidedByExternal {
		return tamossv1alpha1.ProviderOwnershipExternal
	}
	return tamossv1alpha1.ProviderOwnershipManaged
}

func s3Ownership(provider tamossv1alpha1.S3BackendProvidedBy) tamossv1alpha1.ProviderOwnership {
	if provider == tamossv1alpha1.S3BackendProvidedByExternal {
		return tamossv1alpha1.ProviderOwnershipExternal
	}
	return tamossv1alpha1.ProviderOwnershipManaged
}

func authOwnership(provider tamossv1alpha1.AuthProvidedBy) tamossv1alpha1.ProviderOwnership {
	switch provider {
	case tamossv1alpha1.AuthProvidedByAuthentikBlueprints:
		return tamossv1alpha1.ProviderOwnershipManaged
	case tamossv1alpha1.AuthProvidedByExternal:
		return tamossv1alpha1.ProviderOwnershipExternal
	default:
		return ""
	}
}

func routingProviderStatus(tamoss *tamossv1alpha1.Tamoss) tamossv1alpha1.ProviderDomainStatus {
	if tamoss.Spec.HTTPRoute.Enabled {
		return providerDomainStatus("httproute", tamossv1alpha1.ProviderOwnershipManaged)
	}
	if tamoss.Spec.Ingress.IsEnabled() {
		return providerDomainStatus("ingress", tamossv1alpha1.ProviderOwnershipManaged)
	}
	return providerDomainStatus("external", tamossv1alpha1.ProviderOwnershipExternal)
}

func resolvedTamossStatus(tamoss *tamossv1alpha1.Tamoss) tamossv1alpha1.ResolvedStatus {
	status := tamossv1alpha1.ResolvedStatus{
		Images: tamossv1alpha1.ResolvedImageStatus{
			API:                           resolvedImageRef(tamoss.Spec.API.Image, defaults.DefaultAPIRepository),
			UI:                            resolvedImageRef(tamoss.Spec.UI.Image, defaults.DefaultUIRepository),
			Worker:                        resolvedImageRef(tamoss.Spec.API.Image, defaults.DefaultAPIRepository),
			SchemaMigrationPostgresClient: schemaMigrationPostgresClientImage(tamoss),
		},
		Versions: tamossv1alpha1.ResolvedVersionStatus{
			Schema:  schemabundle.SchemaVersion,
			Tamoss:  resolvedRuntimeVersion(tamoss),
			TAMSAPI: defaults.DefaultTAMSAPIVersion,
		},
	}
	if tamoss.Spec.Backends.DB.Provider() == tamossv1alpha1.BackendProvidedByCNPG && tamoss.Spec.Backends.DB.CNPG != nil {
		status.Images.CNPGPostgres = cnpg.PostgresImage(tamoss.Spec.Backends.DB.CNPG.PostgresVersion)
	}
	if tamoss.Spec.Backends.S3.Provider() == tamossv1alpha1.S3BackendProvidedByRustFSOperator && tamoss.Spec.Backends.S3.RustFSOperator != nil {
		status.Images.RustFS = tamoss.Spec.Backends.S3.RustFSOperator.Image
	}
	if tamoss.Spec.API.IsEnabled() {
		status.Resources.API = tamossResourceName(tamoss, "api")
	}
	if tamoss.Spec.UI.IsEnabled() {
		status.Resources.UI = tamossResourceName(tamoss, "ui")
	}
	if tamoss.Spec.Worker.IsEnabled() {
		status.Resources.Worker = tamossResourceName(tamoss, "worker")
	}
	if tamossUsesManagedS3(tamoss) {
		status.Resources.DefaultStorageBackend = defaultStorageBackendName(tamoss)
	}
	if tamoss.Spec.HTTPRoute.Enabled || tamoss.Spec.Ingress.IsEnabled() {
		if tamoss.Spec.API.IsEnabled() {
			status.Routes.API = tamossResourceName(tamoss, "api")
		}
		if tamoss.Spec.UI.IsEnabled() {
			status.Routes.UI = tamossResourceName(tamoss, "ui")
		}
	}
	if tamoss.Spec.Secrets.APIToken.Generate {
		status.GeneratedSecrets.APIToken = tamossResourceName(tamoss, "api-token")
	}
	if tamoss.Spec.Auth.Provider() == tamossv1alpha1.AuthProvidedByAuthentikBlueprints {
		status.GeneratedSecrets.OAuth2Credentials = tamoss.OAuth2CredentialsSecretName()
	}
	if tamoss.Spec.API.IsEnabled() || tamoss.Spec.Worker.IsEnabled() {
		status.GeneratedSecrets.StorageBackendCredentials = workload_renderer.StorageBackendCredentialsSecretName(tamoss)
	}
	return status
}

func resolvedImageRef(image tamossv1alpha1.ImageSpec, fallbackRepository string) string {
	repository := image.Repository
	if repository == "" {
		repository = fallbackRepository
	}
	tag := image.Tag
	if tag == "" {
		tag = defaults.DefaultOperandTag
	}
	return fmt.Sprintf("%s:%s", repository, tag)
}

func resolvedRuntimeVersion(tamoss *tamossv1alpha1.Tamoss) string {
	if tamoss.Spec.API.Image.Tag != "" {
		return tamoss.Spec.API.Image.Tag
	}
	return defaults.DefaultOperandTag
}

func authStatus(tamoss *tamossv1alpha1.Tamoss) tamossv1alpha1.AuthStatus {
	status := tamossv1alpha1.AuthStatus{
		Provider: tamoss.Spec.Auth.Provider(),
	}
	if status.Provider == tamossv1alpha1.AuthProvidedByAuthentikBlueprints {
		status.ApplicationSlug = tamoss.Spec.Auth.ApplicationSlug(tamoss.Namespace, tamoss.Name)
		status.ManagedBlueprint = authentik.ManagedBlueprintName(tamoss)
	}
	return status
}

func endpointStatus(tamoss *tamossv1alpha1.Tamoss) tamossv1alpha1.EndpointStatus {
	if tamoss.Spec.HTTPRoute.Enabled {
		return tamossv1alpha1.EndpointStatus{
			API: firstHostnameURL(tamoss.Spec.HTTPRoute.API.Hostnames, "https"),
			UI:  firstHostnameURL(tamoss.Spec.HTTPRoute.UI.Hostnames, "https"),
		}
	}
	if tamoss.Spec.Ingress.IsEnabled() {
		return tamossv1alpha1.EndpointStatus{
			API: ingressURL(tamoss.Spec.Ingress.API.Host, len(tamoss.Spec.Ingress.TLS) > 0),
			UI:  ingressURL(tamoss.Spec.Ingress.UI.Web.Host, len(tamoss.Spec.Ingress.TLS) > 0),
		}
	}
	return tamossv1alpha1.EndpointStatus{}
}

func firstHostnameURL(hostnames []string, scheme string) string {
	if len(hostnames) == 0 {
		return ""
	}
	return fmt.Sprintf("%s://%s", scheme, hostnames[0])
}

func ingressURL(host string, tls bool) string {
	if host == "" {
		return ""
	}
	scheme := "http"
	if tls {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}
