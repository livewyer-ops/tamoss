package controller

import (
	"os"
	"strings"
	"testing"
)

func TestProviderCRDMapConstructionBoundaries(t *testing.T) {
	for _, path := range []string{
		"workload_renderer/httproute.go",
		"workload_renderer/ingress.go",
		"workload_renderer/s3_ingress.go",
	} {
		source := readControllerSource(t, path)
		if strings.Contains(source, "map[string]interface{}{") {
			t.Fatalf("%s builds provider CRDs with raw maps; keep this in typed adapters", path)
		}
	}

	for _, path := range []string{
		"workload_renderer/traefik_middleware.go",
		"backend/rustfs/tenant.go",
	} {
		source := readControllerSource(t, path)
		if !strings.Contains(source, "map[string]interface{}{") {
			t.Fatalf("%s should remain an explicit unstructured provider boundary", path)
		}
	}
}

func readControllerSource(t *testing.T, path string) string {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(source)
}
