package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/consoleapi"
)

const testConsoleProof = "0123456789abcdef0123456789abcdef"

func TestConsoleAuthenticatorFromEnvironmentRequiresExplicitMode(t *testing.T) {
	clearConsoleAuthEnvironment(t)
	if _, err := consoleAuthenticatorFromEnvironment(); err == nil || !strings.Contains(err.Error(), consoleAuthModeEnv) {
		t.Fatalf("consoleAuthenticatorFromEnvironment() error = %v, want missing mode", err)
	}
}

func TestConsoleAuthenticatorFromEnvironmentAllowsExplicitDevelopmentMode(t *testing.T) {
	clearConsoleAuthEnvironment(t)
	t.Setenv(consoleAuthModeEnv, consoleAuthModeDevelopment)
	authenticator, err := consoleAuthenticatorFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := authenticator.Authenticate(httptest.NewRequest(http.MethodGet, consoleapi.RuntimePath, nil))
	if err != nil {
		t.Fatal(err)
	}
	if identity.Method != "development-anonymous" || !identity.CanView() {
		t.Fatalf("unexpected development identity: %#v", identity)
	}
}

func TestConsoleAuthenticatorFromEnvironmentAllowsFailClosedUnavailableMode(t *testing.T) {
	clearConsoleAuthEnvironment(t)
	t.Setenv(consoleAuthModeEnv, consoleAuthModeUnavailable)
	authenticator, err := consoleAuthenticatorFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authenticator.Authenticate(httptest.NewRequest(http.MethodGet, consoleapi.RuntimePath, nil)); !errors.Is(err, consoleapi.ErrUnauthenticated) {
		t.Fatalf("unavailable authenticator error = %v, want %v", err, consoleapi.ErrUnauthenticated)
	}
}

func TestConsoleAuthenticatorFromEnvironmentLoadsForwardAuthFiles(t *testing.T) {
	clearConsoleAuthEnvironment(t)
	proofPath := filepath.Join(t.TempDir(), "proof")
	if err := os.WriteFile(proofPath, []byte(testConsoleProof+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(consoleAuthModeEnv, consoleAuthModeForwardAuth)
	t.Setenv(consoleForwardAuthSecretFileEnv, proofPath)
	t.Setenv(consoleGroupRoleBindingsEnv, `[{"groupName":"tamoss-admins","permissions":["admin"]}]`)

	authenticator, err := consoleAuthenticatorFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, consoleapi.RuntimePath, nil)
	request.Header.Set(consoleapi.ForwardAuthSecretHeader, testConsoleProof)
	request.Header.Set(consoleapi.ForwardAuthSubjectHeader, "user-123")
	request.Header.Set(consoleapi.ForwardAuthGroupsHeader, "tamoss-admins")
	identity, err := authenticator.Authenticate(request)
	if err != nil {
		t.Fatal(err)
	}
	if !identity.HasRole(consoleapi.RoleViewer) || !identity.HasRole(consoleapi.RoleOperator) || !identity.HasRole(consoleapi.RoleIngestRunner) {
		t.Fatalf("admin binding did not expand to all Console roles: %#v", identity)
	}
}

func TestConsoleAuthenticatorFromEnvironmentRejectsInvalidForwardAuthConfig(t *testing.T) {
	tests := []struct {
		name         string
		mode         string
		secretPath   string
		bindingsJSON string
	}{
		{name: "unsupported mode", mode: "optional"},
		{name: "missing secret path", mode: consoleAuthModeForwardAuth, bindingsJSON: `[{"groupName":"viewers","permissions":["viewer"]}]`},
		{name: "relative secret path", mode: consoleAuthModeForwardAuth, secretPath: "proof", bindingsJSON: `[{"groupName":"viewers","permissions":["viewer"]}]`},
		{name: "missing bindings", mode: consoleAuthModeForwardAuth, secretPath: "valid"},
		{name: "invalid bindings", mode: consoleAuthModeForwardAuth, secretPath: "valid", bindingsJSON: `[{"groupName":"viewers","permissions":["owner"]}]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearConsoleAuthEnvironment(t)
			t.Setenv(consoleAuthModeEnv, test.mode)
			secretPath := test.secretPath
			if secretPath == "valid" {
				secretPath = filepath.Join(t.TempDir(), "proof")
				if err := os.WriteFile(secretPath, []byte(testConsoleProof), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv(consoleForwardAuthSecretFileEnv, secretPath)
			t.Setenv(consoleGroupRoleBindingsEnv, test.bindingsJSON)
			if _, err := consoleAuthenticatorFromEnvironment(); err == nil {
				t.Fatal("consoleAuthenticatorFromEnvironment() succeeded, want error")
			}
		})
	}
}

func TestReadForwardAuthSecretFileIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proof")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxForwardAuthSecretFileSize+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readForwardAuthSecretFile(path); err == nil {
		t.Fatal("readForwardAuthSecretFile() succeeded, want size error")
	}
}

func TestForwardAuthConfigDoesNotAcceptWrongProof(t *testing.T) {
	clearConsoleAuthEnvironment(t)
	path := filepath.Join(t.TempDir(), "proof")
	if err := os.WriteFile(path, []byte(testConsoleProof), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(consoleAuthModeEnv, consoleAuthModeForwardAuth)
	t.Setenv(consoleForwardAuthSecretFileEnv, path)
	t.Setenv(consoleGroupRoleBindingsEnv, `[{"groupName":"viewers","permissions":["viewer"]}]`)
	authenticator, err := consoleAuthenticatorFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, consoleapi.RuntimePath, nil)
	request.Header.Set(consoleapi.ForwardAuthSecretHeader, strings.Repeat("x", len(testConsoleProof)))
	request.Header.Set(consoleapi.ForwardAuthSubjectHeader, "user-123")
	request.Header.Set(consoleapi.ForwardAuthGroupsHeader, "viewers")
	if _, err := authenticator.Authenticate(request); !errors.Is(err, consoleapi.ErrUnauthenticated) {
		t.Fatalf("Authenticate() error = %v, want %v", err, consoleapi.ErrUnauthenticated)
	}
}

func TestRuntimeCacheScopesEveryInformer(t *testing.T) {
	namespaces := map[string]cache.Config{"tams": {}}
	scopes := runtimeCacheByObject(namespaces, "media")
	wantLabelSelector := instanceLabel + "=media"
	seen := map[string]bool{}
	for object, scope := range scopes {
		if len(scope.Namespaces) != 1 {
			t.Fatalf("%T cache namespaces = %#v, want only tams", object, scope.Namespaces)
		}
		if _, found := scope.Namespaces["tams"]; !found {
			t.Fatalf("%T cache namespaces = %#v, want tams", object, scope.Namespaces)
		}

		kind := ""
		switch object.(type) {
		case *tamossv1alpha1.Tamoss:
			kind = "Tamoss"
			if scope.Label != nil || scope.Field == nil || scope.Field.String() != "metadata.name=media" {
				t.Fatalf("Tamoss cache must select the exact instance name: %#v", scope)
			}
		case *tamossv1alpha1.StorageBackend:
			kind = "StorageBackend"
			if scope.Label != nil || scope.Field != nil {
				t.Fatalf("%T cache must be namespace-wide: %#v", object, scope)
			}
		case *corev1.Event:
			kind = "Event"
			if scope.Label != nil || scope.Field != nil {
				t.Fatalf("%T cache must be namespace-wide: %#v", object, scope)
			}
		case *appsv1.Deployment:
			kind = "Deployment"
		case *appsv1.ReplicaSet:
			kind = "ReplicaSet"
		case *corev1.Service:
			kind = "Service"
		case *discoveryv1.EndpointSlice:
			kind = "EndpointSlice"
		case *corev1.Pod:
			kind = "Pod"
		case *batchv1.Job:
			kind = "Job"
		default:
			t.Fatalf("unexpected runtime informer type %T", object)
		}
		if kind == "" {
			t.Fatalf("runtime informer kind was not classified for %T", object)
		}
		if kind != "Tamoss" && kind != "StorageBackend" && kind != "Event" {
			if scope.Field != nil || scope.Label == nil || scope.Label.String() != wantLabelSelector {
				t.Fatalf("%s cache label selector = %v, want %q", kind, scope.Label, wantLabelSelector)
			}
		}
		if seen[kind] {
			t.Fatalf("runtime informer/cache contains duplicate %s entries", kind)
		}
		seen[kind] = true
	}
	for _, kind := range []string{
		"Tamoss", "Deployment", "ReplicaSet", "Service", "EndpointSlice",
		"StorageBackend", "Pod", "Job", "Event",
	} {
		if !seen[kind] {
			t.Fatalf("runtime informer/cache is missing %s", kind)
		}
	}
	for object := range scopes {
		if _, found := object.(*tamossv1alpha1.IngestRun); found {
			t.Fatal("IngestRun must use bounded live GETs, not an informer")
		}
	}
}

func clearConsoleAuthEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{consoleAuthModeEnv, consoleForwardAuthSecretFileEnv, consoleGroupRoleBindingsEnv} {
		t.Setenv(name, "")
	}
}
