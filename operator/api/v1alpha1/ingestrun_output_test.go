package v1alpha1

import (
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestValidateIngestRunOutputAcceptsTAMSMetadata(t *testing.T) {
	output := &IngestRunOutputIntent{FlowMetadata: IngestRunFlowMetadata{
		Label:       "8.2 ingest test",
		Description: "Acceptance ingest",
		Tags: map[string]apiextensionsv1.JSON{
			"editorial_purpose": {Raw: []byte(`["testing","review"]`)},
			"owner":             {Raw: []byte(`"media-operations"`)},
		},
	}}
	if err := ValidateIngestRunOutput(output); err != nil {
		t.Fatal(err)
	}
	got, err := IngestRunFlowMetadataJSON(output)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"description":"Acceptance ingest","label":"8.2 ingest test","tags":{"editorial_purpose":["testing","review"],"owner":"media-operations"}}`
	if got != want {
		t.Fatalf("metadata = %s, want %s", got, want)
	}
}

func TestValidateIngestRunOutputRejectsReservedAndInvalidTags(t *testing.T) {
	for name, tags := range map[string]map[string]apiextensionsv1.JSON{
		"reserved": {"_TAMSIN_source": {Raw: []byte(`"spoofed"`)}},
		"object":   {"owner": {Raw: []byte(`{"name":"media"}`)}},
		"mixed":    {"owner": {Raw: []byte(`["media",4]`)}},
	} {
		t.Run(name, func(t *testing.T) {
			output := &IngestRunOutputIntent{FlowMetadata: IngestRunFlowMetadata{Label: "test", Tags: tags}}
			if err := ValidateIngestRunOutput(output); err == nil {
				t.Fatal("expected invalid output metadata")
			}
		})
	}
}

func TestValidateIngestRunOutputRejectsEmptyFlowMetadata(t *testing.T) {
	if err := ValidateIngestRunOutput(&IngestRunOutputIntent{}); err == nil {
		t.Fatal("expected empty Flow output metadata to be rejected")
	}
}

func TestValidateIngestRunOutputBoundsRenderedMetadata(t *testing.T) {
	output := &IngestRunOutputIntent{FlowMetadata: IngestRunFlowMetadata{Description: strings.Repeat("x", maxIngestRunOutputMetadataBytes)}}
	if err := ValidateIngestRunOutput(output); err == nil {
		t.Fatal("expected oversized output metadata to be rejected")
	}
}
