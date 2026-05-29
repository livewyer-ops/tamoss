package authentik

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultProbeTimeout = 5 * time.Second
)

type discoveryDocument struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

func ProbeWithClient(ctx context.Context, client *http.Client, issuerURL, applicationSlug string) error {
	if client == nil {
		client = http.DefaultClient
	}
	discoveryURL, err := discoveryURL(issuerURL, applicationSlug)
	if err != nil {
		return err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultProbeTimeout)
		defer cancel()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("probe %s: %w", discoveryURL, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("probe %s: unexpected HTTP status %d", discoveryURL, response.StatusCode)
	}
	var doc discoveryDocument
	if err := json.NewDecoder(response.Body).Decode(&doc); err != nil {
		return fmt.Errorf("probe %s: invalid OIDC discovery document: %w", discoveryURL, err)
	}
	if doc.Issuer == "" || doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" || doc.JWKSURI == "" {
		return fmt.Errorf("probe %s: incomplete OIDC discovery document", discoveryURL)
	}
	return nil
}

func discoveryURL(issuerURL, applicationSlug string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(issuerURL))
	if err != nil {
		return "", err
	}
	if base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("issuerURL %q must include scheme and host", issuerURL)
	}
	return url.JoinPath(base.String(), "application", "o", applicationSlug, ".well-known", "openid-configuration")
}
