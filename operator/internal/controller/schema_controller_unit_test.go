package controller

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func TestSchemaJobWithDriftedTemplateIsDeletedAndRecreated(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := recoveryTamoss()
	stale := schemaMigrationJob(tamoss, false)
	stale.Spec.Template.Spec.Containers[0].Image = "livewyer/tamoss-api:superseded"
	client := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(tamoss, stale).Build()
	controller := SchemaController{Client: client, Scheme: scheme}

	result, err := controller.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected drifted schema job reconcile: %v", err)
	}
	if result.Ready || result.Reason != operatorstatus.ReasonMigrationInProgress {
		t.Fatalf("expected in-progress result after stale job deletion, got %#v", result)
	}
	if result.RecoveryEvent == nil {
		t.Fatalf("expected recovery event for stale job deletion, got %#v", result)
	}
	err = client.Get(ctx, types.NamespacedName{Name: stale.Name, Namespace: stale.Namespace}, &batchv1.Job{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected stale schema job to be deleted, got %v", err)
	}

	if _, err := controller.Reconcile(ctx, tamoss); err != nil {
		t.Fatalf("expected schema job relaunch: %v", err)
	}
	relaunched := &batchv1.Job{}
	if err := client.Get(ctx, types.NamespacedName{Name: stale.Name, Namespace: stale.Namespace}, relaunched); err != nil {
		t.Fatalf("expected relaunched schema job: %v", err)
	}
	if got := relaunched.Spec.Template.Spec.Containers[0].Image; got != schemaMigrationRuntimeImage(tamoss) {
		t.Fatalf("expected relaunched job to use the desired image, got %q", got)
	}
}

func TestSchemaJobFailedWithDriftedTemplateIsRecreated(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := recoveryTamoss()
	stale := schemaMigrationJob(tamoss, false)
	stale.Spec.Template.Spec.Containers[0].Image = "livewyer/tamoss-api:superseded"
	stale.Status = failedJobFixture(stale.Name, stale.Namespace).Status
	client := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(tamoss, stale).Build()
	controller := SchemaController{Client: client, Scheme: scheme}

	result, err := controller.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected failed drifted schema job reconcile: %v", err)
	}
	if result.Ready || result.Degraded || result.Reason != operatorstatus.ReasonMigrationInProgress {
		t.Fatalf("expected stale failed job deletion result, got %#v", result)
	}
	err = client.Get(ctx, types.NamespacedName{Name: stale.Name, Namespace: stale.Namespace}, &batchv1.Job{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected stale failed schema job to be deleted, got %v", err)
	}
}

func TestSchemaJobWithMatchingTemplateIsLeftRunning(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := recoveryTamoss()
	running := schemaMigrationJob(tamoss, false)
	client := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(tamoss, running).Build()
	controller := SchemaController{Client: client, Scheme: scheme}

	result, err := controller.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected running schema job reconcile: %v", err)
	}
	if result.Ready || result.Reason != operatorstatus.ReasonMigrationInProgress {
		t.Fatalf("expected running job result, got %#v", result)
	}
	if err := client.Get(ctx, types.NamespacedName{Name: running.Name, Namespace: running.Namespace}, &batchv1.Job{}); err != nil {
		t.Fatalf("expected matching schema job to remain, got %v", err)
	}
}

func TestSchemaJobDriftDoesNotOverrideTerminalFailure(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := recoveryTamoss()
	state := terminalSchemaState(tamoss, "")
	stale := schemaMigrationJob(tamoss, false)
	stale.Spec.Template.Spec.Containers[0].Image = "livewyer/tamoss-api:superseded"
	stale.Status = failedJobFixture(stale.Name, stale.Namespace).Status
	client := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(tamoss, state, stale).Build()
	controller := SchemaController{Client: client, Scheme: scheme}

	result, err := controller.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected terminal failure reconcile: %v", err)
	}
	if !result.Degraded || result.Reason != operatorstatus.ReasonSchemaMigrationFailed {
		t.Fatalf("expected terminal failure to be preserved, got %#v", result)
	}
	if err := client.Get(ctx, types.NamespacedName{Name: stale.Name, Namespace: stale.Namespace}, &batchv1.Job{}); err != nil {
		t.Fatalf("expected terminally failed schema job to remain, got %v", err)
	}
}

func TestSchemaJobTemplateDriftedComparesRenderedFields(t *testing.T) {
	tamoss := recoveryTamoss()
	desired := schemaMigrationJob(tamoss, false)

	same := desired.DeepCopy()
	if schemaJobTemplateDrifted(same, desired) {
		t.Fatal("expected identical templates to not be drifted")
	}

	defaulted := desired.DeepCopy()
	defaulted.Spec.Template.Spec.Containers[0].TerminationMessagePath = "/dev/termination-log"
	if schemaJobTemplateDrifted(defaulted, desired) {
		t.Fatal("expected server-defaulted fields to be ignored")
	}

	changedArgs := desired.DeepCopy()
	changedArgs.Spec.Template.Spec.Containers[0].Args = append(changedArgs.Spec.Template.Spec.Containers[0].Args, "--apply-fixtures")
	changedEnv := desired.DeepCopy()
	changedEnv.Spec.Template.Spec.Containers[0].Env = append(changedEnv.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "EXTRA", Value: "1"})
	changedImage := desired.DeepCopy()
	changedImage.Spec.Template.Spec.Containers[0].Image = "livewyer/tamoss-api:superseded"
	for name, live := range map[string]*batchv1.Job{
		"args":  changedArgs,
		"env":   changedEnv,
		"image": changedImage,
	} {
		if !schemaJobTemplateDrifted(live, desired) {
			t.Fatalf("expected %s change to be reported as drift", name)
		}
	}
}
