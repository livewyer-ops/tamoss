package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	"github.com/livewyer-ops/tamsin/ingestevent"
)

const testTAMSinRunID = "313c7f9a-7249-4c39-a55d-2f1de6b0b0aa"

const (
	testRootFlowID   = "713d391c-828a-513e-9929-65e1bab9c35b"
	testMemberFlowID = "69d2a402-b8db-5faa-a970-0aed4f2acfc2"
	testSourceID     = "6148f737-4536-5442-8897-c20b647e8836"
)

func succeededIngestSummary() *ingestStreamSummary {
	return &ingestStreamSummary{
		RunID: testTAMSinRunID, LastSequence: 6, Outcome: ingestevent.RunSucceeded,
		ExitCode: 0, RunFinished: true, Total: 1, Succeeded: 1, InputsCompleted: 1,
	}
}

func partialIngestSummary() *ingestStreamSummary {
	return &ingestStreamSummary{
		RunID: testTAMSinRunID, LastSequence: 9, Outcome: ingestevent.RunPartial,
		ExitCode: 4, RunFinished: true, Total: 2, Succeeded: 1, Failed: 1, InputsCompleted: 2,
	}
}

func testIngestEventStream(t *testing.T, outcome ingestevent.RunOutcome) string {
	t.Helper()
	var sink bytes.Buffer
	encoder, err := ingestevent.NewEncoder(&sink, testTAMSinRunID)
	if err != nil {
		t.Fatal(err)
	}
	emit := func(scope *ingestevent.Scope, event ingestevent.Event) {
		t.Helper()
		if _, err := encoder.Emit(scope, event); err != nil {
			t.Fatalf("emit %s: %v", event.EventType(), err)
		}
	}
	emit(nil, ingestevent.Hello{
		ToolVersion: "1.0.0-rc.1", ToolCommit: "d3cb6838", ResultSchemaVersion: "2.1",
		ProfilePolicyVersion: "1", MaxEventBytes: ingestevent.DefaultMaxEventBytes,
		Capabilities: []string{"terminal_results", "tams_flow_profiles"},
	})
	total := 1
	if outcome == ingestevent.RunPartial {
		total = 2
	}
	emit(nil, ingestevent.RunStarted{
		StartedAt: time.Now().UTC(), Profile: "essence-segments", ProfileVersion: "1",
		DryRunMode: "off", VerificationMode: "auto", RequestedInputs: ingestevent.KnownInputCount(uint64(total)),
	})
	sink.WriteString("{\"time\":\"2026-08-12T09:58:01Z\",\"level\":\"INFO\",\"msg\":\"preparing input\"}\n")
	for index := range total {
		emit(ingestevent.InputScope(index), ingestevent.InputDeclared{Input: "https://media.example.test/input.mp4"})
	}
	emit(nil, ingestevent.ManifestFinished{TotalInputs: uint64(total)})
	succeeded, failed := uint64(0), uint64(0)
	for index := range total {
		emit(ingestevent.InputScope(index), ingestevent.InputStarted{StartedAt: time.Now().UTC()})
		input := ingestevent.InputFinished{
			Input: "https://media.example.test/input.mp4", Profile: "essence-segments", ProfileVersion: "1",
			Verification: ingestevent.VerificationNotRequested,
		}
		if outcome == ingestevent.RunFailed || outcome == ingestevent.RunInterrupted || (outcome == ingestevent.RunPartial && index == total-1) {
			input.Status = ingestevent.InputFailed
			input.Verification = ingestevent.VerificationNotReached
			input.ErrorCode = ingestevent.DiagnosticCodeConfigInvalid
			input.Message = "Input failed."
			if outcome == ingestevent.RunInterrupted {
				input.ErrorCode = ingestevent.InputErrorCodeRunInterrupted
			}
			failed++
		} else {
			emit(ingestevent.FlowScope(index, testRootFlowID), ingestevent.FlowPlanned{
				FlowID: testRootFlowID, SourceID: testSourceID, Kind: ingestevent.FlowKindCollection,
				Root: true, Format: "urn:x-nmos:format:multi",
			})
			emit(ingestevent.FlowScope(index, testMemberFlowID), ingestevent.FlowPlanned{
				FlowID: testMemberFlowID, SourceID: testSourceID, Kind: ingestevent.FlowKindEssence,
				Role: "video", ParentFlowID: testRootFlowID, Format: "urn:x-nmos:format:video", Container: "video/mp2t",
			})
			emit(ingestevent.FlowScope(index, testRootFlowID), ingestevent.FlowResult{
				FlowID: testRootFlowID, SourceID: testSourceID, Kind: ingestevent.FlowKindCollection,
				Disposition: ingestevent.FlowWritten,
			})
			emit(ingestevent.FlowScope(index, testMemberFlowID), ingestevent.FlowResult{
				FlowID: testMemberFlowID, SourceID: testSourceID, Kind: ingestevent.FlowKindEssence,
				Role: "video", Disposition: ingestevent.FlowWritten,
			})
			input.Status = ingestevent.InputIngested
			input.RootFlowID = testRootFlowID
			input.FlowCount = 2
			succeeded++
		}
		emit(ingestevent.InputScope(index), input)
	}
	exitCode := 0
	if outcome == ingestevent.RunPartial || outcome == ingestevent.RunFailed {
		exitCode = 4
	}
	if outcome == ingestevent.RunInterrupted {
		emit(nil, ingestevent.RunCancellationRequested{Reason: ingestevent.CancellationSignal})
		exitCode = 8
	}
	emit(nil, ingestevent.RunFinished{
		Outcome: outcome, ExitCode: exitCode, Total: uint64(total), Succeeded: succeeded, Failed: failed,
		BytesUploaded: 4367815,
	})
	if err := encoder.Finalize(); err != nil {
		t.Fatal(err)
	}
	return sink.String()
}

func TestDecodeIngestStreamUsesPublishedProtocolReducer(t *testing.T) {
	summary, err := decodeIngestStream(strings.NewReader(testIngestEventStream(t, ingestevent.RunSucceeded)))
	if err != nil {
		t.Fatal(err)
	}
	if summary.RunID != testTAMSinRunID || !summary.RunFinished || summary.Outcome != ingestevent.RunSucceeded {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.Total != 1 || summary.Succeeded != 1 || summary.Failed != 0 || summary.InputsCompleted != 1 {
		t.Fatalf("counters = %+v", summary)
	}
	if summary.BytesUploaded != 4367815 || summary.LastSequence <= 0 {
		t.Fatalf("terminal facts = %+v", summary)
	}
	if summary.Output == nil || summary.Output.RootFlowID != testRootFlowID || summary.Output.SourceID != testSourceID {
		t.Fatalf("output = %+v", summary.Output)
	}
	if len(summary.Output.MemberFlows) != 1 || summary.Output.MemberFlows[0].ID != testMemberFlowID ||
		summary.Output.MemberFlows[0].Role != "video" || summary.Output.MemberFlows[0].Format != "urn:x-nmos:format:video" {
		t.Fatalf("member Flows = %+v", summary.Output.MemberFlows)
	}
}

func TestIngestOutputProjectionIsDeterministicAndBounded(t *testing.T) {
	input := &ingestevent.InputState{
		Finished: &ingestevent.InputFinished{RootFlowID: testRootFlowID},
		PlannedFlows: map[string]ingestevent.FlowPlanned{
			testRootFlowID: {FlowID: testRootFlowID, SourceID: testSourceID, Root: true},
		},
		FlowResults: map[string]ingestevent.FlowResult{
			testRootFlowID: {FlowID: testRootFlowID, SourceID: testSourceID},
		},
	}
	for index := range maxIngestOutputMemberFlows + 2 {
		flowID := fmt.Sprintf("00000000-0000-4000-8000-%012x", index+1)
		input.PlannedFlows[flowID] = ingestevent.FlowPlanned{FlowID: flowID, Format: "urn:x-nmos:format:video", Role: "video"}
		input.FlowResults[flowID] = ingestevent.FlowResult{FlowID: flowID, Role: "video"}
	}
	output := ingestOutputFromState(ingestevent.State{Inputs: map[int]*ingestevent.InputState{0: input}})
	if output == nil || !output.MemberFlowsTruncated || len(output.MemberFlows) != maxIngestOutputMemberFlows {
		t.Fatalf("output = %+v", output)
	}
	if output.MemberFlows[0].ID != "00000000-0000-4000-8000-000000000001" ||
		output.MemberFlows[len(output.MemberFlows)-1].ID != "00000000-0000-4000-8000-000000000010" {
		t.Fatalf("member order = %+v", output.MemberFlows)
	}
	if got := ingestOutputFromState(ingestevent.State{Inputs: map[int]*ingestevent.InputState{0: input, 1: {}}}); got != nil {
		t.Fatalf("multi-input output = %+v, want nil", got)
	}
}

func TestDecodeIngestStreamRejectsIncompleteAndMalformedStreams(t *testing.T) {
	complete := testIngestEventStream(t, ingestevent.RunSucceeded)
	lastRecord := strings.LastIndex(strings.TrimSuffix(complete, "\n"), "\n")
	for name, stream := range map[string]string{
		"incomplete": complete[:lastRecord+1],
		"malformed":  "not-json\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeIngestStream(strings.NewReader(stream)); !errors.Is(err, errIngestStreamInvalid) {
				t.Fatalf("error = %v, want invalid stream", err)
			}
		})
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
			Name: job.Name + "-pod", Namespace: job.Namespace, Labels: map[string]string{"job-name": job.Name},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID}},
		},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name: "tamsin", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: exitCode, FinishedAt: metav1.Now(),
			}},
		}}},
	}
}

func terminalIngestFixture(t *testing.T, exitCode int32) (*tamossv1alpha1.IngestRun, *batchv1.Job, *corev1.Pod) {
	t.Helper()
	run := ingestRunWithRecordedJob(testIngestRun())
	job := ownedIngestJob(run)
	conditionType := batchv1.JobComplete
	if exitCode != 0 {
		conditionType = batchv1.JobFailed
	}
	job.Status.Conditions = []batchv1.JobCondition{{Type: conditionType, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Now()}}
	job.Status.CompletionTime = &metav1.Time{Time: time.Now()}
	return run, job, terminatedIngestPod(job, exitCode)
}

func TestIngestRunRecordsProtocolConfirmedOutcomeOnStatus(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run, job, pod := terminalIngestFixture(t, 0)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, testIngestTamoss(), job, pod).Build()
	reconciler := &IngestRunReconciler{
		Client: k8sClient, Scheme: scheme, APIReader: k8sClient,
		PodLogs: staticPodLogReader{stream: testIngestEventStream(t, ingestevent.RunSucceeded)},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatal(err)
	}
	reloaded := reloadIngestRun(t, ctx, k8sClient, run)
	if reloaded.Status.Phase != tamossv1alpha1.IngestRunPhaseSucceeded || reloaded.Status.TamsinRunID != testTAMSinRunID {
		t.Fatalf("status = %+v", reloaded.Status)
	}
	if reloaded.Status.Progress.InputsSucceeded != 1 || reloaded.Status.Progress.BytesUploaded != 4367815 {
		t.Fatalf("progress = %+v", reloaded.Status.Progress)
	}
	if reloaded.Status.Output == nil || reloaded.Status.Output.RootFlowID != testRootFlowID || len(reloaded.Status.Output.MemberFlows) != 1 {
		t.Fatalf("output = %+v", reloaded.Status.Output)
	}
}

func TestIngestRunRejectsExitCodeMismatch(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run, job, pod := terminalIngestFixture(t, 4)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, testIngestTamoss(), job, pod).Build()
	reconciler := &IngestRunReconciler{
		Client: k8sClient, Scheme: scheme, APIReader: k8sClient,
		PodLogs: staticPodLogReader{stream: testIngestEventStream(t, ingestevent.RunSucceeded)},
	}
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatal(err)
	}
	reloaded := reloadIngestRun(t, ctx, k8sClient, run)
	if reloaded.Status.Phase != tamossv1alpha1.IngestRunPhaseFailed || conditionReason(reloaded.Status.Conditions, ingestRunReadyCondition) != "IngestProtocolInvalid" {
		t.Fatalf("status = %+v", reloaded.Status)
	}
}

func TestIngestRunWaitsForUnavailableStreamThenFailsClosed(t *testing.T) {
	ctx := context.Background()
	scheme := ingestRunTestScheme(t)
	run, job, pod := terminalIngestFixture(t, 0)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&tamossv1alpha1.IngestRun{}, &tamossv1alpha1.Tamoss{}).
		WithObjects(run, testIngestTamoss(), job, pod).Build()
	reconciler := &IngestRunReconciler{
		Client: k8sClient, Scheme: scheme, APIReader: k8sClient,
		PodLogs: staticPodLogReader{err: errors.New("logs not ready")},
	}
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatal(err)
	}
	reloaded := reloadIngestRun(t, ctx, k8sClient, run)
	if reloaded.Status.Phase != tamossv1alpha1.IngestRunPhaseRunning || conditionReason(reloaded.Status.Conditions, ingestRunReadyCondition) != "IngestResultPending" {
		t.Fatalf("pending status = %+v", reloaded.Status)
	}

	job.Status.CompletionTime = &metav1.Time{Time: time.Now().Add(-2 * ingestTerminalObservationDeadline)}
	if err := k8sClient.Status().Update(ctx, job); err != nil {
		t.Fatal(err)
	}
	reconciler.now = func() time.Time { return time.Now() }
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatal(err)
	}
	reloaded = reloadIngestRun(t, ctx, k8sClient, run)
	if reloaded.Status.Phase != tamossv1alpha1.IngestRunPhaseFailed || conditionReason(reloaded.Status.Conditions, ingestRunReadyCondition) != "IngestResultUnavailable" {
		t.Fatalf("terminal status = %+v", reloaded.Status)
	}
}

func conditionReason(conditions []metav1.Condition, conditionType string) string {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Reason
		}
	}
	return ""
}
