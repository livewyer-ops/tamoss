package discovery

import (
	"testing"
)

func TestHintForSeededEntries(t *testing.T) {
	hint, ok := HintFor("cnpg")
	if !ok {
		t.Fatalf("expected CNPG remediation hint")
	}
	if hint.GVR != CNPGClustersGVR {
		t.Fatalf("expected GVR %s, got %s", CNPGClustersGVR.String(), hint.GVR.String())
	}
	if hint.DependencyName != "CloudNativePG" {
		t.Fatalf("expected CloudNativePG dependency name, got %q", hint.DependencyName)
	}

	hint, ok = HintFor("rustfs-operator")
	if !ok {
		t.Fatalf("expected RustFS Operator remediation hint")
	}
	if hint.GVR != RustFSTenantsGVR {
		t.Fatalf("expected GVR %s, got %s", RustFSTenantsGVR.String(), hint.GVR.String())
	}
	if hint.DependencyName != "RustFS Operator" {
		t.Fatalf("expected RustFS Operator dependency name, got %q", hint.DependencyName)
	}
	if RustFSOperatorChartVersion != "0.1.0" {
		t.Fatalf("expected pinned RustFS Operator chart version 0.1.0, got %q", RustFSOperatorChartVersion)
	}
}

func TestHintForUnknownProvider(t *testing.T) {
	if _, ok := HintFor("external"); ok {
		t.Fatalf("did not expect remediation hint for external provider")
	}
}
