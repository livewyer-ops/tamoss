package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

// ownedIngestJob builds a Job that ingestJobBelongsToRun accepts, so these
// tests exercise the tracking path rather than the ownership-conflict guard.
func ownedIngestJob(run *tamossv1alpha1.IngestRun) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingestJobName(run.Name),
			Namespace: run.Namespace,
			UID:       types.UID("job-uid"),
			Labels: map[string]string{
				ingestRunLabel:       ingestRunSelectorValue(run.Name),
				ingestRunTargetLabel: run.Spec.TamossRef.Name,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: tamossv1alpha1.SchemeGroupVersion.String(),
				Kind:       "IngestRun",
				Name:       run.Name,
				UID:        run.UID,
				Controller: ptr.To(true),
			}},
		},
	}
}

func ingestRunWithRecordedJob(run *tamossv1alpha1.IngestRun) *tamossv1alpha1.IngestRun {
	run.Status.Phase = tamossv1alpha1.IngestRunPhaseRunning
	run.Status.JobRef = tamossv1alpha1.IngestRunJobStatus{Name: ingestJobName(run.Name), UID: types.UID("job-uid")}
	return run
}

func reloadIngestRun(t *testing.T, ctx context.Context, c client.Client, run *tamossv1alpha1.IngestRun) *tamossv1alpha1.IngestRun {
	t.Helper()
	reloaded := &tamossv1alpha1.IngestRun{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(run), reloaded); err != nil {
		t.Fatalf("reload IngestRun: %v", err)
	}
	return reloaded
}

// A Job removed by its TTL after the run finished used to park the run in a
// non-terminal Pending phase with no requeue, which blocked retries forever.
func TestIngestRunFailsTerminallyWhenRecordedJobDisappears(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := ingestRunWithRecordedJob(testIngestRun())
	tamoss := testIngestTamoss()
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, tamoss).
		Build()
	reconciler := &IngestRunReconciler{Client: k8sClient, Scheme: scheme, APIReader: k8sClient}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("a terminal outcome must not requeue, got %+v", result)
	}
	reloaded := reloadIngestRun(t, ctx, k8sClient, run)
	if reloaded.Status.Phase != tamossv1alpha1.IngestRunPhaseFailed {
		t.Fatalf("phase = %q, want Failed", reloaded.Status.Phase)
	}
	if !isIngestRunTerminal(reloaded.Status.Phase) {
		t.Fatal("a run whose Job has gone must reach a terminal phase so retries are unblocked")
	}
	if reloaded.Status.CompletedAt == nil {
		t.Fatal("expected a completion timestamp on the terminal phase")
	}
	ready := findIngestCondition(t, reloaded, operatorstatus.ConditionReady)
	if ready.Reason != "IngestJobMissing" {
		t.Fatalf("Ready reason = %q, want IngestJobMissing", ready.Reason)
	}
}

// A cold or lagging informer cache must never be enough to fail a healthy run.
func TestIngestRunWaitsWhenMissingJobIsStillLive(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := ingestRunWithRecordedJob(testIngestRun())
	tamoss := testIngestTamoss()
	cached := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, tamoss).
		Build()
	live := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ownedIngestJob(run)).
		Build()
	reconciler := &IngestRunReconciler{Client: cached, Scheme: scheme, APIReader: live}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected a requeue while the cache catches up")
	}
	reloaded := reloadIngestRun(t, ctx, cached, run)
	if reloaded.Status.Phase == tamossv1alpha1.IngestRunPhaseFailed {
		t.Fatal("a cache miss must not fail a run whose Job still exists")
	}
}

func TestIngestRunWaitsForResultVerificationBeforeDeadline(t *testing.T) {
	phase, reason, _, _, err := ingestPhaseFromJob(
		context.Background(),
		fake.NewClientBuilder().WithScheme(ingestRunTestScheme(t)).Build(),
		completedIngestJob(time.Now().Add(-time.Minute)),
		unverifiedIngestResult(),
		time.Now(),
		nil,
	)
	if err != nil {
		t.Fatalf("phase evaluation failed: %v", err)
	}
	if phase != tamossv1alpha1.IngestRunPhaseRunning || reason != "ResultVerificationPending" {
		t.Fatalf("phase/reason = %q/%q, want Running/ResultVerificationPending", phase, reason)
	}
}

// A recorded result that never passes verification must not poll until the
// Job's TTL removes it and leave the run with no terminal phase at all.
func TestIngestRunFailsWhenResultVerificationExceedsDeadline(t *testing.T) {
	finished := time.Now().Add(-2 * ingestTerminalObservationDeadline)
	phase, reason, _, progressing, err := ingestPhaseFromJob(
		context.Background(),
		fake.NewClientBuilder().WithScheme(ingestRunTestScheme(t)).Build(),
		completedIngestJob(finished),
		unverifiedIngestResult(),
		time.Now(),
		nil,
	)
	if err != nil {
		t.Fatalf("phase evaluation failed: %v", err)
	}
	if phase != tamossv1alpha1.IngestRunPhaseFailed || reason != "ResultVerificationTimeout" {
		t.Fatalf("phase/reason = %q/%q, want Failed/ResultVerificationTimeout", phase, reason)
	}
	if progressing {
		t.Fatal("a timed-out run is not progressing")
	}
	if !isIngestRunTerminal(phase) {
		t.Fatal("the verification deadline must produce a terminal phase")
	}
}

// The Pod carrying the exit code can be garbage collected before it is seen.
func TestIngestRunFailsWhenExitCodeIsUnobservableAfterDeadline(t *testing.T) {
	failed := time.Now().Add(-2 * ingestTerminalObservationDeadline)
	job := ownedIngestJob(testIngestRun())
	job.Status.Conditions = []batchv1.JobCondition{{
		Type:               batchv1.JobFailed,
		Status:             corev1.ConditionTrue,
		Message:            "BackoffLimitExceeded",
		LastTransitionTime: metav1.NewTime(failed),
	}}
	phase, reason, message, _, err := ingestPhaseFromJob(
		context.Background(),
		fake.NewClientBuilder().WithScheme(ingestRunTestScheme(t)).Build(),
		job,
		tamossv1alpha1.IngestRunResultStatus{},
		time.Now(),
		nil,
	)
	if err != nil {
		t.Fatalf("phase evaluation failed: %v", err)
	}
	if phase != tamossv1alpha1.IngestRunPhaseFailed || reason != "IngestFailed" {
		t.Fatalf("phase/reason = %q/%q, want Failed/IngestFailed", phase, reason)
	}
	if message != "BackoffLimitExceeded" {
		t.Fatalf("message = %q, want the Job's own failure message", message)
	}
}

// Readiness gates admit new work. Re-applying them to an attempt that already
// has a Job regressed a running run to Pending and ignored the Job's progress.
func TestIngestRunTracksExistingJobWhenTamossNotReady(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := ingestRunWithRecordedJob(testIngestRun())
	tamoss := testIngestTamoss()
	tamoss.Status.Conditions[0].Status = metav1.ConditionFalse
	job := ownedIngestJob(run)
	job.Status.Active = 1
	job.Status.StartTime = ptr.To(metav1.Now())
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, tamoss, job).
		Build()
	reconciler := &IngestRunReconciler{Client: k8sClient, Scheme: scheme, APIReader: k8sClient}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	reloaded := reloadIngestRun(t, ctx, k8sClient, run)
	if reloaded.Status.Phase != tamossv1alpha1.IngestRunPhaseRunning {
		t.Fatalf("phase = %q, want Running tracked from the Job", reloaded.Status.Phase)
	}
	ready := findIngestCondition(t, reloaded, operatorstatus.ConditionReady)
	if ready.Reason == "TamossNotReady" {
		t.Fatal("a transient instance readiness dip must not regress an in-flight run")
	}
}

// The Job is owned by the IngestRun, not the Tamoss, so nothing else stops it
// uploading to a deleted endpoint until its active deadline expires.
func TestIngestRunStopsJobWhenTargetTamossIsDeleted(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := ingestRunWithRecordedJob(testIngestRun())
	job := ownedIngestJob(run)
	job.Status.Active = 1
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}).
		WithObjects(run, job).
		Build()
	reconciler := &IngestRunReconciler{Client: k8sClient, Scheme: scheme, APIReader: k8sClient}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	remaining := &batchv1.Job{}
	err := k8sClient.Get(ctx, types.NamespacedName{Namespace: run.Namespace, Name: ingestJobName(run.Name)}, remaining)
	if err == nil {
		t.Fatal("expected the orphaned Tamsin Job to be deleted")
	}
	if !apierrors.IsNotFound(err) {
		t.Fatalf("unexpected Job read error: %v", err)
	}
	reloaded := reloadIngestRun(t, ctx, k8sClient, run)
	if reloaded.Status.Phase != tamossv1alpha1.IngestRunPhaseFailed {
		t.Fatalf("phase = %q, want Failed", reloaded.Status.Phase)
	}
	ready := findIngestCondition(t, reloaded, operatorstatus.ConditionReady)
	if ready.Reason != operatorstatus.ReasonTamossNotFound {
		t.Fatalf("Ready reason = %q, want %q", ready.Reason, operatorstatus.ReasonTamossNotFound)
	}
}

// An IngestRun created without spec.options must still resolve, because the
// nested defaults do not materialise when the parent object is absent.
func TestResolveIngestStorageBackendAcceptsUnsetReference(t *testing.T) {
	reconciler := &IngestRunReconciler{}
	spec := defaultIngestRunSpec(tamossv1alpha1.IngestRunSpec{
		TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "example"},
		InputRef:  tamossv1alpha1.IngestInputReference{Kind: "StagedObject", ID: "staged-123"},
	})
	if spec.Options.StorageBackendRef != nil {
		t.Fatal("an unset storage backend reference must stay nil so it is omitted from the wire form")
	}
	id, reason, _, err := reconciler.resolveIngestStorageBackend(context.Background(), testIngestRun(), spec)
	if err != nil || reason != "" || id != "" {
		t.Fatalf("resolve = (%q, %q, %v), want the instance default backend", id, reason, err)
	}
}

// unverifiedIngestResult is a durable result that was recorded but has not
// passed digest verification. Verification only applies once something records
// a result: Tamsin publishes no digest for its own journal.
func unverifiedIngestResult() tamossv1alpha1.IngestRunResultStatus {
	result := verifiedIngestResult()
	result.Verified = false
	return result
}

// A run that records no durable result must still reach a terminal phase on the
// Job's own outcome, because Tamsin 0.1.0-rc.2 offers nothing to verify.
func TestIngestRunSucceedsWithoutARecordedDurableResult(t *testing.T) {
	phase, reason, _, _, err := ingestPhaseFromJob(
		context.Background(),
		fake.NewClientBuilder().WithScheme(ingestRunTestScheme(t)).Build(),
		completedIngestJob(time.Now().Add(-time.Minute)),
		tamossv1alpha1.IngestRunResultStatus{},
		time.Now(),
		nil,
	)
	if err != nil {
		t.Fatalf("phase evaluation failed: %v", err)
	}
	if phase != tamossv1alpha1.IngestRunPhaseSucceeded || reason != "IngestSucceeded" {
		t.Fatalf("phase/reason = %q/%q, want Succeeded/IngestSucceeded", phase, reason)
	}
	if !isIngestRunTerminal(phase) {
		t.Fatal("a completed ingest must reach a terminal phase")
	}
}

func completedIngestJob(finished time.Time) *batchv1.Job {
	job := ownedIngestJob(testIngestRun())
	job.Status.CompletionTime = ptr.To(metav1.NewTime(finished))
	job.Status.Conditions = []batchv1.JobCondition{{
		Type:               batchv1.JobComplete,
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.NewTime(finished),
	}}
	return job
}

func findIngestCondition(t *testing.T, run *tamossv1alpha1.IngestRun, conditionType string) metav1.Condition {
	t.Helper()
	for _, condition := range run.Status.Conditions {
		if strings.EqualFold(condition.Type, conditionType) {
			return condition
		}
	}
	t.Fatalf("condition %q not found in %#v", conditionType, run.Status.Conditions)
	return metav1.Condition{}
}
