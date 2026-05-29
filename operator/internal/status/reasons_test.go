package status

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var reasonNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

type statusKind string

const (
	statusKindCondition statusKind = "condition"
	statusKindPhase     statusKind = "phase"
	statusKindReason    statusKind = "reason"
)

func TestExportedStatusConstantsHaveKubernetesCompatibleNames(t *testing.T) {
	constants := exportedStatusConstants(t)
	for _, kind := range []statusKind{statusKindCondition, statusKindPhase, statusKindReason} {
		for name := range constants[kind] {
			if strings.TrimSpace(name) == "" {
				t.Fatalf("%s status constant has empty value", kind)
			}
			if kind == statusKindReason && !reasonNamePattern.MatchString(name) {
				t.Fatalf("reason %q is not one-word CamelCase compatible", name)
			}
		}
	}
}

func TestControllersUseRegistryBackedStatusNames(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	checks := map[string]*regexp.Regexp{
		filepath.Join(repoRoot, "internal", "controller", "tamoss_controller.go"): regexp.MustCompile(
			`(condition(Ready|Progressing|SchemaMigrated|BackendsReady|BackupPolicyReady|IdentityBlueprintSubmitted|IdentityReady|Upgradeable|RoutingReady|HostnamesReady|Degraded|Paused)\b|eventReason[A-Z][A-Za-z]*\b|Status\.Phase\s*=\s*"|recordPhase\(")`,
		),
		filepath.Join(repoRoot, "internal", "controller", "storagebackend_controller.go"): regexp.MustCompile(
			`(conditionStorageBackend[A-Za-z]*\b|conditionExternalS3DiagnosticReady\b|eventReason[A-Z][A-Za-z]*\b|Status\.Phase\s*=\s*")`,
		),
		filepath.Join(repoRoot, "internal", "controller", "backend", "cnpg", "status.go"): regexp.MustCompile(
			`(TamossConditionBackendsReady|EventReasonBackupFailed)`,
		),
		filepath.Join(repoRoot, "internal", "controller", "backend", "cnpg", "secrets.go"): regexp.MustCompile(
			`(^|[^.A-Za-z0-9_])(ReasonWaitingForCNPGSecret|ReasonCNPGSecretKeyMissing)\b`,
		),
		filepath.Join(repoRoot, "internal", "controller", "backend", "rustfs", "status.go"): regexp.MustCompile(
			`(TamossConditionBackendsReady|EventReasonTenantFailed)`,
		),
	}

	for path, pattern := range checks {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if match := pattern.FindString(string(content)); match != "" {
			t.Fatalf("%s uses controller-local status string %q", path, match)
		}
	}
}

func TestControllersDoNotInlineRegisteredPublicReasons(t *testing.T) {
	reasons := exportedStatusConstants(t)[statusKindReason]
	controllerRoot := filepath.Clean(filepath.Join("..", "controller"))

	var findings []string
	for _, path := range productionGoFiles(t, controllerRoot) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, ok := stringLiteralValue(t, literal)
			if !ok {
				return true
			}
			if _, registered := reasons[value]; registered {
				position := fset.Position(literal.Pos())
				findings = append(findings, fmt.Sprintf("%s:%d inlines registered reason %q", path, position.Line, value))
			}
			return true
		})
	}

	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("controller code must use status reason constants:\n%s", strings.Join(findings, "\n"))
	}
}

func exportedStatusConstants(t *testing.T) map[statusKind]map[string]struct{} {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "reasons.go", nil, 0)
	if err != nil {
		t.Fatalf("parse reasons.go: %v", err)
	}

	values := map[statusKind]map[string]struct{}{
		statusKindCondition: {},
		statusKindPhase:     {},
		statusKindReason:    {},
	}
	for _, declaration := range file.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, specNode := range gen.Specs {
			spec := specNode.(*ast.ValueSpec)
			for index, name := range spec.Names {
				kind, ok := statusConstantKind(name.Name)
				if !ok || !name.IsExported() {
					continue
				}
				if index >= len(spec.Values) {
					t.Fatalf("status constant %s must declare an explicit string value", name.Name)
				}
				value, ok := stringLiteralValue(t, spec.Values[index])
				if !ok {
					t.Fatalf("status constant %s must be a string literal", name.Name)
				}
				if _, duplicate := values[kind][value]; duplicate {
					t.Fatalf("duplicate %s status value %q", kind, value)
				}
				values[kind][value] = struct{}{}
			}
		}
	}
	return values
}

func statusConstantKind(name string) (statusKind, bool) {
	switch {
	case strings.HasPrefix(name, "Condition"):
		return statusKindCondition, true
	case strings.HasPrefix(name, "Phase"):
		return statusKindPhase, true
	case strings.HasPrefix(name, "Reason"):
		return statusKindReason, true
	default:
		return "", false
	}
}

func stringLiteralValue(t *testing.T, expression ast.Expr) (string, bool) {
	t.Helper()
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		t.Fatalf("unquote %s: %v", literal.Value, err)
	}
	return value, true
}

func productionGoFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(files)
	return files
}
