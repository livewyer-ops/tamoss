package releases

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/livewyer-ops/tamoss/operator/internal/schema"
)

// Release records the runtime shipped with an exact product release.
type Release struct {
	Version      string        `json:"version"`
	SourceCommit string        `json:"sourceCommit,omitempty"`
	UpgradeFrom  []string      `json:"upgradeFrom"`
	Schema       schema.Target `json:"schema"`
	Images       Images        `json:"images"`
}

type Images struct {
	API                 string `json:"api"`
	UI                  string `json:"ui"`
	Console             string `json:"console"`
	PostgresClient      string `json:"postgresClient"`
	CNPGPostgresVersion string `json:"cnpgPostgresVersion"`
	RustFS              string `json:"rustfs"`
	TAMSin              string `json:"tamsin"`
}

type Catalogue map[string]Release

type SelectionError struct {
	Reason  string
	Message string
}

func (e *SelectionError) Error() string { return e.Message }

func Load(path string) (Catalogue, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is operator installation configuration, not tenant input.
	if err != nil {
		return nil, err
	}
	var entries []Release
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	catalogue := Catalogue{}
	for _, entry := range entries {
		if entry.Version == "" || strings.TrimSpace(entry.Version) != entry.Version {
			return nil, fmt.Errorf("invalid release version %q", entry.Version)
		}
		if _, exists := catalogue[entry.Version]; exists {
			return nil, fmt.Errorf("duplicate release %q", entry.Version)
		}
		if err := schema.ValidateVersion(entry.Schema.Version); err != nil {
			return nil, fmt.Errorf("release %s: %w", entry.Version, err)
		}
		for field, value := range map[string]string{
			"databaseRevision": entry.Schema.DatabaseRevision, "tamsAPI": entry.Schema.TAMSAPI,
			"api": entry.Images.API, "ui": entry.Images.UI, "console": entry.Images.Console,
			"postgresClient": entry.Images.PostgresClient, "cnpgPostgresVersion": entry.Images.CNPGPostgresVersion,
			"rustfs": entry.Images.RustFS, "tamsin": entry.Images.TAMSin,
		} {
			if value == "" || strings.ContainsAny(value, " \t\r\n") {
				return nil, fmt.Errorf("release %s has invalid %s", entry.Version, field)
			}
		}
		catalogue[entry.Version] = entry
	}
	if len(catalogue) == 0 {
		return nil, fmt.Errorf("release catalogue is empty")
	}
	return catalogue, nil
}

func (c Catalogue) Select(version, current, pending string) (Release, error) {
	if version == "" {
		return Release{}, &SelectionError{"VersionRequired", "Set spec.version to the installed TAMOSS release and wait for status.currentVersion to match and Ready=True before selecting an upgrade. For a new instance, select a release supported by this operator"}
	}
	target, exists := c[version]
	if !exists {
		return Release{}, &SelectionError{"UnsupportedVersion", fmt.Sprintf("Release %q is not supported by this operator installation. Supported releases: %s. Apply a published operator install bundle supporting the requested release and all existing instance releases, then wait for the operator rollout before retrying", version, strings.Join(slices.Sorted(maps.Keys(c)), ", "))}
	}
	if pending != "" && pending != version {
		return Release{}, &SelectionError{"UpgradeInProgress", fmt.Sprintf("Complete the selected upgrade to %q before changing spec.version", pending)}
	}
	if current != "" && current != version && (c[current].Version == "" || !slices.Contains(target.UpgradeFrom, current)) {
		return Release{}, &SelectionError{"UnsupportedUpgrade", fmt.Sprintf("Release %q cannot be upgraded directly to %q. Follow the supported upgrade sequence in the release notes; downgrades are unsupported", current, version)}
	}
	return target, nil
}
