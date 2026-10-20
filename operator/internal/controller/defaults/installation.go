package defaults

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

// Installation contains site settings, independently of product releases.
type Installation struct {
	Profile          tamossv1alpha1.TamossProfile `json:"profile,omitempty"`
	BaseDomain       string                       `json:"baseDomain,omitempty"`
	IngressClassName string                       `json:"ingressClassName,omitempty"`
	ClusterIssuer    string                       `json:"clusterIssuer,omitempty"`
	ConsoleEnabled   *bool                        `json:"consoleEnabled,omitempty"`
	Authentik        InstallationAuthentik        `json:"authentik,omitempty"`
	source           string
	revision         string
	loadError        error
}

type InstallationAuthentik struct {
	PlatformNamespace string                          `json:"platformNamespace,omitempty"`
	IssuerURL         string                          `json:"issuerURL,omitempty"`
	InternalURL       string                          `json:"internalURL,omitempty"`
	APITokenSecretRef tamossv1alpha1.SecretKeyRefSpec `json:"apiTokenSecretRef,omitempty"`
}

// LoadInstallation reads the mounted file once. Invalid configuration blocks
// reconciliation while allowing the operator to report the error in status.
func LoadInstallation(path string) *Installation {
	if path == "" {
		return nil
	}
	config := &Installation{source: path}
	data, err := os.ReadFile(path) // #nosec G304 -- path is operator installation configuration.
	if err == nil {
		config.revision = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		err = yaml.UnmarshalStrict(data, config)
	}
	if err == nil {
		err = config.validate()
	}
	config.loadError = err
	if err == nil && config.Profile == "" && config.BaseDomain == "" && config.IngressClassName == "" && config.ClusterIssuer == "" && config.ConsoleEnabled == nil && config.Authentik == (InstallationAuthentik{}) {
		return nil
	}
	return config
}

func (c *Installation) validate() error {
	switch c.Profile {
	case "", tamossv1alpha1.TamossProfileLocalKind, tamossv1alpha1.TamossProfileEdge, tamossv1alpha1.TamossProfileSingleServer, tamossv1alpha1.TamossProfileMultiServer:
	default:
		return fmt.Errorf("installation profile is not supported")
	}
	for field, value := range map[string]string{
		"baseDomain": c.BaseDomain, "ingressClassName": c.IngressClassName, "clusterIssuer": c.ClusterIssuer,
		"authentik.platformNamespace": c.Authentik.PlatformNamespace, "authentik.apiTokenSecretRef.name": c.Authentik.APITokenSecretRef.Name,
	} {
		if value != "" && len(validation.IsDNS1123Subdomain(value)) != 0 {
			return fmt.Errorf("installation %s must be a DNS name", field)
		}
	}
	if key := c.Authentik.APITokenSecretRef.Key; key != "" && len(validation.IsConfigMapKey(key)) != 0 {
		return fmt.Errorf("installation Authentik token reference has an invalid key")
	}
	for _, value := range []string{c.Authentik.IssuerURL, c.Authentik.InternalURL} {
		if value == "" {
			continue
		}
		parsed, err := url.Parse(value)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("installation Authentik URLs must be HTTP or HTTPS base URLs without credentials")
		}
	}
	return nil
}

func (c *Installation) Revision() string {
	if c == nil {
		return ""
	}
	return c.revision
}

func (c *Installation) Status() tamossv1alpha1.InstallationDefaultsStatus {
	if c == nil {
		return tamossv1alpha1.InstallationDefaultsStatus{}
	}
	return tamossv1alpha1.InstallationDefaultsStatus{Source: c.source, Revision: c.revision}
}

func (c *Installation) AppliedTo(tamoss *tamossv1alpha1.Tamoss) bool {
	return tamoss.Status.AppliedDefaultsRevision == c.Revision()
}

// Apply fills only omitted site settings before the existing profile resolver.
func (c *Installation) Apply(tamoss *tamossv1alpha1.Tamoss) error {
	if c == nil {
		return nil
	}
	if c.loadError != nil {
		return fmt.Errorf("invalid installation defaults: %w", c.loadError)
	}
	spec := &tamoss.Spec
	if spec.Profile == "" {
		spec.Profile = c.Profile
	}
	if spec.PublicEndpoint.BaseDomain == "" && c.BaseDomain != "" {
		domain := tamoss.Name + "." + tamoss.Namespace + "." + c.BaseDomain
		if len(validation.IsDNS1123Subdomain("app."+domain)) != 0 {
			return fmt.Errorf("derived instance domain exceeds DNS limits; set spec.publicEndpoint.baseDomain")
		}
		spec.PublicEndpoint.BaseDomain = domain
	}
	defaultString(&spec.PublicEndpoint.TLSSecretName, tamoss.ResourceName("tls"))
	defaultString(&spec.PublicEndpoint.S3TLSSecretName, tamoss.ResourceName("s3-tls"))
	defaultString(&spec.Ingress.ClassName, c.IngressClassName)
	if spec.Ingress.Annotations == nil && c.ClusterIssuer != "" {
		spec.Ingress.Annotations = map[string]string{defaultCertManagerIssuerAnnotation: c.ClusterIssuer}
	}
	if spec.Console.Enabled == nil && c.ConsoleEnabled != nil {
		value := *c.ConsoleEnabled
		spec.Console.Enabled = &value
	}
	managed := spec.Auth.Provider() == tamossv1alpha1.AuthProvidedByAuthentikBlueprints ||
		(spec.Profile != "" && spec.Profile != tamossv1alpha1.TamossProfileEdge && spec.Auth.Provider() != tamossv1alpha1.AuthProvidedByNone && !externalAuthConfigured(spec.Auth.External))
	if managed {
		if spec.Auth.AuthentikBlueprints == nil {
			spec.Auth.AuthentikBlueprints = &tamossv1alpha1.AuthentikBlueprintsSpec{}
		}
		auth := spec.Auth.AuthentikBlueprints
		defaultString(&auth.PlatformNamespace, c.Authentik.PlatformNamespace)
		defaultString(&auth.IssuerURL, c.Authentik.IssuerURL)
		if auth.IssuerURL == "" && c.BaseDomain != "" {
			auth.IssuerURL = "https://auth." + c.BaseDomain
		}
		defaultString(&auth.InternalURL, c.Authentik.InternalURL)
		defaultString(&auth.APITokenSecretRef.Name, c.Authentik.APITokenSecretRef.Name)
		defaultString(&auth.APITokenSecretRef.Key, c.Authentik.APITokenSecretRef.Key)
	}
	return nil
}

func defaultString(target *string, value string) {
	if *target == "" {
		*target = strings.TrimSuffix(value, "/")
	}
}
