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

func TestIsSupportedStartingVersion(t *testing.T) {
	if !IsSupportedStartingVersion("") {
		t.Fatal("empty state should be supported as a fresh install")
	}
	if !IsSupportedStartingVersion(SchemaVersion) {
		t.Fatal("current schema version should be supported")
	}
	if IsSupportedStartingVersion("unknown") {
		t.Fatal("unknown schema version should not be supported")
	}
}
