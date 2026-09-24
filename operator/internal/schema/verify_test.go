package schema

import "testing"

func TestSchemaVersionDefaultsToDevelopmentMarker(t *testing.T) {
	if SchemaVersion == "" || SchemaVersion == "0.0.0" {
		t.Fatalf("expected non-placeholder schema version, got %q", SchemaVersion)
	}
	if SchemaVersion != DevelopmentSchemaVersion {
		t.Fatalf("expected default schema version %q, got %q", DevelopmentSchemaVersion, SchemaVersion)
	}
}

func TestSupportedTAMSAPIVersionDefaultsTo82(t *testing.T) {
	if SupportedTAMSAPIVersion != "8.2" {
		t.Fatalf("expected default TAMS API compatibility version %q, got %q", "8.2", SupportedTAMSAPIVersion)
	}
}

func TestValidateVersionRejectsPlaceholders(t *testing.T) {
	for _, version := range []string{"", "0.0.0", "v0.0.0"} {
		if err := ValidateVersion(version); err == nil {
			t.Fatalf("expected %q to be rejected", version)
		}
	}
}

func TestVerifyAcceptsDevelopmentSchemaVersion(t *testing.T) {
	if err := Verify(); err != nil {
		t.Fatalf("expected schema verification to accept development version: %v", err)
	}
}

func TestTargetSupportsOnlyItsSchemaAndPredecessor(t *testing.T) {
	target := Target{Version: "current", PreviousVersion: "previous"}
	for _, version := range []string{"", "current", "previous"} {
		if !target.Supports(version) {
			t.Fatalf("rejected %q", version)
		}
	}
	if target.Supports("unknown") {
		t.Fatal("accepted unsupported schema")
	}
}
