package authentik

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestProxyOutpostClientReconcilesAndDeletes(t *testing.T) {
	tamoss := proxyOutpostFixture()
	state := proxyOutpostServerState{oauth: proxyOutpostOAuth(), outpost: proxyOutpostEmbeddedOutpost()}
	server := httptest.NewServer(http.HandlerFunc(state.handle))
	defer server.Close()

	client := ProxyOutpostClient{BaseURL: server.URL, Token: "test-token"}
	if err := client.Reconcile(context.Background(), tamoss, successfulManagedBlueprint()); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if state.proxy.Name != "tamoss-tams-example-ui-proxy" {
		t.Fatalf("expected proxy provider to be created, got %#v", state.proxy)
	}
	if state.proxyRequest.ExternalHost != "https://app.example.com" {
		t.Fatalf("expected external app host, got %q", state.proxyRequest.ExternalHost)
	}
	if state.proxyRequest.InternalHost != "http://example-ui.tams.svc.cluster.local:3000" {
		t.Fatalf("expected internal UI service host, got %q", state.proxyRequest.InternalHost)
	}
	if state.application.Slug != "tamoss-tams-example-ui" || state.applicationRequest.Provider != 42 {
		t.Fatalf("expected proxy application to target provider 42, got app=%#v request=%#v", state.application, state.applicationRequest)
	}
	if got := state.outpost.Providers; len(got) != 2 || got[0] != 7 || got[1] != 42 {
		t.Fatalf("expected embedded outpost providers [7 42], got %#v", got)
	}
	if state.outpost.Config["authentik_host"] != "https://auth.example.com" ||
		state.outpost.Config["authentik_host_browser"] != "https://auth.example.com" {
		t.Fatalf("expected public Authentik host in outpost config, got %#v", state.outpost.Config)
	}
	if state.proxyPostCount != 1 || state.applicationPostCount != 1 || state.outpostPutCount != 1 {
		t.Fatalf("expected one create/update per resource, got proxy POSTs=%d application POSTs=%d outpost PUTs=%d", state.proxyPostCount, state.applicationPostCount, state.outpostPutCount)
	}

	if err := client.Reconcile(context.Background(), tamoss, successfulManagedBlueprint()); err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}
	if state.proxyPutCount != 0 || state.applicationPutCount != 0 || state.outpostPutCount != 1 {
		t.Fatalf("expected second reconcile to make no updates, got proxy PUTs=%d application PUTs=%d outpost PUTs=%d", state.proxyPutCount, state.applicationPutCount, state.outpostPutCount)
	}

	if err := client.Delete(context.Background(), tamoss); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if state.proxy.PK != 0 {
		t.Fatalf("expected proxy provider to be deleted, got %#v", state.proxy)
	}
	if state.application.PK != "" {
		t.Fatalf("expected proxy application to be deleted, got %#v", state.application)
	}
	if got := state.outpost.Providers; len(got) != 1 || got[0] != 7 {
		t.Fatalf("expected embedded outpost provider 42 to be removed, got %#v", got)
	}
}

func TestUIProxyExternalHostUsesExplicitPublicUIURL(t *testing.T) {
	tamoss := proxyOutpostFixture()
	tamoss.Spec.PublicEndpoint.UIURL = "https://app.example.com:30443/"

	got, err := UIProxyExternalHost(tamoss)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://app.example.com:30443" {
		t.Fatalf("proxy external host = %q", got)
	}
}

func TestUIProxyExternalHostRejectsInvalidPublicUIURL(t *testing.T) {
	tests := []string{
		"https://user@app.example.com:30443",
		"javascript://app.example.com",
		"https://other.example.com:30443",
		"https://app.example.com/path",
		"https://app.example.com:99999",
	}
	for _, uiURL := range tests {
		t.Run(uiURL, func(t *testing.T) {
			tamoss := proxyOutpostFixture()
			tamoss.Spec.PublicEndpoint.UIURL = uiURL
			if _, err := UIProxyExternalHost(tamoss); err == nil {
				t.Fatalf("expected public UI URL %q to be rejected", uiURL)
			}
		})
	}
}

func TestProxyOutpostClientFindsExistingApplicationWithSearch(t *testing.T) {
	tamoss := proxyOutpostFixture()
	state := proxyOutpostServerState{
		oauth:                     proxyOutpostOAuth(),
		proxy:                     proxyProvider{PK: 42, Name: "tamoss-tams-example-ui-proxy"},
		application:               application{PK: "existing-app-id", Name: "tamoss-tams-example-ui", Slug: "tamoss-tams-example-ui", Provider: 41},
		applicationSlugQueryStale: true,
		outpost:                   proxyOutpostEmbeddedOutpost(),
	}
	server := httptest.NewServer(http.HandlerFunc(state.handle))
	defer server.Close()

	client := ProxyOutpostClient{BaseURL: server.URL, Token: "test-token"}
	if err := client.Reconcile(context.Background(), tamoss, successfulManagedBlueprint()); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if state.applicationPostCount != 0 {
		t.Fatalf("expected existing application to be updated, got %d POSTs", state.applicationPostCount)
	}
	if state.applicationPutCount != 1 {
		t.Fatalf("expected existing application to be updated, got %d PUTs", state.applicationPutCount)
	}
	if !state.applicationSearchSeen {
		t.Fatalf("expected application lookup to use Authentik search query")
	}
}

func TestProxyOutpostClientReappliesBlueprintWhenOAuthProviderIsMissing(t *testing.T) {
	tamoss := proxyOutpostFixture()
	state := proxyOutpostServerState{outpost: proxyOutpostEmbeddedOutpost()}
	server := httptest.NewServer(http.HandlerFunc(state.handle))
	defer server.Close()

	client := ProxyOutpostClient{BaseURL: server.URL, Token: "test-token"}
	if err := client.Reconcile(context.Background(), tamoss, successfulManagedBlueprint()); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if state.blueprintApplyCount != 1 {
		t.Fatalf("expected missing OAuth provider to force one Blueprint apply, got %d", state.blueprintApplyCount)
	}
	if state.proxy.PK == 0 {
		t.Fatalf("expected proxy resources to reconcile after OAuth provider recovery")
	}
}

func TestProxyOutpostClientDoesNotReapplyTransientBlueprintWhenOAuthProviderIsMissing(t *testing.T) {
	tamoss := proxyOutpostFixture()
	state := proxyOutpostServerState{outpost: proxyOutpostEmbeddedOutpost()}
	server := httptest.NewServer(http.HandlerFunc(state.handle))
	defer server.Close()

	client := ProxyOutpostClient{BaseURL: server.URL, Token: "test-token"}
	transient := successfulManagedBlueprint()
	transient.Status = "unknown"
	transient.LastApplied = "2026-07-21T19:19:28Z"
	err := client.Reconcile(context.Background(), tamoss, transient)
	if err == nil || !strings.Contains(err.Error(), "OAuth2 provider") {
		t.Fatalf("expected missing OAuth provider error, got %v", err)
	}
	if state.blueprintApplyCount != 0 {
		t.Fatalf("expected transient Blueprint not to be reapplied, got %d applies", state.blueprintApplyCount)
	}
}

type proxyOutpostServerState struct {
	oauth                     oauthProvider
	proxy                     proxyProvider
	proxyRequest              proxyProviderRequest
	proxyPostCount            int
	proxyPutCount             int
	application               application
	applicationRequest        applicationRequest
	applicationSlugQueryStale bool
	applicationSearchSeen     bool
	applicationPostCount      int
	applicationPutCount       int
	applicationDeleteCount    int
	outpost                   outpost
	outpostPutCount           int
	blueprintApplyCount       int
}

func (s *proxyOutpostServerState) handle(w http.ResponseWriter, r *http.Request) {
	if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v3/providers/oauth2/":
		_ = json.NewEncoder(w).Encode(oauthProviderList{Results: oauthProviderResults(s.oauth)})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v3/managed/blueprints/blueprint-id/apply/":
		s.blueprintApplyCount++
		s.oauth = proxyOutpostOAuth()
		_ = json.NewEncoder(w).Encode(successfulManagedBlueprint())
	case r.Method == http.MethodGet && r.URL.Path == "/api/v3/providers/proxy/":
		_ = json.NewEncoder(w).Encode(proxyProviderList{Results: proxyProviderResults(s.proxy)})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v3/providers/proxy/":
		if err := json.NewDecoder(r.Body).Decode(&s.proxyRequest); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.proxyPostCount++
		s.proxy = proxyProviderFromRequest(42, s.proxyRequest)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(s.proxy)
	case r.Method == http.MethodPut && r.URL.Path == "/api/v3/providers/proxy/42/":
		if err := json.NewDecoder(r.Body).Decode(&s.proxyRequest); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.proxyPutCount++
		s.proxy = proxyProviderFromRequest(42, s.proxyRequest)
		_ = json.NewEncoder(w).Encode(s.proxy)
	case r.Method == http.MethodDelete && r.URL.Path == "/api/v3/providers/proxy/42/":
		s.proxy = proxyProvider{}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v3/core/applications/":
		query := r.URL.Query()
		if query.Get("search") != "" {
			s.applicationSearchSeen = true
			_ = json.NewEncoder(w).Encode(applicationList{Results: applicationResults(s.application)})
			return
		}
		if s.applicationSlugQueryStale && query.Get("slug") != "" {
			_ = json.NewEncoder(w).Encode(applicationList{Results: []application{{
				PK:   "other-app-id",
				Name: "other-app",
				Slug: "other-app",
			}}})
			return
		}
		_ = json.NewEncoder(w).Encode(applicationList{Results: applicationResults(s.application)})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v3/core/applications/":
		if err := json.NewDecoder(r.Body).Decode(&s.applicationRequest); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.applicationPostCount++
		if s.application.PK != "" && s.application.Slug == s.applicationRequest.Slug {
			http.Error(w, `{"slug":["Application with this slug already exists."],"provider":["Application with this provider already exists."]}`, http.StatusBadRequest)
			return
		}
		s.application = application{PK: "app-id", Name: s.applicationRequest.Name, Slug: s.applicationRequest.Slug, Provider: s.applicationRequest.Provider}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(s.application)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v3/core/applications/"):
		if strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v3/core/applications/"), "/") != s.application.Slug {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&s.applicationRequest); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.applicationPutCount++
		s.application = application{PK: s.application.PK, Name: s.applicationRequest.Name, Slug: s.applicationRequest.Slug, Provider: s.applicationRequest.Provider}
		_ = json.NewEncoder(w).Encode(s.application)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v3/core/applications/"):
		if strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v3/core/applications/"), "/") != s.application.Slug {
			http.NotFound(w, r)
			return
		}
		s.applicationDeleteCount++
		s.application = application{}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v3/outposts/instances/":
		_ = json.NewEncoder(w).Encode(outpostList{Results: []outpost{s.outpost}})
	case r.Method == http.MethodPut && r.URL.Path == "/api/v3/outposts/instances/outpost-id/":
		var request outpostRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.outpost.Name = request.Name
		s.outpost.Type = request.Type
		s.outpost.Providers = request.Providers
		s.outpost.Config = request.Config
		s.outpost.Managed = request.Managed
		s.outpostPutCount++
		_ = json.NewEncoder(w).Encode(s.outpost)
	default:
		http.NotFound(w, r)
	}
}

func proxyOutpostOAuth() oauthProvider {
	return oauthProvider{
		PK:                5,
		Name:              "tamoss-tams-example",
		AuthorizationFlow: "authorization-flow",
		InvalidationFlow:  "invalidation-flow",
		PropertyMappings:  []string{"openid", "profile"},
	}
}

func proxyOutpostEmbeddedOutpost() outpost {
	return outpost{
		PK:        "outpost-id",
		Name:      embeddedOutpostName,
		Type:      "proxy",
		Providers: []int{7},
		Config:    map[string]any{"log_level": "info"},
		Managed:   "goauthentik.io/outposts/embedded",
	}
}

func proxyProviderResults(current proxyProvider) []proxyProvider {
	if current.PK == 0 {
		return nil
	}
	return []proxyProvider{current}
}

func oauthProviderResults(current oauthProvider) []oauthProvider {
	if current.PK == 0 {
		return nil
	}
	return []oauthProvider{current}
}

func successfulManagedBlueprint() ManagedBlueprint {
	return ManagedBlueprint{PK: "blueprint-id", Name: "tamoss-tams-example", Status: "successful"}
}

func proxyProviderFromRequest(pk int, request proxyProviderRequest) proxyProvider {
	return proxyProvider{
		PK:                        pk,
		Name:                      request.Name,
		AuthorizationFlow:         request.AuthorizationFlow,
		InvalidationFlow:          request.InvalidationFlow,
		PropertyMappings:          append(append([]string(nil), request.PropertyMappings...), "authentik-managed"),
		ExternalHost:              request.ExternalHost,
		InternalHost:              request.InternalHost,
		InternalHostSSLValidation: request.InternalHostSSLValidation,
		Mode:                      request.Mode,
	}
}

func applicationResults(current application) []application {
	if current.PK == "" {
		return nil
	}
	return []application{current}
}

func proxyOutpostFixture() *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "tams"},
		Spec: tamossv1alpha1.TamossSpec{
			UI: tamossv1alpha1.UIComponentSpec{Enabled: ptr.To(true)},
			Auth: tamossv1alpha1.AuthSpec{
				ProvidedBy: tamossv1alpha1.AuthProvidedByAuthentikBlueprints,
				Required:   true,
				AuthentikBlueprints: &tamossv1alpha1.AuthentikBlueprintsSpec{
					PlatformNamespace: "auth",
					IssuerURL:         "https://auth.example.com",
					InternalURL:       "http://authentik-server.auth.svc.cluster.local",
				},
			},
			Ingress: tamossv1alpha1.IngressSpec{
				Enabled:   ptr.To(true),
				ClassName: "traefik",
				TLS:       []networkingv1.IngressTLS{{SecretName: "tamoss-tls"}},
				UI: tamossv1alpha1.UIIngressSpec{Web: tamossv1alpha1.IngressHostSpec{
					Host: "app.example.com",
				}},
			},
		},
	}
}
