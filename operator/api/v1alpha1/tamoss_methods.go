package v1alpha1

import (
	"fmt"
	"strings"
)

func (t *Tamoss) ResourceName(suffix string) string {
	name := t.Name
	if t.Spec.FullnameOverride != "" {
		name = t.Spec.FullnameOverride
	}
	return appendResourceSuffix(name, suffix)
}

func (t *Tamoss) DBConnection() DBExternalSpec {
	return t.Spec.Backends.DB.Connection(t.ResourceName(""))
}

func (t *Tamoss) S3Connection() S3ExternalSpec {
	return t.Spec.Backends.S3.ConnectionForNamespace(t.ResourceName(""), t.Namespace)
}

func (t *Tamoss) OAuth2CredentialsSecretName() string {
	return t.ResourceName("oauth2-creds")
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func int32Value(value *int32, fallback int32) int32 {
	if value == nil {
		return fallback
	}
	return *value
}

func (a APIComponentSpec) IsEnabled() bool {
	return boolValue(a.Enabled, true)
}

func (w WorkerComponentSpec) IsEnabled() bool {
	return boolValue(w.Enabled, false)
}

func (u UIComponentSpec) IsEnabled() bool {
	return boolValue(u.Enabled, true)
}

func (c ConsoleComponentSpec) IsEnabled() bool {
	return boolValue(c.Enabled, false)
}

func (s TamossSpec) ConsoleEnabled() bool {
	return s.Console.IsEnabled()
}

func (w WorkloadCommonSpec) DesiredReplicaCount() int32 {
	return int32Value(w.ReplicaCount, 1)
}

func (p PDBSpec) IsEnabled() bool {
	return boolValue(p.Enabled, false)
}

func (d DBBackendSpec) ShouldApplyFixtures() bool {
	return boolValue(d.ApplyFixtures, false)
}

func (m DBCNPGMonitoringSpec) ShouldEnablePodMonitor() bool {
	return boolValue(m.EnablePodMonitor, false)
}

func (d DBBackendSpec) Provider() BackendProvidedBy {
	if d.ProvidedBy == "" {
		if d.CNPG != nil {
			return BackendProvidedByCNPG
		}
		return BackendProvidedByExternal
	}
	return d.ProvidedBy
}

func (d DBBackendSpec) Connection(baseName string) DBExternalSpec {
	switch d.Provider() {
	case BackendProvidedByCNPG:
		return DBExternalSpec{
			Host:     appendResourceSuffix(baseName, "db-rw"),
			Port:     "5432",
			Database: "tams",
			Auth: SecretReferenceSpec{
				ExistingSecret: appendResourceSuffix(baseName, "db-app"),
				SecretKeys: SecretKeySpec{
					Username: "username",
					Password: "password",
				},
			},
		}
	default:
		if d.External == nil {
			return DBExternalSpec{}
		}
		return *d.External
	}
}

func (s S3BackendSpec) Provider() S3BackendProvidedBy {
	if s.ProvidedBy == "" {
		if s.RustFSOperator != nil {
			return S3BackendProvidedByRustFSOperator
		}
		return S3BackendProvidedByExternal
	}
	return s.ProvidedBy
}

func (a AuthSpec) Provider() AuthProvidedBy {
	if a.ProvidedBy == "" {
		return AuthProvidedByExternal
	}
	return a.ProvidedBy
}

func (a AuthSpec) RequiredForRuntime() bool {
	if a.Provider() == AuthProvidedByNone {
		return false
	}
	return a.Required
}

func (a AuthSpec) AllowsUnscopedOAuth2FullAccess() bool {
	return boolValue(a.OAuth2AllowUnscopedFullAccess, true)
}

func (a AuthSpec) OAuth2Config(namespace, name string) OAuth2Spec {
	switch a.Provider() {
	case AuthProvidedByExternal:
		if a.External == nil {
			return OAuth2Spec{}
		}
		return a.External.OAuth2
	case AuthProvidedByAuthentikBlueprints:
		if a.AuthentikBlueprints == nil {
			return OAuth2Spec{}
		}
		publicBase := a.AuthentikBlueprints.IssuerURL
		internalBase := a.AuthentikBlueprints.InternalURL
		if internalBase == "" {
			internalBase = publicBase
		}
		slug := a.ApplicationSlug(namespace, name)
		return OAuth2Spec{
			Enabled:    true,
			Issuer:     authentikProviderURL(publicBase, slug),
			JWKSURI:    authentikProviderJWKSURL(internalBase, slug),
			Algorithms: []string{"RS256"},
		}
	default:
		return OAuth2Spec{}
	}
}

func (a AuthSpec) ApplicationSlug(namespace, name string) string {
	if a.AuthentikBlueprints != nil && a.AuthentikBlueprints.ApplicationSlug != "" {
		return a.AuthentikBlueprints.ApplicationSlug
	}
	return fmt.Sprintf("tamoss-%s-%s", namespace, name)
}

func (s S3BackendSpec) Connection(baseName string) S3ExternalSpec {
	return s.ConnectionForNamespace(baseName, "")
}

func (s S3BackendSpec) ConnectionForNamespace(baseName, namespace string) S3ExternalSpec {
	switch s.Provider() {
	case S3BackendProvidedByRustFSOperator:
		spec := S3RustFSOperatorSpec{}
		if s.RustFSOperator != nil {
			spec = *s.RustFSOperator
		}
		bucket := spec.Bucket.Name
		if bucket == "" {
			bucket = "tamoss"
		}
		secret := spec.CredsSecret.ExistingSecret
		if secret == "" {
			secret = appendResourceSuffix(baseName, "s3-creds")
		}
		host := appendResourceSuffix(baseName, "s3")
		if namespace != "" {
			host = fmt.Sprintf("%s.%s.svc", host, namespace)
		}
		endpoint := S3EndpointSpec{
			Default: EndpointURLSpec{URL: fmt.Sprintf("http://%s:9000", host)},
		}
		if spec.PublicEndpoint.URL != "" {
			endpoint.Public = EndpointURLSpec{URL: spec.PublicEndpoint.URL}
		}
		return S3ExternalSpec{
			Endpoint: endpoint,
			Auth: SecretReferenceSpec{
				ExistingSecret: secret,
				SecretKeys: SecretKeySpec{
					AccessKey: "RUSTFS_ACCESS_KEY",
					SecretKey: "RUSTFS_SECRET_KEY",
				},
			},
			Region: "us-east-1",
			Bucket: bucket,
		}
	default:
		if s.External == nil {
			return S3ExternalSpec{}
		}
		return *s.External
	}
}

func (n NetworkPolicySpec) IsEnabled() bool {
	return n.Enabled != nil && *n.Enabled
}

func (i IngressSpec) IsEnabled() bool {
	return boolValue(i.Enabled, false)
}

func appendResourceSuffix(baseName, suffix string) string {
	if suffix == "" {
		return baseName
	}
	return fmt.Sprintf("%s-%s", baseName, suffix)
}

func authentikProviderURL(baseURL, slug string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" || slug == "" {
		return ""
	}
	return fmt.Sprintf("%s/application/o/%s/", base, slug)
}

func authentikProviderJWKSURL(baseURL, slug string) string {
	providerURL := authentikProviderURL(baseURL, slug)
	if providerURL == "" {
		return ""
	}
	return providerURL + "jwks/"
}
