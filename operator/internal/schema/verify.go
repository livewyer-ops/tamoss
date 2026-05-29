package schema

import (
	"fmt"
	"strings"
)

const DevelopmentSchemaVersion = "dev"

var (
	SchemaVersion                  = DevelopmentSchemaVersion
	PreviousSupportedSchemaVersion = ""
)

func Verify() error {
	return ValidateVersion(SchemaVersion)
}

func ValidateVersion(version string) error {
	value := strings.TrimSpace(version)
	switch value {
	case "":
		return fmt.Errorf("schema version is empty")
	case "0.0.0", "v0.0.0":
		return fmt.Errorf("schema version %q is a placeholder", version)
	default:
		return nil
	}
}

func IsSupportedStartingVersion(version string) bool {
	value := strings.TrimSpace(version)
	if value == "" || value == SchemaVersion {
		return true
	}
	return PreviousSupportedSchemaVersion != "" && value == PreviousSupportedSchemaVersion
}
