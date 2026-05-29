package authentik

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagedBlueprintClientCreatesAppliesAndUpdates(t *testing.T) {
	var current ManagedBlueprint
	var creates, updates, applies int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("expected bearer token, got %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/managed/blueprints/":
			_ = json.NewEncoder(w).Encode(managedBlueprintList{Results: resultList(current)})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/managed/blueprints/":
			creates++
			var request managedBlueprintRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			if request.Path != "" {
				t.Fatalf("expected internal managed blueprint storage path, got %q", request.Path)
			}
			current = ManagedBlueprint{PK: "blueprint-id", Name: request.Name, Path: request.Path, Content: request.Content, Status: "unknown"}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(current)
		case r.Method == http.MethodPut && r.URL.Path == "/api/v3/managed/blueprints/blueprint-id/":
			updates++
			var request managedBlueprintRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode update request: %v", err)
			}
			current.Content = request.Content
			_ = json.NewEncoder(w).Encode(current)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/managed/blueprints/blueprint-id/apply/":
			applies++
			current.Status = "successful"
			_ = json.NewEncoder(w).Encode(current)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := ManagedBlueprintClient{BaseURL: server.URL, Token: "test-token"}
	first, err := client.Reconcile(context.Background(), "tamoss-default-example", "", []byte("version: 1\n"))
	if err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	if first.Status != "successful" || creates != 1 || updates != 0 || applies != 1 {
		t.Fatalf("unexpected first reconcile result: %#v creates=%d updates=%d applies=%d", first, creates, updates, applies)
	}
	second, err := client.Reconcile(context.Background(), "tamoss-default-example", "", []byte("version: 1\nentries: []\n"))
	if err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}
	if second.Content != "version: 1\nentries: []\n" || creates != 1 || updates != 1 || applies != 2 {
		t.Fatalf("unexpected second reconcile result: %#v creates=%d updates=%d applies=%d", second, creates, updates, applies)
	}
}

func TestManagedBlueprintClientReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer server.Close()

	client := ManagedBlueprintClient{BaseURL: server.URL, Token: "bad-token"}
	_, err := client.Reconcile(context.Background(), "tamoss-default-example", "", []byte("version: 1\n"))
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("expected HTTP 403 error, got %v", err)
	}
}

func resultList(current ManagedBlueprint) []ManagedBlueprint {
	if current.PK == "" {
		return nil
	}
	return []ManagedBlueprint{current}
}
