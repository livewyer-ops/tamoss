package controller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamsin/ingestevent"
)

const (
	ingestStreamMaxLineBytes   = 2 << 20
	maxIngestOutputMemberFlows = 16
	// Progress snapshots are suppressed in the rendered Job, so a stream near
	// this bound is malformed rather than merely busy.
	ingestStreamMaxBytes = 64 << 20
)

var (
	errIngestStreamUnavailable = errors.New("TAMSin event stream is unavailable")
	errIngestStreamInvalid     = errors.New("TAMSin event stream is invalid")
)

// IngestPodLogReader fetches a container's log stream. Split from the
// controller-runtime client because that client cannot read subresource
// streams; the manager wires a plain clientset adapter.
type IngestPodLogReader interface {
	PodLogs(ctx context.Context, namespace, pod, container string) (io.ReadCloser, error)
}

// NewIngestPodLogReader builds the clientset-backed log reader.
func NewIngestPodLogReader(config *rest.Config) (IngestPodLogReader, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("build ingest log reader: %w", err)
	}
	return clientsetPodLogReader{clientset: clientset}, nil
}

type clientsetPodLogReader struct {
	clientset kubernetes.Interface
}

func (r clientsetPodLogReader) PodLogs(ctx context.Context, namespace, pod, container string) (io.ReadCloser, error) {
	return r.clientset.CoreV1().
		Pods(namespace).
		GetLogs(pod, &corev1.PodLogOptions{Container: container}).
		Stream(ctx)
}

// ingestStreamSummary is the bounded identity and terminal outcome retained
// from TAMSin's validated event stream. Free-form diagnostics and input
// locations never enter Kubernetes status.
type ingestStreamSummary struct {
	RunID           string
	LastSequence    int64
	Outcome         ingestevent.RunOutcome
	ExitCode        int32
	RunFinished     bool
	Total           int32
	Succeeded       int32
	Failed          int32
	InputsCompleted int32
	BytesUploaded   int64
	Output          *tamossv1alpha1.IngestRunOutputStatus
}

func decodeIngestStream(reader io.Reader) (*ingestStreamSummary, error) {
	limited := &io.LimitedReader{R: reader, N: ingestStreamMaxBytes + 1}
	events, err := extractIngestProtocolEvents(limited)
	if err != nil {
		return nil, err
	}
	if limited.N == 0 {
		return nil, fmt.Errorf("%w: stream exceeds %d bytes", errIngestStreamInvalid, ingestStreamMaxBytes)
	}
	state, err := ingestevent.Reduce(events)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errIngestStreamInvalid, err)
	}
	if state.Finished == nil || state.NextSequence == 0 {
		return nil, fmt.Errorf("%w: stream has no terminal event", errIngestStreamInvalid)
	}
	finished := state.Finished
	if state.NextSequence-1 > math.MaxInt64 || finished.Total > math.MaxInt32 ||
		finished.Succeeded > math.MaxInt32 || finished.Failed > math.MaxInt32 ||
		finished.BytesUploaded > math.MaxInt64 || len(state.Inputs) > math.MaxInt32 ||
		finished.ExitCode < math.MinInt32 || finished.ExitCode > math.MaxInt32 {
		return nil, fmt.Errorf("%w: terminal counters exceed status bounds", errIngestStreamInvalid)
	}
	completed := 0
	for _, input := range state.Inputs {
		if input.Finished != nil {
			completed++
		}
	}
	return &ingestStreamSummary{
		RunID: state.RunID,
		// #nosec G115 -- the protocol value is bounded to MaxInt64 above.
		LastSequence: int64(state.NextSequence - 1),
		Outcome:      finished.Outcome,
		// #nosec G115 -- the protocol value is bounded to int32 above.
		ExitCode:        int32(finished.ExitCode),
		RunFinished:     true,
		Total:           int32(finished.Total),
		Succeeded:       int32(finished.Succeeded),
		Failed:          int32(finished.Failed),
		InputsCompleted: int32(completed),
		BytesUploaded:   int64(finished.BytesUploaded),
		Output:          ingestOutputFromState(state),
	}, nil
}

func ingestOutputFromState(state ingestevent.State) *tamossv1alpha1.IngestRunOutputStatus {
	if len(state.Inputs) != 1 {
		return nil
	}
	input := state.Inputs[0]
	if input == nil || input.Finished == nil || input.Finished.RootFlowID == "" {
		return nil
	}
	root, found := input.FlowResults[input.Finished.RootFlowID]
	if !found {
		return nil
	}
	flowIDs := make([]string, 0, len(input.FlowResults)-1)
	for flowID := range input.FlowResults {
		if flowID != input.Finished.RootFlowID {
			flowIDs = append(flowIDs, flowID)
		}
	}
	sort.Strings(flowIDs)
	output := &tamossv1alpha1.IngestRunOutputStatus{
		RootFlowID: input.Finished.RootFlowID,
		SourceID:   root.SourceID,
	}
	if len(flowIDs) > maxIngestOutputMemberFlows {
		output.MemberFlowsTruncated = true
		flowIDs = flowIDs[:maxIngestOutputMemberFlows]
	}
	output.MemberFlows = make([]tamossv1alpha1.IngestRunOutputFlowStatus, 0, len(flowIDs))
	for _, flowID := range flowIDs {
		result := input.FlowResults[flowID]
		planned := input.PlannedFlows[flowID]
		role := result.Role
		if role == "" {
			role = planned.Role
		}
		output.MemberFlows = append(output.MemberFlows, tamossv1alpha1.IngestRunOutputFlowStatus{
			ID: flowID, Format: planned.Format, Role: role,
		})
	}
	return output
}

// extractIngestProtocolEvents separates TAMSin protocol records from the
// ordinary structured log records merged into the Kubernetes Pod log stream.
// Every record that declares a protocol is retained so the upstream reducer,
// not this transport adapter, decides whether it is valid or compatible.
func extractIngestProtocolEvents(reader io.Reader) (io.Reader, error) {
	var events bytes.Buffer
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), ingestStreamMaxLineBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var record map[string]json.RawMessage
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		if _, declaresProtocol := record["protocol"]; !declaresProtocol {
			continue
		}
		events.Write(line)
		events.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: scan merged Pod logs: %v", errIngestStreamInvalid, err)
	}
	return bytes.NewReader(events.Bytes()), nil
}

// collectIngestStream reads and validates the terminal TAMSin Pod's event
// stream. Transport absence may be transient; a malformed or exit-code-
// mismatched complete stream is terminally invalid.
func (r *IngestRunReconciler) collectIngestStream(ctx context.Context, job *batchv1.Job) (*ingestStreamSummary, error) {
	if r.PodLogs == nil {
		return nil, errIngestStreamUnavailable
	}
	podName, exitCode, found, err := terminalIngestPodResult(ctx, r.Client, job)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errIngestStreamUnavailable
	}
	stream, err := r.PodLogs.PodLogs(ctx, job.Namespace, podName, "tamsin")
	if err != nil {
		return nil, fmt.Errorf("%w: read terminal Pod log", errIngestStreamUnavailable)
	}
	defer func() { _ = stream.Close() }()
	summary, err := decodeIngestStream(stream)
	if err != nil {
		return nil, err
	}
	if summary.ExitCode != exitCode {
		return nil, fmt.Errorf("%w: run.finished exit_code %d does not match container exit code %d", errIngestStreamInvalid, summary.ExitCode, exitCode)
	}
	return summary, nil
}

// terminalIngestPodResult selects the owned Pod whose TAMSin container
// terminated last, matching the Job attempt whose stream determines outcome.
func terminalIngestPodResult(ctx context.Context, reader client.Reader, job *batchv1.Job) (string, int32, bool, error) {
	pods := &corev1.PodList{}
	if err := reader.List(ctx, pods, client.InNamespace(job.Namespace), client.MatchingLabels{"job-name": job.Name}); err != nil {
		return "", 0, false, fmt.Errorf("list terminal Pods for Job %s/%s: %w", job.Namespace, job.Name, err)
	}
	var name string
	var selected *corev1.ContainerStateTerminated
	for i := range pods.Items {
		if job.UID == "" || !podOwnedByJobUID(&pods.Items[i], job.UID) {
			continue
		}
		for _, status := range pods.Items[i].Status.ContainerStatuses {
			terminated := status.State.Terminated
			if status.Name != "tamsin" || terminated == nil {
				continue
			}
			if selected == nil || terminated.FinishedAt.After(selected.FinishedAt.Time) {
				selected = terminated
				name = pods.Items[i].Name
			}
		}
	}
	if selected == nil {
		return "", 0, false, nil
	}
	return name, selected.ExitCode, true, nil
}

func applyIngestStreamSummary(status *tamossv1alpha1.IngestRunStatus, summary *ingestStreamSummary) {
	if summary == nil {
		return
	}
	status.TamsinRunID = summary.RunID
	if summary.LastSequence > status.LastEventSequence {
		status.LastEventSequence = summary.LastSequence
	}
	status.Progress.InputsTotal = summary.Total
	status.Progress.InputsCompleted = summary.InputsCompleted
	status.Progress.InputsSucceeded = summary.Succeeded
	status.Progress.InputsFailed = summary.Failed
	status.Progress.BytesUploaded = summary.BytesUploaded
	status.Output = summary.Output.DeepCopy()
}
