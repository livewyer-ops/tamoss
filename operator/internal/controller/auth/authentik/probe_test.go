package authentik

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProbeSucceedsWithValidDiscoveryDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/application/o/tamoss-prod/.well-known/openid-configuration" {
			t.Fatalf("unexpected probe path %q", r.URL.Path)
		}
		if _, err := fmt.Fprint(w, `{"issuer":"https://auth.example.com/application/o/tamoss-prod/","authorization_endpoint":"https://auth.example.com/auth","token_endpoint":"https://auth.example.com/token","jwks_uri":"https://auth.example.com/jwks"}`); err != nil {
			t.Fatalf("write discovery response: %v", err)
		}
	}))
	defer server.Close()

	if err := ProbeWithClient(context.Background(), server.Client(), server.URL, "tamoss-prod"); err != nil {
		t.Fatalf("expected probe to succeed: %v", err)
	}
}

func TestProbeHandlesClusterInternalIssuerURL(t *testing.T) {
	got, err := discoveryURL("http://authentik.auth.svc:9000", "tamoss-prod")
	if err != nil {
		t.Fatalf("discoveryURL failed: %v", err)
	}
	want := "http://authentik.auth.svc:9000/application/o/tamoss-prod/.well-known/openid-configuration"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestProbeReportsNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	err := ProbeWithClient(context.Background(), server.Client(), server.URL, "missing")
	if err == nil || !strings.Contains(err.Error(), "unexpected HTTP status 404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
}

func TestProbeReportsConnectionRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	err := ProbeWithClient(context.Background(), server.Client(), url, "down")
	if err == nil {
		t.Fatalf("expected connection error")
	}
}

func TestProbeHonoursContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := ProbeWithClient(ctx, server.Client(), server.URL, "slow")
	if err == nil {
		t.Fatalf("expected timeout error")
	}
}
