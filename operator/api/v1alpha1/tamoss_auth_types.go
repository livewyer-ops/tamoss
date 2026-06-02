package v1alpha1

type AuthProvidedBy string

const (
	AuthProvidedByExternal            AuthProvidedBy = "external"
	AuthProvidedByNone                AuthProvidedBy = "none"
	AuthProvidedByAuthentikBlueprints AuthProvidedBy = "authentik-blueprints"
)

// +kubebuilder:validation:XValidation:rule="(self.providedBy == 'external' && !has(self.authentikBlueprints)) || (self.providedBy == 'none' && !has(self.external) && !has(self.authentikBlueprints)) || (self.providedBy == 'authentik-blueprints' && !has(self.external))",message="only the auth block matching providedBy may be set"
type AuthSpec struct {
	//+kubebuilder:validation:Enum=external;none;authentik-blueprints
	//+kubebuilder:default=external
	ProvidedBy AuthProvidedBy `json:"providedBy,omitempty"`
	//+kubebuilder:default=true
	Required bool `json:"required,omitempty"`
	//+kubebuilder:default=false
	TrustForwardAuthHeaders       bool                     `json:"trustForwardAuthHeaders,omitempty"`
	OAuth2AllowUnscopedFullAccess *bool                    `json:"oauth2AllowUnscopedFullAccess,omitempty"`
	External                      *AuthExternalSpec        `json:"external,omitempty"`
	AuthentikBlueprints           *AuthentikBlueprintsSpec `json:"authentikBlueprints,omitempty"`
}

type AuthExternalSpec struct {
	OAuth2 OAuth2Spec `json:"oauth2,omitempty"`
}

type OAuth2Spec struct {
	//+kubebuilder:default=false
	Enabled  bool   `json:"enabled,omitempty"`
	Issuer   string `json:"issuer,omitempty"`
	JWKSURI  string `json:"jwksUri,omitempty"`
	Audience string `json:"audience,omitempty"`
	//+kubebuilder:default={RS256}
	Algorithms              []string                          `json:"algorithms,omitempty"`
	ClientCredentialsSecret OAuth2ClientCredentialsSecretSpec `json:"clientCredentialsSecret,omitempty"`
}

type OAuth2ClientCredentialsSecretSpec struct {
	ExistingSecret string `json:"existingSecret,omitempty"`
}

type AuthentikBlueprintsSpec struct {
	// PlatformNamespace is the namespace where the shared Authentik platform runs.
	//+kubebuilder:validation:MinLength=1
	PlatformNamespace string `json:"platformNamespace,omitempty"`
	// IssuerURL is the public base Authentik URL used as the OAuth issuer.
	//+kubebuilder:validation:MinLength=1
	IssuerURL string `json:"issuerURL,omitempty"`
	// InternalURL is the optional in-cluster Authentik base URL used for API calls, readiness probes, and JWKS retrieval.
	//+kubebuilder:validation:MinLength=1
	InternalURL string `json:"internalURL,omitempty"`
	// APITokenSecretRef references the platform Authentik API token used to reconcile managed Blueprints. Empty defaults to Secret/authentik-api-token key token in platformNamespace.
	APITokenSecretRef SecretKeyRefSpec `json:"apiTokenSecretRef,omitempty"`
	// ApplicationSlug names the Authentik Application and Provider. Empty defaults to tamoss-{namespace}-{name}.
	ApplicationSlug string                      `json:"applicationSlug,omitempty"`
	RedirectURIs    []string                    `json:"redirectURIs,omitempty"`
	GroupBindings   []AuthentikGroupBindingSpec `json:"groupBindings,omitempty"`
}

type SecretKeyRefSpec struct {
	Name string `json:"name,omitempty"`
	Key  string `json:"key,omitempty"`
}

type AuthentikGroupBindingSpec struct {
	//+kubebuilder:validation:MinLength=1
	GroupName   string   `json:"groupName,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}
