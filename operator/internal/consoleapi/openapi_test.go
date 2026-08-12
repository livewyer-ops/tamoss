package consoleapi

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

type openAPITestDocument struct {
	Paths      map[string]map[string]any `json:"paths"`
	Components struct {
		Schemas map[string]openAPITestSchema `json:"schemas"`
	} `json:"components"`
}

type openAPITestSchema struct {
	Ref        string                       `json:"$ref"`
	Properties map[string]openAPITestSchema `json:"properties"`
	Required   []string                     `json:"required"`
	AllOf      []openAPITestSchema          `json:"allOf"`
}

func TestConsoleOpenAPITracksRoutesAndGoContracts(t *testing.T) {
	document := loadConsoleOpenAPI(t)
	wantRoutes := map[string][]string{
		strings.TrimPrefix(HealthPath, APIBasePath):                                              {"get"},
		strings.TrimPrefix(ReadinessPath, APIBasePath):                                           {"get"},
		strings.TrimPrefix(RuntimePath, APIBasePath):                                             {"get"},
		strings.TrimPrefix(RuntimeEventsPath, APIBasePath):                                       {"get"},
		strings.TrimPrefix(SessionPath, APIBasePath):                                             {"get"},
		strings.TrimPrefix(IngestRunsPath, APIBasePath):                                          {"get"},
		strings.TrimPrefix(IngestRunsPath, APIBasePath) + "/{name}":                              {"get"},
		strings.TrimPrefix(strings.TrimPrefix(IngestRunCancelPathPattern, "POST "), APIBasePath): {"post"},
	}
	if len(document.Paths) != len(wantRoutes) {
		t.Fatalf("OpenAPI paths = %d, want %d", len(document.Paths), len(wantRoutes))
	}
	for path, methods := range wantRoutes {
		pathItem, found := document.Paths[path]
		if !found {
			t.Errorf("OpenAPI is missing path %q", path)
			continue
		}
		gotMethods := make([]string, 0, len(pathItem))
		for _, method := range []string{"get", "post", "put", "patch", "delete"} {
			if _, found := pathItem[method]; found {
				gotMethods = append(gotMethods, method)
			}
		}
		slices.Sort(methods)
		if !slices.Equal(gotMethods, methods) {
			t.Errorf("OpenAPI methods for %s = %v, want %v", path, gotMethods, methods)
		}
	}

	contracts := map[string]any{
		"RuntimeSnapshot":          RuntimeSnapshot{},
		"RuntimeInstance":          Instance{},
		"RuntimeInstanceCondition": InstanceCondition{},
		"RuntimeResourceCondition": ResourceCondition{},
		"RuntimeWorkload":          Workload{},
		"RuntimeService":           Service{},
		"RuntimeServicePort":       ServicePort{},
		"RuntimeEndpointSlice":     EndpointSlice{},
		"RuntimeEndpointSlicePort": EndpointSlicePort{},
		"RuntimePod":               Pod{},
		"RuntimeJob":               Job{},
		"RuntimeEvent":             KubernetesEvent{},
		"RuntimeObjectReference":   ObjectReference{},
		"Capability":               Capability{},
		"SessionIdentity":          SessionIdentity{},
		"IngestRunCapabilities":    IngestRunCapabilities{},
		"SessionCapabilities":      SessionCapabilities{},
		"SessionResponse":          SessionResponse{},
		"IngestRunProgress":        IngestRunProgress{},
		"IngestRunSummary":         IngestRunSummary{},
		"IngestRunCondition":       IngestRunCondition{},
		"IngestRunOptions":         IngestRunOptions{},
		"IngestRunTAMSFlowProfile": IngestRunTAMSFlowProfile{},
		"IngestRunFlowMetadata":    IngestRunFlowMetadata{},
		"IngestRunOutputIntent":    IngestRunOutputIntent{},
		"IngestRunObjectReference": IngestRunObjectReference{},
		"IngestRunResult":          IngestRunResult{},
		"IngestRunOutputFlow":      IngestRunOutputFlow{},
		"IngestRunOutput":          IngestRunOutput{},
		"IngestRunDetail":          IngestRunDetail{},
		"IngestRunPageInformation": IngestRunPageInformation{},
		"IngestRunListResponse":    IngestRunListPage{},
		"CancelIngestRunRequest":   CancelIngestRunRequest{},
		"CancelIngestRunResponse":  CancelIngestRunResponse{},
	}
	for schemaName, goValue := range contracts {
		schema, found := document.Components.Schemas[schemaName]
		if !found {
			t.Errorf("OpenAPI is missing schema %q", schemaName)
			continue
		}
		gotProperties, gotRequired := resolveOpenAPISchema(t, document, schema)
		wantProperties, wantRequired := goJSONContract(reflect.TypeOf(goValue))
		if !slices.Equal(gotProperties, wantProperties) {
			t.Errorf("OpenAPI %s properties = %v, want Go JSON fields %v", schemaName, gotProperties, wantProperties)
		}
		if !slices.Equal(gotRequired, wantRequired) {
			t.Errorf("OpenAPI %s required = %v, want Go required fields %v", schemaName, gotRequired, wantRequired)
		}
	}
}

// TestConsoleOpenAPIDocumentsTheCursorQueryBinding keeps the published contract
// honest about ingestRunCursorCodec: the cursor authenticates the limit and
// phase it was issued with, so reusing it with either changed is a 400 rather
// than a silently re-anchored traversal.
func TestConsoleOpenAPIDocumentsTheCursorQueryBinding(t *testing.T) {
	t.Parallel()
	var document struct {
		Paths map[string]struct {
			Get struct {
				Description string `json:"description"`
				Parameters  []struct {
					Name        string `json:"name"`
					In          string `json:"in"`
					Description string `json:"description"`
				} `json:"parameters"`
				Responses map[string]struct {
					Description string `json:"description"`
				} `json:"responses"`
			} `json:"get"`
		} `json:"paths"`
	}
	encoded, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("read Console OpenAPI: %v", err)
	}
	if err := yaml.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode Console OpenAPI: %v", err)
	}
	operation, found := document.Paths[strings.TrimPrefix(IngestRunsPath, APIBasePath)]
	if !found {
		t.Fatalf("OpenAPI is missing %s", IngestRunsPath)
	}

	descriptions := make(map[string]string, len(operation.Get.Parameters))
	for _, parameter := range operation.Get.Parameters {
		if parameter.In == "query" {
			descriptions[parameter.Name] = parameter.Description
		}
	}
	for _, name := range []string{"limit", "phase", "cursor"} {
		if descriptions[name] == "" {
			t.Fatalf("OpenAPI query parameter %q has no description", name)
		}
		if !strings.Contains(descriptions[name], "invalid_cursor") {
			t.Errorf("OpenAPI %q description omits the invalid_cursor outcome: %q", name, descriptions[name])
		}
	}
	for _, phrase := range []string{"limit", "phase"} {
		if !strings.Contains(descriptions["cursor"], phrase) {
			t.Errorf("OpenAPI cursor description omits the %s binding: %q", phrase, descriptions["cursor"])
		}
	}
	badRequest := operation.Get.Responses["400"].Description
	if !strings.Contains(badRequest, "invalid_cursor") || !strings.Contains(badRequest, "invalid_query") {
		t.Errorf("OpenAPI 400 description does not name both list failure codes: %q", badRequest)
	}
}

func loadConsoleOpenAPI(t *testing.T) openAPITestDocument {
	t.Helper()
	encoded, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("read Console OpenAPI: %v", err)
	}
	var document openAPITestDocument
	if err := yaml.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode Console OpenAPI: %v", err)
	}
	return document
}

func resolveOpenAPISchema(
	t *testing.T,
	document openAPITestDocument,
	schema openAPITestSchema,
) ([]string, []string) {
	t.Helper()
	properties := make(map[string]struct{})
	required := make(map[string]struct{})
	var visit func(openAPITestSchema)
	visit = func(current openAPITestSchema) {
		if current.Ref != "" {
			const prefix = "#/components/schemas/"
			name := strings.TrimPrefix(current.Ref, prefix)
			resolved, found := document.Components.Schemas[name]
			if !strings.HasPrefix(current.Ref, prefix) || !found {
				t.Fatalf("unresolved OpenAPI schema reference %q", current.Ref)
			}
			visit(resolved)
		}
		for name := range current.Properties {
			properties[name] = struct{}{}
		}
		for _, name := range current.Required {
			required[name] = struct{}{}
		}
		for _, member := range current.AllOf {
			visit(member)
		}
	}
	visit(schema)
	return sortedSet(properties), sortedSet(required)
}

func goJSONContract(objectType reflect.Type) ([]string, []string) {
	properties := make(map[string]struct{})
	required := make(map[string]struct{})
	var visit func(reflect.Type)
	visit = func(current reflect.Type) {
		for index := range current.NumField() {
			field := current.Field(index)
			tag := field.Tag.Get("json")
			if field.Anonymous && tag == "" {
				visit(field.Type)
				continue
			}
			parts := strings.Split(tag, ",")
			if len(parts) == 0 || parts[0] == "" || parts[0] == "-" {
				continue
			}
			properties[parts[0]] = struct{}{}
			if !slices.Contains(parts[1:], "omitempty") {
				required[parts[0]] = struct{}{}
			}
		}
	}
	visit(objectType)
	return sortedSet(properties), sortedSet(required)
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}
