package v1alpha1

import (
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestStorageBackendDefaultsExternalS3RegistrationOnly(t *testing.T) {
	spec := StorageBackendSpec{
		Provider:   StorageBackendProviderExternalS3,
		BucketName: "media",
		Endpoint: S3EndpointSpec{
			Default: EndpointURLSpec{URL: "https://s3.example.test"},
		},
	}

	spec.ApplyDefaults("media", "archive")

	if spec.Region != "us-east-1" {
		t.Fatalf("expected default region, got %q", spec.Region)
	}
	if spec.Endpoint.Default.URL != "https://s3.example.test" {
		t.Fatalf("expected explicit default endpoint to be preserved, got %q", spec.Endpoint.Default.URL)
	}
	if spec.Endpoint.Public.URL != "https://s3.example.test" {
		t.Fatalf("expected public endpoint to default to default endpoint, got %q", spec.Endpoint.Public.URL)
	}
	if spec.Label != "tamoss.us-east-1:s3:media" {
		t.Fatalf("expected derived label, got %q", spec.Label)
	}
	if !spec.IsExternalObjectStore() {
		t.Fatal("external-s3 provider should be treated as external object storage")
	}
}

func TestStorageBackendExternalS3DoesNotInferCloudEndpoint(t *testing.T) {
	spec := StorageBackendSpec{
		Provider:   StorageBackendProviderExternalS3,
		BucketName: "media",
	}

	spec.ApplyDefaults("media", "archive")

	if spec.Endpoint.Default.URL != "" {
		t.Fatalf("expected external-s3 default endpoint to remain explicit, got %q", spec.Endpoint.Default.URL)
	}
	if spec.Endpoint.Public.URL != "" {
		t.Fatalf("expected external-s3 public endpoint to remain explicit without default endpoint, got %q", spec.Endpoint.Public.URL)
	}
}

func TestValidateStorageBackendTagsAcceptsTAMSValueUnion(t *testing.T) {
	tags := map[string]apiextensionsv1.JSON{
		"tier":   {Raw: []byte(`"hot"`)},
		"access": {Raw: []byte(`["programme","archive"]`)},
		"empty":  {Raw: []byte(`[]`)},
	}

	if err := ValidateStorageBackendTags(tags); err != nil {
		t.Fatalf("expected scalar and array tag values to be valid: %v", err)
	}
}

func TestValidateStorageBackendTagsReportsTheFirstInvalidKey(t *testing.T) {
	tags := map[string]apiextensionsv1.JSON{
		"z": {Raw: []byte("null")},
		"a": {Raw: []byte("7")},
	}
	if err := ValidateStorageBackendTags(tags); err == nil || !strings.HasPrefix(err.Error(), `tag "a"`) {
		t.Fatalf("expected the lexically first invalid tag, got %v", err)
	}
	if err := ValidateStorageBackendTags(nil); err != nil {
		t.Fatalf("nil tags must remain valid: %v", err)
	}
}

func TestValidateStorageBackendTagsRejectsValuesOutsideTAMSUnion(t *testing.T) {
	tests := map[string]string{
		"number":      `7`,
		"boolean":     `true`,
		"null":        `null`,
		"object":      `{"nested":"value"}`,
		"mixed-array": `["valid",7]`,
	}

	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			tags := map[string]apiextensionsv1.JSON{
				"invalid": {Raw: []byte(raw)},
			}
			if err := ValidateStorageBackendTags(tags); err == nil {
				t.Fatalf("expected %s tag value to be rejected", name)
			}
		})
	}
}
