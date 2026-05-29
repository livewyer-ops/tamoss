package v1alpha1

import "testing"

func TestExternalBackendConnectionsRemainSupported(t *testing.T) {
	db := DBBackendSpec{
		ProvidedBy: BackendProvidedByExternal,
		External: &DBExternalSpec{
			Host:     "postgres.example.internal",
			Port:     "5432",
			Database: "tams",
			Auth: SecretReferenceSpec{
				ExistingSecret: "postgres-creds",
				SecretKeys: SecretKeySpec{
					Username: "username",
					Password: "password",
				},
			},
		},
	}
	s3 := S3BackendSpec{
		ProvidedBy: S3BackendProvidedByExternal,
		External: &S3ExternalSpec{
			Endpoint: S3EndpointSpec{
				Default: EndpointURLSpec{URL: "https://s3.example.com"},
			},
			Region: "eu-west-2",
			Bucket: "tamoss",
			Auth: SecretReferenceSpec{
				ExistingSecret: "s3-creds",
				SecretKeys: SecretKeySpec{
					AccessKey: "accessKey",
					SecretKey: "secretKey",
				},
			},
		},
	}

	if got := db.Connection("tamoss").Host; got != "postgres.example.internal" {
		t.Fatalf("expected external database host preserved, got %q", got)
	}
	if got := s3.Connection("tamoss").Endpoint.Default.URL; got != "https://s3.example.com" {
		t.Fatalf("expected external S3 endpoint preserved, got %q", got)
	}
}

func TestBundledBlocksDoNotInferManagedProvider(t *testing.T) {
	db := DBBackendSpec{Bundled: &DBBundledSpec{}}
	s3 := S3BackendSpec{Bundled: &S3BundledSpec{}}

	if got := db.Provider(); got != BackendProvidedByExternal {
		t.Fatalf("expected bundled database block to stop inferring a provider, got %s", got)
	}
	if got := s3.Provider(); got != S3BackendProvidedByExternal {
		t.Fatalf("expected bundled S3 block to stop inferring a provider, got %s", got)
	}
}
