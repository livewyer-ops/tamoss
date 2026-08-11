package controller

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

// sintelStream is a compressed replica of the stream a real run produced:
// same protocol, envelope, and terminal payload shape.
const sintelStream = `{"protocol":"tamsin.ingest.events","protocol_version":"1.0","type":"hello","seq":0,"run_id":"313c7f9a-7249-4c39-a55d-2f1de6b0b0aa","emitted_at":"2026-08-10T18:42:12Z","elapsed_ms":0,"payload":{"tool_version":"0.1.0-rc.2"}}
{"protocol":"tamsin.ingest.events","protocol_version":"1.0","type":"run.started","seq":1,"run_id":"313c7f9a-7249-4c39-a55d-2f1de6b0b0aa","emitted_at":"2026-08-10T18:42:12Z","elapsed_ms":1,"payload":{}}
{"protocol":"tamsin.ingest.events","protocol_version":"1.0","type":"input.declared","seq":2,"run_id":"313c7f9a-7249-4c39-a55d-2f1de6b0b0aa","emitted_at":"2026-08-10T18:42:12Z","elapsed_ms":2,"payload":{"input":"https://media.example.test/sintel.mp4"}}
{"protocol":"tamsin.ingest.events","protocol_version":"1.0","type":"object.result","seq":9,"run_id":"313c7f9a-7249-4c39-a55d-2f1de6b0b0aa","emitted_at":"2026-08-10T18:42:16Z","elapsed_ms":4000,"scope":{"input_index":0},"payload":{"object_id":"o1","timerange":"[0:0_14:0)","bytes":847158,"status":"ingested"}}
{"protocol":"tamsin.ingest.events","protocol_version":"1.0","type":"input.finished","seq":22,"run_id":"313c7f9a-7249-4c39-a55d-2f1de6b0b0aa","emitted_at":"2026-08-10T18:42:18Z","elapsed_ms":5800,"scope":{"input_index":0},"payload":{"input":"https://media.example.test/sintel.mp4","profile":"editorial","profile_version":"1","status":"ingested","verification":"verified","flow_count":3,"object_count":11}}
{"protocol":"tamsin.ingest.events","protocol_version":"1.0","type":"run.finished","seq":23,"run_id":"313c7f9a-7249-4c39-a55d-2f1de6b0b0aa","emitted_at":"2026-08-10T18:42:18Z","elapsed_ms":5883,"payload":{"outcome":"succeeded","exit_code":0,"total":1,"succeeded":1,"failed":0,"elapsed_ms":5883,"bytes_staged":4372373,"bytes_uploaded":4367815,"bytes_verified":4367815,"retries":0,"objects_verified":11,"objects_retracted":0,"objects_stranded":0}}`

func TestDecodeIngestStreamReducesTheRealProtocol(t *testing.T) {
	summary := decodeIngestStream(strings.NewReader(sintelStream))
	if summary.RunID != "313c7f9a-7249-4c39-a55d-2f1de6b0b0aa" {
		t.Fatalf("runID = %q", summary.RunID)
	}
	if !summary.RunFinished || summary.LastSequence != 23 {
		t.Fatalf("finished=%t lastSeq=%d, want true/23", summary.RunFinished, summary.LastSequence)
	}
	if summary.Total != 1 || summary.Succeeded != 1 || summary.Failed != 0 || summary.InputsCompleted != 1 {
		t.Fatalf("counters = %+v", summary)
	}
	if summary.BytesUploaded != 4367815 {
		t.Fatalf("bytesUploaded = %d, want 4367815", summary.BytesUploaded)
	}
}

// The protocol requires consumers to ignore unknown types and fields, and the
// operator must not choke on garbage lines from a corrupted stream.
func TestDecodeIngestStreamIgnoresUnknownAndMalformedLines(t *testing.T) {
	stream := `not json at all
{"protocol":"other.protocol","type":"run.finished","seq":9,"payload":{"succeeded":5}}
{"protocol":"tamsin.ingest.events","type":"future.event","seq":3,"run_id":"r","payload":{"new_field":true}}
{"protocol":"tamsin.ingest.events","type":"run.finished","seq":4,"run_id":"r","payload":{"outcome":"failed","exit_code":4,"total":2,"succeeded":0,"failed":2,"bytes_uploaded":0}}`
	summary := decodeIngestStream(strings.NewReader(stream))
	if summary.LastSequence != 4 || !summary.RunFinished {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.Succeeded != 0 || summary.Failed != 2 || summary.Total != 2 {
		t.Fatalf("counters = %+v, foreign protocol must not contribute", summary)
	}
}

// Exit code 4 means at least one input failed; only the stream can say whether
// any succeeded. All-failed must be Failed, mixed must stay PartiallySucceeded.
func TestIngestPhaseUsesStreamToDisambiguateExitFour(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := testIngestRun()
	job := ownedIngestJob(run)
	job.Status.Conditions = []batchv1.JobCondition{{
		Type: batchv1.JobFailed, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Now(),
	}}
	pod := terminatedIngestPod(job, 4)
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(job, pod).Build()

	allFailed := &ingestStreamSummary{RunFinished: true, Total: 2, Succeeded: 0, Failed: 2}
	phase, reason, _, _, err := ingestPhaseFromJob(ctx, reader, job, tamossv1alpha1.IngestRunResultStatus{}, time.Now(), allFailed)
	if err != nil {
		t.Fatalf("phase evaluation failed: %v", err)
	}
	if phase != tamossv1alpha1.IngestRunPhaseFailed || reason != "IngestFailed" {
		t.Fatalf("all-failed run got %q/%q, want Failed/IngestFailed", phase, reason)
	}

	mixed := &ingestStreamSummary{RunFinished: true, Total: 2, Succeeded: 1, Failed: 1}
	phase, reason, _, _, err = ingestPhaseFromJob(ctx, reader, job, tamossv1alpha1.IngestRunResultStatus{}, time.Now(), mixed)
	if err != nil {
		t.Fatalf("phase evaluation failed: %v", err)
	}
	if phase != tamossv1alpha1.IngestRunPhasePartiallySucceeded {
		t.Fatalf("mixed run got %q/%q, want PartiallySucceeded", phase, reason)
	}
}

type staticPodLogReader struct {
	stream string
	err    error
}

func (r staticPodLogReader) PodLogs(context.Context, string, string, string) (io.ReadCloser, error) {
	if r.err != nil {
		return nil, r.err
	}
	return io.NopCloser(strings.NewReader(r.stream)), nil
}

func terminatedIngestPod(job *batchv1.Job, exitCode int32) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      job.Name + "-pod",
			Namespace: job.Namespace,
			Labels:    map[string]string{"job-name": job.Name},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID,
			}},
		},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name: "tamsin",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: exitCode, FinishedAt: metav1.Now(),
			}},
		}}},
	}
}

// A completed Job's reconcile must record the stream's counters and identity
// on status, which is what the console's ingest page renders.
func TestIngestRunRecordsStreamOutcomeOnStatus(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := ingestRunWithRecordedJob(testIngestRun())
	job := ownedIngestJob(run)
	job.Status.Conditions = []batchv1.JobCondition{{
		Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Now(),
	}}
	job.Status.CompletionTime = &metav1.Time{Time: time.Now()}
	pod := terminatedIngestPod(job, 0)
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, testIngestTamoss(), job, pod).
		Build()
	reconciler := &IngestRunReconciler{
		Client:    k8sClient,
		Scheme:    scheme,
		APIReader: k8sClient,
		PodLogs:   staticPodLogReader{stream: sintelStream},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	reloaded := reloadIngestRun(t, ctx, k8sClient, run)
	if reloaded.Status.Phase != tamossv1alpha1.IngestRunPhaseSucceeded {
		t.Fatalf("phase = %q, want Succeeded", reloaded.Status.Phase)
	}
	progress := reloaded.Status.Progress
	if progress.InputsTotal != 1 || progress.InputsCompleted != 1 || progress.InputsSucceeded != 1 || progress.InputsFailed != 0 {
		t.Fatalf("progress = %+v, want 1/1 succeeded", progress)
	}
	if progress.BytesUploaded != 4367815 {
		t.Fatalf("bytesUploaded = %d, want 4367815", progress.BytesUploaded)
	}
	if reloaded.Status.TamsinRunID != "313c7f9a-7249-4c39-a55d-2f1de6b0b0aa" {
		t.Fatalf("tamsinRunId = %q", reloaded.Status.TamsinRunID)
	}
	if reloaded.Status.LastEventSequence != 23 {
		t.Fatalf("lastEventSequence = %d, want 23", reloaded.Status.LastEventSequence)
	}
}

// Collection is enrichment, never a gate: a garbage-collected Pod or an
// unreadable stream must not stop the run reaching its terminal phase.
func TestIngestRunTerminatesWhenStreamCollectionFails(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run := ingestRunWithRecordedJob(testIngestRun())
	job := ownedIngestJob(run)
	job.Status.Conditions = []batchv1.JobCondition{{
		Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Now(),
	}}
	job.Status.CompletionTime = &metav1.Time{Time: time.Now()}
	pod := terminatedIngestPod(job, 0)
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, testIngestTamoss(), job, pod).
		Build()
	reconciler := &IngestRunReconciler{
		Client:    k8sClient,
		Scheme:    scheme,
		APIReader: k8sClient,
		PodLogs:   staticPodLogReader{err: errors.New("logs rotated away")},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	reloaded := reloadIngestRun(t, ctx, k8sClient, run)
	if reloaded.Status.Phase != tamossv1alpha1.IngestRunPhaseSucceeded {
		t.Fatalf("phase = %q, want Succeeded despite failed collection", reloaded.Status.Phase)
	}
	if reloaded.Status.TamsinRunID != "" || reloaded.Status.Progress.BytesUploaded != 0 {
		t.Fatalf("status must stay unenriched when collection fails: %+v", reloaded.Status)
	}
}
