package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	httpCredentialSecretKey = "TAMSIN_SOURCE_HTTP_HEADERS"
	s3AccessKeySecretKey    = "AWS_ACCESS_KEY_ID"
	s3SecretKeySecretKey    = "AWS_SECRET_ACCESS_KEY"
	s3SessionTokenSecretKey = "AWS_SESSION_TOKEN"
	ingestDNSLookupTimeout  = 5 * time.Second
)

type IngestHostResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// SourcePolicyResolver validates an immutable IngestRun selector against the
// target instance policy. It returns an audit digest and only the name of any
// source-owned Secret; Secret values never enter status or Job arguments.
type SourcePolicyResolver struct {
	Client       client.Reader
	HostResolver IngestHostResolver
}

type tamsinSourcePolicy struct {
	Version string                  `json:"version"`
	Mode    string                  `json:"mode"`
	HTTP    *tamsinHTTPSourcePolicy `json:"http,omitempty"`
	S3      *tamsinS3SourcePolicy   `json:"s3,omitempty"`
}

type tamsinHTTPSourcePolicy struct {
	Origin                string   `json:"origin"`
	PathPrefixes          []string `json:"pathPrefixes,omitempty"`
	AllowPrivateAddresses bool     `json:"allowPrivateAddresses,omitempty"`
}

type tamsinS3SourcePolicy struct {
	Endpoint              string   `json:"endpoint"`
	Bucket                string   `json:"bucket"`
	KeyPrefixes           []string `json:"keyPrefixes,omitempty"`
	PathStyle             bool     `json:"pathStyle,omitempty"`
	AllowPrivateAddresses bool     `json:"allowPrivateAddresses,omitempty"`
}

type ingestSourcePolicySnapshot struct {
	SourceName           string             `json:"sourceName"`
	Policy               tamsinSourcePolicy `json:"policy"`
	CredentialSecretName string             `json:"credentialSecretName,omitempty"`
	S3Region             string             `json:"s3Region,omitempty"`
}

func (r SourcePolicyResolver) Resolve(
	ctx context.Context,
	tamoss *tamossv1alpha1.Tamoss,
	input tamossv1alpha1.IngestRunInput,
	maxInputs int32,
) (ResolvedIngestInputs, error) {
	mode := tamoss.Spec.Ingest.SourcePolicy.Mode
	if mode == "" {
		mode = tamossv1alpha1.IngestSourcePolicyDisabled
	}
	if mode == tamossv1alpha1.IngestSourcePolicyDisabled {
		return ResolvedIngestInputs{}, fmt.Errorf("ingest source policy is Disabled")
	}
	if maxInputs < 1 {
		return ResolvedIngestInputs{}, fmt.Errorf("maxInputs must be positive")
	}

	sources := make(map[string]tamossv1alpha1.IngestSourceSpec, len(tamoss.Spec.Ingest.Sources))
	for _, source := range tamoss.Spec.Ingest.Sources {
		if _, found := sources[source.Name]; found {
			return ResolvedIngestInputs{}, fmt.Errorf("ingest source name %q is duplicated", source.Name)
		}
		sources[source.Name] = source
	}

	if input.SourceRef == nil {
		if mode != tamossv1alpha1.IngestSourcePolicyPublicHTTPS || input.Kind != tamossv1alpha1.IngestInputKindHTTP {
			return ResolvedIngestInputs{}, fmt.Errorf("sourceRef is required by source policy mode %s", mode)
		}
		selector, err := r.validatePublicHTTPSSelector(ctx, input.URI)
		if err != nil {
			return ResolvedIngestInputs{}, err
		}
		return resolvedSourcePolicy(selector, "public-https", tamsinSourcePolicy{Version: "1", Mode: "publicHTTPS"}, nil, "", nil)
	}

	source, found := sources[input.SourceRef.Name]
	if !found {
		return ResolvedIngestInputs{}, fmt.Errorf("ingest source %q does not exist", input.SourceRef.Name)
	}
	if tamossv1alpha1.IngestSourceKind(input.Kind) != source.Kind {
		return ResolvedIngestInputs{}, fmt.Errorf("ingest source %q is kind %q, not %q", source.Name, source.Kind, input.Kind)
	}

	switch input.Kind {
	case tamossv1alpha1.IngestInputKindHTTP:
		if source.HTTP == nil || source.S3 != nil {
			return ResolvedIngestInputs{}, fmt.Errorf("HTTP ingest source %q has invalid configuration", source.Name)
		}
		selector, err := validateNamedHTTPSelector(input.URI, *source.HTTP)
		if err != nil {
			return ResolvedIngestInputs{}, err
		}
		if err := r.validateResolvedHost(ctx, source.HTTP.Origin, source.HTTP.AllowPrivateAddresses); err != nil {
			return ResolvedIngestInputs{}, fmt.Errorf("HTTP source origin is not permitted: %w", err)
		}
		if err := r.validateCredentialSecret(ctx, tamoss.Namespace, source.HTTP.CredentialSecretRef, tamossv1alpha1.IngestSourceKindHTTP); err != nil {
			return ResolvedIngestInputs{}, err
		}
		policy := tamsinSourcePolicy{Version: "1", Mode: "restricted", HTTP: &tamsinHTTPSourcePolicy{
			Origin: source.HTTP.Origin, PathPrefixes: source.HTTP.PathPrefixes, AllowPrivateAddresses: source.HTTP.AllowPrivateAddresses,
		}}
		return resolvedSourcePolicy(selector, source.Name, policy, source.HTTP.CredentialSecretRef, tamossv1alpha1.IngestSourceKindHTTP, nil)
	case tamossv1alpha1.IngestInputKindS3:
		if source.S3 == nil || source.HTTP != nil {
			return ResolvedIngestInputs{}, fmt.Errorf("S3 ingest source %q has invalid configuration", source.Name)
		}
		selector, err := validateNamedS3Selector(input.URI, *source.S3)
		if err != nil {
			return ResolvedIngestInputs{}, err
		}
		if err := r.validateResolvedHost(ctx, source.S3.Endpoint, source.S3.AllowPrivateAddresses); err != nil {
			return ResolvedIngestInputs{}, fmt.Errorf("S3 source endpoint is not permitted: %w", err)
		}
		if err := r.validateCredentialSecret(ctx, tamoss.Namespace, source.S3.CredentialSecretRef, tamossv1alpha1.IngestSourceKindS3); err != nil {
			return ResolvedIngestInputs{}, err
		}
		policy := tamsinSourcePolicy{Version: "1", Mode: "restricted", S3: &tamsinS3SourcePolicy{
			Endpoint: source.S3.Endpoint, Bucket: source.S3.Bucket, KeyPrefixes: source.S3.KeyPrefixes, PathStyle: source.S3.PathStyle, AllowPrivateAddresses: source.S3.AllowPrivateAddresses,
		}}
		return resolvedSourcePolicy(selector, source.Name, policy, source.S3.CredentialSecretRef, tamossv1alpha1.IngestSourceKindS3, source.S3)
	default:
		return ResolvedIngestInputs{}, fmt.Errorf("unsupported ingest input kind %q", input.Kind)
	}
}

func resolvedSourcePolicy(selector, sourceName string, policy tamsinSourcePolicy, secretRef *corev1.LocalObjectReference, credentialKind tamossv1alpha1.IngestSourceKind, s3 *tamossv1alpha1.S3IngestSourceSpec) (ResolvedIngestInputs, error) {
	credentialSecretName := ""
	if secretRef != nil {
		credentialSecretName = secretRef.Name
	}
	s3Region := ""
	if s3 != nil {
		s3Region = s3.Region
	}
	digestInput, err := json.Marshal(ingestSourcePolicySnapshot{
		SourceName: sourceName, Policy: policy, CredentialSecretName: credentialSecretName, S3Region: s3Region,
	})
	if err != nil {
		return ResolvedIngestInputs{}, fmt.Errorf("encode ingest source policy snapshot: %w", err)
	}
	digest := sha256.Sum256(digestInput)
	resolved := ResolvedIngestInputs{
		Selectors:      []string{selector},
		ExpectedInputs: 1,
		SourceName:     sourceName,
		PolicyDigest:   hex.EncodeToString(digest[:]),
		CredentialKind: credentialKind,
	}
	resolved.CredentialSecretName = credentialSecretName
	if s3 != nil {
		resolved.S3Endpoint = s3.Endpoint
		resolved.S3Region = s3.Region
		resolved.S3PathStyle = s3.PathStyle
		if strings.HasSuffix(selector, "/") {
			resolved.ExpectedInputs = 0
		}
	}
	return resolved, nil
}

func (r SourcePolicyResolver) validateCredentialSecret(ctx context.Context, namespace string, ref *corev1.LocalObjectReference, kind tamossv1alpha1.IngestSourceKind) error {
	if ref == nil {
		return nil
	}
	if strings.TrimSpace(ref.Name) == "" {
		return fmt.Errorf("credentialSecretRef.name must not be empty")
	}
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("credential Secret %q does not exist", ref.Name)
		}
		return fmt.Errorf("read credential Secret %q: %w", ref.Name, err)
	}
	switch kind {
	case tamossv1alpha1.IngestSourceKindHTTP:
		raw := secret.Data[httpCredentialSecretKey]
		if len(raw) == 0 {
			return fmt.Errorf("credential Secret %q does not contain %s", ref.Name, httpCredentialSecretKey)
		}
		var headers []string
		if err := json.Unmarshal(raw, &headers); err != nil || len(headers) == 0 {
			return fmt.Errorf("credential Secret %q key %s must be a non-empty JSON string array", ref.Name, httpCredentialSecretKey)
		}
		for _, header := range headers {
			name, _, found := strings.Cut(header, ":")
			if !found || strings.TrimSpace(name) == "" || strings.ContainsAny(header, "\r\n") {
				return fmt.Errorf("credential Secret %q key %s contains an invalid HTTP header", ref.Name, httpCredentialSecretKey)
			}
		}
	case tamossv1alpha1.IngestSourceKindS3:
		if len(secret.Data[s3AccessKeySecretKey]) == 0 || len(secret.Data[s3SecretKeySecretKey]) == 0 {
			return fmt.Errorf("credential Secret %q must contain %s and %s", ref.Name, s3AccessKeySecretKey, s3SecretKeySecretKey)
		}
	}
	return nil
}

func (r SourcePolicyResolver) validatePublicHTTPSSelector(ctx context.Context, raw string) (string, error) {
	parsed, err := validateIngestSelectorURL(raw, "https")
	if err != nil {
		return "", err
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", fmt.Errorf("public HTTPS input must use port 443")
	}
	if err := r.validateResolvedHost(ctx, parsed.String(), false); err != nil {
		return "", fmt.Errorf("public HTTPS input is not permitted: %w", err)
	}
	return parsed.String(), nil
}

func (r SourcePolicyResolver) validateResolvedHost(ctx context.Context, raw string, allowPrivate bool) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("host is invalid")
	}
	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if !allowPrivate && !isPublicIngestAddress(ip) {
			return fmt.Errorf("host resolves to a non-public address")
		}
		return nil
	}
	resolver := r.HostResolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	lookupCtx, cancel := context.WithTimeout(ctx, ingestDNSLookupTimeout)
	defer cancel()
	addresses, err := resolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return fmt.Errorf("resolve host %q: %w", host, err)
	}
	if len(addresses) == 0 {
		return fmt.Errorf("host %q resolved to no addresses", host)
	}
	if allowPrivate {
		return nil
	}
	for _, address := range addresses {
		if !isPublicIngestAddress(address.IP) {
			return fmt.Errorf("host %q resolves to a non-public address", host)
		}
	}
	return nil
}

func isPublicIngestAddress(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() &&
		!ip.IsMulticast() && !ip.IsUnspecified()
}

func validateNamedHTTPSelector(raw string, source tamossv1alpha1.HTTPIngestSourceSpec) (string, error) {
	selector, err := validateIngestSelectorURL(raw, "https")
	if err != nil {
		return "", err
	}
	origin, err := validateSourceEndpoint(source.Origin)
	if err != nil {
		return "", fmt.Errorf("invalid HTTP source origin: %w", err)
	}
	if origin.Path != "" && origin.Path != "/" {
		return "", fmt.Errorf("HTTP source origin must not contain a path")
	}
	if !strings.EqualFold(selector.Scheme, origin.Scheme) || !strings.EqualFold(selector.Host, origin.Host) {
		return "", fmt.Errorf("HTTP input is outside source origin %q", source.Origin)
	}
	selectorPath, err := canonicalHTTPPath(selector.EscapedPath())
	if err != nil {
		return "", err
	}
	prefixes := make([]string, 0, len(source.PathPrefixes))
	for _, prefix := range source.PathPrefixes {
		if !strings.HasPrefix(prefix, "/") {
			return "", fmt.Errorf("HTTP source path prefixes must start with /")
		}
		canonical, err := canonicalHTTPPath(prefix)
		if err != nil {
			return "", fmt.Errorf("invalid HTTP source path prefix: %w", err)
		}
		prefixes = append(prefixes, canonical)
	}
	if !matchesPathPrefix(selectorPath, prefixes) {
		return "", fmt.Errorf("HTTP input path is outside the source path prefixes")
	}
	return selector.String(), nil
}

func validateNamedS3Selector(raw string, source tamossv1alpha1.S3IngestSourceSpec) (string, error) {
	if strings.TrimSpace(source.Region) == "" {
		return "", fmt.Errorf("S3 source region must not be empty")
	}
	if strings.TrimSpace(source.Bucket) == "" || strings.ContainsAny(source.Bucket, "/@?# ") {
		return "", fmt.Errorf("S3 source bucket is invalid")
	}
	for _, prefix := range source.KeyPrefixes {
		if prefix == "" || strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, "?#") {
			return "", fmt.Errorf("S3 source key prefixes must be non-empty relative keys without queries or fragments")
		}
	}
	selector, err := validateIngestSelectorURL(raw, "s3")
	if err != nil {
		return "", err
	}
	if selector.Host != source.Bucket {
		return "", fmt.Errorf("S3 input bucket %q does not match source bucket %q", selector.Host, source.Bucket)
	}
	key := strings.TrimPrefix(selector.EscapedPath(), "/")
	if key == "" {
		return "", fmt.Errorf("S3 input must select an object or bounded prefix")
	}
	if !matchesKeyPrefix(key, source.KeyPrefixes) {
		return "", fmt.Errorf("S3 input key is outside the source key prefixes")
	}
	endpoint, err := validateSourceEndpoint(source.Endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid S3 source endpoint: %w", err)
	}
	if endpoint.Path != "" && endpoint.Path != "/" {
		return "", fmt.Errorf("invalid S3 source endpoint: path is not permitted")
	}
	return selector.String(), nil
}

func validateIngestSelectorURL(raw, scheme string) (*url.URL, error) {
	if len(raw) > 2048 {
		return nil, fmt.Errorf("input URI exceeds 2048 bytes")
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme != scheme || parsed.Host == "" {
		return nil, fmt.Errorf("input URI must be an absolute %s URI", scheme)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("input URI must not contain user information, a query, or a fragment")
	}
	return parsed, nil
}

func validateSourceEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("must be an absolute HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("must not contain user information, a query, or a fragment")
	}
	return parsed, nil
}

func canonicalHTTPPath(escapedPath string) (string, error) {
	decoded, err := url.PathUnescape(escapedPath)
	if err != nil || strings.Contains(decoded, "\\") {
		return "", fmt.Errorf("HTTP input path is not canonical")
	}
	normalised := path.Clean("/" + strings.TrimPrefix(decoded, "/"))
	trimmed := strings.TrimSuffix(decoded, "/")
	if trimmed == "" {
		trimmed = "/"
	}
	if normalised != trimmed {
		return "", fmt.Errorf("HTTP input path must not contain dot segments")
	}
	return normalised, nil
}

func matchesPathPrefix(httpPath string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, prefix := range prefixes {
		candidate := strings.TrimSuffix(prefix, "/")
		if httpPath == candidate || strings.HasPrefix(httpPath, strings.TrimSuffix(candidate, "/")+"/") {
			return true
		}
	}
	return false
}

func matchesKeyPrefix(key string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(key, strings.TrimPrefix(prefix, "/")) {
			return true
		}
	}
	return false
}

// PublishedEndpointResolver selects the instance's own published TAMS endpoint.
type PublishedEndpointResolver struct {
	Client client.Reader
}

func (r PublishedEndpointResolver) Resolve(ctx context.Context, namespace, name string) (string, error) {
	tamoss := &tamossv1alpha1.Tamoss{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, tamoss); err != nil {
		return "", fmt.Errorf("read published endpoint: %w", err)
	}
	endpoint := tamoss.Status.Endpoints.API
	if endpoint == "" {
		return "", fmt.Errorf("instance %s/%s has not published an API endpoint", namespace, name)
	}
	return endpoint, nil
}
