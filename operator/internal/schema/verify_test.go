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

func TestSupportedTAMSAPIVersionDefaultsTo81(t *testing.T) {
	if SupportedTAMSAPIVersion != "8.1" {
		t.Fatalf("expected default TAMS API compatibility version %q, got %q", "8.1", SupportedTAMSAPIVersion)
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

func TestIsSupportedStartingVersionAcceptsPreviousRevision(t *testing.T) {
	currentVersion := SchemaVersion
	previousVersion := PreviousSupportedSchemaVersion
	defer func() {
		SchemaVersion = currentVersion
		PreviousSupportedSchemaVersion = previousVersion
	}()

	SchemaVersion = "8.1.0-oss1"
	PreviousSupportedSchemaVersion = "8.0.0-oss1"

	if !IsSupportedStartingVersion("8.0.0-oss1") {
		t.Fatal("previous supported schema revision should be accepted")
	}
	if IsSupportedStartingVersion("7.9.0-oss1") {
		t.Fatal("unsupported previous schema revision should be rejected")
	}
}
