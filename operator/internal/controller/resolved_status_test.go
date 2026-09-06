package controller

import (
	"encoding/json"
	"strings"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
)

func TestResolvedTamossStatusUsesProfileDefaults(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"},
		Spec: tamossv1alpha1.TamossSpec{
			Profile: tamossv1alpha1.TamossProfileLocalKind,
			Secrets: tamossv1alpha1.SecretsSpec{
				APIToken: tamossv1alpha1.APITokenSecretSpec{Generate: true},
			},
		},
	}
	defaults.Apply(tamoss)

	setCommonTamossStatus(tamoss)

	if tamoss.Status.Endpoints.API != "https://api.tamoss.localtest.me" {
		t.Fatalf("expected default API endpoint, got %q", tamoss.Status.Endpoints.API)
	}
	if tamoss.Status.Endpoints.UI != "https://app.tamoss.localtest.me" {
		t.Fatalf("expected default UI endpoint, got %q", tamoss.Status.Endpoints.UI)
	}
	if tamoss.Status.Resolved.Images.API != "livewyer/tamoss-api:dev" {
		t.Fatalf("expected default API image, got %q", tamoss.Status.Resolved.Images.API)
	}
	if tamoss.Status.Resolved.Images.UI != "livewyer/tamoss-ui:dev" {
		t.Fatalf("expected default UI image, got %q", tamoss.Status.Resolved.Images.UI)
	}
	if tamoss.Status.Resolved.Images.Console != "livewyer/tamoss-console-api:dev" {
		t.Fatalf("expected default Console image, got %q", tamoss.Status.Resolved.Images.Console)
	}
	if tamoss.Status.Resolved.Images.Worker != "livewyer/tamoss-api:dev" {
		t.Fatalf("expected worker to use API image, got %q", tamoss.Status.Resolved.Images.Worker)
	}
	if tamoss.Status.Resolved.Images.SchemaMigrationPostgresClient != defaults.DefaultPostgresClientImage {
		t.Fatalf("expected default schema migration image, got %q", tamoss.Status.Resolved.Images.SchemaMigrationPostgresClient)
	}
	if tamoss.Status.Resolved.Images.CNPGPostgres != "ghcr.io/cloudnative-pg/postgresql:"+defaults.DefaultCNPGPostgresVersion {
		t.Fatalf("expected default CNPG Postgres image, got %q", tamoss.Status.Resolved.Images.CNPGPostgres)
	}
	if tamoss.Status.Resolved.Images.RustFS != defaults.DefaultRustFSImage {
		t.Fatalf("expected default RustFS image, got %q", tamoss.Status.Resolved.Images.RustFS)
	}
	if tamoss.Status.Resolved.Images.TAMSin != defaults.DefaultTAMSinImage {
		t.Fatalf("expected default TAMSin image, got %q", tamoss.Status.Resolved.Images.TAMSin)
	}
	if tamoss.Status.Resolved.Versions.Schema == "0.0.0" ||
		tamoss.Status.Resolved.Versions.Schema == "" {
		t.Fatalf("expected non-placeholder schema version, got %#v", tamoss.Status.Resolved.Versions)
	}
	if tamoss.Status.Resolved.Versions.Tamoss != "dev" {
		t.Fatalf("expected runtime version from local-kind image tag, got %#v", tamoss.Status.Resolved.Versions)
	}
	if tamoss.Status.Resolved.Versions.TAMSAPI != schemabundle.SupportedTAMSAPIVersion {
		t.Fatalf("expected TAMS API compatibility version, got %#v", tamoss.Status.Resolved.Versions)
	}
	if tamoss.Status.Resolved.Resources.API != "example-api" ||
		tamoss.Status.Resolved.Resources.UI != "example-ui" ||
		tamoss.Status.Resolved.Resources.Worker != "example-worker" {
		t.Fatalf("expected workload resource names, got %#v", tamoss.Status.Resolved.Resources)
	}
	if tamoss.Status.Resolved.Resources.Console != "" {
		t.Fatalf("did not expect an opt-in Console resource, got %q", tamoss.Status.Resolved.Resources.Console)
	}
	if tamoss.Status.Resolved.Resources.DefaultStorageBackend != "example-storage-default" {
		t.Fatalf("expected default StorageBackend resource name, got %q", tamoss.Status.Resolved.Resources.DefaultStorageBackend)
	}
	if tamoss.Status.Resolved.GeneratedSecrets.APIToken != "example-api-token" {
		t.Fatalf("expected generated API token Secret name, got %q", tamoss.Status.Resolved.GeneratedSecrets.APIToken)
	}
	if tamoss.Status.Resolved.GeneratedSecrets.OAuth2Credentials != "example-oauth2-creds" {
		t.Fatalf("expected generated OAuth2 credentials Secret name, got %q", tamoss.Status.Resolved.GeneratedSecrets.OAuth2Credentials)
	}
	if tamoss.Status.Resolved.GeneratedSecrets.StorageBackendCredentials != "example-storage-backend-credentials" {
		t.Fatalf("expected storage backend credentials Secret name, got %q", tamoss.Status.Resolved.GeneratedSecrets.StorageBackendCredentials)
	}
}

func TestEndpointStatusUsesExplicitPublicUIURL(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		Spec: tamossv1alpha1.TamossSpec{
			PublicEndpoint: tamossv1alpha1.PublicEndpointSpec{
				UIURL: "https://app.example.com:30443/",
			},
			Ingress: tamossv1alpha1.IngressSpec{
				Enabled: ptr.To(true),
				API:     tamossv1alpha1.IngressHostSpec{Host: "api.example.com"},
				UI: tamossv1alpha1.UIIngressSpec{Web: tamossv1alpha1.IngressHostSpec{
					Host: "app.example.com",
				}},
				TLS: []networkingv1.IngressTLS{{}},
			},
		},
	}

	status := endpointStatus(tamoss)
	if status.UI != "https://app.example.com:30443" {
		t.Fatalf("expected explicit public UI URL in status, got %q", status.UI)
	}
}

func TestResolvedTamossStatusReflectsOverridesAndRedactsSecrets(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"},
		Spec: tamossv1alpha1.TamossSpec{
			FullnameOverride: "custom",
			API: tamossv1alpha1.APIComponentSpec{
				Image: tamossv1alpha1.ImageSpec{Repository: "registry.example.com/tamoss-api", Tag: "v1.2.3"},
			},
			UI: tamossv1alpha1.UIComponentSpec{
				Image: tamossv1alpha1.ImageSpec{Repository: "registry.example.com/tamoss-ui", Tag: "v1.2.4"},
			},
			Console: tamossv1alpha1.ConsoleComponentSpec{
				Enabled: ptr.To(true),
				Image:   tamossv1alpha1.ImageSpec{Repository: "registry.example.com/tamoss-console-api", Tag: "v1.2.5"},
			},
			Images: tamossv1alpha1.ComponentImagesSpec{
				SchemaMigrationPostgresClient: "registry.example.com/postgres-client:v16.4",
			},
			Backends: tamossv1alpha1.BackendsSpec{
				DB: tamossv1alpha1.DBBackendSpec{
					ProvidedBy: tamossv1alpha1.BackendProvidedByCNPG,
					CNPG: &tamossv1alpha1.DBCNPGSpec{
						PostgresVersion: "16.4",
					},
				},
				S3: tamossv1alpha1.S3BackendSpec{
					ProvidedBy: tamossv1alpha1.S3BackendProvidedByRustFSOperator,
					RustFSOperator: &tamossv1alpha1.S3RustFSOperatorSpec{
						Image: "registry.example.com/rustfs:v1",
					},
				},
			},
			Secrets: tamossv1alpha1.SecretsSpec{
				APIToken: tamossv1alpha1.APITokenSecretSpec{Generate: false, Token: "do-not-expose"},
			},
		},
	}
	defaults.Apply(tamoss)

	status := resolvedTamossStatus(tamoss, "registry.example.com/tamsin@sha256:"+strings.Repeat("b", 64))

	if status.Images.API != "registry.example.com/tamoss-api:v1.2.3" {
		t.Fatalf("expected overridden API image, got %q", status.Images.API)
	}
	if status.Images.UI != "registry.example.com/tamoss-ui:v1.2.4" {
		t.Fatalf("expected overridden UI image, got %q", status.Images.UI)
	}
	if status.Images.Console != "registry.example.com/tamoss-console-api:v1.2.5" || status.Resources.Console != "custom-console" {
		t.Fatalf("expected overridden Console status, got image=%q resource=%q", status.Images.Console, status.Resources.Console)
	}
	if status.Images.SchemaMigrationPostgresClient != "registry.example.com/postgres-client:v16.4" {
		t.Fatalf("expected overridden schema migration image, got %q", status.Images.SchemaMigrationPostgresClient)
	}
	if status.Images.CNPGPostgres != "ghcr.io/cloudnative-pg/postgresql:16.4" {
		t.Fatalf("expected overridden CNPG Postgres image, got %q", status.Images.CNPGPostgres)
	}
	if status.Images.RustFS != "registry.example.com/rustfs:v1" {
		t.Fatalf("expected overridden RustFS image, got %q", status.Images.RustFS)
	}
	if status.Images.TAMSin != "registry.example.com/tamsin@sha256:"+strings.Repeat("b", 64) {
		t.Fatalf("expected overridden TAMSin image, got %q", status.Images.TAMSin)
	}
	if status.GeneratedSecrets.APIToken != "" {
		t.Fatalf("did not expect a generated API token Secret when token is supplied inline, got %q", status.GeneratedSecrets.APIToken)
	}
	if status.GeneratedSecrets.StorageBackendCredentials != "custom-storage-backend-credentials" {
		t.Fatalf("expected fullnameOverride in generated Secret names, got %q", status.GeneratedSecrets.StorageBackendCredentials)
	}
	payload, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal resolved status: %v", err)
	}
	if strings.Contains(string(payload), "do-not-expose") {
		t.Fatalf("resolved status leaked API token: %s", string(payload))
	}
}

func TestResolvedStorageBackendStatusExposesReferencesOnly(t *testing.T) {
	storageBackend := storageBackendFixture()
	storageBackend.Spec = externalStorageBackendSpecFixture()
	storageBackend.Spec.Credentials = tamossv1alpha1.SecretReferenceSpec{
		ExistingSecret: "archive-s3-creds",
		SecretKeys: tamossv1alpha1.SecretKeySpec{
			AccessKey: "accessKeyID",
			SecretKey: "secretAccessKey",
		},
	}

	status := resolvedStorageBackendStatus(storageBackend)

	if status.Provider != tamossv1alpha1.StorageBackendProviderExternalS3 {
		t.Fatalf("expected external-s3 provider, got %q", status.Provider)
	}
	if status.BackendID != storageBackend.Spec.ID || status.BucketName != storageBackend.Spec.BucketName {
		t.Fatalf("expected backend ID and bucket in status, got %#v", status)
	}
	if status.EndpointURL != "https://s3.eu-west-2.amazonaws.com" {
		t.Fatalf("expected default endpoint URL, got %q", status.EndpointURL)
	}
	if status.PublicEndpointURL != "https://archive.s3.eu-west-2.amazonaws.com" {
		t.Fatalf("expected public endpoint URL, got %q", status.PublicEndpointURL)
	}
	if status.CredentialsSecret != "archive-s3-creds" {
		t.Fatalf("expected credentials Secret name, got %q", status.CredentialsSecret)
	}
	payload, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal storage backend resolved status: %v", err)
	}
	if strings.Contains(string(payload), "accessKeyID") || strings.Contains(string(payload), "secretAccessKey") {
		t.Fatalf("resolved status leaked Secret key names: %s", string(payload))
	}
}

func TestProviderStatusClassifiesManagedProfileDefaults(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"},
		Spec: tamossv1alpha1.TamossSpec{
			Profile: tamossv1alpha1.TamossProfileLocalKind,
		},
	}
	defaults.Apply(tamoss)

	status := providerStatus(tamoss)

	if status.DB.Provider != string(tamossv1alpha1.BackendProvidedByCNPG) ||
		status.DB.Ownership != tamossv1alpha1.ProviderOwnershipManaged {
		t.Fatalf("expected managed CNPG ownership, got %#v", status.DB)
	}
	if status.S3.Provider != string(tamossv1alpha1.S3BackendProvidedByRustFSOperator) ||
		status.S3.Ownership != tamossv1alpha1.ProviderOwnershipManaged {
		t.Fatalf("expected managed RustFS ownership, got %#v", status.S3)
	}
	if status.Auth.Provider != string(tamossv1alpha1.AuthProvidedByAuthentikBlueprints) ||
		status.Auth.Ownership != tamossv1alpha1.ProviderOwnershipManaged {
		t.Fatalf("expected managed Authentik ownership, got %#v", status.Auth)
	}
	if status.Routing.Provider != "ingress" ||
		status.Routing.Ownership != tamossv1alpha1.ProviderOwnershipManaged {
		t.Fatalf("expected managed ingress routing ownership, got %#v", status.Routing)
	}
}

func TestProviderStatusClassifiesExternalAndDisabledAuth(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"},
		Spec:       minimalTamossSpec(),
	}
	tamoss.Spec.Auth = tamossv1alpha1.AuthSpec{ProvidedBy: tamossv1alpha1.AuthProvidedByNone}
	defaults.Apply(tamoss)

	status := providerStatus(tamoss)

	if status.DB.Provider != string(tamossv1alpha1.BackendProvidedByExternal) ||
		status.DB.Ownership != tamossv1alpha1.ProviderOwnershipExternal {
		t.Fatalf("expected external PostgreSQL ownership, got %#v", status.DB)
	}
	if status.S3.Provider != string(tamossv1alpha1.S3BackendProvidedByExternal) ||
		status.S3.Ownership != tamossv1alpha1.ProviderOwnershipExternal {
		t.Fatalf("expected external S3 ownership, got %#v", status.S3)
	}
	if status.Auth.Provider != string(tamossv1alpha1.AuthProvidedByNone) ||
		status.Auth.Ownership != "" {
		t.Fatalf("expected no auth ownership claim, got %#v", status.Auth)
	}
	if status.Routing.Provider != "external" ||
		status.Routing.Ownership != tamossv1alpha1.ProviderOwnershipExternal {
		t.Fatalf("expected external routing ownership, got %#v", status.Routing)
	}
}
