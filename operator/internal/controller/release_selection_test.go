package controller

import (
	"context"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/workload_renderer"
	"github.com/livewyer-ops/tamoss/operator/internal/releases"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func selectionCatalogue(t *testing.T) releases.Catalogue {
	t.Helper()
	catalogue, err := releases.Load("../../config/releases/catalogue.json")
	if err != nil {
		t.Fatal(err)
	}
	return catalogue
}

func TestReleaseSelectionIsPerInstanceAndDoesNotPersistDefaults(t *testing.T) {
	ctx := context.Background()
	catalogue := selectionCatalogue(t)
	old := catalogue["8.2.0-oss1"]
	next := catalogue["dev"]
	first := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Name: "first", Namespace: "media", UID: "first"}, Spec: tamossv1alpha1.TamossSpec{Version: old.Version, Profile: tamossv1alpha1.TamossProfileEdge}, Status: tamossv1alpha1.TamossStatus{CurrentVersion: old.Version}}
	second := first.DeepCopy()
	second.Name = "second"
	second.UID = "second"
	raw := first.DeepCopy()
	// Adding a new release to an operator must leave the original render and schema unchanged.
	before, err := resolveTamoss(first, releases.Catalogue{old.Version: old}, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := resolveTamoss(first, catalogue, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Spec, after.Spec) || !reflect.DeepEqual(first.Spec, raw.Spec) {
		t.Fatal("operator update changed the pin or persisted defaults")
	}
	if resolvedImageRef(after.Spec.API.Image, "") != old.Images.API {
		t.Fatal("published API digest was not preserved")
	}
	for _, object := range workload_renderer.Render(after) {
		if deployment, ok := object.(*appsv1.Deployment); ok && (strings.HasSuffix(deployment.Name, "-api") || strings.HasSuffix(deployment.Name, "-worker")) {
			if deployment.Spec.Template.Spec.Containers[0].Image != old.Images.API {
				t.Fatalf("wrong rendered digest: %s", deployment.Name)
			}
		}
	}
	scheme := storageBackendTestScheme(t)
	firstState := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: first.ResourceName("schema-state"), Namespace: first.Namespace}, Data: map[string]string{schemaStateAppliedVersionKey: old.Schema.Version}}
	secondState := firstState.DeepCopy()
	secondState.Name = second.ResourceName("schema-state")
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(fakeApplyInterceptor()).WithObjects(first, second, firstState, secondState).Build()
	first.Spec.Version = next.Version
	upgraded, err := resolveTamoss(first, catalogue, nil)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.Spec.Backends.S3.RustFSOperator.Image != next.Images.RustFS {
		t.Fatal("RustFS did not follow release selection")
	}
	migration := &SchemaController{Client: c, Scheme: scheme, Target: next.Schema}
	result, err := migration.Reconcile(ctx, upgraded)
	if err != nil || result.Ready {
		t.Fatalf("expected new migration: %#v, %v", result, err)
	}
	job := &batchv1.Job{}
	desired := migration.schemaMigrationJob(upgraded, false)
	if err := c.Get(ctx, client.ObjectKeyFromObject(desired), job); err != nil {
		t.Fatal(err)
	}
	if job.Spec.Template.Spec.Containers[0].Image != next.Images.API || job.Spec.Template.Spec.Containers[0].Args[4] != next.Schema.DatabaseRevision {
		t.Fatal("migration did not use the selected runtime and revision")
	}
	unchanged, err := resolveTamoss(second, catalogue, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err = (&SchemaController{Client: c, Scheme: scheme, Target: old.Schema}).Reconcile(ctx, unchanged)
	if err != nil || !result.Ready || resolvedImageRef(unchanged.Spec.API.Image, "") != old.Images.API {
		t.Fatal("upgrading one instance changed its neighbour")
	}
}

func TestReleaseOverridesAndRemoval(t *testing.T) {
	catalogue := selectionCatalogue(t)
	instance := &tamossv1alpha1.Tamoss{Spec: tamossv1alpha1.TamossSpec{Version: "dev", Profile: tamossv1alpha1.TamossProfileEdge}}
	instance.Spec.API.Image.Repository = "mirror.example/api"
	instance.Spec.API.Image.Tag = "custom"
	instance.Spec.Images.TAMSin = "mirror.example/ingest@sha256:" + strings.Repeat("f", 64)
	instance.Spec.Backends.DB.CNPG = &tamossv1alpha1.DBCNPGSpec{PostgresVersion: "18"}
	instance.Spec.Backends.S3.RustFSOperator = &tamossv1alpha1.S3RustFSOperatorSpec{Image: "rustfs/rustfs:pinned"}
	resolved, err := resolveTamoss(instance, catalogue, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Spec.API.Image.Tag != "custom" || resolved.Spec.Images.TAMSin != instance.Spec.Images.TAMSin || resolved.Spec.Backends.DB.CNPG.PostgresVersion != "18" || resolved.Spec.Backends.S3.RustFSOperator.Image != "rustfs/rustfs:pinned" {
		t.Fatal("explicit or previously defaulted pin was overwritten")
	}
	instance.Spec.Backends.S3.RustFSOperator.Image = ""
	instance.Spec.Backends.DB.CNPG.PostgresVersion = ""
	resolved, err = resolveTamoss(instance, catalogue, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Spec.Backends.DB.CNPG.PostgresVersion != catalogue["dev"].Images.CNPGPostgresVersion || resolved.Spec.Backends.S3.RustFSOperator.Image != catalogue["dev"].Images.RustFS {
		t.Fatal("removed overrides did not follow release defaults")
	}
}

func TestVersionGateLeavesExistingWorkloadsUntouched(t *testing.T) {
	for _, tc := range []struct{ version, current, observed, reason, defaults string }{
		{"", "", "8.2.0-oss1", "VersionRequired", ""},
		{"unknown", "", "8.2.0-oss1", "UnsupportedVersion", ""},
		{"8.2.0-oss1", "8.2.0-oss2-rc2", "8.2.0-oss1", "UnsupportedUpgrade", ""},
		{"dev", "", "8.2.0-oss1", "VersionAdoptionRequired", ""},
		{"8.2.0-oss1", "8.2.0-oss1", "future", operatorstatus.ReasonUnsupportedSchemaVersion, ""},
		{"dev", "dev", "", "InvalidInstallationDefaults", "profile: invalid"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			ctx := context.Background()
			scheme := hibernateTestScheme(t)
			instance := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "media", UID: "old", Generation: 2}, Spec: tamossv1alpha1.TamossSpec{Version: tc.version, Profile: tamossv1alpha1.TamossProfileEdge}, Status: tamossv1alpha1.TamossStatus{CurrentVersion: tc.current}}
			deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "old-api", Namespace: "media"}, Spec: appsv1.DeploymentSpec{Replicas: ptr.To(int32(1))}}
			state := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "old-schema-state", Namespace: "media"}, Data: map[string]string{schemaStateAppliedVersionKey: tc.observed}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(instance).WithObjects(instance, deployment, state).Build()
			r := &TamossReconciler{Client: c, Scheme: scheme, Releases: selectionCatalogue(t), InstanceDefaults: installationConfig(t, tc.defaults)}
			if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(instance)}); err != nil {
				t.Fatal(err)
			}
			actual := &appsv1.Deployment{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(deployment), actual); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual.Spec, deployment.Spec) {
				t.Fatal("blocked version mutated workload")
			}
			if err := c.Get(ctx, client.ObjectKeyFromObject(instance), instance); err != nil {
				t.Fatal(err)
			}
			ready := meta.FindStatusCondition(instance.Status.Conditions, operatorstatus.ConditionReady)
			if ready == nil || ready.Reason != tc.reason || instance.Status.CurrentVersion != tc.current {
				t.Fatalf("wrong blocked status: %#v", instance.Status)
			}
			jobs := &batchv1.JobList{}
			if err := c.List(ctx, jobs); err != nil {
				t.Fatal(err)
			}
			if len(jobs.Items) > 0 {
				t.Fatal("blocked version started a Job")
			}
		})
	}
}

func TestReleaseCompletionWaitsForRequestedWorkloadGeneration(t *testing.T) {
	ctx := context.Background()
	instance := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media"}, Spec: tamossv1alpha1.TamossSpec{Version: "dev"}}
	instance.Spec.API.Enabled = ptr.To(true)
	instance.Spec.UI.Enabled = ptr.To(false)
	instance.Spec.API.Image = tamossv1alpha1.ImageSpec{Repository: "api", Tag: "next"}
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "example-api", Namespace: "media", Generation: 2}, Spec: appsv1.DeploymentSpec{Replicas: ptr.To(int32(1)), Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Image: "api:next"}}}}}, Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}}
	c := fake.NewClientBuilder().WithScheme(hibernateTestScheme(t)).WithStatusSubresource(deployment).WithObjects(deployment).Build()
	r := &TamossReconciler{Client: c}
	if r.workloadRolloutsReady(ctx, instance) {
		t.Fatal("stale Ready status completed upgrade")
	}
	deployment.Status.ObservedGeneration = 2
	if err := c.Status().Update(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	if !r.workloadRolloutsReady(ctx, instance) {
		t.Fatal("completed rollout did not become ready")
	}
	instance.Spec.API.Image.Tag = "later"
	if r.workloadRolloutsReady(ctx, instance) {
		t.Fatal("cached previous image completed upgrade")
	}
}

func TestReleaseSelectionCannotChangeAfterBackendWorkBegins(t *testing.T) {
	ctx := context.Background()
	catalogue := selectionCatalogue(t)
	raw := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Name: "instance", Namespace: "media"}, Spec: tamossv1alpha1.TamossSpec{Version: "dev", Profile: tamossv1alpha1.TamossProfileEdge}, Status: tamossv1alpha1.TamossStatus{CurrentVersion: "8.2.0-oss1", SchemaVersion: "8.2.0-oss1"}}
	c := fake.NewClientBuilder().WithScheme(hibernateTestScheme(t)).WithStatusSubresource(raw).WithObjects(raw).Build()
	r := &TamossReconciler{Client: c, Releases: catalogue}
	resolved, err := resolveTamoss(raw, catalogue, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.beginRelease(ctx, resolved); err != nil {
		t.Fatal(err)
	}
	if resolved.Spec.Backends.S3.RustFSOperator.Image != catalogue["dev"].Images.RustFS {
		t.Fatal("status response discarded calculated defaults")
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(raw), raw); err != nil {
		t.Fatal(err)
	}
	if raw.Status.CurrentVersion != "8.2.0-oss1" || raw.Status.SchemaVersion != "8.2.0-oss1" || raw.Status.Upgrade.TargetVersion != "dev" {
		t.Fatal("starting an upgrade changed the applied version")
	}
	raw.Spec.Version = "8.2.0-oss1"
	if _, err := resolveTamoss(raw, catalogue, nil); releaseErrorReason(err) != "UpgradeInProgress" {
		t.Fatalf("unfinished upgrade accepted a rollback: %v", err)
	}
	if err := r.validateReleaseState(ctx, raw); releaseErrorReason(err) != "UpgradeInProgress" {
		t.Fatalf("reconciliation did not block an unfinished upgrade: %v", err)
	}
	raw.Spec.Version = "dev"
	resolved, err = resolveTamoss(raw, catalogue, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.patchTamossStatusObservation(ctx, resolved, tamossStatusObservation{
		Ready: boolCondition(true, "Ready", "Ready"), Schema: &SchemaResult{Ready: true},
	}); err != nil {
		t.Fatal(err)
	}
	if resolved.Status.CurrentVersion != "dev" || resolved.Status.Upgrade.TargetVersion != "" {
		t.Fatal("completed upgrade did not record its version")
	}
}

func TestDependentControllersUseTheInstanceSchemaTarget(t *testing.T) {
	ctx := context.Background()
	catalogue := selectionCatalogue(t)
	c := fake.NewClientBuilder().WithScheme(hibernateTestScheme(t)).Build()
	storage := &StorageBackendReconciler{Client: c, Releases: catalogue}
	profiles := &FlowProfileReconciler{Client: c, Releases: catalogue}
	for _, version := range []string{"8.2.0-oss1", "8.2.0-oss2-rc2", "dev"} {
		instance := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Name: version, Namespace: "media"}, Spec: tamossv1alpha1.TamossSpec{Version: version}, Status: tamossv1alpha1.TamossStatus{CurrentVersion: version}}
		state := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: instance.ResourceName("schema-state"), Namespace: instance.Namespace}, Data: map[string]string{schemaStateAppliedVersionKey: catalogue[version].Schema.Version}}
		if err := c.Create(ctx, state); err != nil {
			t.Fatal(err)
		}
		if !storage.schemaStateReady(ctx, instance) || !profiles.flowProfileSchemaReady(ctx, instance) {
			t.Fatalf("dependent controllers rejected the selected schema for %s", version)
		}
		if version != "dev" {
			instance.Spec.Version = "dev"
			if storage.schemaStateReady(ctx, instance) || profiles.flowProfileSchemaReady(ctx, instance) || tamossReadyForIngest(instance, nil) {
				t.Fatal("dependent controller admitted work before the selected upgrade")
			}
		}
	}
}
