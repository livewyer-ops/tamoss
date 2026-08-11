package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/backend/rustfs"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func TestDefaultStorageBackendUsesTamossS3Connection(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"},
		Spec: tamossv1alpha1.TamossSpec{
			Backends: tamossv1alpha1.BackendsSpec{
				S3: tamossv1alpha1.S3BackendSpec{
					ProvidedBy: tamossv1alpha1.S3BackendProvidedByRustFSOperator,
					Tags: map[string]apiextensionsv1.JSON{
						"access": {Raw: []byte(`["archive","programme"]`)},
						"tier":   {Raw: []byte(`"hot"`)},
					},
					RustFSOperator: &tamossv1alpha1.S3RustFSOperatorSpec{
						PublicEndpoint: tamossv1alpha1.S3PublicEndpointSpec{URL: "https://s3.example.test"},
						Bucket:         tamossv1alpha1.S3RustFSOperatorBucketSpec{Name: "media"},
					},
				},
			},
		},
	}

	storageBackend := defaultStorageBackend(tamoss)

	if storageBackend.Name != "example-storage-default" {
		t.Fatalf("expected default storage backend name, got %q", storageBackend.Name)
	}
	if storageBackend.Spec.ID != tamossv1alpha1.DefaultStorageBackendID {
		t.Fatalf("expected default backend ID %q, got %q", tamossv1alpha1.DefaultStorageBackendID, storageBackend.Spec.ID)
	}
	if storageBackend.Spec.BucketName != "media" {
		t.Fatalf("expected bucket media, got %q", storageBackend.Spec.BucketName)
	}
	if storageBackend.Spec.Endpoint.Public.URL != "https://s3.example.test" {
		t.Fatalf("expected public endpoint to flow through, got %q", storageBackend.Spec.Endpoint.Public.URL)
	}
	if storageBackend.Spec.Endpoint.Default.URL != "http://example-s3.media.svc:9000" {
		t.Fatalf("expected namespaced default endpoint, got %q", storageBackend.Spec.Endpoint.Default.URL)
	}
	if storageBackend.Spec.Credentials.ExistingSecret != "example-s3-creds" {
		t.Fatalf("expected generated RustFS credentials secret, got %q", storageBackend.Spec.Credentials.ExistingSecret)
	}
	if got := string(storageBackend.Spec.Tags["access"].Raw); got != `["archive","programme"]` {
		t.Fatalf("expected default backend tags to flow through, got %#v", storageBackend.Spec.Tags)
	}
	if got := string(storageBackend.Spec.Tags["tier"].Raw); got != `"hot"` {
		t.Fatalf("expected scalar default backend tag to flow through, got %#v", storageBackend.Spec.Tags)
	}
}

func TestManagedBucketCORSUsesExactPublicUIOrigin(t *testing.T) {
	tamoss := tamossFixture()
	tamoss.Spec.PublicEndpoint.UIURL = "https://app.example.test:30443/"
	tamoss.Spec.Ingress.UI.Web.Host = "app.example.test"
	tamoss.Spec.Ingress.TLS = []networkingv1.IngressTLS{{}}
	tamoss.Spec.API.CORS.AllowedOrigins = []string{
		"https://app.example.test:30443",
		"https://tools.example.test",
	}

	want := []string{
		"https://app.example.test:30443",
		"https://app.example.test",
		"https://tools.example.test",
	}
	if got := storageBackendCORSOrigins(tamoss); !slices.Equal(got, want) {
		t.Fatalf("managed bucket CORS origins = %#v, want %#v", got, want)
	}
}

func TestStorageBackendSchemaStateRequiresCurrentVersion(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := tamossFixture()
	reconciler := StorageBackendReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(
			tamoss,
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "example-schema-state", Namespace: "media"},
				Data:       map[string]string{schemaStateAppliedVersionKey: "0.0.0"},
			},
		).Build(),
		Scheme: scheme,
	}

	if reconciler.schemaStateReady(ctx, tamoss) {
		t.Fatal("expected stale schema state to block StorageBackend database registration")
	}

	state := &corev1.ConfigMap{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: "example-schema-state", Namespace: "media"}, state); err != nil {
		t.Fatalf("expected schema state: %v", err)
	}
	state.Data[schemaStateAppliedVersionKey] = schemabundle.SchemaVersion
	if err := reconciler.Client.Update(ctx, state); err != nil {
		t.Fatalf("update schema state: %v", err)
	}
	if !reconciler.schemaStateReady(ctx, tamoss) {
		t.Fatal("expected current schema state to allow StorageBackend database registration")
	}
}

func envValue(env []corev1.EnvVar, name string) string {
	for _, item := range env {
		if item.Name == name {
			return item.Value
		}
	}
	return ""
}

func TestStorageBackendRegistrationJobUsesPostgresAndTAMSMetadata(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"},
		Spec: tamossv1alpha1.TamossSpec{
			Images: tamossv1alpha1.ComponentImagesSpec{
				SchemaMigrationPostgresClient: "postgres:test",
			},
			Backends: tamossv1alpha1.BackendsSpec{
				DB: tamossv1alpha1.DBBackendSpec{ProvidedBy: tamossv1alpha1.BackendProvidedByCNPG},
			},
		},
	}
	storageBackend := &tamossv1alpha1.StorageBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "archive", Namespace: "media"},
		Spec: tamossv1alpha1.StorageBackendSpec{
			TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "example"},
		},
	}
	spec := tamossv1alpha1.StorageBackendSpec{
		ID:             "33333333-3333-4333-8333-333333333333",
		TamossRef:      tamossv1alpha1.TamossReferenceSpec{Name: "example"},
		Provider:       tamossv1alpha1.StorageBackendProviderRustFS,
		Label:          "tamoss.us-east-1:s3:archive",
		Region:         "us-east-1",
		StoreProduct:   "s3",
		StoreType:      "http_object_store",
		BucketName:     "archive",
		Endpoint:       tamossv1alpha1.S3EndpointSpec{Default: tamossv1alpha1.EndpointURLSpec{URL: "http://example-s3:9000"}},
		DefaultStorage: false,
		Tags: map[string]apiextensionsv1.JSON{
			"access": {Raw: []byte(`["archive","programme"]`)},
			"tier":   {Raw: []byte(`"cold"`)},
		},
	}

	job := storageBackendRegistrationJob(storageBackend, tamoss, spec, "desired")
	container := job.Spec.Template.Spec.Containers[0]

	if job.Name != "archive-db-register" {
		t.Fatalf("expected db registration job name, got %q", job.Name)
	}
	if container.Image != "postgres:test" {
		t.Fatalf("expected configured Postgres helper image, got %q", container.Image)
	}
	if envValue(container.Env, "POSTGRES_HOST") != "example-db-rw" {
		t.Fatalf("expected CNPG app host in env, got %#v", container.Env)
	}
	if envValue(container.Env, "TAMOSS_STORAGE_BACKEND_ID") != spec.ID {
		t.Fatalf("expected backend ID env, got %#v", container.Env)
	}
	if got := envValue(container.Env, "TAMOSS_STORAGE_BACKEND_TAGS"); got != `{"access":["archive","programme"],"tier":"cold"}` {
		t.Fatalf("expected deterministic backend tags env, got %q", got)
	}
	script := container.Command[2]
	for _, expected := range []string{
		"INSERT INTO tamoss_storage_backends",
		"ON CONFLICT (id) DO UPDATE",
		"jsonb_build_object",
		"'tags', :'tags'::jsonb",
		"tags = EXCLUDED.tags",
		"record = jsonb_set(record, '{default_storage}', 'false'::jsonb, true)",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("expected registration script to contain %q, got %s", expected, script)
		}
	}
}

func TestStorageBackendRegistrationHashIncludesTags(t *testing.T) {
	spec := storageBackendSpecFixture()
	withoutTags := storageBackendRegistrationHash(spec)
	spec.Tags = map[string]apiextensionsv1.JSON{"access": {Raw: []byte(`["programme"]`)}}
	withTags := storageBackendRegistrationHash(spec)

	if withoutTags == withTags {
		t.Fatal("expected storage backend tags to change the registration hash")
	}
	spec.Tags = map[string]apiextensionsv1.JSON{"access": {Raw: []byte(`"programme"`)}}
	withScalarTag := storageBackendRegistrationHash(spec)
	if withTags == withScalarTag {
		t.Fatal("expected scalar and singleton-array tags to retain distinct hash semantics")
	}
	if got := storageBackendTagsJSON(map[string]apiextensionsv1.JSON{
		"z": {Raw: []byte(`["last"]`)},
		"a": {Raw: []byte(`"first"`)},
	}); got != `{"a":"first","z":["last"]}` {
		t.Fatalf("expected stable sorted tag JSON, got %q", got)
	}
	if got := storageBackendTagsJSON(map[string]apiextensionsv1.JSON{
		"access": {Raw: []byte(`[ "archive", "programme" ]`)},
	}); got != `{"access":["archive","programme"]}` {
		t.Fatalf("expected canonical tag JSON independent of source whitespace, got %q", got)
	}
}

func TestStorageBackendInvalidTagsDegradeBeforeProvisioning(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	storageBackend := storageBackendFixture()
	storageBackend.Finalizers = []string{storageBackendFinalizer}
	storageBackend.Spec.Tags = map[string]apiextensionsv1.JSON{
		"invalid": {Raw: []byte(`7`)},
	}
	recorder := record.NewFakeRecorder(10)
	reconciler := StorageBackendReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.StorageBackend{}).
			WithObjects(storageBackend).
			Build(),
		Scheme:   scheme,
		Recorder: recorder,
	}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: storageBackend.Name, Namespace: storageBackend.Namespace}})
	if err != nil {
		t.Fatalf("expected invalid tags to update status without a reconcile error: %v", err)
	}
	updated := &tamossv1alpha1.StorageBackend{}
	if err := reconciler.Client.Get(ctx, client.ObjectKeyFromObject(storageBackend), updated); err != nil {
		t.Fatalf("get updated StorageBackend: %v", err)
	}
	ready := findCondition(t, updated.Status.Conditions, operatorstatus.ConditionReady)
	if ready.Status != metav1.ConditionFalse || ready.Reason != operatorstatus.ReasonInvalidStorageBackendTags {
		t.Fatalf("expected invalid tags to degrade StorageBackend, got %#v", ready)
	}
	if updated.Status.Phase != operatorstatus.PhaseDegraded {
		t.Fatalf("expected degraded phase, got %q", updated.Status.Phase)
	}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: storageBackendResourceName(storageBackend, "bucket-init"), Namespace: storageBackend.Namespace}, &batchv1.Job{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected invalid tags to block provisioning, got %v", err)
	}
	event := <-recorder.Events
	if !strings.Contains(event, operatorstatus.ReasonInvalidStorageBackendTags) {
		t.Fatalf("expected invalid tags warning event, got %q", event)
	}
}

func TestStorageBackendDeregistrationJobUsesPostgres(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"},
		Spec: tamossv1alpha1.TamossSpec{
			Images: tamossv1alpha1.ComponentImagesSpec{
				SchemaMigrationPostgresClient: "postgres:test",
			},
			Backends: tamossv1alpha1.BackendsSpec{
				DB: tamossv1alpha1.DBBackendSpec{ProvidedBy: tamossv1alpha1.BackendProvidedByCNPG},
			},
		},
	}
	storageBackend := &tamossv1alpha1.StorageBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "archive", Namespace: "media"},
		Spec: tamossv1alpha1.StorageBackendSpec{
			TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "example"},
		},
	}
	spec := storageBackendSpecFixture()

	job := storageBackendDeregistrationJob(storageBackend, tamoss, spec, "desired")
	container := job.Spec.Template.Spec.Containers[0]

	if job.Name != "archive-db-deregister" {
		t.Fatalf("expected db deregistration job name, got %q", job.Name)
	}
	if container.Image != "postgres:test" {
		t.Fatalf("expected configured Postgres helper image, got %q", container.Image)
	}
	if envValue(container.Env, "POSTGRES_HOST") != "example-db-rw" {
		t.Fatalf("expected CNPG app host in env, got %#v", container.Env)
	}
	if envValue(container.Env, "TAMOSS_STORAGE_BACKEND_ID") != spec.ID {
		t.Fatalf("expected backend ID env, got %#v", container.Env)
	}
	script := container.Command[2]
	for _, expected := range []string{
		"to_regclass('public.tamoss_storage_backends')",
		"DELETE FROM tamoss_storage_backends",
		"WHERE id = :'backend_id'::uuid",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("expected deregistration script to contain %q, got %s", expected, script)
		}
	}
}

func TestStorageBackendRuntimeCredentialsSecretRendersReferencedSecrets(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := tamossFixture()
	storageBackend := storageBackendFixture()
	external := storageBackendFixture()
	external.Name = "external"
	external.Spec = externalStorageBackendSpecFixture()
	external.Spec.ID = "44444444-4444-4444-8444-444444444444"
	external.Spec.Credentials = tamossv1alpha1.SecretReferenceSpec{
		ExistingSecret: "external-creds",
		SecretKeys: tamossv1alpha1.SecretKeySpec{
			AccessKey: "accessKeyID",
			SecretKey: "secretAccessKey",
		},
	}
	defaultCreds := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "archive-s3", Namespace: "media"},
		Data: map[string][]byte{
			"RUSTFS_ACCESS_KEY": []byte("rustfs-access"),
			"RUSTFS_SECRET_KEY": []byte("rustfs-secret"),
		},
	}
	externalCreds := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "external-creds", Namespace: "media"},
		Data: map[string][]byte{
			"accessKeyID":     []byte("external-access"),
			"secretAccessKey": []byte("external-secret"),
		},
	}
	client := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
		WithScheme(scheme).
		WithObjects(tamoss, storageBackend, external, defaultCreds, externalCreds).
		Build()

	secret, err := storageBackendRuntimeCredentialsSecret(ctx, client, tamoss)
	if err != nil {
		t.Fatalf("expected runtime credentials secret, got error %v", err)
	}

	var payload struct {
		Credentials []struct {
			StorageBackendID string `json:"storageBackendId"`
			AccessKey        string `json:"accessKey"`
			SecretKey        string `json:"secretKey"`
		} `json:"credentials"`
	}
	if err := json.Unmarshal(secret.Data["credentials.json"], &payload); err != nil {
		t.Fatalf("expected valid credentials JSON: %v", err)
	}
	if len(payload.Credentials) != 2 {
		t.Fatalf("expected two credential entries, got %#v", payload.Credentials)
	}
	if string(secret.Data["credentials.json"]) != "" &&
		strings.Contains(string(secret.Data["credentials.json"]), "endpoint") {
		t.Fatalf("runtime credentials should not include backend metadata: %s", string(secret.Data["credentials.json"]))
	}
	if payload.Credentials[0].StorageBackendID == "" ||
		payload.Credentials[0].AccessKey == "" ||
		payload.Credentials[0].SecretKey == "" {
		t.Fatalf("expected populated credentials, got %#v", payload.Credentials)
	}
}

func TestStorageBackendRuntimeCredentialsSecretSkipsHibernateDestinations(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := tamossFixture()
	media := storageBackendFixture()
	hibernate := storageBackendFixture()
	hibernate.Name = "hibernate"
	hibernate.Spec = externalStorageBackendSpecFixture()
	hibernate.Spec.Usage = tamossv1alpha1.StorageBackendUsageHibernate
	hibernate.Spec.ID = "55555555-5555-4555-8555-555555555555"
	hibernate.Spec.Credentials = tamossv1alpha1.SecretReferenceSpec{
		ExistingSecret: "hibernate-creds",
		SecretKeys: tamossv1alpha1.SecretKeySpec{
			AccessKey: "accessKeyID",
			SecretKey: "secretAccessKey",
		},
	}
	mediaCreds := storageBackendCredentialsSecret(media)
	hibernateCreds := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "hibernate-creds", Namespace: "media"},
		Data: map[string][]byte{
			"accessKeyID":     []byte("hibernate-access"),
			"secretAccessKey": []byte("hibernate-secret"),
		},
	}
	client := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
		WithScheme(scheme).
		WithObjects(tamoss, media, hibernate, mediaCreds, hibernateCreds).
		Build()

	secret, err := storageBackendRuntimeCredentialsSecret(ctx, client, tamoss)
	if err != nil {
		t.Fatalf("expected runtime credentials secret, got error %v", err)
	}

	var payload struct {
		Credentials []struct {
			StorageBackendID string `json:"storageBackendId"`
		} `json:"credentials"`
	}
	if err := json.Unmarshal(secret.Data["credentials.json"], &payload); err != nil {
		t.Fatalf("expected valid credentials JSON: %v", err)
	}
	if len(payload.Credentials) != 1 || payload.Credentials[0].StorageBackendID != media.Spec.ID {
		t.Fatalf("expected only media credentials, got %#v", payload.Credentials)
	}
}

func TestStorageBackendEventsAreEmittedAndDeduped(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	reconciler := &StorageBackendReconciler{Recorder: recorder}
	original := &tamossv1alpha1.StorageBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "archive", Namespace: "media"},
	}
	updated := original.DeepCopy()
	operatorstatus.SetConditionBool(&updated.Status.Conditions, updated.Generation, operatorstatus.ConditionReady, false, operatorstatus.ReasonMissingSecret, "Required credentials Secret archive-s3 was not found")

	reconciler.recordStorageBackendEvents(original, updated)
	reconciler.recordStorageBackendEvents(original, updated)

	if got := len(recorder.Events); got != 1 {
		t.Fatalf("expected one deduped warning event, got %d", got)
	}
	event := <-recorder.Events
	if !strings.Contains(event, operatorstatus.ReasonMissingSecret) {
		t.Fatalf("expected MissingSecret event, got %q", event)
	}
}

func TestStorageBackendBucketCreatedEvent(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	reconciler := &StorageBackendReconciler{Recorder: recorder}
	original := &tamossv1alpha1.StorageBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "archive", Namespace: "media"},
	}
	updated := original.DeepCopy()
	operatorstatus.SetConditionBool(&updated.Status.Conditions, updated.Generation, operatorstatus.ConditionBucketReady, true, operatorstatus.ReasonBucketReady, "StorageBackend bucket is ready")

	reconciler.recordStorageBackendEvents(original, updated)

	if got := len(recorder.Events); got != 1 {
		t.Fatalf("expected one bucket-created event, got %d", got)
	}
	event := <-recorder.Events
	if !strings.Contains(event, operatorstatus.ReasonStorageBackendBucketCreated) {
		t.Fatalf("expected bucket-created event, got %q", event)
	}
}

func TestHibernateStorageBackendSkipsDatabaseRegistration(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := tamossFixture()
	storageBackend := storageBackendFixture()
	storageBackend.Finalizers = []string{storageBackendFinalizer}
	storageBackend.Spec = externalStorageBackendSpecFixture()
	storageBackend.Spec.Usage = tamossv1alpha1.StorageBackendUsageHibernate
	storageBackend.Spec.Credentials = tamossv1alpha1.SecretReferenceSpec{
		ExistingSecret: "hibernate-creds",
		SecretKeys: tamossv1alpha1.SecretKeySpec{
			AccessKey: "accessKeyID",
			SecretKey: "secretAccessKey",
		},
	}
	creds := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "hibernate-creds", Namespace: "media"},
		Data: map[string][]byte{
			"accessKeyID":     []byte("hibernate-access"),
			"secretAccessKey": []byte("hibernate-secret"),
		},
	}
	reconciler := StorageBackendReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.StorageBackend{}).
			WithObjects(tamoss, storageBackend, creds).
			Build(),
		Scheme: scheme,
	}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: storageBackend.Name, Namespace: storageBackend.Namespace}})
	if err != nil {
		t.Fatalf("expected hibernate StorageBackend reconcile to succeed, got error %v", err)
	}
	updated := &tamossv1alpha1.StorageBackend{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: storageBackend.Name, Namespace: storageBackend.Namespace}, updated); err != nil {
		t.Fatalf("get updated StorageBackend: %v", err)
	}
	ready := findCondition(t, updated.Status.Conditions, operatorstatus.ConditionReady)
	if ready.Status != metav1.ConditionTrue || ready.Reason != operatorstatus.ReasonStorageBackendReady {
		t.Fatalf("expected ready hibernate destination, got %#v", ready)
	}
	database := findCondition(t, updated.Status.Conditions, operatorstatus.ConditionDatabaseReady)
	if database.Status != metav1.ConditionTrue || database.Reason != operatorstatus.ReasonDatabaseRegistrationSkipped {
		t.Fatalf("expected skipped database registration, got %#v", database)
	}
	job := &batchv1.Job{}
	err = reconciler.Client.Get(ctx, types.NamespacedName{Name: storageBackendResourceName(storageBackend, "db-register"), Namespace: storageBackend.Namespace}, job)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected no database registration Job, got %v", err)
	}
	secret := &corev1.Secret{}
	err = reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.ResourceName("storage-backend-credentials"), Namespace: tamoss.Namespace}, secret)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected no runtime credentials Secret, got %v", err)
	}
}

func TestStorageBackendRuntimeCredentialsSecretOmitsMissingReferencedSecret(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := tamossFixture()
	storageBackend := storageBackendFixture()
	external := storageBackendFixture()
	external.Name = "external"
	external.Spec = externalStorageBackendSpecFixture()
	external.Spec.ID = "44444444-4444-4444-8444-444444444444"
	external.Spec.Credentials.ExistingSecret = "missing-creds"
	defaultCreds := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "archive-s3", Namespace: "media"},
		Data: map[string][]byte{
			"RUSTFS_ACCESS_KEY": []byte("rustfs-access"),
			"RUSTFS_SECRET_KEY": []byte("rustfs-secret"),
		},
	}
	client := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
		WithScheme(scheme).
		WithObjects(tamoss, storageBackend, external, defaultCreds).
		Build()

	secret, err := storageBackendRuntimeCredentialsSecret(ctx, client, tamoss)
	if err != nil {
		t.Fatalf("expected runtime credentials secret, got error %v", err)
	}

	var payload struct {
		Credentials []struct {
			StorageBackendID string `json:"storageBackendId"`
		} `json:"credentials"`
	}
	if err := json.Unmarshal(secret.Data["credentials.json"], &payload); err != nil {
		t.Fatalf("expected valid credentials JSON: %v", err)
	}
	if len(payload.Credentials) != 1 || payload.Credentials[0].StorageBackendID != storageBackend.Spec.ID {
		t.Fatalf("expected only the available credentials, got %#v", payload.Credentials)
	}
}

func TestStorageBackendCredentialSecretWatchMapsToReferencingBackends(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	storageBackend := storageBackendFixture()
	reconciler := StorageBackendReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(storageBackend).WithIndex(&tamossv1alpha1.StorageBackend{}, storageBackendCredentialsSecretIndex, storageBackendCredentialsSecretIndexValue).Build(),
		Scheme: scheme,
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "archive-s3", Namespace: "media"}}

	requests := reconciler.storageBackendCredentialSecretRequests(ctx, secret)

	if len(requests) != 1 || requests[0].Name != "archive" || requests[0].Namespace != "media" {
		t.Fatalf("expected archive StorageBackend reconcile request, got %#v", requests)
	}
}

func TestStorageBackendCredentialSecretWatchIgnoresUnreferencedSecrets(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	storageBackend := storageBackendFixture()
	reconciler := StorageBackendReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(storageBackend).WithIndex(&tamossv1alpha1.StorageBackend{}, storageBackendCredentialsSecretIndex, storageBackendCredentialsSecretIndexValue).Build(),
		Scheme: scheme,
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unreferenced", Namespace: "media"}}

	requests := reconciler.storageBackendCredentialSecretRequests(ctx, secret)

	if len(requests) != 0 {
		t.Fatalf("expected unreferenced Secret to be ignored, got %#v", requests)
	}
}

func TestStorageBackendBucketUsesNativeClientAndDoesNotCreateJob(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	storageBackend := storageBackendFixture()
	bucketClient := &fakeBucketClient{}
	reconciler := StorageBackendReconciler{
		Client:       fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(storageBackend, storageBackendCredentialsSecret(storageBackend)).Build(),
		Scheme:       scheme,
		BucketClient: bucketClient,
	}

	result, err := reconciler.reconcileStorageBackendBucket(ctx, storageBackend, tamossFixture(), storageBackendSpecFixture())
	if err != nil {
		t.Fatalf("expected native bucket reconcile to succeed, got error %v", err)
	}
	if !result.Ready || result.Reason != operatorstatus.ReasonBucketReady {
		t.Fatalf("unexpected native bucket result: %#v", result)
	}
	if bucketClient.ensureCalls != 1 {
		t.Fatalf("expected one native bucket ensure call, got %d", bucketClient.ensureCalls)
	}
	err = reconciler.Client.Get(ctx, types.NamespacedName{Name: storageBackendResourceName(storageBackend, "bucket-init"), Namespace: storageBackend.Namespace}, &batchv1.Job{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected no bucket init Job, got %v", err)
	}
}

func TestStorageBackendBucketNativeFailureSurfacesStatusAndEvent(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	storageBackend := storageBackendFixture()
	storageBackend.Finalizers = []string{storageBackendFinalizer}
	recorder := record.NewFakeRecorder(10)
	reconciler := StorageBackendReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.StorageBackend{}).
			WithObjects(storageBackend, tamossFixture(), storageBackendCredentialsSecret(storageBackend)).
			Build(),
		Scheme:       scheme,
		Recorder:     recorder,
		BucketClient: &fakeBucketClient{ensureErr: fmt.Errorf("dial tcp: connection refused")},
	}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: storageBackend.Name, Namespace: storageBackend.Namespace}})
	if err != nil {
		t.Fatalf("expected native bucket failure to update status, got error %v", err)
	}
	updated := &tamossv1alpha1.StorageBackend{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: storageBackend.Name, Namespace: storageBackend.Namespace}, updated); err != nil {
		t.Fatalf("get updated StorageBackend: %v", err)
	}
	ready := findCondition(t, updated.Status.Conditions, operatorstatus.ConditionReady)
	if ready.Status != metav1.ConditionFalse || ready.Reason != operatorstatus.ReasonBucketCreationFailed {
		t.Fatalf("expected bucket creation failure condition, got %#v", ready)
	}
	event := <-recorder.Events
	if !strings.Contains(event, operatorstatus.ReasonBucketCreationFailed) {
		t.Fatalf("expected bucket creation failure event, got %q", event)
	}
}

func TestExternalStorageBackendReferenceCreatesReusableState(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	storageBackend := storageBackendFixture()
	spec := externalStorageBackendSpecFixture()
	storageBackend.Spec = spec
	state := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "archive-bucket-state", Namespace: "media"}}
	reconciler := StorageBackendReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(storageBackend, state).Build(),
		Scheme: scheme,
	}

	result, err := reconciler.reconcileStorageBackendBucket(ctx, storageBackend, tamossFixture(), spec)
	if err != nil {
		t.Fatalf("expected external storage reference to reconcile, got %v", err)
	}
	if !result.Ready || result.Reason != operatorstatus.ReasonBucketReady {
		t.Fatalf("unexpected result: %#v", result)
	}

	state = &corev1.ConfigMap{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: "archive-bucket-state", Namespace: "media"}, state); err != nil {
		t.Fatalf("expected bucket state ConfigMap: %v", err)
	}
	hash := storageBackendExternalBucketHash(spec)
	if !storageBackendBucketReady(state, spec, hash) {
		t.Fatalf("expected state to mark external storage metadata ready, got %#v", state.Data)
	}

	result, err = reconciler.reconcileStorageBackendBucket(ctx, storageBackend, tamossFixture(), spec)
	if err != nil {
		t.Fatalf("expected cached external storage state to succeed, got %v", err)
	}
	if !result.Ready {
		t.Fatalf("expected cached state ready result, got %#v", result)
	}
}

func TestFinalizeExternalStorageBackendSkipsBucketDeletion(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	storageBackend := storageBackendFixture()
	spec := externalStorageBackendSpecFixture()
	storageBackend.Spec = spec
	now := metav1.Now()
	storageBackend.Finalizers = []string{storageBackendFinalizer}
	storageBackend.DeletionTimestamp = &now

	dbDeregister := succeededJobFixture("archive-db-deregister", "media")
	dbDeregister.Labels = map[string]string{"tamoss.livewyer.io/storage-backend": "archive"}
	dbDeregister.Annotations = map[string]string{
		storageBackendDesiredHashAnnotation: storageBackendDeregistrationHash(spec),
	}
	schemaState := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "example-schema-state", Namespace: "media"},
		Data:       map[string]string{schemaStateAppliedVersionKey: schemabundle.SchemaVersion},
	}
	reconciler := StorageBackendReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(
			storageBackend,
			tamossFixture(),
			dbDeregister,
			schemaState,
		).Build(),
		Scheme: scheme,
	}

	result, err := reconciler.finalizeStorageBackend(ctx, storageBackend)
	if err != nil {
		t.Fatalf("expected external S3 finalization to complete, got %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("did not expect requeue after completed cleanup: %#v", result)
	}
	updated := &tamossv1alpha1.StorageBackend{}
	err = reconciler.Client.Get(ctx, types.NamespacedName{Name: "archive", Namespace: "media"}, updated)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("expected StorageBackend to be deleted or have its finalizer removed: %v", err)
	}
	if err == nil && len(updated.Finalizers) != 0 {
		t.Fatalf("expected finalizer removed, got %#v", updated.Finalizers)
	}
}

func TestStorageBackendDatabaseRegistrationRetriesFailedJob(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	storageBackend := storageBackendFixture()
	failedJob := failedJobFixture(storageBackendResourceName(storageBackend, "db-register"), storageBackend.Namespace)
	reconciler := StorageBackendReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(storageBackend, failedJob).Build(),
		Scheme: scheme,
	}

	result, err := reconciler.reconcileStorageBackendDatabase(ctx, storageBackend, tamossFixture(), storageBackendSpecFixture())
	if err != nil {
		t.Fatalf("expected reconcile to retry failed database registration job, got error %v", err)
	}
	if result.Ready || result.Degraded || result.Reason != operatorstatus.ReasonDatabaseRegistrationRetrying {
		t.Fatalf("unexpected retry result: %#v", result)
	}
	err = reconciler.Client.Get(ctx, types.NamespacedName{Name: failedJob.Name, Namespace: failedJob.Namespace}, &batchv1.Job{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected failed database registration job to be deleted, got %v", err)
	}
}

func TestFinalizeStorageBackendRunsBucketAndDatabaseCleanup(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	storageBackend := storageBackendFixture()
	now := metav1.Now()
	storageBackend.Finalizers = []string{storageBackendFinalizer}
	storageBackend.DeletionTimestamp = &now
	spec := storageBackend.Spec
	spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)

	tamoss := tamossFixture()
	dbDeregister := succeededJobFixture("archive-db-deregister", "media")
	dbDeregister.Labels = map[string]string{"tamoss.livewyer.io/storage-backend": "archive"}
	dbDeregister.Annotations = map[string]string{
		storageBackendDesiredHashAnnotation: storageBackendDeregistrationHash(spec),
	}
	bucketState := storageBackendBucketStateConfigMap(storageBackend, spec, storageBackendBucketHash(rustfsBucketTarget(tamoss, spec)))
	dbState := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "archive-db-state", Namespace: "media"}}
	cleanupPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "archive-bucket-delete-pod",
			Namespace: "media",
			Labels: map[string]string{
				"tamoss.livewyer.io/storage-backend": "archive",
			},
		},
	}
	schemaState := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "example-schema-state", Namespace: "media"},
		Data:       map[string]string{schemaStateAppliedVersionKey: schemabundle.SchemaVersion},
	}
	reconciler := StorageBackendReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(
			storageBackend,
			tamoss,
			dbDeregister,
			bucketState,
			dbState,
			cleanupPod,
			schemaState,
			storageBackendCredentialsSecret(storageBackend),
		).Build(),
		Scheme:       scheme,
		BucketClient: &fakeBucketClient{},
	}

	result, err := reconciler.finalizeStorageBackend(ctx, storageBackend)
	if err != nil {
		t.Fatalf("expected finalization to complete, got error %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("did not expect requeue after completed cleanup: %#v", result)
	}
	updated := &tamossv1alpha1.StorageBackend{}
	err = reconciler.Client.Get(ctx, types.NamespacedName{Name: "archive", Namespace: "media"}, updated)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("expected StorageBackend to be deleted or have its finalizer removed: %v", err)
	}
	if err == nil && len(updated.Finalizers) != 0 {
		t.Fatalf("expected finalizer removed, got %#v", updated.Finalizers)
	}
	for _, key := range []types.NamespacedName{
		{Name: "archive-db-deregister", Namespace: "media"},
		{Name: "archive-bucket-state", Namespace: "media"},
		{Name: "archive-db-state", Namespace: "media"},
	} {
		if strings.Contains(key.Name, "state") {
			err := reconciler.Client.Get(ctx, key, &corev1.ConfigMap{})
			if !apierrors.IsNotFound(err) {
				t.Fatalf("expected cleanup configmap %s to be deleted, got %v", key.Name, err)
			}
			continue
		}
		err := reconciler.Client.Get(ctx, key, &batchv1.Job{})
		if !apierrors.IsNotFound(err) {
			t.Fatalf("expected cleanup job %s to be deleted, got %v", key.Name, err)
		}
	}
	err = reconciler.Client.Get(ctx, types.NamespacedName{Name: cleanupPod.Name, Namespace: cleanupPod.Namespace}, &corev1.Pod{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected cleanup pod to be deleted, got %v", err)
	}
}

func TestFinalizeStorageBackendSkipsBucketDeletionWithoutBucketState(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	storageBackend := storageBackendFixture()
	now := metav1.Now()
	storageBackend.Finalizers = []string{storageBackendFinalizer}
	storageBackend.DeletionTimestamp = &now
	spec := storageBackend.Spec
	spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)

	dbDeregister := succeededJobFixture("archive-db-deregister", "media")
	dbDeregister.Labels = map[string]string{"tamoss.livewyer.io/storage-backend": "archive"}
	dbDeregister.Annotations = map[string]string{
		storageBackendDesiredHashAnnotation: storageBackendDeregistrationHash(spec),
	}
	schemaState := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "example-schema-state", Namespace: "media"},
		Data:       map[string]string{schemaStateAppliedVersionKey: schemabundle.SchemaVersion},
	}
	reconciler := StorageBackendReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(
			storageBackend,
			tamossFixture(),
			dbDeregister,
			schemaState,
		).Build(),
		Scheme: scheme,
	}

	result, err := reconciler.finalizeStorageBackend(ctx, storageBackend)
	if err != nil {
		t.Fatalf("expected finalization to skip bucket deletion without state, got error %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("did not expect requeue after skipped bucket cleanup: %#v", result)
	}
	bucketDelete := &batchv1.Job{}
	err = reconciler.Client.Get(ctx, types.NamespacedName{Name: "archive-bucket-delete", Namespace: "media"}, bucketDelete)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected no bucket delete job without bucket state, got %v", err)
	}
}

func TestFinalizeStorageBackendSkipsRuntimeCredentialsWhenTamossDeleting(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	storageBackend := storageBackendFixture()
	storageBackend.Spec = externalStorageBackendSpecFixture()
	now := metav1.Now()
	storageBackend.Finalizers = []string{storageBackendFinalizer}
	storageBackend.DeletionTimestamp = &now
	tamoss := tamossFixture()
	tamoss.Finalizers = []string{tamossFinalizer}
	tamoss.DeletionTimestamp = &now
	spec := storageBackend.Spec
	spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
	dbDeregister := succeededJobFixture("archive-db-deregister", "media")
	dbDeregister.Labels = map[string]string{"tamoss.livewyer.io/storage-backend": "archive"}
	dbDeregister.Annotations = map[string]string{
		storageBackendDesiredHashAnnotation: storageBackendDeregistrationHash(spec),
	}
	schemaState := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "example-schema-state", Namespace: "media"},
		Data:       map[string]string{schemaStateAppliedVersionKey: schemabundle.SchemaVersion},
	}
	reconciler := StorageBackendReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(
			storageBackend,
			tamoss,
			dbDeregister,
			schemaState,
		).Build(),
		Scheme: scheme,
	}

	result, err := reconciler.finalizeStorageBackend(ctx, storageBackend)
	if err != nil {
		t.Fatalf("expected finalization to skip runtime credentials while Tamoss is deleting, got error %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("did not expect requeue after completed cleanup: %#v", result)
	}
	err = reconciler.Client.Get(ctx, types.NamespacedName{Name: "example-storage-backend-credentials", Namespace: "media"}, &corev1.Secret{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected no runtime credentials secret refresh during parent deletion, got %v", err)
	}
}

func storageBackendFixture() *tamossv1alpha1.StorageBackend {
	return &tamossv1alpha1.StorageBackend{
		TypeMeta: metav1.TypeMeta{
			APIVersion: tamossv1alpha1.GroupVersion.String(),
			Kind:       "StorageBackend",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "archive",
			Namespace: "media",
			UID:       "storage-backend-uid",
		},
		Spec: storageBackendSpecFixture(),
	}
}

func tamossFixture() *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{
		TypeMeta: metav1.TypeMeta{
			APIVersion: tamossv1alpha1.GroupVersion.String(),
			Kind:       "Tamoss",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"},
	}
}

func storageBackendSpecFixture() tamossv1alpha1.StorageBackendSpec {
	return tamossv1alpha1.StorageBackendSpec{
		ID:         "33333333-3333-4333-8333-333333333333",
		TamossRef:  tamossv1alpha1.TamossReferenceSpec{Name: "example"},
		Provider:   tamossv1alpha1.StorageBackendProviderRustFS,
		BucketName: "archive",
		Endpoint: tamossv1alpha1.S3EndpointSpec{
			Default: tamossv1alpha1.EndpointURLSpec{URL: "http://example-s3:9000"},
		},
		Credentials: tamossv1alpha1.SecretReferenceSpec{
			ExistingSecret: "archive-s3",
		},
	}
}

func storageBackendCredentialsSecret(storageBackend *tamossv1alpha1.StorageBackend) *corev1.Secret {
	spec := storageBackend.Spec
	spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: spec.Credentials.ExistingSecret, Namespace: storageBackend.Namespace},
		Data: map[string][]byte{
			storageBackendAccessKey(spec): []byte("rustfs-access"),
			storageBackendSecretKey(spec): []byte("rustfs-secret"),
		},
	}
}

func externalStorageBackendSpecFixture() tamossv1alpha1.StorageBackendSpec {
	spec := storageBackendSpecFixture()
	spec.Provider = tamossv1alpha1.StorageBackendProviderExternalS3
	spec.Region = "eu-west-2"
	spec.Endpoint = tamossv1alpha1.S3EndpointSpec{
		Default: tamossv1alpha1.EndpointURLSpec{URL: "https://s3.eu-west-2.amazonaws.com"},
		Public:  tamossv1alpha1.EndpointURLSpec{URL: "https://archive.s3.eu-west-2.amazonaws.com"},
	}
	return spec
}

func failedJobFixture(name, namespace string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Status: batchv1.JobStatus{
			Failed: 1,
			Conditions: []batchv1.JobCondition{{
				Type:   batchv1.JobFailed,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}

type fakeBucketClient struct {
	ensureCalls       int
	deleteCalls       int
	deletePrefixCalls int
	ensureErr         error
	deleteErr         error
	deletePrefixErr   error
}

func (f *fakeBucketClient) Ensure(_ context.Context, _ rustfs.BucketTarget, _ rustfs.BucketCredentials) error {
	f.ensureCalls++
	return f.ensureErr
}

func (f *fakeBucketClient) Delete(_ context.Context, _ rustfs.BucketTarget, _ rustfs.BucketCredentials) error {
	f.deleteCalls++
	return f.deleteErr
}

func (f *fakeBucketClient) DeletePrefix(_ context.Context, _ rustfs.BucketTarget, _ rustfs.BucketCredentials, _ string) (rustfs.DeletePrefixResult, error) {
	f.deletePrefixCalls++
	return rustfs.DeletePrefixResult{}, f.deletePrefixErr
}

func succeededJobFixture(name, namespace string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Status: batchv1.JobStatus{
			Succeeded: 1,
			Conditions: []batchv1.JobCondition{{
				Type:   batchv1.JobComplete,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}

func storageBackendTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add batchv1 scheme: %v", err)
	}
	if err := tamossv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add tamoss scheme: %v", err)
	}
	return scheme
}
