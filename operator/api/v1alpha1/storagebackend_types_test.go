package v1alpha1

import "testing"

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
