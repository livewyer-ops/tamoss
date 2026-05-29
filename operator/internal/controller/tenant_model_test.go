package controller

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestNamespaceAllowedSupportsMultipleTenantNamespaces(t *testing.T) {
	watchNamespaces := ParseWatchNamespaces("tams-team-a, tams-team-b")
	reconciler := TamossReconciler{WatchNamespaces: watchNamespaces}

	for _, namespace := range []string{"tams-team-a", "tams-team-b"} {
		if !reconciler.namespaceAllowed(namespace) {
			t.Fatalf("expected namespace %q to be watched", namespace)
		}
	}
	if reconciler.namespaceAllowed("tams-team-c") {
		t.Fatalf("expected unwatched namespace to be ignored")
	}
}

func TestNoSupersededTenantPlatformAPITypes(t *testing.T) {
	forbidden := map[string]struct{}{
		"Tenant":         {},
		"TamossTenant":   {},
		"TamossPlatform": {},
	}
	assertNoForbiddenGoTypes(t, "../../api/v1alpha1", forbidden)
	assertNoForbiddenTamossCRDs(t, "../../config/crd/bases", forbidden)
}

func assertNoForbiddenGoTypes(t *testing.T, root string, forbidden map[string]struct{}) {
	t.Helper()
	fileSet := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if _, blocked := forbidden[typeSpec.Name.Name]; blocked {
					t.Fatalf("superseded API type %q found in %s", typeSpec.Name.Name, path)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan API types: %v", err)
	}
}

func assertNoForbiddenTamossCRDs(t *testing.T, root string, forbidden map[string]struct{}) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var crd struct {
			Spec struct {
				Group string `json:"group"`
				Names struct {
					Kind string `json:"kind"`
				} `json:"names"`
			} `json:"spec"`
		}
		if err := yaml.Unmarshal(data, &crd); err != nil {
			return err
		}
		if crd.Spec.Group != "tamoss.livewyer.io" {
			return nil
		}
		if _, blocked := forbidden[crd.Spec.Names.Kind]; blocked {
			t.Fatalf("superseded CRD kind %q found in %s", crd.Spec.Names.Kind, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan CRDs: %v", err)
	}
}
