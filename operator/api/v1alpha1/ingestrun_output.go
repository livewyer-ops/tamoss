package v1alpha1

import (
	"encoding/json"
	"fmt"
	"strings"
)

const maxIngestRunOutputMetadataBytes = 16 * 1024

// ValidateIngestRunOutput checks constraints which Kubernetes structural
// schemas cannot express for preserved arbitrary JSON tag values.
func ValidateIngestRunOutput(output *IngestRunOutputIntent) error {
	if output == nil {
		return nil
	}
	if output.FlowMetadata.Label == "" && output.FlowMetadata.Description == "" && len(output.FlowMetadata.Tags) == 0 {
		return fmt.Errorf("flowMetadata must set label, description, or tags")
	}
	for key := range output.FlowMetadata.Tags {
		if strings.HasPrefix(strings.ToLower(key), "_tamsin_") {
			return fmt.Errorf("tag %q uses the reserved _tamsin_ prefix", key)
		}
	}
	if err := ValidateStorageBackendTags(output.FlowMetadata.Tags); err != nil {
		return err
	}
	encoded, err := IngestRunFlowMetadataJSON(output)
	if err != nil {
		return err
	}
	if len(encoded) > maxIngestRunOutputMetadataBytes {
		return fmt.Errorf("flow metadata exceeds %d bytes", maxIngestRunOutputMetadataBytes)
	}
	return nil
}

// IngestRunFlowMetadataJSON returns stable JSON for TAMSin's
// --flow-metadata argument. encoding/json orders map keys, so equivalent intent
// produces an identical immutable Job template.
func IngestRunFlowMetadataJSON(output *IngestRunOutputIntent) (string, error) {
	if output == nil {
		return "", nil
	}
	metadata := make(map[string]any, 3)
	if output.FlowMetadata.Label != "" {
		metadata["label"] = output.FlowMetadata.Label
	}
	if output.FlowMetadata.Description != "" {
		metadata["description"] = output.FlowMetadata.Description
	}
	if len(output.FlowMetadata.Tags) > 0 {
		tags := make(map[string]json.RawMessage, len(output.FlowMetadata.Tags))
		for key, value := range output.FlowMetadata.Tags {
			tags[key] = value.Raw
		}
		metadata["tags"] = tags
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("encode Flow metadata: %w", err)
	}
	return string(encoded), nil
}
