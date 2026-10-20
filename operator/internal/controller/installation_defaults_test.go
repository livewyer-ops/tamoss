package controller

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/workload_renderer"
)

func installationConfig(t *testing.T, data string) *defaults.Installation {
	t.Helper()
	path := filepath.Join(t.TempDir(), "defaults.yaml")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return defaults.LoadInstallation(path)
}

func TestInstallationDefaultsAndOverrides(t *testing.T) {
	catalogue := selectionCatalogue(t)
	config := installationConfig(t, "profile: single-server\nbaseDomain: example.com\nclusterIssuer: site-issuer\nconsoleEnabled: true\nauthentik:\n  platformNamespace: identity\n  internalURL: http://authentik.identity.svc:9000\n")
	instance := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Name: "media", Namespace: "team"}, Spec: tamossv1alpha1.TamossSpec{Version: "dev"}}
	original := instance.DeepCopy()
	resolved, err := resolveTamoss(instance, catalogue, config)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(instance, original) {
		t.Fatal("resolving installation defaults mutated the stored resource")
	}
	if resolved.Spec.Profile != tamossv1alpha1.TamossProfileSingleServer || resolved.Spec.Ingress.API.Host != "api.media.team.example.com" || resolved.Spec.PublicEndpoint.TLSSecretName != "media-tls" || !resolved.Spec.ConsoleEnabled() {
		t.Fatalf("unexpected default instance: %+v", resolved.Spec)
	}
	if auth := resolved.Spec.Auth.AuthentikBlueprints; auth.IssuerURL != "https://auth.example.com" || auth.PlatformNamespace != "identity" || auth.InternalURL != "http://authentik.identity.svc:9000" {
		t.Fatalf("instance did not use shared authentication: %+v", auth)
	}
	neighbour := instance.DeepCopy()
	neighbour.Namespace = "other"
	other, err := resolveTamoss(neighbour, catalogue, config)
	if err != nil || other.Spec.Ingress.API.Host == resolved.Spec.Ingress.API.Host {
		t.Fatal("namespace identity was lost in the public hostname")
	}
	instance.Spec.Console.Enabled = ptr.To(false)
	instance.Spec.Ingress.Enabled = ptr.To(false)
	instance.Spec.PublicEndpoint.BaseDomain = "old.example.com"
	instance.Spec.PublicEndpoint.TLSSecretName = "existing-tls"
	instance.Spec.FullnameOverride = "existing"
	instance.Spec.API.Image.Tag = "custom"
	instance.Spec.Auth.ProvidedBy = tamossv1alpha1.AuthProvidedByNone
	instance.Spec.Backends.DB.External = &tamossv1alpha1.DBExternalSpec{Host: "external-db"}
	instance.Spec.Backends.S3.External = &tamossv1alpha1.S3ExternalSpec{Bucket: "external-bucket"}
	resolved, err = resolveTamoss(instance, catalogue, config)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Spec.ConsoleEnabled() || resolved.Spec.Ingress.IsEnabled() || resolved.Spec.PublicEndpoint.BaseDomain != "old.example.com" || resolved.Spec.PublicEndpoint.TLSSecretName != "existing-tls" || resolved.ResourceName("api") != "existing-api" || resolved.Spec.API.Image.Tag != "custom" || resolved.Spec.Auth.Provider() != tamossv1alpha1.AuthProvidedByNone || resolved.Spec.Backends.DB.Provider() != tamossv1alpha1.BackendProvidedByExternal || resolved.Spec.Backends.S3.Provider() != tamossv1alpha1.S3BackendProvidedByExternal {
		t.Fatal("explicit instance settings were replaced")
	}
	instance.Spec.Console.Enabled = nil
	resolved, err = resolveTamoss(instance, catalogue, config)
	if err != nil || !resolved.Spec.ConsoleEnabled() {
		t.Fatal("removing an override did not restore inheritance")
	}
}

func TestInstallationDefaultsValidation(t *testing.T) {
	instance := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Name: "media", Namespace: "team"}, Spec: tamossv1alpha1.TamossSpec{Version: "dev"}}
	catalogue := selectionCatalogue(t)
	for _, data := range []string{"version: dev", "images: {}", "profile: unknown", "baseDomain: https://example.com", "consoleEnabled: invalid", "authentik:\n  issuerURL: https://user:password@example.com", "profile: edge\nprofile: single-server"} {
		_, err := resolveTamoss(instance, catalogue, installationConfig(t, data))
		if releaseErrorReason(err) != "InvalidInstallationDefaults" {
			t.Fatalf("invalid installation was not blocked: %q, %v", data, err)
		}
	}
	if _, err := resolveTamoss(instance, catalogue, nil); releaseErrorReason(err) != "InstallationDefaultsRequired" {
		t.Fatalf("version-only instance without defaults: %v", err)
	}
	instance.Spec.Profile = tamossv1alpha1.TamossProfileEdge
	if _, err := resolveTamoss(instance, catalogue, installationConfig(t, "{}")); err != nil {
		t.Fatal("explicit installation no longer works:", err)
	}
}

func TestInstallationDefaultsWaitForRollout(t *testing.T) {
	config := installationConfig(t, "profile: edge\nbaseDomain: example.com")
	instance := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Name: "media", Namespace: "team"}, Spec: tamossv1alpha1.TamossSpec{Version: "dev"}}
	resolved, err := resolveTamoss(instance, selectionCatalogue(t), config)
	if err != nil {
		t.Fatal(err)
	}
	r := &TamossReconciler{InstanceDefaults: config, Scheme: storageBackendTestScheme(t)}
	if err := appsv1.AddToScheme(r.Scheme); err != nil {
		t.Fatal(err)
	}
	var objects []client.Object
	for _, object := range workload_renderer.Render(resolved) {
		if d, ok := object.(*appsv1.Deployment); ok {
			d.Generation = 1
			d.Status = appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: *d.Spec.Replicas, UpdatedReplicas: *d.Spec.Replicas, AvailableReplicas: *d.Spec.Replicas}
			objects = append(objects, d)
		}
	}
	r.Client = fake.NewClientBuilder().WithScheme(r.Scheme).WithObjects(objects...).Build()
	if r.workloadRolloutsReady(context.Background(), resolved) || config.AppliedTo(resolved) {
		t.Fatal("previous rollout was accepted for new installation defaults")
	}
	for _, object := range objects {
		r.stampInstallationDefaults(object)
		if err := r.Client.Update(context.Background(), object); err != nil {
			t.Fatal(err)
		}
	}
	if !r.workloadRolloutsReady(context.Background(), resolved) {
		t.Fatal("completed defaults rollout was not accepted")
	}
	resolved.Status.AppliedDefaultsRevision = config.Revision()
	if !config.AppliedTo(resolved) || config.Status().Source == "" {
		t.Fatal("applied configuration was not recorded")
	}
	changed := installationConfig(t, "profile: edge\nbaseDomain: changed.example.com")
	if changed.AppliedTo(resolved) || !strings.HasPrefix(changed.Revision(), "sha256:") {
		t.Fatal("changed defaults did not invalidate readiness")
	}
}

func TestInstallationDefaultsGateDependentWork(t *testing.T) {
	ctx := context.Background()
	config := installationConfig(t, "profile: local-kind\nbaseDomain: example.com")
	catalogue := selectionCatalogue(t)
	instance := testIngestTamoss()
	state := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: instance.ResourceName("schema-state"), Namespace: instance.Namespace}, Data: map[string]string{schemaStateAppliedVersionKey: catalogue[instance.Spec.Version].Schema.Version}}
	c := fake.NewClientBuilder().WithScheme(hibernateTestScheme(t)).WithObjects(state).Build()
	profiles := &FlowProfileReconciler{Client: c, Releases: catalogue, InstanceDefaults: config}
	storage := &StorageBackendReconciler{Client: c, Releases: catalogue, InstanceDefaults: config}
	if !tamossReadyForIngest(instance, nil) || !storage.schemaStateReady(ctx, instance) {
		t.Fatal("fixture must have a ready schema and workload")
	}
	if tamossReadyForIngest(instance, config) || profiles.flowProfileSchemaReady(ctx, instance) {
		t.Fatal("new work accepted a previous defaults rollout")
	}
	if !storage.schemaStateReady(ctx, instance) {
		t.Fatal("storage registration must remain available to finish bootstrap")
	}
	instance.Status.AppliedDefaultsRevision = config.Revision()
	if !tamossReadyForIngest(instance, config) || !profiles.flowProfileSchemaReady(ctx, instance) {
		t.Fatal("completed defaults rollout did not admit new work")
	}
}
