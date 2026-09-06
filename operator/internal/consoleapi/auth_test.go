package consoleapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const testForwardAuthSecret = "0123456789abcdef0123456789abcdef"

func TestForwardAuthAuthenticatorRequiresProofIdentityAndMappedRole(t *testing.T) {
	t.Parallel()
	authenticator := newTestForwardAuthAuthenticator(t)

	tests := []struct {
		name       string
		headers    map[string]string
		wantError  error
		wantRoles  []Role
		wantMethod string
	}{
		{
			name: "viewer",
			headers: map[string]string{
				ForwardAuthSecretHeader:   testForwardAuthSecret,
				ForwardAuthSubjectHeader:  "user-123",
				ForwardAuthUsernameHeader: "alice",
				ForwardAuthGroupsHeader:   "unrelated|tamoss-viewers",
			},
			wantRoles:  []Role{RoleViewer},
			wantMethod: "forward-auth",
		},
		{
			name: "operator implies viewer",
			headers: map[string]string{
				ForwardAuthSecretHeader:  testForwardAuthSecret,
				ForwardAuthSubjectHeader: "user-456",
				ForwardAuthGroupsHeader:  "tamoss-operators",
			},
			wantRoles:  []Role{RoleViewer, RoleOperator},
			wantMethod: "forward-auth",
		},
		{
			name: "roles are additive and groups are deduplicated",
			headers: map[string]string{
				ForwardAuthSecretHeader:  testForwardAuthSecret,
				ForwardAuthSubjectHeader: "user-789",
				ForwardAuthGroupsHeader:  "tamoss-runners|tamoss-viewers|tamoss-runners",
			},
			wantRoles:  []Role{RoleViewer, RoleIngestRunner},
			wantMethod: "forward-auth",
		},
		{
			name: "admin is a compatibility alias for all Console roles",
			headers: map[string]string{
				ForwardAuthSecretHeader:  testForwardAuthSecret,
				ForwardAuthSubjectHeader: "user-admin",
				ForwardAuthGroupsHeader:  "tamoss-admins",
			},
			wantRoles:  []Role{RoleViewer, RoleOperator, RoleIngestRunner},
			wantMethod: "forward-auth",
		},
		{
			name: "missing proof",
			headers: map[string]string{
				ForwardAuthSubjectHeader: "user-123",
				ForwardAuthGroupsHeader:  "tamoss-viewers",
			},
			wantError: ErrUnauthenticated,
		},
		{
			name: "wrong proof",
			headers: map[string]string{
				ForwardAuthSecretHeader:  "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
				ForwardAuthSubjectHeader: "user-123",
				ForwardAuthGroupsHeader:  "tamoss-viewers",
			},
			wantError: ErrUnauthenticated,
		},
		{
			name: "missing subject",
			headers: map[string]string{
				ForwardAuthSecretHeader: testForwardAuthSecret,
				ForwardAuthGroupsHeader: "tamoss-viewers",
			},
			wantError: ErrUnauthenticated,
		},
		{
			name: "non-printable subject",
			headers: map[string]string{
				ForwardAuthSecretHeader:  testForwardAuthSecret,
				ForwardAuthSubjectHeader: "user\x00name",
				ForwardAuthGroupsHeader:  "tamoss-viewers",
			},
			wantError: ErrUnauthenticated,
		},
		{
			name: "unmapped group",
			headers: map[string]string{
				ForwardAuthSecretHeader:  testForwardAuthSecret,
				ForwardAuthSubjectHeader: "user-123",
				ForwardAuthGroupsHeader:  "other-viewers",
			},
			wantError: ErrForbidden,
		},
		{
			name: "group matching is exact",
			headers: map[string]string{
				ForwardAuthSecretHeader:  testForwardAuthSecret,
				ForwardAuthSubjectHeader: "user-123",
				ForwardAuthGroupsHeader:  "prefix-tamoss-viewers-suffix",
			},
			wantError: ErrForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodGet, RuntimePath, nil)
			for name, value := range test.headers {
				request.Header.Set(name, value)
			}
			identity, err := authenticator.Authenticate(request)
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("Authenticate() error = %v, want %v", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if identity.Subject != test.headers[ForwardAuthSubjectHeader] || identity.Username != test.headers[ForwardAuthUsernameHeader] || identity.Method != test.wantMethod {
				t.Fatalf("unexpected identity: %#v", identity)
			}
			if !reflect.DeepEqual(identity.Roles, test.wantRoles) {
				t.Fatalf("roles = %#v, want %#v", identity.Roles, test.wantRoles)
			}
		})
	}
}

func TestForwardAuthAuthenticatorRejectsMalformedConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config ForwardAuthConfig
	}{
		{name: "short secret", config: ForwardAuthConfig{SharedSecret: []byte("short"), GroupRoleBindings: map[string][]Role{"group": {RoleViewer}}}},
		{name: "no bindings", config: ForwardAuthConfig{SharedSecret: []byte(testForwardAuthSecret)}},
		{name: "empty group", config: ForwardAuthConfig{SharedSecret: []byte(testForwardAuthSecret), GroupRoleBindings: map[string][]Role{"": {RoleViewer}}}},
		{name: "group delimiter", config: ForwardAuthConfig{SharedSecret: []byte(testForwardAuthSecret), GroupRoleBindings: map[string][]Role{"one|two": {RoleViewer}}}},
		{name: "non-printable group", config: ForwardAuthConfig{SharedSecret: []byte(testForwardAuthSecret), GroupRoleBindings: map[string][]Role{"group\x00": {RoleViewer}}}},
		{name: "empty roles", config: ForwardAuthConfig{SharedSecret: []byte(testForwardAuthSecret), GroupRoleBindings: map[string][]Role{"group": {}}}},
		{name: "unknown role", config: ForwardAuthConfig{SharedSecret: []byte(testForwardAuthSecret), GroupRoleBindings: map[string][]Role{"group": {"owner"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewForwardAuthAuthenticator(test.config); err == nil {
				t.Fatal("NewForwardAuthAuthenticator() succeeded, want error")
			}
		})
	}
}

func TestParseGroupRoleBindingsJSON(t *testing.T) {
	t.Parallel()
	bindings, err := ParseGroupRoleBindingsJSON([]byte(`[
		{"groupName":"ops","permissions":["operator","viewer","operator"]},
		{"groupName":"runners","permissions":["ingest-runner"]}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bindings["ops"], []Role{RoleViewer, RoleOperator}) || !reflect.DeepEqual(bindings["runners"], []Role{RoleIngestRunner}) {
		t.Fatalf("unexpected bindings: %#v", bindings)
	}

	for _, value := range []string{
		``,
		`[]`,
		`[{"groupName":"ops","permissions":["owner"]}]`,
		`[{"groupName":"ops","permissions":["viewer"],"unknown":true}]`,
		`[{"groupName":"ops","permissions":["viewer"]},{"groupName":"ops","permissions":["operator"]}]`,
		`[{"groupName":"ops","permissions":["viewer"]}] [{"groupName":"other","permissions":["viewer"]}]`,
		`[{"groupName":"ops","permissions":["viewer"]}] trailing`,
	} {
		if _, err := ParseGroupRoleBindingsJSON([]byte(value)); err == nil {
			t.Fatalf("ParseGroupRoleBindingsJSON(%q) succeeded, want error", value)
		}
	}
}

func TestForwardAuthAuthenticatorBoundsGroupHeader(t *testing.T) {
	t.Parallel()
	authenticator := newTestForwardAuthAuthenticator(t)
	request := httptest.NewRequest(http.MethodGet, RuntimePath, nil)
	request.Header.Set(ForwardAuthSecretHeader, testForwardAuthSecret)
	request.Header.Set(ForwardAuthSubjectHeader, "user-123")
	request.Header.Set(ForwardAuthGroupsHeader, strings.Repeat("x", maxForwardAuthGroupsLength+1))
	if _, err := authenticator.Authenticate(request); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authenticate() error = %v, want %v", err, ErrForbidden)
	}
}

func TestForwardAuthAuthenticatorRejectsRepeatedHeaders(t *testing.T) {
	t.Parallel()
	authenticator := newTestForwardAuthAuthenticator(t)
	tests := []struct {
		header string
		second string
	}{
		// Repeating the valid proof must fail too: the first value is never
		// trusted in isolation.
		{header: ForwardAuthSecretHeader, second: testForwardAuthSecret},
		{header: ForwardAuthSecretHeader, second: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"},
		{header: ForwardAuthSubjectHeader, second: "user-attacker"},
		{header: ForwardAuthUsernameHeader, second: "attacker"},
		{header: ForwardAuthGroupsHeader, second: "tamoss-admins"},
	}
	for _, test := range tests {
		t.Run(test.header+"/"+test.second, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodGet, RuntimePath, nil)
			request.Header.Set(ForwardAuthSecretHeader, testForwardAuthSecret)
			request.Header.Set(ForwardAuthSubjectHeader, "user-123")
			request.Header.Set(ForwardAuthUsernameHeader, "alice")
			request.Header.Set(ForwardAuthGroupsHeader, "tamoss-viewers")
			request.Header.Add(test.header, test.second)
			if _, err := authenticator.Authenticate(request); !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("Authenticate() error = %v, want %v", err, ErrUnauthenticated)
			}
		})
	}
}

func TestForwardAuthAuthenticatorRejectsMalformedGroupHeader(t *testing.T) {
	t.Parallel()
	authenticator := newTestForwardAuthAuthenticator(t)
	request := httptest.NewRequest(http.MethodGet, RuntimePath, nil)
	request.Header.Set(ForwardAuthSecretHeader, testForwardAuthSecret)
	request.Header.Set(ForwardAuthSubjectHeader, "user-123")
	request.Header.Set(ForwardAuthGroupsHeader, "tamoss-viewers||tamoss-operators")
	if _, err := authenticator.Authenticate(request); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authenticate() error = %v, want %v", err, ErrForbidden)
	}
}

func TestDevelopmentAnonymousAuthenticatorIsExplicitViewer(t *testing.T) {
	t.Parallel()
	identity, err := NewDevelopmentAnonymousAuthenticator().Authenticate(httptest.NewRequest(http.MethodGet, RuntimePath, nil))
	if err != nil {
		t.Fatal(err)
	}
	if identity.Method != "development-anonymous" || identity.Subject != "development-anonymous" || !identity.CanView() {
		t.Fatalf("unexpected development identity: %#v", identity)
	}
}

func newTestForwardAuthAuthenticator(t *testing.T) Authenticator {
	t.Helper()
	authenticator, err := NewForwardAuthAuthenticator(ForwardAuthConfig{
		SharedSecret: []byte(testForwardAuthSecret),
		GroupRoleBindings: map[string][]Role{
			"tamoss-viewers":   {RoleViewer},
			"tamoss-operators": {RoleOperator},
			"tamoss-runners":   {RoleIngestRunner},
			"tamoss-admins":    {roleAdminAlias},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return authenticator
}
