package controller

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	// ingestEventMaxLineBytes is double Tamsin's published max_event_bytes, so
	// a compliant stream always fits and an overlong line ends collection
	// rather than the reconcile.
	ingestEventMaxLineBytes = 2 << 20
	// ingestStreamMaxBytes bounds how much of a Pod's output the operator will
	// read. Progress snapshots are suppressed in the rendered Job, so a stream
	// anywhere near this size is malformed, not busy.
	ingestStreamMaxBytes = 64 << 20
)

// IngestPodLogReader fetches a container's log stream. Split from the
// controller-runtime client because that client cannot read subresource
// streams; the manager wires a plain clientset adapter.
type IngestPodLogReader interface {
	PodLogs(ctx context.Context, namespace, pod, container string) (io.ReadCloser, error)
}

// NewIngestPodLogReader builds the clientset-backed log reader. The
// controller-runtime client cannot stream the log subresource, so this is the
// one place the operator holds a plain clientset.
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

// ingestStreamSummary is what the operator keeps from a Tamsin event stream:
// the durable identity and outcome counters, never free-form text. Messages
// on the stream are Tamsin's to publish and the Console's to fetch elsewhere;
// projecting them into status would smuggle unbounded text into the API.
type ingestStreamSummary struct {
	RunID           string
	LastSequence    int64
	RunFinished     bool
	Total           int32
	Succeeded       int32
	Failed          int32
	InputsCompleted int32
	BytesUploaded   int64
}

// decodeIngestStream reduces a tamsin.ingest.events v1 NDJSON stream to the
// counters recorded on IngestRun status. It consumes the published upstream
// protocol directly: unknown event types and fields are ignored, as the
// protocol requires of consumers within major version 1.
func decodeIngestStream(reader io.Reader) ingestStreamSummary {
	summary := ingestStreamSummary{}
	scanner := bufio.NewScanner(io.LimitReader(reader, ingestStreamMaxBytes))
	scanner.Buffer(make([]byte, 64<<10), ingestEventMaxLineBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event struct {
			Protocol string `json:"protocol"`
			Type     string `json:"type"`
			Sequence int64  `json:"seq"`
			RunID    string `json:"run_id"`
			Payload  struct {
				Status        string `json:"status"`
				Total         int32  `json:"total"`
				Succeeded     int32  `json:"succeeded"`
				Failed        int32  `json:"failed"`
				BytesUploaded int64  `json:"bytes_uploaded"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.Protocol != "tamsin.ingest.events" {
			continue
		}
		if summary.RunID == "" {
			summary.RunID = event.RunID
		}
		if event.Sequence > summary.LastSequence {
			summary.LastSequence = event.Sequence
		}
		switch event.Type {
		case "input.finished":
			// Every declared input gets exactly one terminal event, so this
			// count is completion; run.finished carries the verdict split.
			summary.InputsCompleted++
		case "run.finished":
			summary.RunFinished = true
			summary.Total = event.Payload.Total
			summary.Succeeded = event.Payload.Succeeded
			summary.Failed = event.Payload.Failed
			summary.BytesUploaded = event.Payload.BytesUploaded
		}
	}
	return summary
}

// collectIngestStream reads the finished Tamsin Pod's event stream and reduces
// it. Collection is enrichment, never a gate: any failure returns nil and the
// run still reaches its terminal phase on the Job's own outcome, because a
// garbage-collected Pod must not reopen the wedge this controller removed.
func (r *IngestRunReconciler) collectIngestStream(ctx context.Context, run *tamossv1alpha1.IngestRun, job *batchv1.Job) *ingestStreamSummary {
	if r.PodLogs == nil {
		return nil
	}
	podName := r.terminalIngestPodName(ctx, job)
	if podName == "" {
		return nil
	}
	stream, err := r.PodLogs.PodLogs(ctx, job.Namespace, podName, "tamsin")
	if err != nil {
		// Collection failing must be visible at the default log level: the
		// run still terminates, but its counters silently staying empty took
		// a debug rollout to explain when this hid behind V(1).
		log.FromContext(ctx).Info("unable to read the Tamsin event stream; run counters will stay empty", "ingestRun", run.Name, "pod", podName, "error", err.Error())
		return nil
	}
	defer func() { _ = stream.Close() }()
	summary := decodeIngestStream(stream)
	if summary.LastSequence == 0 && summary.RunID == "" {
		return nil
	}
	return &summary
}

// terminalIngestPodName selects the Pod carrying the run's final stream: the
// owned Pod whose tamsin container terminated last, matching how the exit
// code is read.
func (r *IngestRunReconciler) terminalIngestPodName(ctx context.Context, job *batchv1.Job) string {
	pods := &corev1.PodList{}
	if err := r.Client.List(ctx, pods, client.InNamespace(job.Namespace), client.MatchingLabels{"job-name": job.Name}); err != nil {
		return ""
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
	return name
}

// applyIngestStreamSummary records the collected counters. inputsTotal from
// the stream wins over the resolver's expectation because it is what Tamsin
// actually processed.
func applyIngestStreamSummary(status *tamossv1alpha1.IngestRunStatus, summary *ingestStreamSummary) {
	if summary == nil {
		return
	}
	if summary.RunID != "" {
		status.TamsinRunID = summary.RunID
	}
	if summary.LastSequence > status.LastEventSequence {
		status.LastEventSequence = summary.LastSequence
	}
	if summary.Total > 0 {
		status.Progress.InputsTotal = summary.Total
	}
	status.Progress.InputsCompleted = summary.InputsCompleted
	if summary.RunFinished {
		status.Progress.InputsSucceeded = summary.Succeeded
		status.Progress.InputsFailed = summary.Failed
		status.Progress.BytesUploaded = summary.BytesUploaded
	}
}
