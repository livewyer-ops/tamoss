package controller

import (
	"context"
	"slices"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
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

func TestDriftedSchemaJobWaitsForForegroundPodCleanupBeforeReplacement(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := recoveryTamoss()
	stale := schemaMigrationJob(tamoss, false)
	stale.UID = types.UID("stale-schema-job")
	stale.Spec.Template.Spec.Containers[0].Image = "livewyer/tamoss-api:superseded"
	controller := true
	blockOwnerDeletion := true
	stalePodLabels := schemaLabels(tamoss)
	stalePodLabels[batchv1.JobNameLabel] = stale.Name
	stalePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stale.Name + "-old",
			Namespace: stale.Namespace,
			Labels:    stalePodLabels,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         batchv1.SchemeGroupVersion.String(),
				Kind:               "Job",
				Name:               stale.Name,
				UID:                stale.UID,
				Controller:         &controller,
				BlockOwnerDeletion: &blockOwnerDeletion,
			}},
		},
	}
	var deletePropagation metav1.DeletionPropagation
	deleteObserved := false
	interceptors := fakeApplyInterceptor()
	interceptors.Delete = func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
		if _, deletingJob := obj.(*batchv1.Job); deletingJob {
			options := (&client.DeleteOptions{}).ApplyOptions(opts)
			if options.PropagationPolicy != nil {
				deletePropagation = *options.PropagationPolicy
				deleteObserved = true
			}
		}
		return c.Delete(ctx, obj, opts...)
	}
	fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptors).WithScheme(scheme).WithObjects(tamoss, stale, stalePod).Build()
	schemaController := SchemaController{Client: fakeClient, Scheme: scheme}
	jobKey := types.NamespacedName{Name: stale.Name, Namespace: stale.Namespace}

	result, err := schemaController.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected drifted schema job reconcile: %v", err)
	}
	if result.Ready || result.Reason != operatorstatus.ReasonMigrationInProgress {
		t.Fatalf("expected stale job deletion to remain in progress, got %#v", result)
	}
	if !deleteObserved || deletePropagation != metav1.DeletePropagationForeground {
		t.Fatalf("expected foreground Job deletion, got observed=%t propagation=%q", deleteObserved, deletePropagation)
	}
	if err := fakeClient.Get(ctx, jobKey, &batchv1.Job{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected fake client to remove stale Job while retaining its Pod, got %v", err)
	}

	result, err = schemaController.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected stale Pod cleanup wait: %v", err)
	}
	if result.Ready || result.Reason != operatorstatus.ReasonMigrationInProgress {
		t.Fatalf("expected stale Pod cleanup to remain in progress, got %#v", result)
	}
	if result.RequeueAfter != schemaJobCleanupRequeueAfter {
		t.Fatalf("expected stale Pod cleanup to schedule a retry, got %#v", result)
	}
	if err := fakeClient.Get(ctx, jobKey, &batchv1.Job{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected replacement Job to remain blocked while the old Pod exists, got %v", err)
	}

	if err := fakeClient.Delete(ctx, stalePod); err != nil {
		t.Fatalf("delete stale schema Pod: %v", err)
	}
	if _, err := schemaController.Reconcile(ctx, tamoss); err != nil {
		t.Fatalf("expected replacement schema Job launch: %v", err)
	}
	replacement := &batchv1.Job{}
	if err := fakeClient.Get(ctx, jobKey, replacement); err != nil {
		t.Fatalf("expected replacement schema Job: %v", err)
	}
	if got := replacement.Spec.Template.Spec.Containers[0].Image; got != schemaMigrationRuntimeImage(tamoss) {
		t.Fatalf("expected replacement Job to use desired image, got %q", got)
	}
}

func TestObsoleteVersionSchemaJobAndPodAreRemovedBeforeCurrentLaunch(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := recoveryTamoss()
	desired := schemaMigrationJob(tamoss, false)
	obsolete := desired.DeepCopy()
	obsolete.Name = tamossResourceName(tamoss, "schema-migrate-8-1-0-oss2")
	obsolete.UID = types.UID("obsolete-schema-job")
	if err := controllerutil.SetControllerReference(tamoss, obsolete, scheme); err != nil {
		t.Fatalf("set obsolete schema Job owner: %v", err)
	}
	controller := true
	labels := schemaLabels(tamoss)
	labels[batchv1.JobNameLabel] = obsolete.Name
	obsoletePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      obsolete.Name + "-old",
			Namespace: obsolete.Namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: batchv1.SchemeGroupVersion.String(),
				Kind:       "Job",
				Name:       obsolete.Name,
				UID:        obsolete.UID,
				Controller: &controller,
			}},
		},
	}
	var deletePropagation metav1.DeletionPropagation
	interceptors := fakeApplyInterceptor()
	interceptors.Delete = func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
		if _, deletingJob := obj.(*batchv1.Job); deletingJob {
			options := (&client.DeleteOptions{}).ApplyOptions(opts)
			if options.PropagationPolicy != nil {
				deletePropagation = *options.PropagationPolicy
			}
		}
		return c.Delete(ctx, obj, opts...)
	}
	fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptors).WithScheme(scheme).WithObjects(tamoss, obsolete, obsoletePod).Build()
	controllerUnderTest := SchemaController{Client: fakeClient, Scheme: scheme}
	desiredKey := client.ObjectKeyFromObject(desired)

	result, err := controllerUnderTest.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("remove obsolete schema Job: %v", err)
	}
	if result.RequeueAfter != schemaJobCleanupRequeueAfter || deletePropagation != metav1.DeletePropagationForeground {
		t.Fatalf("expected foreground obsolete Job cleanup with retry, got result=%#v propagation=%q", result, deletePropagation)
	}
	if !slices.ContainsFunc(result.ManagedObjects, func(obj client.Object) bool {
		return obj.GetName() == obsolete.Name
	}) {
		t.Fatalf("expected obsolete Job to remain managed while foreground deletion completes, got %#v", result.ManagedObjects)
	}
	if err := fakeClient.Get(ctx, desiredKey, &batchv1.Job{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected current schema Job to remain absent during obsolete Job cleanup, got %v", err)
	}

	result, err = controllerUnderTest.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("wait for obsolete schema Pod: %v", err)
	}
	if result.RequeueAfter != schemaJobCleanupRequeueAfter {
		t.Fatalf("expected obsolete Pod cleanup to schedule a retry, got %#v", result)
	}
	if err := fakeClient.Get(ctx, desiredKey, &batchv1.Job{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected current schema Job blocked by obsolete Pod, got %v", err)
	}

	if err := fakeClient.Delete(ctx, obsoletePod); err != nil {
		t.Fatalf("delete obsolete schema Pod: %v", err)
	}
	if _, err := controllerUnderTest.Reconcile(ctx, tamoss); err != nil {
		t.Fatalf("launch current schema Job: %v", err)
	}
	if err := fakeClient.Get(ctx, desiredKey, &batchv1.Job{}); err != nil {
		t.Fatalf("expected current schema Job after obsolete cleanup: %v", err)
	}
}

func TestFailureOnlySchemaStateRetriesBootstrapWithFixtures(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := recoveryTamoss()
	applyFixtures := true
	tamoss.Spec.Backends.DB.ApplyFixtures = &applyFixtures
	failed := schemaMigrationJob(tamoss, true)
	failed.UID = types.UID("failed-schema-job")
	state := schemaFailureStateConfigMap(tamoss, failed, nil, 1)
	if schemaStateHasAppliedVersion(state) {
		t.Fatalf("expected failure-only state without applied version, got %#v", state.Data)
	}
	fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(tamoss, state).Build()
	controller := SchemaController{Client: fakeClient, Scheme: scheme}

	if _, err := controller.Reconcile(ctx, tamoss); err != nil {
		t.Fatalf("expected failure-only schema state retry: %v", err)
	}
	retry := &batchv1.Job{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: failed.Name, Namespace: failed.Namespace}, retry); err != nil {
		t.Fatalf("expected retried schema Job: %v", err)
	}
	if !slices.Contains(retry.Spec.Template.Spec.Containers[0].Args, "--apply-fixtures") {
		t.Fatalf("expected bootstrap fixtures on failure-only retry, got %#v", retry.Spec.Template.Spec.Containers[0].Args)
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

func TestSucceededPinned81SchemaJobIsDeletedWithoutStamping82State(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := recoveryTamoss()
	tamoss.Spec.API.Image.Tag = "8.1.0-oss6"
	stale := schemaMigrationJob(tamoss, false)
	stale.Spec.Template.Spec.Containers[0].Args = []string{"run", "tamoss-db", "migrate"}
	stale.Status.Succeeded = 1
	client := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(tamoss, stale).Build()
	controller := SchemaController{Client: client, Scheme: scheme}

	result, err := controller.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected succeeded stale schema job reconcile: %v", err)
	}
	if result.Ready || result.Reason != operatorstatus.ReasonMigrationInProgress {
		t.Fatalf("expected stale completed job to remain untrusted, got %#v", result)
	}
	if result.RecoveryEvent == nil {
		t.Fatalf("expected recovery event for stale completed job deletion, got %#v", result)
	}
	jobKey := types.NamespacedName{Name: stale.Name, Namespace: stale.Namespace}
	if err := client.Get(ctx, jobKey, &batchv1.Job{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected stale completed schema job to be deleted, got %v", err)
	}
	stateKey := types.NamespacedName{Name: tamossResourceName(tamoss, "schema-state"), Namespace: tamoss.Namespace}
	if err := client.Get(ctx, stateKey, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected stale completed job not to stamp schema state, got %v", err)
	}

	if _, err := controller.Reconcile(ctx, tamoss); err != nil {
		t.Fatalf("expected schema job relaunch: %v", err)
	}
	relaunched := &batchv1.Job{}
	if err := client.Get(ctx, jobKey, relaunched); err != nil {
		t.Fatalf("expected relaunched schema job: %v", err)
	}
	wantArgs := []string{"run", "tamoss-db", "migrate", "--revision", schemabundle.CurrentDatabaseRevision}
	if got := relaunched.Spec.Template.Spec.Containers[0].Args; !slices.Equal(got, wantArgs) {
		t.Fatalf("expected relaunched job to target database revision %q, got %#v", schemabundle.CurrentDatabaseRevision, got)
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

func TestPinned81TerminalFailureIsReplacedAfter82ImageUpdate(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := recoveryTamoss()
	tamoss.Spec.API.Image.Tag = "8.1.0-oss6"
	stale := schemaMigrationJob(tamoss, false)
	stale.Status = failedJobFixture(stale.Name, stale.Namespace).Status
	state := terminalSchemaState(tamoss, "")

	// env:apply updates the CR after the new operator has already attempted the
	// migration with the old pinned image.
	tamoss.Spec.API.Image.Tag = "8.2.0-oss1"
	client := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(tamoss, state, stale).Build()
	controller := SchemaController{Client: client, Scheme: scheme}

	result, err := controller.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected updated image to supersede terminal failure: %v", err)
	}
	if result.Ready || result.Degraded || result.Reason != operatorstatus.ReasonMigrationInProgress {
		t.Fatalf("expected stale failed job deletion result, got %#v", result)
	}
	jobKey := types.NamespacedName{Name: stale.Name, Namespace: stale.Namespace}
	if err := client.Get(ctx, jobKey, &batchv1.Job{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected stale 8.1 schema job to be deleted, got %v", err)
	}

	if _, err := controller.Reconcile(ctx, tamoss); err != nil {
		t.Fatalf("expected updated schema job relaunch: %v", err)
	}
	relaunched := &batchv1.Job{}
	if err := client.Get(ctx, jobKey, relaunched); err != nil {
		t.Fatalf("expected updated schema job: %v", err)
	}
	if got := relaunched.Spec.Template.Spec.Containers[0].Image; got != "livewyer/tamoss-api:8.2.0-oss1" {
		t.Fatalf("expected updated API image, got %q", got)
	}
}

func TestMatchingSchemaJobPreservesTerminalFailure(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := recoveryTamoss()
	state := terminalSchemaState(tamoss, "")
	failed := schemaMigrationJob(tamoss, false)
	failed.Status = failedJobFixture(failed.Name, failed.Namespace).Status
	client := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(tamoss, state, failed).Build()
	controller := SchemaController{Client: client, Scheme: scheme}

	result, err := controller.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected matching terminal failure reconcile: %v", err)
	}
	if !result.Degraded || result.Reason != operatorstatus.ReasonSchemaMigrationFailed {
		t.Fatalf("expected matching terminal failure to be preserved, got %#v", result)
	}
	if err := client.Get(ctx, types.NamespacedName{Name: failed.Name, Namespace: failed.Namespace}, &batchv1.Job{}); err != nil {
		t.Fatalf("expected matching terminally failed schema job to remain, got %v", err)
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
