package main

import (
	"errors"
	"io"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestManagerRuntimeManifestSettings(t *testing.T) {
	deployment := findDeployment(t, yamlDocuments(t, "../config/manager/manager.yaml"), "controller-manager")
	manager := findContainer(t, deployment, "manager")
	assertManagerRuntimeSettings(t, manager)
}

func TestAuthProxyResourceSettings(t *testing.T) {
	deployment := findDeployment(
		t,
		yamlDocuments(t, "../config/default/manager_auth_proxy_patch.yaml"),
		"controller-manager",
	)
	proxy := findContainer(t, deployment, "kube-rbac-proxy")
	resources := asMap(t, proxy["resources"], "proxy resources")
	limits := asMap(t, resources["limits"], "proxy limits")
	requests := asMap(t, resources["requests"], "proxy requests")
	if limits["memory"] != "256Mi" {
		t.Fatalf("expected proxy memory limit 256Mi, got %#v", limits["memory"])
	}
	if requests["memory"] != "128Mi" {
		t.Fatalf("expected proxy memory request 128Mi, got %#v", requests["memory"])
	}
}

func TestRenderedOperatorInstallIncludesRuntimeSettings(t *testing.T) {
	deployment := findDeployment(t, yamlDocuments(t, "../../deploy/operator/install.yaml"), "operator-controller-manager")
	manager := findContainer(t, deployment, "manager")
	assertManagerRuntimeSettings(t, manager)
	proxy := findContainer(t, deployment, "kube-rbac-proxy")
	resources := asMap(t, proxy["resources"], "rendered proxy resources")
	limits := asMap(t, resources["limits"], "rendered proxy limits")
	requests := asMap(t, resources["requests"], "rendered proxy requests")
	if limits["memory"] != "256Mi" || requests["memory"] != "128Mi" {
		t.Fatalf("expected rendered proxy memory 128Mi request / 256Mi limit, got requests=%#v limits=%#v", requests, limits)
	}
}

func TestOperatorCleanupUsernameIsNamespaceAndServiceAccountScoped(t *testing.T) {
	tests := []struct {
		namespace      string
		serviceAccount string
		want           string
	}{
		{
			namespace:      "tamoss-system",
			serviceAccount: "operator-controller-manager",
			want:           "system:serviceaccount:tamoss-system:operator-controller-manager",
		},
		{namespace: "", serviceAccount: "operator-controller-manager", want: ""},
		{namespace: "tamoss-system", serviceAccount: "", want: ""},
	}
	for _, test := range tests {
		if got := operatorCleanupUsername(test.namespace, test.serviceAccount); got != test.want {
			t.Fatalf("operatorCleanupUsername(%q, %q) = %q, want %q", test.namespace, test.serviceAccount, got, test.want)
		}
	}
}

func assertManagerRuntimeSettings(t *testing.T, manager map[string]any) {
	t.Helper()
	env := envByName(t, manager)
	if env["POD_NAMESPACE"] != "metadata.namespace" {
		t.Fatalf("expected POD_NAMESPACE downward API env, got %#v", env["POD_NAMESPACE"])
	}
	if env["POD_SERVICE_ACCOUNT_NAME"] != "spec.serviceAccountName" {
		t.Fatalf("expected POD_SERVICE_ACCOUNT_NAME downward API env, got %#v", env["POD_SERVICE_ACCOUNT_NAME"])
	}
	if env["TAMOSS_AUTHENTIK_PROBE_TIMEOUT"] != "30s" {
		t.Fatalf("expected Authentik probe timeout 30s, got %#v", env["TAMOSS_AUTHENTIK_PROBE_TIMEOUT"])
	}
	startupProbe := asMap(t, manager["startupProbe"], "manager startupProbe")
	failureThreshold := intValue(t, startupProbe["failureThreshold"], "startupProbe.failureThreshold")
	periodSeconds := intValue(t, startupProbe["periodSeconds"], "startupProbe.periodSeconds")
	if got := failureThreshold * periodSeconds; got < 60 {
		t.Fatalf("expected startup probe window >= 60s, got %ds", got)
	}
	resources := asMap(t, manager["resources"], "manager resources")
	limits := asMap(t, resources["limits"], "manager limits")
	requests := asMap(t, resources["requests"], "manager requests")
	if limits["cpu"] != "1" || limits["memory"] != "512Mi" {
		t.Fatalf("unexpected manager limits: %#v", limits)
	}
	if requests["cpu"] != "500m" || requests["memory"] != "256Mi" {
		t.Fatalf("unexpected manager requests: %#v", requests)
	}
}

func envByName(t *testing.T, container map[string]any) map[string]any {
	t.Helper()
	env := map[string]any{}
	for _, item := range asSlice(t, container["env"], "env") {
		entry := asMap(t, item, "env entry")
		name, ok := entry["name"].(string)
		if !ok {
			t.Fatalf("expected env name string, got %#v", entry["name"])
		}
		if value, ok := entry["value"]; ok {
			env[name] = value
			continue
		}
		valueFrom := asMap(t, entry["valueFrom"], "env valueFrom")
		fieldRef := asMap(t, valueFrom["fieldRef"], "env fieldRef")
		env[name] = fieldRef["fieldPath"]
	}
	return env
}

func yamlDocuments(t *testing.T, path string) []map[string]any {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()

	decoder := yaml.NewDecoder(file)
	var docs []map[string]any
	for {
		var doc map[string]any
		err := decoder.Decode(&doc)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return docs
			}
			t.Fatalf("decode %s: %v", path, err)
		}
		if len(doc) > 0 {
			docs = append(docs, doc)
		}
	}
}

func findDeployment(t *testing.T, docs []map[string]any, name string) map[string]any {
	t.Helper()
	for _, doc := range docs {
		if doc["kind"] != "Deployment" {
			continue
		}
		metadata := asMap(t, doc["metadata"], "metadata")
		if metadata["name"] == name {
			return doc
		}
	}
	t.Fatalf("Deployment %s not found", name)
	return nil
}

func findContainer(t *testing.T, deployment map[string]any, name string) map[string]any {
	t.Helper()
	spec := asMap(t, deployment["spec"], "deployment spec")
	template := asMap(t, spec["template"], "template")
	podSpec := asMap(t, template["spec"], "pod spec")
	containers := asSlice(t, podSpec["containers"], "containers")
	for _, item := range containers {
		container := asMap(t, item, "container")
		if container["name"] == name {
			return container
		}
	}
	t.Fatalf("container %s not found", name)
	return nil
}

func asMap(t *testing.T, value any, name string) map[string]any {
	t.Helper()
	typed, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected %s to be a map, got %T", name, value)
	}
	return typed
}

func asSlice(t *testing.T, value any, name string) []any {
	t.Helper()
	typed, ok := value.([]any)
	if !ok {
		t.Fatalf("expected %s to be a slice, got %T", name, value)
	}
	return typed
}

func intValue(t *testing.T, value any, name string) int {
	t.Helper()
	typed, ok := value.(int)
	if !ok {
		t.Fatalf("expected %s to be an int, got %T", name, value)
	}
	return typed
}
