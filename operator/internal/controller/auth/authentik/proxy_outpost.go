package authentik

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	embeddedOutpostName = "authentik Embedded Outpost"
)

type ProxyOutpostClient struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

type oauthProvider struct {
	PK                int      `json:"pk"`
	Name              string   `json:"name"`
	AuthorizationFlow string   `json:"authorization_flow"`
	InvalidationFlow  string   `json:"invalidation_flow"`
	PropertyMappings  []string `json:"property_mappings"`
}

type oauthProviderList struct {
	Results []oauthProvider `json:"results"`
}

type proxyProvider struct {
	PK                        int      `json:"pk"`
	Name                      string   `json:"name"`
	AuthorizationFlow         string   `json:"authorization_flow"`
	InvalidationFlow          string   `json:"invalidation_flow"`
	PropertyMappings          []string `json:"property_mappings"`
	ExternalHost              string   `json:"external_host"`
	InternalHost              string   `json:"internal_host"`
	InternalHostSSLValidation bool     `json:"internal_host_ssl_validation"`
	Mode                      string   `json:"mode"`
}

type proxyProviderList struct {
	Results []proxyProvider `json:"results"`
}

type proxyProviderRequest struct {
	Name                      string   `json:"name"`
	AuthorizationFlow         string   `json:"authorization_flow"`
	InvalidationFlow          string   `json:"invalidation_flow,omitempty"`
	PropertyMappings          []string `json:"property_mappings,omitempty"`
	ExternalHost              string   `json:"external_host"`
	InternalHost              string   `json:"internal_host"`
	InternalHostSSLValidation bool     `json:"internal_host_ssl_validation"`
	Mode                      string   `json:"mode"`
}

type application struct {
	PK       string `json:"pk"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Provider int    `json:"provider"`
}

type applicationList struct {
	Results []application `json:"results"`
}

type applicationRequest struct {
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Provider int    `json:"provider"`
}

type outpost struct {
	PK        string         `json:"pk"`
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	Providers []int          `json:"providers"`
	Config    map[string]any `json:"config"`
	Managed   string         `json:"managed,omitempty"`
}

type outpostList struct {
	Results []outpost `json:"results"`
}

type outpostRequest struct {
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	Providers []int          `json:"providers"`
	Config    map[string]any `json:"config"`
	Managed   string         `json:"managed,omitempty"`
}

func ForwardAuthRequired(tamoss *tamossv1alpha1.Tamoss) bool {
	return tamoss.Spec.Auth.Provider() == tamossv1alpha1.AuthProvidedByAuthentikBlueprints &&
		tamoss.Spec.Auth.RequiredForRuntime() &&
		tamoss.Spec.Auth.AuthentikBlueprints != nil &&
		tamoss.Spec.Ingress.IsEnabled() &&
		tamoss.Spec.UI.IsEnabled() &&
		strings.EqualFold(strings.TrimSpace(tamoss.Spec.Ingress.ClassName), "traefik") &&
		strings.TrimSpace(tamoss.Spec.Ingress.UI.Web.Host) != ""
}

func ProxyProviderName(tamoss *tamossv1alpha1.Tamoss) string {
	return tamoss.Spec.Auth.ApplicationSlug(tamoss.Namespace, tamoss.Name) + "-ui-proxy"
}

func ProxyApplicationSlug(tamoss *tamossv1alpha1.Tamoss) string {
	return tamoss.Spec.Auth.ApplicationSlug(tamoss.Namespace, tamoss.Name) + "-ui"
}

func UIProxyExternalHost(tamoss *tamossv1alpha1.Tamoss) (string, error) {
	scheme := "http"
	if len(tamoss.Spec.Ingress.TLS) > 0 {
		scheme = "https"
	}
	host := strings.TrimSpace(tamoss.Spec.Ingress.UI.Web.Host)
	rawURL := strings.TrimSpace(tamoss.Spec.PublicEndpoint.UIURL)
	if rawURL == "" {
		return fmt.Sprintf("%s://%s", scheme, host), nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" ||
		(!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("publicEndpoint.uiURL must be an absolute HTTP(S) origin")
	}
	if host != "" && !strings.EqualFold(parsed.Hostname(), host) {
		return "", fmt.Errorf("publicEndpoint.uiURL host must match ingress.ui.web.host")
	}
	if port := parsed.Port(); port != "" {
		value, parseErr := strconv.ParseUint(port, 10, 16)
		if parseErr != nil || value == 0 {
			return "", fmt.Errorf("publicEndpoint.uiURL contains an invalid port")
		}
	}
	return strings.ToLower(parsed.Scheme) + "://" + parsed.Host, nil
}

func UIProxyInternalHost(tamoss *tamossv1alpha1.Tamoss) string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", tamoss.ResourceName("ui"), tamoss.Namespace, uiServicePort(tamoss))
}

func OutpostForwardAuthAddress(tamoss *tamossv1alpha1.Tamoss) string {
	return outpostForwardAuthAddress(tamoss, "traefik")
}

func OutpostNginxAuthAddress(tamoss *tamossv1alpha1.Tamoss) string {
	return outpostForwardAuthAddress(tamoss, "nginx")
}

func outpostForwardAuthAddress(tamoss *tamossv1alpha1.Tamoss, provider string) string {
	base := authentikBaseURL(tamoss)
	joined, err := url.JoinPath(base, "outpost.goauthentik.io", "auth", provider)
	if err == nil {
		return joined
	}
	return strings.TrimRight(base, "/") + "/outpost.goauthentik.io/auth/" + provider
}

func OutpostExternalService(tamoss *tamossv1alpha1.Tamoss) (string, int32) {
	parsed, err := url.Parse(authentikBaseURL(tamoss))
	if err != nil || parsed.Host == "" {
		return "", 0
	}
	host := parsed.Hostname()
	port := int32(80)
	if parsed.Scheme == "https" {
		port = 443
	}
	if explicit := parsed.Port(); explicit != "" {
		parsedPort, err := strconv.ParseInt(explicit, 10, 32)
		if err == nil && parsedPort > 0 {
			port = int32(parsedPort)
		}
	}
	if host == "" {
		if splitHost, _, err := net.SplitHostPort(parsed.Host); err == nil {
			host = splitHost
		} else {
			host = parsed.Host
		}
	}
	return host, port
}

func (c ProxyOutpostClient) Reconcile(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, managedBlueprint ManagedBlueprint) error {
	if !ForwardAuthRequired(tamoss) {
		return nil
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultAPIOperationTimeout)
		defer cancel()
	}
	api, err := c.managedBlueprintClient()
	if err != nil {
		return err
	}
	slug := tamoss.Spec.Auth.ApplicationSlug(tamoss.Namespace, tamoss.Name)
	oauth, err := findOAuthProvider(ctx, api, slug)
	if err != nil {
		return err
	}
	// A successful Blueprint can retain its status after a managed object is
	// deleted manually. Reapply only for that proven-drift case; transient
	// Blueprint states remain protected from duplicate queued applies.
	if oauth.PK == 0 && managedBlueprint.PK != "" && managedBlueprint.Status == "successful" {
		log.FromContext(ctx).Info("reapplying Authentik managed Blueprint to repair missing OAuth2 provider",
			"name", managedBlueprint.Name,
			"provider", slug,
		)
		applied, err := api.apply(ctx, managedBlueprint.PK)
		if err != nil {
			return fmt.Errorf("reapply Authentik managed Blueprint %q: %w", managedBlueprint.Name, err)
		}
		if applied.Status == "error" {
			return fmt.Errorf("authentik managed blueprint %q applied with status error", managedBlueprint.Name)
		}
		oauth, err = findOAuthProvider(ctx, api, slug)
		if err != nil {
			return err
		}
	}
	if oauth.PK == 0 {
		return fmt.Errorf("authentik OAuth2 provider %q was not found", slug)
	}
	proxy, err := upsertProxyProvider(ctx, api, tamoss, oauth)
	if err != nil {
		return err
	}
	if err := upsertProxyApplication(ctx, api, tamoss, proxy.PK); err != nil {
		return err
	}
	return addProviderToEmbeddedOutpost(ctx, api, tamoss, proxy.PK)
}

func (c ProxyOutpostClient) Delete(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultAPIOperationTimeout)
		defer cancel()
	}
	api, err := c.managedBlueprintClient()
	if err != nil {
		return err
	}
	proxy, err := findProxyProvider(ctx, api, ProxyProviderName(tamoss))
	if err != nil {
		return err
	}
	if proxy.PK != 0 {
		if err := removeProviderFromEmbeddedOutpost(ctx, api, proxy.PK); err != nil {
			return err
		}
	}
	if err := deleteProxyApplication(ctx, api, ProxyApplicationSlug(tamoss)); err != nil {
		return err
	}
	if proxy.PK != 0 {
		return deleteProxyProvider(ctx, api, proxy.PK)
	}
	return nil
}

func (c ProxyOutpostClient) managedBlueprintClient() (ManagedBlueprintClient, error) {
	if strings.TrimSpace(c.Token) == "" {
		return ManagedBlueprintClient{}, fmt.Errorf("authentik API token is required")
	}
	return ManagedBlueprintClient(c), nil
}

// findByQuery lists an Authentik API collection filtered by a query parameter
// and returns the first result accepted by match.
func findByQuery[T any](ctx context.Context, api ManagedBlueprintClient, segments []string, queryKey, queryValue string, match func(T) bool) (T, error) {
	var zero T
	endpoint, err := api.apiURL(segments...)
	if err != nil {
		return zero, err
	}
	query := endpoint.Query()
	query.Set(queryKey, queryValue)
	query.Set("page_size", "100")
	for {
		endpoint.RawQuery = query.Encode()
		var list struct {
			Results    []T `json:"results"`
			Pagination struct {
				Next int `json:"next"`
			} `json:"pagination"`
		}
		if err := api.doJSON(ctx, http.MethodGet, endpoint.String(), nil, &list); err != nil {
			return zero, err
		}
		for _, result := range list.Results {
			if match(result) {
				return result, nil
			}
		}
		if list.Pagination.Next <= 0 {
			return zero, nil
		}
		query.Set("page", strconv.Itoa(list.Pagination.Next))
	}
}

func findBySearch[T any](ctx context.Context, api ManagedBlueprintClient, segments []string, searchValue string, match func(T) bool) (T, error) {
	return findByQuery(ctx, api, segments, "search", searchValue, match)
}

func findOAuthProvider(ctx context.Context, api ManagedBlueprintClient, name string) (oauthProvider, error) {
	return findBySearch(ctx, api, []string{"providers", "oauth2"}, name, func(provider oauthProvider) bool {
		return provider.Name == name
	})
}

func findProxyProvider(ctx context.Context, api ManagedBlueprintClient, name string) (proxyProvider, error) {
	return findBySearch(ctx, api, []string{"providers", "proxy"}, name, func(provider proxyProvider) bool {
		return provider.Name == name
	})
}

func upsertProxyProvider(ctx context.Context, api ManagedBlueprintClient, tamoss *tamossv1alpha1.Tamoss, oauth oauthProvider) (proxyProvider, error) {
	name := ProxyProviderName(tamoss)
	externalHost, err := UIProxyExternalHost(tamoss)
	if err != nil {
		return proxyProvider{}, err
	}
	request := proxyProviderRequest{
		Name:                      name,
		AuthorizationFlow:         oauth.AuthorizationFlow,
		InvalidationFlow:          oauth.InvalidationFlow,
		PropertyMappings:          append([]string(nil), oauth.PropertyMappings...),
		ExternalHost:              externalHost,
		InternalHost:              UIProxyInternalHost(tamoss),
		InternalHostSSLValidation: false,
		Mode:                      "forward_single",
	}
	existing, err := findProxyProvider(ctx, api, name)
	if err != nil {
		return proxyProvider{}, err
	}
	if proxyProviderMatches(existing, request) {
		return existing, nil
	}
	endpoint, err := api.apiURL("providers", "proxy")
	if err != nil {
		return proxyProvider{}, err
	}
	method := http.MethodPost
	if existing.PK != 0 {
		method = http.MethodPut
		endpoint, err = api.apiURL("providers", "proxy", strconv.Itoa(existing.PK))
		if err != nil {
			return proxyProvider{}, err
		}
	}
	var provider proxyProvider
	if err := api.doJSON(ctx, method, endpoint.String(), request, &provider); err != nil {
		return proxyProvider{}, err
	}
	return provider, nil
}

func upsertProxyApplication(ctx context.Context, api ManagedBlueprintClient, tamoss *tamossv1alpha1.Tamoss, providerPK int) error {
	slug := ProxyApplicationSlug(tamoss)
	request := applicationRequest{
		Name:     slug,
		Slug:     slug,
		Provider: providerPK,
	}
	existing, err := findApplication(ctx, api, slug)
	if err != nil {
		return err
	}
	if existing.PK != "" && existing.Name == request.Name && existing.Slug == request.Slug && existing.Provider == request.Provider {
		return nil
	}
	endpoint, err := api.apiURL("core", "applications")
	if err != nil {
		return err
	}
	method := http.MethodPost
	if existing.PK != "" {
		method = http.MethodPut
		endpoint, err = api.apiURL("core", "applications", slug)
		if err != nil {
			return err
		}
	}
	return api.doJSON(ctx, method, endpoint.String(), request, nil)
}

func findApplication(ctx context.Context, api ManagedBlueprintClient, slug string) (application, error) {
	return findBySearch(ctx, api, []string{"core", "applications"}, slug, func(app application) bool {
		return app.Slug == slug
	})
}

func addProviderToEmbeddedOutpost(ctx context.Context, api ManagedBlueprintClient, tamoss *tamossv1alpha1.Tamoss, providerPK int) error {
	outpost, err := findEmbeddedOutpost(ctx, api)
	if err != nil {
		return err
	}
	if outpost.PK == "" {
		return fmt.Errorf("authentik embedded outpost %q was not found", embeddedOutpostName)
	}
	providers := appendProvider(outpost.Providers, providerPK)
	config := copyMap(outpost.Config)
	publicHost := strings.TrimRight(tamoss.Spec.Auth.AuthentikBlueprints.IssuerURL, "/")
	config["authentik_host"] = publicHost
	config["authentik_host_browser"] = publicHost
	if equalIntValues(outpost.Providers, providers) && reflect.DeepEqual(outpost.Config, config) {
		return nil
	}
	return updateOutpost(ctx, api, outpost, providers, config)
}

func removeProviderFromEmbeddedOutpost(ctx context.Context, api ManagedBlueprintClient, providerPK int) error {
	outpost, err := findEmbeddedOutpost(ctx, api)
	if err != nil || outpost.PK == "" {
		return err
	}
	providers, removed := removeProvider(outpost.Providers, providerPK)
	if !removed {
		return nil
	}
	return updateOutpost(ctx, api, outpost, providers, outpost.Config)
}

func findEmbeddedOutpost(ctx context.Context, api ManagedBlueprintClient) (outpost, error) {
	return findBySearch(ctx, api, []string{"outposts", "instances"}, embeddedOutpostName, func(candidate outpost) bool {
		return candidate.Name == embeddedOutpostName
	})
}

func updateOutpost(ctx context.Context, api ManagedBlueprintClient, current outpost, providers []int, config map[string]any) error {
	endpoint, err := api.apiURL("outposts", "instances", current.PK)
	if err != nil {
		return err
	}
	sort.Ints(providers)
	request := outpostRequest{
		Name:      current.Name,
		Type:      current.Type,
		Providers: providers,
		Config:    copyMap(config),
		Managed:   current.Managed,
	}
	return api.doJSON(ctx, http.MethodPut, endpoint.String(), request, nil)
}

func deleteProxyApplication(ctx context.Context, api ManagedBlueprintClient, slug string) error {
	app, err := findApplication(ctx, api, slug)
	if err != nil || app.PK == "" {
		return err
	}
	endpoint, err := api.apiURL("core", "applications", slug)
	if err != nil {
		return err
	}
	return api.doJSON(ctx, http.MethodDelete, endpoint.String(), nil, nil)
}

func deleteProxyProvider(ctx context.Context, api ManagedBlueprintClient, pk int) error {
	endpoint, err := api.apiURL("providers", "proxy", strconv.Itoa(pk))
	if err != nil {
		return err
	}
	return api.doJSON(ctx, http.MethodDelete, endpoint.String(), nil, nil)
}

func authentikBaseURL(tamoss *tamossv1alpha1.Tamoss) string {
	if tamoss.Spec.Auth.AuthentikBlueprints == nil {
		return ""
	}
	if tamoss.Spec.Auth.AuthentikBlueprints.InternalURL != "" {
		return strings.TrimSpace(tamoss.Spec.Auth.AuthentikBlueprints.InternalURL)
	}
	return strings.TrimSpace(tamoss.Spec.Auth.AuthentikBlueprints.IssuerURL)
}

func uiServicePort(tamoss *tamossv1alpha1.Tamoss) int32 {
	for _, port := range tamoss.Spec.Service.UI.Ports {
		if port.Name == "http" && port.Port > 0 {
			return port.Port
		}
	}
	for _, port := range tamoss.Spec.Service.UI.Ports {
		if port.Port > 0 {
			return port.Port
		}
	}
	return 3000
}

func appendProvider(providers []int, provider int) []int {
	next := append([]int(nil), providers...)
	for _, existing := range next {
		if existing == provider {
			sort.Ints(next)
			return next
		}
	}
	next = append(next, provider)
	sort.Ints(next)
	return next
}

func removeProvider(providers []int, provider int) ([]int, bool) {
	next := make([]int, 0, len(providers))
	removed := false
	for _, existing := range providers {
		if existing == provider {
			removed = true
			continue
		}
		next = append(next, existing)
	}
	return next, removed
}

func copyMap(input map[string]any) map[string]any {
	copied := make(map[string]any, len(input))
	for key, value := range input {
		copied[key] = value
	}
	return copied
}

func proxyProviderMatches(existing proxyProvider, desired proxyProviderRequest) bool {
	return existing.PK != 0 &&
		existing.Name == desired.Name &&
		existing.AuthorizationFlow == desired.AuthorizationFlow &&
		existing.InvalidationFlow == desired.InvalidationFlow &&
		containsStringValues(existing.PropertyMappings, desired.PropertyMappings) &&
		existing.ExternalHost == desired.ExternalHost &&
		existing.InternalHost == desired.InternalHost &&
		existing.InternalHostSSLValidation == desired.InternalHostSSLValidation &&
		existing.Mode == desired.Mode
}

func containsStringValues(existing, desired []string) bool {
	// Authentik adds proxy-specific mappings, so desired values must be a
	// subset; exact equality would cause a PUT on every reconciliation.
	available := make(map[string]struct{}, len(existing))
	for _, value := range existing {
		available[value] = struct{}{}
	}
	for _, value := range desired {
		if _, found := available[value]; !found {
			return false
		}
	}
	return true
}

func equalIntValues(left, right []int) bool {
	left = append([]int(nil), left...)
	right = append([]int(nil), right...)
	sort.Ints(left)
	sort.Ints(right)
	return slices.Equal(left, right)
}

func ServicePortNameForOutpost(port int32) string {
	if port == 443 {
		return "https"
	}
	return "http"
}

func OutpostServicePort(port int32) corev1.ServicePort {
	return corev1.ServicePort{
		Name:     ServicePortNameForOutpost(port),
		Port:     port,
		Protocol: corev1.ProtocolTCP,
	}
}
