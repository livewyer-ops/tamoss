package releases

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishedEntriesAndTransitions(t *testing.T) {
	catalogue, err := Load("../../config/releases/catalogue.json")
	if err != nil {
		t.Fatal(err)
	}
	old := catalogue["8.2.0-oss1"]
	rc := catalogue["8.2.0-oss2-rc2"]
	if rc.Schema.Version != old.Schema.Version || rc.Schema.DatabaseRevision != "20260810_0007" || catalogue["dev"].Schema.DatabaseRevision != "20260924_0008" {
		t.Fatal("published RC schema was replaced by current family metadata")
	}
	for _, tc := range []struct {
		desired, current string
		allowed          bool
	}{
		{old.Version, "", true},
		{rc.Version, old.Version, true},
		{"dev", old.Version, true},
		{"dev", rc.Version, true},
		{old.Version, old.Version, true},
		{old.Version, rc.Version, false},
		{"dev", "8.1.0-oss6", false},
		{"", old.Version, false},
		{"latest", old.Version, false},
	} {
		_, err := catalogue.Select(tc.desired, tc.current, "")
		if (err == nil) != tc.allowed {
			t.Errorf("select %q from %q: %v", tc.desired, tc.current, err)
		}
	}
	data, err := os.ReadFile("../../config/releases/catalogue.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []Release
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func([]Release) []Release{
		func(v []Release) []Release { return append(v, v[0]) },
		func(v []Release) []Release { v[0].Schema.DatabaseRevision = ""; return v },
		func(v []Release) []Release { v[0].Images.API = ""; return v },
		func(v []Release) []Release { return nil },
	} {
		copy := append([]Release{}, entries...)
		raw, err := json.Marshal(mutate(copy))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "catalogue.json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("invalid catalogue accepted: %s", raw)
		}
	}
}
