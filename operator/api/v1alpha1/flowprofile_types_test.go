package v1alpha1

import (
	"regexp"
	"testing"
)

func TestDeterministicFlowProfileID(t *testing.T) {
	first := DeterministicFlowProfileID("media", "hd-avc")
	if first != DeterministicFlowProfileID("media", "hd-avc") {
		t.Fatal("deterministic FlowProfile ID changed between calls")
	}
	if first == DeterministicFlowProfileID("media", "hd-hevc") {
		t.Fatal("different FlowProfile names produced the same ID")
	}
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !pattern.MatchString(first) {
		t.Fatalf("derived ID %q is not a canonical version-5 UUID", first)
	}
}

func TestFlowProfileSpecDefaultsOnlyMissingID(t *testing.T) {
	spec := FlowProfileSpec{ID: "60d9df18-6d9d-4b86-84bf-d1dcf14b3a28"}
	spec.ApplyDefaults("media", "hd-avc")
	if spec.ID != "60d9df18-6d9d-4b86-84bf-d1dcf14b3a28" {
		t.Fatalf("explicit FlowProfile ID was replaced: %q", spec.ID)
	}
}
