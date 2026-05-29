package authentik

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestProxyOutpostClientReconcilesAndDeletes(t *testing.T) {
	tamoss := proxyOutpostFixture()
	state := proxyOutpostServerState{
		oauth: oauthProvider{
			PK:                5,
			Name:              "tamoss-tams-example",
			AuthorizationFlow: "authorization-flow",
			InvalidationFlow:  "invalidation-flow",
			PropertyMappings:  []string{"openid", "profile"},
		},
		outpost: outpost{
			PK:        "outpost-id",
			Name:      embeddedOutpostName,
			Type:      "proxy",
			Providers: []int{7},
			Config:    map[string]any{"log_level": "info"},
			Managed:   "goauthentik.io/outposts/embedded",
		},
	}
	server := httptest.NewServer(http.HandlerFunc(state.handle))
	defer server.Close()

	client := ProxyOutpostClient{BaseURL: server.URL, Token: "test-token"}
	if err := client.Reconcile(context.Background(), tamoss); err != nil {
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

type proxyOutpostServerState struct {
	oauth              oauthProvider
	proxy              proxyProvider
	proxyRequest       proxyProviderRequest
	application        application
	applicationRequest applicationRequest
	outpost            outpost
}

func (s *proxyOutpostServerState) handle(w http.ResponseWriter, r *http.Request) {
	if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v3/providers/oauth2/":
		_ = json.NewEncoder(w).Encode(oauthProviderList{Results: []oauthProvider{s.oauth}})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v3/providers/proxy/":
		_ = json.NewEncoder(w).Encode(proxyProviderList{Results: proxyProviderResults(s.proxy)})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v3/providers/proxy/":
		if err := json.NewDecoder(r.Body).Decode(&s.proxyRequest); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.proxy = proxyProvider{PK: 42, Name: s.proxyRequest.Name}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(s.proxy)
	case r.Method == http.MethodPut && r.URL.Path == "/api/v3/providers/proxy/42/":
		if err := json.NewDecoder(r.Body).Decode(&s.proxyRequest); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.proxy = proxyProvider{PK: 42, Name: s.proxyRequest.Name}
		_ = json.NewEncoder(w).Encode(s.proxy)
	case r.Method == http.MethodDelete && r.URL.Path == "/api/v3/providers/proxy/42/":
		s.proxy = proxyProvider{}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v3/core/applications/":
		_ = json.NewEncoder(w).Encode(applicationList{Results: applicationResults(s.application)})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v3/core/applications/":
		if err := json.NewDecoder(r.Body).Decode(&s.applicationRequest); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.application = application{PK: "app-id", Name: s.applicationRequest.Name, Slug: s.applicationRequest.Slug}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(s.application)
	case r.Method == http.MethodPut && r.URL.Path == "/api/v3/core/applications/tamoss-tams-example-ui/":
		if err := json.NewDecoder(r.Body).Decode(&s.applicationRequest); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.application = application{PK: "app-id", Name: s.applicationRequest.Name, Slug: s.applicationRequest.Slug}
		_ = json.NewEncoder(w).Encode(s.application)
	case r.Method == http.MethodDelete && r.URL.Path == "/api/v3/core/applications/tamoss-tams-example-ui/":
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
		_ = json.NewEncoder(w).Encode(s.outpost)
	default:
		http.NotFound(w, r)
	}
}

func proxyProviderResults(current proxyProvider) []proxyProvider {
	if current.PK == 0 {
		return nil
	}
	return []proxyProvider{current}
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
