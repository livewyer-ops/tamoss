package controller

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

const testIngestEndpoint = "https://tams.example.test/v8.1"

func TestDesiredIngestJobUsesFixedSecureTemplate(t *testing.T) {
	run := testIngestRun()
	spec := defaultIngestRunSpec(run.Spec)
	tamoss := testIngestTamoss()
	image := "registry.example/tamsin@sha256:" + strings.Repeat("a", 64)

	job := desiredIngestJob(run, spec, tamoss, testIngestEndpoint, image, "", []string{"s3://staging/run/input.mp4"})
	if job.Spec.Template.Spec.AutomountServiceAccountToken == nil || *job.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatal("expected service account token automount to be disabled")
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 2 {
		t.Fatalf("backoffLimit = %v, want 2", job.Spec.BackoffLimit)
	}
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 21600 {
		t.Fatalf("activeDeadlineSeconds = %v, want 21600", job.Spec.ActiveDeadlineSeconds)
	}
	pod := job.Spec.Template.Spec
	if pod.SecurityContext == nil || pod.SecurityContext.RunAsNonRoot == nil || !*pod.SecurityContext.RunAsNonRoot {
		t.Fatal("expected a non-root Pod security context")
	}
	container := pod.Containers[0]
	if container.Image != image {
		t.Fatalf("image = %q, want configured immutable image", container.Image)
	}
	if got := job.Labels["app.kubernetes.io/instance"]; got != tamoss.Name {
		t.Fatalf("instance label = %q, want %q", got, tamoss.Name)
	}
	if container.SecurityContext == nil || container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("expected privilege escalation to be disabled")
	}
	if container.SecurityContext.ReadOnlyRootFilesystem == nil || !*container.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatal("expected a read-only root filesystem")
	}
	if !slices.Contains(container.Args, "s3://staging/run/input.mp4") {
		t.Fatalf("args do not contain the declared input: %#v", container.Args)
	}
	if !slices.Contains(container.Args, testIngestEndpoint) {
		t.Fatalf("args do not contain the approved HTTPS endpoint: %#v", container.Args)
	}
	if !slices.Contains(container.Args, "none") || slices.Contains(container.Args, "never") {
		t.Fatalf("args do not use Tamsin's supported non-interactive progress mode: %#v", container.Args)
	}
	if slices.Contains(container.Args, "--insecure-skip-verify") {
		t.Fatalf("non-local profile disables TLS verification: %#v", container.Args)
	}
	for _, forbidden := range []string{"--image", "--command", "--env", "--service-account"} {
		if slices.Contains(container.Args, forbidden) {
			t.Fatalf("unsafe user-controlled flag %q reached the Job", forbidden)
		}
	}
	var tokenEnv *corev1.EnvVar
	for i := range container.Env {
		if container.Env[i].Name == "TAMSIN_AUTH_TOKEN" {
			tokenEnv = &container.Env[i]
		}
	}
	if tokenEnv == nil || tokenEnv.ValueFrom == nil || tokenEnv.ValueFrom.SecretKeyRef == nil {
		t.Fatal("expected TAMSIN_AUTH_TOKEN to come from a Secret key reference")
	}
	if got := tokenEnv.ValueFrom.SecretKeyRef.Name; got != "example-api-token" {
		t.Fatalf("token Secret = %q, want example-api-token", got)
	}
}

func TestDesiredIngestJobAcceptsLocalKindSelfSignedTLS(t *testing.T) {
	run := testIngestRun()
	tamoss := testIngestTamoss()
	tamoss.Spec.Profile = tamossv1alpha1.TamossProfileLocalKind
	job := desiredIngestJob(
		run,
		defaultIngestRunSpec(run.Spec),
		tamoss,
		testIngestEndpoint,
		"registry.example/tamsin@sha256:"+strings.Repeat("a", 64),
		"",
		[]string{"s3://staging/run/input.mp4"},
	)

	if !slices.Contains(job.Spec.Template.Spec.Containers[0].Args, "--insecure-skip-verify") {
		t.Fatalf("local-kind args do not accept the disposable self-signed endpoint: %#v", job.Spec.Template.Spec.Containers[0].Args)
	}
}

func TestDesiredIngestJobBoundsLongRunLabel(t *testing.T) {
	run := testIngestRun()
	run.Name = strings.Repeat("a", 44) + "." + strings.Repeat("b", 20)
	spec := defaultIngestRunSpec(run.Spec)
	job := desiredIngestJob(
		run,
		spec,
		testIngestTamoss(),
		testIngestEndpoint,
		"registry.example/tamsin@sha256:"+strings.Repeat("a", 64),
		"",
		[]string{"s3://staging/run/input.mp4"},
	)
	label := job.Labels[ingestRunLabel]
	if len(label) > 63 {
		t.Fatalf("IngestRun label length = %d, want at most 63", len(label))
	}
	if label != ingestRunSelectorValue(run.Name) || job.Spec.Template.Labels[ingestRunLabel] != label {
		t.Fatal("bounded label must be consistent across the Job and Pod template")
	}
	if problems := kvalidation.IsDNS1123Subdomain(job.Name); len(problems) > 0 {
		t.Fatalf("generated Job name %q is invalid: %v", job.Name, problems)
	}
}

func TestIngestRunReconcileCreatesJobAndReportsRunning(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	tamoss := testIngestTamoss()
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}, &batchv1.Job{}).
		WithObjects(run, tamoss).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("b", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("create reconcile failed: %v", err)
	}
	job := &batchv1.Job{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: run.Namespace, Name: ingestJobName(run.Name)}, job); err != nil {
		t.Fatalf("expected Tamsin Job: %v", err)
	}
	if len(job.OwnerReferences) != 1 || job.OwnerReferences[0].Name != run.Name {
		t.Fatalf("unexpected Job owner references: %#v", job.OwnerReferences)
	}
	job.Status.Active = 1
	now := metav1.Now()
	job.Status.StartTime = &now
	if err := k8sClient.Status().Update(ctx, job); err != nil {
		t.Fatalf("update Job status: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("running reconcile failed: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, request.NamespacedName, updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	if updated.Status.Phase != tamossv1alpha1.IngestRunPhaseRunning {
		t.Fatalf("phase = %q, want Running", updated.Status.Phase)
	}
	if updated.Status.JobRef.Name != job.Name {
		t.Fatalf("status job = %q, want %q", updated.Status.JobRef.Name, job.Name)
	}
}

func TestIngestRunWithoutConfiguredImageStaysPending(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{Client: k8sClient, Scheme: scheme}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Fatalf("static configuration failure should not poll, got %#v", result)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	if updated.Status.Phase != tamossv1alpha1.IngestRunPhasePending {
		t.Fatalf("phase = %q, want Pending", updated.Status.Phase)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "TamsinRuntimeUnavailable" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
	jobs := &batchv1.JobList{}
	if err := k8sClient.List(ctx, jobs, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("expected no Jobs, got %d", len(jobs.Items))
	}
}

func TestIngestRunWithoutApprovedInputResolverStaysPending(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{
		Client:      k8sClient,
		Scheme:      scheme,
		TamsinImage: "registry.example/tamsin@sha256:" + strings.Repeat("c", 64),
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Fatalf("static input-resolver failure should not poll, got %#v", result)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "InputResolverUnavailable" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
	jobs := &batchv1.JobList{}
	if err := k8sClient.List(ctx, jobs, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("expected no Jobs, got %d", len(jobs.Items))
	}
}

func TestIngestRunWithoutApprovedEndpointResolverStaysPending(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{
		Client:        k8sClient,
		Scheme:        scheme,
		TamsinImage:   "registry.example/tamsin@sha256:" + strings.Repeat("c", 64),
		InputResolver: staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Fatalf("static endpoint-resolver failure should not poll, got %#v", result)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "IngestEndpointResolverUnavailable" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
}

func TestIngestRunDoesNotReplayMissingRecordedJob(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	run.Status.JobRef = tamossv1alpha1.IngestRunJobStatus{Name: ingestJobName(run.Name), UID: types.UID("deleted-job-uid")}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("c", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Fatalf("missing recorded Job should not poll, got %#v", result)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "IngestJobMissing" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
	jobs := &batchv1.JobList{}
	if err := k8sClient.List(ctx, jobs, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("expected no replayed Jobs, got %d", len(jobs.Items))
	}
}

func TestResolvedIngestInputsRejectCredentialsAndSignedURLs(t *testing.T) {
	for _, location := range []string{
		"https://user:password@example.test/media.mov",
		"https://example.test/media.mov?signature=secret",
		"file:///tmp/media.mov",
	} {
		err := validateResolvedIngestInputs(ResolvedIngestInputs{Selectors: []string{location}}, 1)
		if err == nil {
			t.Fatalf("expected %q to be rejected", location)
		}
	}
}

func TestResolvedIngestInputsBoundsJobSelectors(t *testing.T) {
	selectors := make([]string, maxIngestSelectors+1)
	for i := range selectors {
		selectors[i] = "s3://staging/run/input.mp4"
	}
	if err := validateResolvedIngestInputs(ResolvedIngestInputs{Selectors: selectors}, 10_000); err == nil {
		t.Fatal("expected an oversized top-level selector list to be rejected")
	}
	if err := validateResolvedIngestInputs(ResolvedIngestInputs{
		Selectors:      []string{"s3://staging/run/manifest.txt"},
		ExpectedInputs: 10_001,
	}, 10_000); err == nil {
		t.Fatal("expected an oversized expanded input count to be rejected")
	}
}

func TestValidateIngestEndpointRequiresHTTPSWithoutCredentials(t *testing.T) {
	for _, endpoint := range []string{
		"http://tams-api.media.svc.cluster.local:8000/v8.1",
		"https://user:secret@tams.example.test/v8.1",
		"https://tams.example.test/v8.1?token=secret",
		"https://tams.example.test/v8.1#fragment",
		"https:///v8.1",
	} {
		if _, err := validateIngestEndpoint(endpoint); err == nil {
			t.Fatalf("expected endpoint %q to be rejected", endpoint)
		}
	}

	got, err := validateIngestEndpoint("  " + testIngestEndpoint + "  ")
	if err != nil {
		t.Fatalf("expected approved endpoint to be accepted: %v", err)
	}
	if got != testIngestEndpoint {
		t.Fatalf("endpoint = %q, want %q", got, testIngestEndpoint)
	}
}

func TestIngestRunRetryRequiresMatchingParentUID(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	run.Spec.RetryOf = &tamossv1alpha1.IngestRunReference{Name: "previous-run", UID: "original-parent-uid"}
	parent := testIngestRun()
	parent.Name = "previous-run"
	parent.UID = types.UID("replacement-parent-uid")
	parent.Status.Phase = tamossv1alpha1.IngestRunPhaseFailed
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, parent, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("d", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "RetryParentReplaced" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
	jobs := &batchv1.JobList{}
	if err := k8sClient.List(ctx, jobs, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("expected no Jobs, got %d", len(jobs.Items))
	}
}

func TestIngestRunRetryCannotCrossTamossInstances(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	parent := testIngestRun()
	parent.Name = "previous-run"
	parent.UID = types.UID("parent-uid")
	parent.Spec.TamossRef.Name = "other-instance"
	parent.Status.Phase = tamossv1alpha1.IngestRunPhaseFailed
	run.Spec.RetryOf = &tamossv1alpha1.IngestRunReference{Name: parent.Name, UID: string(parent.UID)}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, parent, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("d", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "RetryParentTargetMismatch" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
	jobs := &batchv1.JobList{}
	if err := k8sClient.List(ctx, jobs, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("expected no Jobs, got %d", len(jobs.Items))
	}
}

func TestIngestRunRetryIncrementsAttempt(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	parent := testIngestRun()
	parent.Name = "previous-run"
	parent.UID = types.UID("parent-uid")
	parent.Status.Phase = tamossv1alpha1.IngestRunPhasePartiallySucceeded
	parent.Status.Attempt = 3
	run.Spec.RetryOf = &tamossv1alpha1.IngestRunReference{Name: parent.Name, UID: string(parent.UID)}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}, &batchv1.Job{}).
		WithObjects(run, parent, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("e", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	if updated.Status.Attempt != 4 {
		t.Fatalf("attempt = %d, want 4", updated.Status.Attempt)
	}
}

func TestIngestRunRetryRequiresTheParentConfiguration(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	parent := testIngestRun()
	parent.Name = "previous-run"
	parent.UID = types.UID("parent-uid")
	parent.Status.Phase = tamossv1alpha1.IngestRunPhaseFailed
	run.Spec.RetryOf = &tamossv1alpha1.IngestRunReference{Name: parent.Name, UID: string(parent.UID)}
	run.Spec.Profile = tamossv1alpha1.IngestRunProfilePreserve
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, parent, testIngestTamoss()).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("e", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "RetryParentConfigurationMismatch" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
}

func TestIngestRunResolvesApprovedStorageBackendDestination(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	storageBackend := testIngestStorageBackend()
	run.Spec.Options.StorageBackendRef = &tamossv1alpha1.IngestStorageBackendReference{Name: storageBackend.Name}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}, &tamossv1alpha1.StorageBackend{}, &batchv1.Job{}).
		WithObjects(run, testIngestTamoss(), storageBackend).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("f", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	job := &batchv1.Job{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: run.Namespace, Name: ingestJobName(run.Name)}, job); err != nil {
		t.Fatalf("get Job: %v", err)
	}
	args := job.Spec.Template.Spec.Containers[0].Args
	wantID := storageBackend.Status.BackendID
	for i := range args {
		if args[i] == "--storage-id" && i+1 < len(args) && args[i+1] == wantID {
			return
		}
	}
	t.Fatalf("Job args do not contain the approved storage ID %q: %#v", wantID, args)
}

func TestIngestRunRejectsStorageBackendFromAnotherTamoss(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	storageBackend := testIngestStorageBackend()
	storageBackend.Spec.TamossRef.Name = "other-instance"
	run.Spec.Options.StorageBackendRef = &tamossv1alpha1.IngestStorageBackendReference{Name: storageBackend.Name}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}, &tamossv1alpha1.StorageBackend{}).
		WithObjects(run, testIngestTamoss(), storageBackend).
		Build()
	reconciler := &IngestRunReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		TamsinImage:      "registry.example/tamsin@sha256:" + strings.Repeat("f", 64),
		InputResolver:    staticIngestInputResolver{selectors: []string{"s3://staging/run/input.mp4"}},
		EndpointResolver: staticIngestEndpointResolver{},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(run), updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "IngestStorageBackendTargetMismatch" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
	jobs := &batchv1.JobList{}
	if err := k8sClient.List(ctx, jobs, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list Jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Fatalf("expected no Jobs, got %d", len(jobs.Items))
	}
}

func TestIngestPhaseRecognisesPartialBatchExit(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "tamsin-run", Namespace: "media", UID: types.UID("job-uid")},
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
			Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Message: "backoff limit reached",
		}}},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tamsin-run-abc",
			Namespace: "media",
			Labels:    map[string]string{"job-name": job.Name},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: batchv1.SchemeGroupVersion.String(), Kind: "Job", Name: job.Name, UID: job.UID, Controller: ptr.To(true),
			}},
		},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name: "tamsin", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 4}},
		}}},
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(job, pod).Build()

	phase, reason, _, progressing, err := ingestPhaseFromJob(ctx, reader, job, verifiedIngestResult(), time.Now(), nil)
	if err != nil {
		t.Fatalf("resolve Job phase: %v", err)
	}
	if phase != tamossv1alpha1.IngestRunPhasePartiallySucceeded || reason != "IngestPartiallySucceeded" || progressing {
		t.Fatalf("got phase=%q reason=%q progressing=%t", phase, reason, progressing)
	}
}

func TestIngestPhaseWaitsForOwnedTerminalPod(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "tamsin-run", Namespace: "media", UID: types.UID("job-uid")},
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
			Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Message: "backoff limit reached",
		}}},
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(job).Build()

	phase, reason, _, progressing, err := ingestPhaseFromJob(ctx, reader, job, verifiedIngestResult(), time.Now(), nil)
	if err != nil {
		t.Fatalf("resolve phase before Pod observation: %v", err)
	}
	if phase != tamossv1alpha1.IngestRunPhaseRunning || reason != "ExitCodePending" || progressing {
		t.Fatalf("before Pod observation got phase=%q reason=%q progressing=%t", phase, reason, progressing)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tamsin-run-terminal",
			Namespace: job.Namespace,
			Labels:    map[string]string{"job-name": job.Name},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: batchv1.SchemeGroupVersion.String(), Kind: "Job", Name: job.Name, UID: job.UID, Controller: ptr.To(true),
			}},
		},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name: "tamsin", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, FinishedAt: metav1.Now()}},
		}}},
	}
	if err := reader.Create(ctx, pod); err != nil {
		t.Fatalf("create terminal Pod: %v", err)
	}
	phase, reason, _, progressing, err = ingestPhaseFromJob(ctx, reader, job, verifiedIngestResult(), time.Now(), nil)
	if err != nil {
		t.Fatalf("resolve phase after Pod observation: %v", err)
	}
	if phase != tamossv1alpha1.IngestRunPhaseFailed || reason != "IngestFailed" || progressing {
		t.Fatalf("after Pod observation got phase=%q reason=%q progressing=%t", phase, reason, progressing)
	}
}

func TestIngestPhasePropagatesTerminalPodListErrors(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "tamsin-run", Namespace: "media", UID: types.UID("job-uid")},
		Status:     batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}},
	}
	reader := failingPodListReader{Reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(job).Build()}

	_, _, _, _, err := ingestPhaseFromJob(ctx, reader, job, verifiedIngestResult(), time.Now(), nil)
	if err == nil || !strings.Contains(err.Error(), "list terminal Pods") {
		t.Fatalf("expected Pod list error, got %v", err)
	}
}

func TestIngestPhaseWaitsForDigestVerifiedResult(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "tamsin-run", Namespace: "media"},
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
			Type: batchv1.JobComplete, Status: corev1.ConditionTrue,
		}}},
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(job).Build()

	// A recorded result must still carry a valid digest before the run is
	// believed, so a collector cannot publish a half-written artifact and have
	// the run claim success on it.
	unverified := verifiedIngestResult()
	unverified.Verified = false
	phase, reason, _, progressing, err := ingestPhaseFromJob(ctx, reader, job, unverified, time.Now(), nil)
	if err != nil {
		t.Fatalf("resolve Job phase with an unverified result: %v", err)
	}
	if phase != tamossv1alpha1.IngestRunPhaseRunning || reason != "ResultVerificationPending" || progressing {
		t.Fatalf("with an unverified result got phase=%q reason=%q progressing=%t", phase, reason, progressing)
	}

	// Tamsin 0.1.0-rc.2 publishes no digest for its journal, so a run that
	// records no durable result succeeds on the Job's own terminal state
	// rather than waiting for evidence that never arrives.
	phase, reason, _, _, err = ingestPhaseFromJob(ctx, reader, job, tamossv1alpha1.IngestRunResultStatus{}, time.Now(), nil)
	if err != nil {
		t.Fatalf("resolve Job phase without a recorded result: %v", err)
	}
	if phase != tamossv1alpha1.IngestRunPhaseSucceeded || reason != "IngestSucceeded" {
		t.Fatalf("without a recorded result got phase=%q reason=%q, want Succeeded", phase, reason)
	}

	phase, reason, _, progressing, err = ingestPhaseFromJob(ctx, reader, job, verifiedIngestResult(), time.Now(), nil)
	if err != nil {
		t.Fatalf("resolve Job phase with result: %v", err)
	}
	if phase != tamossv1alpha1.IngestRunPhaseSucceeded || reason != "IngestSucceeded" || progressing {
		t.Fatalf("with verified result got phase=%q reason=%q progressing=%t", phase, reason, progressing)
	}
}

func TestIngestRunCancellationDeletesJobAndRetainsRun(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	run.Spec.DesiredState = tamossv1alpha1.IngestRunDesiredStateCancelled
	job := desiredIngestJob(run, defaultIngestRunSpec(run.Spec), testIngestTamoss(), testIngestEndpoint, "tamsin:test", "", []string{"s3://staging/run/input.mp4"})
	job.OwnerReferences = []metav1.OwnerReference{{APIVersion: tamossv1alpha1.GroupVersion.String(), Kind: "IngestRun", Name: run.Name, UID: run.UID, Controller: ptr.To(true)}}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &batchv1.Job{}).
		WithObjects(run, job).
		Build()
	reconciler := &IngestRunReconciler{Client: k8sClient, Scheme: scheme, TamsinImage: "tamsin:test"}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("cancelling reconcile failed: %v", err)
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected Job to be gone after foreground deletion, got %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("cancelled reconcile failed: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, request.NamespacedName, updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	if updated.Status.Phase != tamossv1alpha1.IngestRunPhaseCancelled {
		t.Fatalf("phase = %q, want Cancelled", updated.Status.Phase)
	}
}

func TestIngestRunCancellationWaitsForOwnedPodsToDisappear(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	run.Spec.DesiredState = tamossv1alpha1.IngestRunDesiredStateCancelled
	run.Status.JobRef = tamossv1alpha1.IngestRunJobStatus{Name: ingestJobName(run.Name), UID: types.UID("job-uid")}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tamsin-daily-news-abc",
			Namespace: run.Namespace,
			Labels: map[string]string{
				ingestRunLabel:       run.Name,
				ingestRunTargetLabel: run.Spec.TamossRef.Name,
			},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: run.Status.JobRef.Name, UID: run.Status.JobRef.UID}},
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}).
		WithObjects(run, pod).
		Build()
	reconciler := &IngestRunReconciler{Client: k8sClient, Scheme: scheme}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}

	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("reconcile while Pod remains: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected cancellation to poll for Pod termination")
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, request.NamespacedName, updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	if updated.Status.Phase == tamossv1alpha1.IngestRunPhaseCancelled {
		t.Fatal("run was marked Cancelled while an owned Pod remained")
	}
	if err := k8sClient.Delete(ctx, pod); err != nil {
		t.Fatalf("delete Pod: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile after Pod deletion: %v", err)
	}
	if err := k8sClient.Get(ctx, request.NamespacedName, updated); err != nil {
		t.Fatalf("get cancelled IngestRun: %v", err)
	}
	if updated.Status.Phase != tamossv1alpha1.IngestRunPhaseCancelled {
		t.Fatalf("phase = %q, want Cancelled", updated.Status.Phase)
	}
}

func TestIngestRunCancellationDoesNotDeleteConflictingJob(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	run.Spec.DesiredState = tamossv1alpha1.IngestRunDesiredStateCancelled
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: ingestJobName(run.Name), Namespace: run.Namespace, UID: types.UID("foreign-job")}}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}).
		WithObjects(run, job).
		Build()
	reconciler := &IngestRunReconciler{Client: k8sClient, Scheme: scheme}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{}); err != nil {
		t.Fatalf("conflicting Job was deleted: %v", err)
	}
	updated := &tamossv1alpha1.IngestRun{}
	if err := k8sClient.Get(ctx, request.NamespacedName, updated); err != nil {
		t.Fatalf("get IngestRun: %v", err)
	}
	condition := conditionByType(updated.Status.Conditions, ingestRunReadyCondition)
	if condition == nil || condition.Reason != "JobOwnershipConflict" {
		t.Fatalf("unexpected Ready condition: %#v", condition)
	}
}

func ingestRunTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	for name, add := range map[string]func(*runtime.Scheme) error{
		"core":   corev1.AddToScheme,
		"batch":  batchv1.AddToScheme,
		"tamoss": tamossv1alpha1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatalf("add %s scheme: %v", name, err)
		}
	}
	return scheme
}

func testIngestRun() *tamossv1alpha1.IngestRun {
	return &tamossv1alpha1.IngestRun{
		ObjectMeta: metav1.ObjectMeta{Name: "daily-news", Namespace: "media", UID: types.UID("run-uid"), Generation: 1},
		Spec: tamossv1alpha1.IngestRunSpec{
			TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "example"},
			InputRef:  tamossv1alpha1.IngestInputReference{Kind: "StagedObject", ID: "staged-123"},
			Profile:   tamossv1alpha1.IngestRunProfileEditorial,
			SizeClass: tamossv1alpha1.IngestRunSizeClassSmall,
			Options: tamossv1alpha1.IngestRunOptions{
				Verify:    ptr.To(true),
				MaxInputs: 1000,
			},
			DesiredState: tamossv1alpha1.IngestRunDesiredStateRunning,
		},
	}
}

func testIngestTamoss() *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "media", UID: types.UID("tamoss-uid"), Generation: 1},
		Spec: tamossv1alpha1.TamossSpec{
			Secrets: tamossv1alpha1.SecretsSpec{APIToken: tamossv1alpha1.APITokenSecretSpec{Generate: true}},
		},
		Status: tamossv1alpha1.TamossStatus{
			ObservedGeneration: 1,
			Conditions: []metav1.Condition{{
				Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready", LastTransitionTime: metav1.Now(), ObservedGeneration: 1,
			}},
		},
	}
}

func testIngestStorageBackend() *tamossv1alpha1.StorageBackend {
	const backendID = "f1ab5b54-9703-42ed-b181-11ba1c794a7f"
	return &tamossv1alpha1.StorageBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "media-primary", Namespace: "media", UID: types.UID("storage-uid"), Generation: 1},
		Spec: tamossv1alpha1.StorageBackendSpec{
			ID:        backendID,
			TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "example"},
			Usage:     tamossv1alpha1.StorageBackendUsageMedia,
		},
		Status: tamossv1alpha1.StorageBackendStatus{
			ObservedGeneration: 1,
			BackendID:          backendID,
			Resolved:           tamossv1alpha1.StorageBackendResolvedStatus{BackendID: backendID},
			Conditions: []metav1.Condition{{
				Type: operatorstatus.ConditionReady, Status: metav1.ConditionTrue, Reason: operatorstatus.ReasonStorageBackendReady,
				LastTransitionTime: metav1.Now(), ObservedGeneration: 1,
			}},
		},
	}
}

func verifiedIngestResult() tamossv1alpha1.IngestRunResultStatus {
	return tamossv1alpha1.IngestRunResultStatus{
		Key:       "ingest-results/run-uid.json",
		SHA256:    strings.Repeat("a", 64),
		Size:      512,
		MediaType: "application/json",
		Verified:  true,
	}
}

type staticIngestInputResolver struct {
	selectors []string
}

func (r staticIngestInputResolver) Resolve(context.Context, string, string, tamossv1alpha1.IngestInputReference, int32) (ResolvedIngestInputs, error) {
	return ResolvedIngestInputs{Selectors: r.selectors, ExpectedInputs: int32(len(r.selectors))}, nil
}

type staticIngestEndpointResolver struct {
	endpoint string
}

type failingPodListReader struct {
	client.Reader
}

func (r failingPodListReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if _, ok := list.(*corev1.PodList); ok {
		return errors.New("temporary Pod cache failure")
	}
	return r.Reader.List(ctx, list, opts...)
}

func (r staticIngestEndpointResolver) Resolve(context.Context, string, string) (string, error) {
	if r.endpoint == "" {
		return testIngestEndpoint, nil
	}
	return r.endpoint, nil
}

func conditionByType(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
