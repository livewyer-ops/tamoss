package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func TestSchemaRetryClearsTerminalFailureAndDeletesFailedJob(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := recoveryTamoss()
	tamoss.Annotations = map[string]string{AnnotationSchemaRetry: "retry-1"}
	state := terminalSchemaState(tamoss, "")
	job := failedJobFixture(tamossResourceName(tamoss, "schema-migrate-"+schemaVersionForName()), tamoss.Namespace)
	client := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(tamoss, state, job).Build()
	controller := SchemaController{Client: client, Scheme: scheme}

	result, err := controller.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected schema retry to reconcile: %v", err)
	}
	if result.Ready || result.Reason != operatorstatus.ReasonSchemaRetryAccepted {
		t.Fatalf("expected retry accepted result, got %#v", result)
	}
	if result.RecoveryEvent == nil || result.RecoveryEvent.Reason != operatorstatus.ReasonSchemaRetryAccepted {
		t.Fatalf("expected retry accepted event, got %#v", result.RecoveryEvent)
	}
	updatedState := &corev1.ConfigMap{}
	if err := client.Get(ctx, types.NamespacedName{Name: state.Name, Namespace: state.Namespace}, updatedState); err != nil {
		t.Fatalf("expected schema state: %v", err)
	}
	if _, found := updatedState.Data[schemaStateFailureCountKey]; found {
		t.Fatalf("expected failure count cleared, got %#v", updatedState.Data)
	}
	if updatedState.Annotations[annotationSchemaRetryDone] != "retry-1" {
		t.Fatalf("expected consumed retry annotation, got %#v", updatedState.Annotations)
	}
	err = client.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, &batchv1.Job{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected failed schema job to be deleted, got %v", err)
	}
}

func TestSchemaRetryDuplicateAnnotationDoesNotResetAgain(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := recoveryTamoss()
	tamoss.Annotations = map[string]string{AnnotationSchemaRetry: "retry-1"}
	state := terminalSchemaState(tamoss, "retry-1")
	job := schemaMigrationJob(tamoss, false)
	job.Status = failedJobFixture(job.Name, job.Namespace).Status
	client := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(tamoss, state, job).Build()
	controller := SchemaController{Client: client, Scheme: scheme}

	result, err := controller.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected duplicate retry reconcile: %v", err)
	}
	if !result.Degraded || result.Reason != operatorstatus.ReasonSchemaMigrationFailed {
		t.Fatalf("expected duplicate retry to preserve terminal failure, got %#v", result)
	}
	if err := client.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, &batchv1.Job{}); err != nil {
		t.Fatalf("expected failed schema job to remain, got %v", err)
	}
}

func TestSchemaRetryRepeatedFailureStartsNewAttemptCount(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := recoveryTamoss()
	tamoss.Annotations = map[string]string{AnnotationSchemaRetry: "retry-1"}
	state := terminalSchemaState(tamoss, "")
	jobName := tamossResourceName(tamoss, "schema-migrate-"+schemaVersionForName())
	failed := failedJobFixture(jobName, tamoss.Namespace)
	client := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(tamoss, state, failed).Build()
	controller := SchemaController{Client: client, Scheme: scheme}

	if _, err := controller.Reconcile(ctx, tamoss); err != nil {
		t.Fatalf("expected retry reset: %v", err)
	}
	if _, err := controller.Reconcile(ctx, tamoss); err != nil {
		t.Fatalf("expected retry job launch: %v", err)
	}
	retryJob := &batchv1.Job{}
	if err := client.Get(ctx, types.NamespacedName{Name: jobName, Namespace: tamoss.Namespace}, retryJob); err != nil {
		t.Fatalf("expected retry job: %v", err)
	}
	retryJob.Status.Failed = 1
	retryJob.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
	if err := client.Status().Update(ctx, retryJob); err != nil {
		t.Fatalf("mark retry job failed: %v", err)
	}
	result, err := controller.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected retry failure reconcile: %v", err)
	}
	if result.SchemaMigration.Attempts != 1 {
		t.Fatalf("expected retry attempt count to restart at 1, got %#v", result.SchemaMigration)
	}
	updatedState := &corev1.ConfigMap{}
	if err := client.Get(ctx, types.NamespacedName{Name: tamossResourceName(tamoss, "schema-state"), Namespace: tamoss.Namespace}, updatedState); err != nil {
		t.Fatalf("expected updated schema state: %v", err)
	}
	if updatedState.Annotations[annotationSchemaRetryDone] != "retry-1" {
		t.Fatalf("expected consumed retry marker to survive failure state, got %#v", updatedState.Annotations)
	}
}

func TestSchemaRetryConsumedMarkerSurvivesSuccessState(t *testing.T) {
	ctx := context.Background()
	scheme := storageBackendTestScheme(t)
	tamoss := recoveryTamoss()
	state := terminalSchemaState(tamoss, "retry-1")
	succeeded := schemaMigrationJob(tamoss, false)
	succeeded.Status = succeededJobFixture(succeeded.Name, succeeded.Namespace).Status
	client := fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(scheme).WithObjects(tamoss, state, succeeded).Build()
	controller := SchemaController{Client: client, Scheme: scheme}

	result, err := controller.Reconcile(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected successful schema reconcile: %v", err)
	}
	if !result.Ready {
		t.Fatalf("expected schema ready, got %#v", result)
	}
	updatedState := &corev1.ConfigMap{}
	if err := client.Get(ctx, types.NamespacedName{Name: tamossResourceName(tamoss, "schema-state"), Namespace: tamoss.Namespace}, updatedState); err != nil {
		t.Fatalf("expected updated schema state: %v", err)
	}
	if updatedState.Annotations[annotationSchemaRetryDone] != "retry-1" {
		t.Fatalf("expected consumed retry marker to survive success state, got %#v", updatedState.Annotations)
	}
}

func TestGeneratedAPITokenRotationReplacesTokenAndAnnotatesRollout(t *testing.T) {
	ctx := context.Background()
	tamoss := recoveryTamoss()
	tamoss.Spec.Secrets.APIToken.Generate = true
	tamoss.Annotations = map[string]string{AnnotationAPITokenRotate: "rotate-1"}
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: tamossResourceName(tamoss, "api-token"), Namespace: tamoss.Namespace},
		Data:       map[string][]byte{apiTokenKey: []byte("old-token")},
	}
	recorder := record.NewFakeRecorder(10)
	reconciler := &TamossReconciler{
		Client:   fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(storageBackendTestScheme(t)).WithObjects(existing).Build(),
		Recorder: recorder,
	}
	secret, apiDeployment, uiDeployment := apiTokenObjects(tamoss)

	if err := reconciler.prepareAPITokenSecret(ctx, tamoss, []client.Object{secret, apiDeployment, uiDeployment}); err != nil {
		t.Fatalf("expected token preparation: %v", err)
	}
	if string(secret.Data[apiTokenKey]) == "old-token" {
		t.Fatal("expected generated token to be rotated")
	}
	if secret.Annotations[annotationAPITokenDone] != "rotate-1" {
		t.Fatalf("expected rotation consumed annotation, got %#v", secret.Annotations)
	}
	if apiDeployment.Spec.Template.Annotations["checksum/api-token-secret"] == "" {
		t.Fatal("expected API checksum annotation")
	}
	if uiDeployment.Spec.Template.Annotations["checksum/api-token-secret"] != "" {
		t.Fatal("UI must not roll when the server-side API token rotates")
	}
	assertEventContains(t, drainRecorder(recorder), operatorstatus.ReasonAPITokenRotationAccepted)
}

func TestGeneratedAPITokenRotationDuplicateValueKeepsToken(t *testing.T) {
	ctx := context.Background()
	tamoss := recoveryTamoss()
	tamoss.Spec.Secrets.APIToken.Generate = true
	tamoss.Annotations = map[string]string{AnnotationAPITokenRotate: "rotate-1"}
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        tamossResourceName(tamoss, "api-token"),
			Namespace:   tamoss.Namespace,
			Annotations: map[string]string{annotationAPITokenDone: "rotate-1"},
		},
		Data: map[string][]byte{apiTokenKey: []byte("stable-token")},
	}
	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).WithScheme(storageBackendTestScheme(t)).WithObjects(existing).Build(),
	}
	secret, _, _ := apiTokenObjects(tamoss)

	if err := reconciler.prepareAPITokenSecret(ctx, tamoss, []client.Object{secret}); err != nil {
		t.Fatalf("expected token preparation: %v", err)
	}
	if string(secret.Data[apiTokenKey]) != "stable-token" {
		t.Fatalf("expected duplicate rotation to keep token, got %q", string(secret.Data[apiTokenKey]))
	}
}

func TestAPITokenRotationRejectsUserSuppliedToken(t *testing.T) {
	tamoss := recoveryTamoss()
	tamoss.Spec.Secrets.APIToken.Generate = false
	tamoss.Spec.Secrets.APIToken.Token = "literal-token"
	tamoss.Annotations = map[string]string{AnnotationAPITokenRotate: "rotate-1"}
	recorder := record.NewFakeRecorder(10)
	reconciler := &TamossReconciler{Recorder: recorder}

	if err := reconciler.prepareAPITokenSecret(context.Background(), tamoss, nil); err != nil {
		t.Fatalf("expected supplied token rotation rejection without error: %v", err)
	}
	events := drainRecorder(recorder)
	assertEventContains(t, events, operatorstatus.ReasonAPITokenRotationRejected)
	assertEventContains(t, events, "supplied directly")
}

func TestAPITokenRotationRejectsGenerateFalseWithoutToken(t *testing.T) {
	tamoss := recoveryTamoss()
	tamoss.Spec.Secrets.APIToken.Generate = false
	tamoss.Annotations = map[string]string{AnnotationAPITokenRotate: "rotate-1"}
	recorder := record.NewFakeRecorder(10)
	reconciler := &TamossReconciler{Recorder: recorder}

	if err := reconciler.prepareAPITokenSecret(context.Background(), tamoss, nil); err != nil {
		t.Fatalf("expected invalid rotation rejection without error when no generated secret is rendered: %v", err)
	}
	events := drainRecorder(recorder)
	assertEventContains(t, events, operatorstatus.ReasonAPITokenRotationRejected)
	assertEventContains(t, events, "generate is false")
}

func recoveryTamoss() *tamossv1alpha1.Tamoss {
	tamoss := tamossFixture()
	tamoss.Generation = 3
	return tamoss
}

func terminalSchemaState(tamoss *tamossv1alpha1.Tamoss, consumedRetry string) *corev1.ConfigMap {
	annotations := map[string]string{}
	if consumedRetry != "" {
		annotations[annotationSchemaRetryDone] = consumedRetry
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        tamossResourceName(tamoss, "schema-state"),
			Namespace:   tamoss.Namespace,
			Annotations: annotations,
		},
		Data: map[string]string{
			schemaStateFailureCountKey:  "3",
			schemaStateFailedGeneration: "3",
			schemaStateFailedVersion:    schemabundle.SchemaVersion,
			schemaStateFailedJobUID:     "failed-job",
			schemaStateFailedAtKey:      "2026-05-22T12:00:00Z",
		},
	}
}

func apiTokenObjects(tamoss *tamossv1alpha1.Tamoss) (*corev1.Secret, *appsv1.Deployment, *appsv1.Deployment) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: tamossResourceName(tamoss, "api-token"), Namespace: tamoss.Namespace}}
	apiDeployment := tokenDeployment(tamoss, "api")
	uiDeployment := tokenDeployment(tamoss, "ui")
	return secret, apiDeployment, uiDeployment
}

func tokenDeployment(tamoss *tamossv1alpha1.Tamoss, component string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tamossResourceName(tamoss, component),
			Namespace: tamoss.Namespace,
			Labels:    map[string]string{"app.kubernetes.io/component": component},
		},
	}
}
