package consoleapi

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

func TestReadOnlyPolicyRulesStayNarrow(t *testing.T) {
	t.Parallel()
	rules := ReadOnlyPolicyRules("media")
	allowedResources := map[string]bool{
		"tamosses":       true,
		"events":         true,
		"pods":           true,
		"services":       true,
		"deployments":    true,
		"jobs":           true,
		"endpointslices": true,
	}
	seen := map[string]bool{}
	for _, rule := range rules {
		for _, resource := range rule.Resources {
			if !allowedResources[resource] {
				t.Fatalf("unexpected resource permission %q", resource)
			}
			seen[resource] = true
			if resource == "tamosses" && (len(rule.ResourceNames) != 1 || rule.ResourceNames[0] != "media") {
				t.Fatalf("Tamoss permission is not instance-scoped: %#v", rule)
			}
		}
		for _, verb := range rule.Verbs {
			switch verb {
			case "get", "list", "watch":
			default:
				t.Fatalf("unexpected verb %q", verb)
			}
		}
	}
	for resource := range allowedResources {
		if !seen[resource] {
			t.Fatalf("missing permission for %q", resource)
		}
	}
}

func TestGeneratedOperatorRBACCoversConsoleRole(t *testing.T) {
	t.Parallel()
	globalRules := readGeneratedRBACRules(t, "role.yaml", "ClusterRole", "manager-role")
	operandRules := readGeneratedRBACRules(t, "manager_cluster_resources_role.yaml", "ClusterRole", "manager-resources-role")

	for _, consoleRule := range ReadOnlyPolicyRules("media") {
		for _, group := range consoleRule.APIGroups {
			rules := operandRules
			if group == "tamoss.livewyer.io" {
				rules = globalRules
			}
			for _, resource := range consoleRule.Resources {
				for _, verb := range consoleRule.Verbs {
					if !policyAllows(rules, group, resource, verb, "media") {
						t.Errorf("operator RBAC cannot grant Console permission %s %s/%s in an operand namespace", verb, group, resource)
					}
				}
			}
		}
	}
}

func readGeneratedRBACRules(t *testing.T, filename, kind, name string) []rbacv1.PolicyRule {
	t.Helper()
	path := filepath.Join("..", "..", "config", "rbac", filename)
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open generated operator RBAC: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })

	var operatorRules []rbacv1.PolicyRule
	decoder := utilyaml.NewYAMLOrJSONDecoder(file, 4096)
	for {
		var document struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Rules []rbacv1.PolicyRule `json:"rules"`
		}
		if err := decoder.Decode(&document); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode generated operator RBAC: %v", err)
		}
		if document.Kind == kind && document.Metadata.Name == name {
			operatorRules = append(operatorRules, document.Rules...)
		}
	}
	if len(operatorRules) == 0 {
		t.Fatalf("generated RBAC %s %q not found in %s", kind, name, path)
	}
	return operatorRules
}

func policyAllows(rules []rbacv1.PolicyRule, group, resource, verb, resourceName string) bool {
	for _, rule := range rules {
		if !contains(rule.APIGroups, group) || !contains(rule.Resources, resource) || !contains(rule.Verbs, verb) {
			continue
		}
		if len(rule.ResourceNames) == 0 || contains(rule.ResourceNames, resourceName) {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want || value == "*" {
			return true
		}
	}
	return false
}
