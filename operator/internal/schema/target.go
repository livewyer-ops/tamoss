package schema

import "strings"

// Target is the schema contract of the selected instance release.
type Target struct {
	Version          string `json:"version"`
	PreviousVersion  string `json:"previousVersion,omitempty"`
	DatabaseRevision string `json:"databaseRevision"`
	TAMSAPI          string `json:"tamsAPI"`
}

func (t Target) Supports(version string) bool {
	value := strings.TrimSpace(version)
	return value == "" || value == t.Version || (t.PreviousVersion != "" && value == t.PreviousVersion)
}
