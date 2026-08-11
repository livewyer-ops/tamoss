package consoleapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"unicode"
)

const (
	ForwardAuthSecretHeader   = "X-TAMOSS-Forward-Auth-Secret"
	ForwardAuthSubjectHeader  = "X-TAMOSS-Forward-Auth-Subject"
	ForwardAuthUsernameHeader = "X-TAMOSS-Forward-Auth-Username"
	ForwardAuthGroupsHeader   = "X-TAMOSS-Forward-Auth-Groups"

	maxForwardAuthIdentityLength = 512
	maxForwardAuthGroupsLength   = 4096
	maxForwardAuthGroups         = 256
	minForwardAuthSecretLength   = 32
)

type Role string

const (
	RoleViewer       Role = "viewer"
	RoleOperator     Role = "operator"
	RoleIngestRunner Role = "ingest-runner"
	roleAdminAlias   Role = "admin"
)

var (
	ErrUnauthenticated = errors.New("console request is unauthenticated")
	ErrForbidden       = errors.New("console request is forbidden")
)

type Identity struct {
	Subject  string
	Username string
	Method   string
	Roles    []Role
}

func (i Identity) HasRole(role Role) bool {
	return slices.Contains(i.Roles, role)
}

func (i Identity) CanView() bool {
	return i.HasRole(RoleViewer)
}

type Authenticator interface {
	Authenticate(*http.Request) (Identity, error)
}

type ForwardAuthConfig struct {
	SharedSecret      []byte
	GroupRoleBindings map[string][]Role
}

type groupRoleBinding struct {
	GroupName   string `json:"groupName"`
	Permissions []Role `json:"permissions"`
}

type forwardAuthAuthenticator struct {
	sharedSecretHash  [sha256.Size]byte
	groupRoleBindings map[string][]Role
}

type developmentAnonymousAuthenticator struct{}

type rejectAuthenticator struct{}

func NewForwardAuthAuthenticator(config ForwardAuthConfig) (Authenticator, error) {
	secret := []byte(strings.TrimSpace(string(config.SharedSecret)))
	if len(secret) < minForwardAuthSecretLength {
		return nil, fmt.Errorf("forward-auth shared secret must contain at least %d characters", minForwardAuthSecretLength)
	}
	bindings, err := validateGroupRoleBindings(config.GroupRoleBindings)
	if err != nil {
		return nil, err
	}
	return &forwardAuthAuthenticator{
		sharedSecretHash:  sha256.Sum256(secret),
		groupRoleBindings: bindings,
	}, nil
}

func NewDevelopmentAnonymousAuthenticator() Authenticator {
	return developmentAnonymousAuthenticator{}
}

func NewUnavailableAuthenticator() Authenticator {
	return rejectAuthenticator{}
}

func ParseGroupRoleBindingsJSON(value []byte) (map[string][]Role, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(string(value))))
	decoder.DisallowUnknownFields()
	var encoded []groupRoleBinding
	if err := decoder.Decode(&encoded); err != nil {
		return nil, fmt.Errorf("decode Console group-role bindings: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, fmt.Errorf("decode Console group-role bindings: multiple JSON values are not allowed")
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode Console group-role bindings: %w", err)
	}
	bindings := make(map[string][]Role, len(encoded))
	for _, binding := range encoded {
		if _, found := bindings[binding.GroupName]; found {
			return nil, fmt.Errorf("decode Console group-role bindings: duplicate group %q", binding.GroupName)
		}
		bindings[binding.GroupName] = binding.Permissions
	}
	return validateGroupRoleBindings(bindings)
}

func (a *forwardAuthAuthenticator) Authenticate(request *http.Request) (Identity, error) {
	// A forward-auth header the gateway sent more than once makes the proof
	// chain ambiguous, so the whole request is refused rather than resolved to
	// its first value. This matches how the command API treats duplicated
	// Origin and X-Forwarded-Proto headers.
	suppliedSecret, single := singleForwardAuthHeader(request.Header, ForwardAuthSecretHeader)
	if !single {
		return Identity{}, ErrUnauthenticated
	}
	suppliedHash := sha256.Sum256([]byte(suppliedSecret))
	if subtle.ConstantTimeCompare(suppliedHash[:], a.sharedSecretHash[:]) != 1 {
		return Identity{}, ErrUnauthenticated
	}

	forwardedSubject, single := singleForwardAuthHeader(request.Header, ForwardAuthSubjectHeader)
	if !single {
		return Identity{}, ErrUnauthenticated
	}
	subject := strings.TrimSpace(forwardedSubject)
	if !validForwardedIdentityValue(subject) {
		return Identity{}, ErrUnauthenticated
	}
	forwardedUsername, single := singleForwardAuthHeader(request.Header, ForwardAuthUsernameHeader)
	if !single {
		return Identity{}, ErrUnauthenticated
	}
	username := strings.TrimSpace(forwardedUsername)
	if username != "" && !validForwardedIdentityValue(username) {
		return Identity{}, ErrUnauthenticated
	}

	forwardedGroupList, single := singleForwardAuthHeader(request.Header, ForwardAuthGroupsHeader)
	if !single {
		return Identity{}, ErrUnauthenticated
	}
	groups, ok := forwardedGroups(forwardedGroupList)
	if !ok {
		return Identity{}, ErrForbidden
	}
	roles := make(map[Role]struct{}, len(a.groupRoleBindings))
	for _, group := range groups {
		for _, role := range a.groupRoleBindings[group] {
			if role == roleAdminAlias {
				roles[RoleViewer] = struct{}{}
				roles[RoleOperator] = struct{}{}
				roles[RoleIngestRunner] = struct{}{}
				continue
			}
			roles[role] = struct{}{}
			if role == RoleOperator || role == RoleIngestRunner {
				roles[RoleViewer] = struct{}{}
			}
		}
	}
	identity := Identity{
		Subject:  subject,
		Username: username,
		Method:   "forward-auth",
		Roles:    orderedRoles(roles),
	}
	if !identity.CanView() {
		return Identity{}, ErrForbidden
	}
	return identity, nil
}

func (developmentAnonymousAuthenticator) Authenticate(_ *http.Request) (Identity, error) {
	return Identity{
		Subject:  "development-anonymous",
		Username: "development-anonymous",
		Method:   "development-anonymous",
		Roles:    []Role{RoleViewer},
	}, nil
}

func (rejectAuthenticator) Authenticate(_ *http.Request) (Identity, error) {
	return Identity{}, ErrUnauthenticated
}

func validateGroupRoleBindings(bindings map[string][]Role) (map[string][]Role, error) {
	if len(bindings) == 0 {
		return nil, fmt.Errorf("at least one Console group-role binding is required")
	}
	if len(bindings) > maxForwardAuthGroups {
		return nil, fmt.Errorf("console group-role bindings exceed the %d-group limit", maxForwardAuthGroups)
	}
	validated := make(map[string][]Role, len(bindings))
	for group, roles := range bindings {
		if !validForwardedIdentityValue(group) || group != strings.TrimSpace(group) || strings.Contains(group, "|") {
			return nil, fmt.Errorf("console group-role binding contains an invalid group name")
		}
		if len(roles) == 0 {
			return nil, fmt.Errorf("console group %q has no roles", group)
		}
		seen := make(map[Role]struct{}, len(roles))
		for _, role := range roles {
			switch role {
			case RoleViewer, RoleOperator, RoleIngestRunner, roleAdminAlias:
			default:
				return nil, fmt.Errorf("console group %q has unsupported role %q", group, role)
			}
			seen[role] = struct{}{}
		}
		validated[group] = orderedConfiguredRoles(seen)
	}
	return validated, nil
}

// singleForwardAuthHeader returns the only value of a forward-auth header. An
// absent header yields an empty value; a repeated header is reported as not
// single so the caller can reject the request.
func singleForwardAuthHeader(header http.Header, name string) (string, bool) {
	values := header.Values(name)
	switch len(values) {
	case 0:
		return "", true
	case 1:
		return values[0], true
	default:
		return "", false
	}
}

func forwardedGroups(value string) ([]string, bool) {
	if len(value) > maxForwardAuthGroupsLength {
		return nil, false
	}
	if strings.TrimSpace(value) == "" {
		return nil, true
	}
	parts := strings.Split(value, "|")
	if len(parts) > maxForwardAuthGroups {
		return nil, false
	}
	groups := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		group := strings.TrimSpace(part)
		if !validForwardedIdentityValue(group) {
			return nil, false
		}
		if _, found := seen[group]; found {
			continue
		}
		seen[group] = struct{}{}
		groups = append(groups, group)
	}
	return groups, true
}

func validForwardedIdentityValue(value string) bool {
	if value == "" || len(value) > maxForwardAuthIdentityLength {
		return false
	}
	for _, character := range value {
		if !unicode.IsPrint(character) {
			return false
		}
	}
	return true
}

func orderedRoles(values map[Role]struct{}) []Role {
	ordered := make([]Role, 0, len(values))
	for _, role := range []Role{RoleViewer, RoleOperator, RoleIngestRunner} {
		if _, found := values[role]; found {
			ordered = append(ordered, role)
		}
	}
	return ordered
}

func orderedConfiguredRoles(values map[Role]struct{}) []Role {
	ordered := orderedRoles(values)
	if _, found := values[roleAdminAlias]; found {
		ordered = append(ordered, roleAdminAlias)
	}
	return ordered
}
