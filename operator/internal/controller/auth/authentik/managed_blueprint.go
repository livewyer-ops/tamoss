package authentik

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	DefaultAPITokenSecretName = "authentik-api-token"
	DefaultAPITokenSecretKey  = "token"
)

type ManagedBlueprintClient struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

type ManagedBlueprint struct {
	PK          string `json:"pk"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Status      string `json:"status"`
	LastApplied string `json:"last_applied"`
	Enabled     bool   `json:"enabled"`
	Content     string `json:"content"`
}

type managedBlueprintList struct {
	Results []ManagedBlueprint `json:"results"`
}

type managedBlueprintRequest struct {
	Name    string         `json:"name"`
	Path    string         `json:"path"`
	Context map[string]any `json:"context"`
	Enabled bool           `json:"enabled"`
	Content string         `json:"content"`
}

type APIError struct {
	Method     string
	URL        string
	StatusCode int
	Body       string
}

func (e APIError) Error() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("%s %s returned HTTP %d", e.Method, e.URL, e.StatusCode)
	}
	return fmt.Sprintf("%s %s returned HTTP %d: %s", e.Method, e.URL, e.StatusCode, body)
}

func ManagedBlueprintName(tamoss *tamossv1alpha1.Tamoss) string {
	return tamoss.Spec.Auth.ApplicationSlug(tamoss.Namespace, tamoss.Name)
}

func ManagedBlueprintPath(_ *tamossv1alpha1.Tamoss) string {
	// Empty path tells Authentik to use internal managed Blueprint storage
	// for the submitted content instead of resolving a mounted file.
	return ""
}

func APITokenSecretName(tamoss *tamossv1alpha1.Tamoss) string {
	if tamoss.Spec.Auth.AuthentikBlueprints != nil && tamoss.Spec.Auth.AuthentikBlueprints.APITokenSecretRef.Name != "" {
		return tamoss.Spec.Auth.AuthentikBlueprints.APITokenSecretRef.Name
	}
	return DefaultAPITokenSecretName
}

func APITokenSecretKey(tamoss *tamossv1alpha1.Tamoss) string {
	if tamoss.Spec.Auth.AuthentikBlueprints != nil && tamoss.Spec.Auth.AuthentikBlueprints.APITokenSecretRef.Key != "" {
		return tamoss.Spec.Auth.AuthentikBlueprints.APITokenSecretRef.Key
	}
	return DefaultAPITokenSecretKey
}

func (c ManagedBlueprintClient) Reconcile(ctx context.Context, name, path string, content []byte) (ManagedBlueprint, error) {
	if strings.TrimSpace(name) == "" {
		return ManagedBlueprint{}, fmt.Errorf("managed blueprint name is required")
	}
	if strings.TrimSpace(c.Token) == "" {
		return ManagedBlueprint{}, fmt.Errorf("authentik API token is required")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultAPIOperationTimeout)
		defer cancel()
	}
	existing, err := c.findByName(ctx, name)
	if err != nil {
		return ManagedBlueprint{}, err
	}
	request := managedBlueprintRequest{
		Name:    name,
		Path:    path,
		Context: map[string]any{},
		Enabled: true,
		Content: string(content),
	}
	if existing.PK != "" && managedBlueprintMatches(existing, request) {
		// A queued apply backlog can leave an already-applied Blueprint in a
		// transient status. Avoid adding duplicate work unless Authentik marks
		// the instance as explicitly unhealthy.
		if existing.Status == "successful" || (existing.LastApplied != "" && !managedBlueprintRequiresApply(existing.Status)) {
			return existing, nil
		}
		log.FromContext(ctx).Info("reapplying matching Authentik managed Blueprint",
			"name", name,
			"status", existing.Status,
			"previouslyApplied", existing.LastApplied != "",
		)
	} else if existing.PK != "" {
		log.FromContext(ctx).Info("updating changed Authentik managed Blueprint",
			"name", name,
			"nameMatches", existing.Name == request.Name,
			"pathMatches", existing.Path == request.Path,
			"enabledMatches", existing.Enabled == request.Enabled,
			"contentMatches", strings.TrimSpace(existing.Content) == strings.TrimSpace(request.Content),
			"existingContentLength", len(strings.TrimSpace(existing.Content)),
			"desiredContentLength", len(strings.TrimSpace(request.Content)),
			"status", existing.Status,
			"previouslyApplied", existing.LastApplied != "",
		)
	}
	var blueprint ManagedBlueprint
	if existing.PK == "" {
		blueprint, err = c.create(ctx, request)
	} else if managedBlueprintMatches(existing, request) {
		blueprint = existing
	} else {
		blueprint, err = c.update(ctx, existing.PK, request)
	}
	if err != nil {
		return ManagedBlueprint{}, err
	}
	applied, err := c.apply(ctx, blueprint.PK)
	if err != nil {
		return ManagedBlueprint{}, err
	}
	if applied.Status == "error" {
		return ManagedBlueprint{}, fmt.Errorf("authentik managed blueprint %q applied with status error", name)
	}
	return applied, nil
}

func managedBlueprintRequiresApply(status string) bool {
	return status == "error" || status == "orphaned"
}

func managedBlueprintMatches(existing ManagedBlueprint, desired managedBlueprintRequest) bool {
	return existing.Name == desired.Name &&
		existing.Path == desired.Path &&
		existing.Enabled == desired.Enabled &&
		strings.TrimSpace(existing.Content) == strings.TrimSpace(desired.Content)
}

func (c ManagedBlueprintClient) DeleteByName(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(c.Token) == "" {
		return nil
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultAPIOperationTimeout)
		defer cancel()
	}
	blueprint, err := c.findByName(ctx, name)
	if err != nil || blueprint.PK == "" {
		return err
	}
	return c.delete(ctx, blueprint.PK)
}

func (c ManagedBlueprintClient) findByName(ctx context.Context, name string) (ManagedBlueprint, error) {
	endpoint, err := c.apiURL("managed", "blueprints")
	if err != nil {
		return ManagedBlueprint{}, err
	}
	query := endpoint.Query()
	query.Set("name", name)
	endpoint.RawQuery = query.Encode()
	var list managedBlueprintList
	if err := c.doJSON(ctx, http.MethodGet, endpoint.String(), nil, &list); err != nil {
		return ManagedBlueprint{}, err
	}
	for _, result := range list.Results {
		if result.Name == name {
			return result, nil
		}
	}
	return ManagedBlueprint{}, nil
}

func (c ManagedBlueprintClient) create(ctx context.Context, request managedBlueprintRequest) (ManagedBlueprint, error) {
	endpoint, err := c.apiURL("managed", "blueprints")
	if err != nil {
		return ManagedBlueprint{}, err
	}
	var blueprint ManagedBlueprint
	err = c.doJSON(ctx, http.MethodPost, endpoint.String(), request, &blueprint)
	return blueprint, err
}

func (c ManagedBlueprintClient) update(ctx context.Context, pk string, request managedBlueprintRequest) (ManagedBlueprint, error) {
	endpoint, err := c.apiURL("managed", "blueprints", pk)
	if err != nil {
		return ManagedBlueprint{}, err
	}
	var blueprint ManagedBlueprint
	err = c.doJSON(ctx, http.MethodPut, endpoint.String(), request, &blueprint)
	return blueprint, err
}

func (c ManagedBlueprintClient) apply(ctx context.Context, pk string) (ManagedBlueprint, error) {
	endpoint, err := c.apiURL("managed", "blueprints", pk, "apply")
	if err != nil {
		return ManagedBlueprint{}, err
	}
	var blueprint ManagedBlueprint
	err = c.doJSON(ctx, http.MethodPost, endpoint.String(), nil, &blueprint)
	return blueprint, err
}

func (c ManagedBlueprintClient) delete(ctx context.Context, pk string) error {
	endpoint, err := c.apiURL("managed", "blueprints", pk)
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodDelete, endpoint.String(), nil, nil)
}

func (c ManagedBlueprintClient) apiURL(parts ...string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(c.BaseURL))
	if err != nil {
		return nil, err
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("authentik base URL %q must include scheme and host", c.BaseURL)
	}
	joined, err := url.JoinPath(base.String(), append([]string{"api", "v3"}, parts...)...)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(joined)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}
	return parsed, nil
}

func (c ManagedBlueprintClient) doJSON(ctx context.Context, method, endpoint string, body any, out any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return APIError{Method: method, URL: endpoint, StatusCode: response.StatusCode, Body: string(responseBody)}
	}
	if out == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(out)
}
