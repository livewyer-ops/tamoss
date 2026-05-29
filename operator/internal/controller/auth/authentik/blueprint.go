package authentik

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	TargetAuthentikVersion = "2026.2"
)

var tamossAPIScopes = []string{
	"tams-api/admin",
	"tams-api/read",
	"tams-api/write",
	"tams-api/delete",
}

func RenderBlueprint(tamoss *tamossv1alpha1.Tamoss, credentials Credentials) ([]byte, error) {
	slug := tamoss.Spec.Auth.ApplicationSlug(tamoss.Namespace, tamoss.Name)
	redirectURIs := RedirectURIs(tamoss)
	entries := tamossScopeMappingEntries()
	entries = append(entries, providerEntries(slug, credentials, redirectURIs)...)
	entries = append(entries, groupEntries(tamoss)...)
	entries = append(entries, applicationEntry(slug))
	root := mapping(
		scalar("version"), intScalar(1),
		scalar("metadata"), mapping(
			scalar("name"), scalar(slug),
			scalar("labels"), mapping(
				scalar("app.kubernetes.io/name"), scalar("tamoss"),
				scalar("app.kubernetes.io/instance"), scalar(tamoss.Name),
				scalar("app.kubernetes.io/managed-by"), scalar("tamoss-operator"),
				scalar("tamoss.livewyer.io/authentik-version"), scalar(TargetAuthentikVersion),
			),
		),
		scalar("entries"), sequence(entries...),
	)

	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		_ = encoder.Close()
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func RedirectURIs(tamoss *tamossv1alpha1.Tamoss) []string {
	if spec := tamoss.Spec.Auth.AuthentikBlueprints; spec != nil && len(spec.RedirectURIs) > 0 {
		return append([]string(nil), spec.RedirectURIs...)
	}

	hosts := map[string]struct{}{}
	if host := strings.TrimSpace(tamoss.Spec.Ingress.UI.Web.Host); host != "" {
		hosts[host] = struct{}{}
	}
	for _, host := range tamoss.Spec.HTTPRoute.UI.Hostnames {
		if host = strings.TrimSpace(host); host != "" {
			hosts[host] = struct{}{}
		}
	}
	orderedHosts := make([]string, 0, len(hosts))
	for host := range hosts {
		orderedHosts = append(orderedHosts, host)
	}
	sort.Strings(orderedHosts)
	uris := make([]string, 0, len(orderedHosts))
	for _, host := range orderedHosts {
		uris = append(uris, fmt.Sprintf("https://%s/auth/callback", host))
	}
	return uris
}

func providerEntries(slug string, credentials Credentials, redirectURIs []string) []*yaml.Node {
	redirects := make([]*yaml.Node, 0, len(redirectURIs))
	for _, uri := range redirectURIs {
		redirects = append(redirects, mapping(
			scalar("matching_mode"), scalar("strict"),
			scalar("url"), scalar(uri),
		))
	}
	return []*yaml.Node{mapping(
		scalar("model"), scalar("authentik_providers_oauth2.oauth2provider"),
		scalar("id"), scalar("provider"),
		scalar("state"), scalar("present"),
		scalar("identifiers"), mapping(
			scalar("name"), scalar(slug),
		),
		scalar("attrs"), mapping(
			scalar("name"), scalar(slug),
			scalar("client_id"), scalar(string(credentials.ClientID)),
			scalar("client_secret"), scalar(string(credentials.ClientSecret)),
			scalar("authorization_flow"), find("authentik_flows.flow", "slug", "default-provider-authorization-implicit-consent"),
			scalar("invalidation_flow"), find("authentik_flows.flow", "slug", "default-provider-invalidation-flow"),
			scalar("signing_key"), find("authentik_crypto.certificatekeypair", "name", "authentik Self-signed Certificate"),
			scalar("redirect_uris"), sequence(redirects...),
			scalar("property_mappings"), sequence(append([]*yaml.Node{
				find("authentik_providers_oauth2.scopemapping", "scope_name", "openid"),
				find("authentik_providers_oauth2.scopemapping", "scope_name", "profile"),
				find("authentik_providers_oauth2.scopemapping", "scope_name", "email"),
			}, tamossScopeMappingRefs()...)...),
		),
	)}
}

func tamossScopeMappingEntries() []*yaml.Node {
	entries := make([]*yaml.Node, 0, len(tamossAPIScopes))
	for _, scope := range tamossAPIScopes {
		entries = append(entries, mapping(
			scalar("model"), scalar("authentik_providers_oauth2.scopemapping"),
			scalar("id"), scalar(scopeMappingID(scope)),
			scalar("state"), scalar("present"),
			scalar("identifiers"), mapping(
				scalar("scope_name"), scalar(scope),
			),
			scalar("attrs"), mapping(
				scalar("name"), scalar("TAMOSS API "+scope),
				scalar("scope_name"), scalar(scope),
				scalar("description"), scalar("Allow TAMOSS API scope "+scope),
				scalar("expression"), scalar("return {}"),
			),
		))
	}
	return entries
}

func tamossScopeMappingRefs() []*yaml.Node {
	refs := make([]*yaml.Node, 0, len(tamossAPIScopes))
	for _, scope := range tamossAPIScopes {
		refs = append(refs, taggedScalar("!KeyOf", scopeMappingID(scope)))
	}
	return refs
}

func scopeMappingID(scope string) string {
	return "scope-" + strings.NewReplacer("/", "-", "_", "-").Replace(scope)
}

func applicationEntry(slug string) *yaml.Node {
	return mapping(
		scalar("model"), scalar("authentik_core.application"),
		scalar("id"), scalar("application"),
		scalar("state"), scalar("present"),
		scalar("identifiers"), mapping(
			scalar("slug"), scalar(slug),
		),
		scalar("attrs"), mapping(
			scalar("name"), scalar(slug),
			scalar("slug"), scalar(slug),
			scalar("provider"), taggedScalar("!KeyOf", "provider"),
		),
	)
}

func groupEntries(tamoss *tamossv1alpha1.Tamoss) []*yaml.Node {
	if tamoss.Spec.Auth.AuthentikBlueprints == nil {
		return nil
	}
	groups := append([]tamossv1alpha1.AuthentikGroupBindingSpec(nil), tamoss.Spec.Auth.AuthentikBlueprints.GroupBindings...)
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].GroupName < groups[j].GroupName
	})
	entries := make([]*yaml.Node, 0, len(groups))
	for _, group := range groups {
		if strings.TrimSpace(group.GroupName) == "" {
			continue
		}
		entries = append(entries, mapping(
			scalar("model"), scalar("authentik_core.group"),
			scalar("state"), scalar("present"),
			scalar("identifiers"), mapping(
				scalar("name"), scalar(group.GroupName),
			),
			scalar("attrs"), mapping(
				scalar("name"), scalar(group.GroupName),
				scalar("attributes"), mapping(
					scalar("tamoss_permissions"), stringSequence(group.Permissions),
				),
			),
		))
	}
	return entries
}

func stringSequence(values []string) *yaml.Node {
	ordered := append([]string(nil), values...)
	sort.Strings(ordered)
	nodes := make([]*yaml.Node, 0, len(ordered))
	for _, value := range ordered {
		nodes = append(nodes, scalar(value))
	}
	return sequence(nodes...)
}

func find(model, key, value string) *yaml.Node {
	return &yaml.Node{
		Kind: yaml.SequenceNode,
		Tag:  "!Find",
		Content: []*yaml.Node{
			scalar(model),
			sequence(scalar(key), scalar(value)),
		},
	}
}

func mapping(nodes ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: nodes}
}

func sequence(nodes ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: nodes}
}

func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func intScalar(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", value)}
}

func taggedScalar(tag, value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
}
