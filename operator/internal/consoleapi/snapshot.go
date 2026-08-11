package consoleapi

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	instanceLabel          = "app.kubernetes.io/instance"
	componentLabel         = "app.kubernetes.io/component"
	apiComponent           = "api"
	maxWorkloads           = 32
	maxServices            = 64
	maxServicePorts        = 16
	maxEndpointSlices      = 128
	maxEndpointSlicePorts  = 16
	maxPods                = 200
	maxJobs                = 100
	maxEvents              = 50
	maxConditions          = 32
	maxKubernetesTextRunes = 1024
)

type SnapshotSource interface {
	Snapshot(context.Context) (RuntimeSnapshot, error)
}

type Snapshotter struct {
	reader    client.Reader
	namespace string
	instance  string
	now       func() time.Time
}

func NewSnapshotter(reader client.Reader, namespace, instance string) (*Snapshotter, error) {
	if reader == nil {
		return nil, fmt.Errorf("kubernetes reader is required")
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	instance = strings.TrimSpace(instance)
	if instance == "" {
		return nil, fmt.Errorf("instance is required")
	}
	return &Snapshotter{
		reader:    reader,
		namespace: namespace,
		instance:  instance,
		now:       time.Now,
	}, nil
}

func (s *Snapshotter) Snapshot(ctx context.Context) (RuntimeSnapshot, error) {
	tamoss := &tamossv1alpha1.Tamoss{}
	key := client.ObjectKey{Namespace: s.namespace, Name: s.instance}
	if err := s.reader.Get(ctx, key, tamoss); err != nil {
		if apierrors.IsNotFound(err) {
			return RuntimeSnapshot{}, fmt.Errorf("tamoss %s/%s not found", s.namespace, s.instance)
		}
		return RuntimeSnapshot{}, fmt.Errorf("read Tamoss %s/%s: %w", s.namespace, s.instance, err)
	}

	// The instance label is a discovery boundary, not ownership proof. Console
	// remains opt-in while ReplicaSet/IngestRun owner-chain validation is an
	// explicit release gate for exposing this API beyond development installs.
	selector := client.MatchingLabels{instanceLabel: s.instance}
	deployments := &appsv1.DeploymentList{}
	if err := s.reader.List(ctx, deployments, client.InNamespace(s.namespace), selector); err != nil {
		return RuntimeSnapshot{}, fmt.Errorf("list Deployments: %w", err)
	}
	services := &corev1.ServiceList{}
	if err := s.reader.List(ctx, services, client.InNamespace(s.namespace), selector); err != nil {
		return RuntimeSnapshot{}, fmt.Errorf("list Services: %w", err)
	}
	endpointSlices := &discoveryv1.EndpointSliceList{}
	if err := s.reader.List(ctx, endpointSlices, client.InNamespace(s.namespace), selector); err != nil {
		return RuntimeSnapshot{}, fmt.Errorf("list EndpointSlices: %w", err)
	}
	pods := &corev1.PodList{}
	if err := s.reader.List(ctx, pods, client.InNamespace(s.namespace), selector); err != nil {
		return RuntimeSnapshot{}, fmt.Errorf("list Pods: %w", err)
	}
	jobs := &batchv1.JobList{}
	if err := s.reader.List(ctx, jobs, client.InNamespace(s.namespace), selector); err != nil {
		return RuntimeSnapshot{}, fmt.Errorf("list Jobs: %w", err)
	}
	events := &corev1.EventList{}
	if err := s.reader.List(ctx, events, client.InNamespace(s.namespace)); err != nil {
		return RuntimeSnapshot{}, fmt.Errorf("list Events: %w", err)
	}

	snapshot := RuntimeSnapshot{
		SchemaVersion:  RuntimeSchemaVersion,
		ObservedAt:     s.now().UTC().Format(time.RFC3339Nano),
		Instance:       instanceFromTamoss(tamoss),
		Workloads:      workloadsFromDeployments(deployments.Items),
		Services:       servicesFromKubernetes(services.Items),
		EndpointSlices: endpointSlicesFromKubernetes(endpointSlices.Items),
		Pods:           podsFromKubernetes(pods.Items),
		Jobs:           jobsFromKubernetes(jobs.Items),
	}
	snapshot.Events = relevantEvents(
		events.Items,
		tamoss,
		deployments.Items,
		services.Items,
		endpointSlices.Items,
		pods.Items,
		jobs.Items,
	)
	return snapshot, nil
}

func instanceFromTamoss(tamoss *tamossv1alpha1.Tamoss) Instance {
	conditions := make([]InstanceCondition, 0, len(tamoss.Status.Conditions))
	for _, condition := range tamoss.Status.Conditions {
		conditions = append(conditions, InstanceCondition{
			Type:               condition.Type,
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            boundedKubernetesText(condition.Message),
			ObservedGeneration: condition.ObservedGeneration,
			LastTransitionTime: formatTime(condition.LastTransitionTime.Time),
		})
	}
	sort.Slice(conditions, func(i, j int) bool { return conditions[i].Type < conditions[j].Type })
	conditions = capSlice(conditions, maxConditions)
	return Instance{
		Name:               tamoss.Name,
		Namespace:          tamoss.Namespace,
		UID:                string(tamoss.UID),
		Generation:         tamoss.Generation,
		ObservedGeneration: tamoss.Status.ObservedGeneration,
		Phase:              tamoss.Status.Phase,
		Conditions:         conditions,
	}
}

func workloadsFromDeployments(items []appsv1.Deployment) []Workload {
	workloads := make([]Workload, 0, len(items))
	for i := range items {
		deployment := &items[i]
		desired := deployment.Status.Replicas
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		conditions := make([]ResourceCondition, 0, len(deployment.Status.Conditions))
		for _, condition := range deployment.Status.Conditions {
			conditions = append(conditions, ResourceCondition{
				Type:               string(condition.Type),
				Status:             string(condition.Status),
				Reason:             condition.Reason,
				Message:            boundedKubernetesText(condition.Message),
				LastTransitionTime: formatTime(condition.LastTransitionTime.Time),
			})
		}
		sort.Slice(conditions, func(i, j int) bool { return conditions[i].Type < conditions[j].Type })
		conditions = capSlice(conditions, maxConditions)
		workloads = append(workloads, Workload{
			Kind:               "Deployment",
			Name:               deployment.Name,
			Component:          deployment.Labels[componentLabel],
			Status:             deploymentStatus(deployment, desired),
			Generation:         deployment.Generation,
			ObservedGeneration: deployment.Status.ObservedGeneration,
			DesiredReplicas:    desired,
			ReadyReplicas:      deployment.Status.ReadyReplicas,
			AvailableReplicas:  deployment.Status.AvailableReplicas,
			UpdatedReplicas:    deployment.Status.UpdatedReplicas,
			Conditions:         conditions,
		})
	}
	sort.Slice(workloads, func(i, j int) bool { return workloads[i].Name < workloads[j].Name })
	return capSlice(workloads, maxWorkloads)
}

func deploymentStatus(deployment *appsv1.Deployment, desired int32) string {
	if desired == 0 {
		return "scaledDown"
	}
	if deployment.Status.ObservedGeneration < deployment.Generation || deployment.Status.UpdatedReplicas < desired {
		return "progressing"
	}
	if deployment.Status.ReadyReplicas >= desired && deployment.Status.AvailableReplicas >= desired {
		return "ready"
	}
	if deployment.Status.ReadyReplicas > 0 || deployment.Status.AvailableReplicas > 0 {
		return "progressing"
	}
	return "unavailable"
}

func servicesFromKubernetes(items []corev1.Service) []Service {
	services := make([]Service, 0, len(items))
	for i := range items {
		service := &items[i]
		ports := make([]ServicePort, 0, min(len(service.Spec.Ports), maxServicePorts))
		for _, port := range capSlice(service.Spec.Ports, maxServicePorts) {
			ports = append(ports, ServicePort{
				Name:       boundedKubernetesText(port.Name),
				Protocol:   boundedKubernetesText(string(port.Protocol)),
				Port:       port.Port,
				TargetPort: boundedKubernetesText(port.TargetPort.String()),
			})
		}
		services = append(services, Service{
			Name:              boundedKubernetesText(service.Name),
			Component:         boundedKubernetesText(service.Labels[componentLabel]),
			Type:              boundedKubernetesText(string(service.Spec.Type)),
			SelectorComponent: boundedKubernetesText(service.Spec.Selector[componentLabel]),
			Ports:             ports,
		})
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return capSlice(services, maxServices)
}

func endpointSlicesFromKubernetes(items []discoveryv1.EndpointSlice) []EndpointSlice {
	slices := make([]EndpointSlice, 0, len(items))
	for i := range items {
		slice := &items[i]
		ports := make([]EndpointSlicePort, 0, min(len(slice.Ports), maxEndpointSlicePorts))
		for _, port := range capSlice(slice.Ports, maxEndpointSlicePorts) {
			projected := EndpointSlicePort{}
			if port.Name != nil {
				projected.Name = boundedKubernetesText(*port.Name)
			}
			if port.Protocol != nil {
				projected.Protocol = boundedKubernetesText(string(*port.Protocol))
			}
			if port.Port != nil {
				projected.Port = *port.Port
			}
			ports = append(ports, projected)
		}

		projected := EndpointSlice{
			Name:           boundedKubernetesText(slice.Name),
			ServiceName:    boundedKubernetesText(slice.Labels[discoveryv1.LabelServiceName]),
			Component:      boundedKubernetesText(slice.Labels[componentLabel]),
			AddressType:    boundedKubernetesText(string(slice.AddressType)),
			Ports:          ports,
			TotalEndpoints: int32(len(slice.Endpoints)), //nolint:gosec // Kubernetes bounds EndpointSlice size far below MaxInt32.
		}
		for _, endpoint := range slice.Endpoints {
			// EndpointSlice defines an omitted ready condition as ready.
			if endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready {
				projected.ReadyEndpoints++
			} else {
				projected.NotReadyEndpoints++
			}
			if endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating {
				projected.TerminatingEndpoints++
			}
		}
		slices = append(slices, projected)
	}
	sort.Slice(slices, func(i, j int) bool {
		leftUnhealthy := slices[i].TotalEndpoints == 0 || slices[i].NotReadyEndpoints > 0 || slices[i].TerminatingEndpoints > 0
		rightUnhealthy := slices[j].TotalEndpoints == 0 || slices[j].NotReadyEndpoints > 0 || slices[j].TerminatingEndpoints > 0
		if leftUnhealthy != rightUnhealthy {
			return leftUnhealthy
		}
		if slices[i].ServiceName != slices[j].ServiceName {
			return slices[i].ServiceName < slices[j].ServiceName
		}
		return slices[i].Name < slices[j].Name
	})
	return capSlice(slices, maxEndpointSlices)
}

func podsFromKubernetes(items []corev1.Pod) []Pod {
	selected := append([]corev1.Pod(nil), items...)
	sort.Slice(selected, func(i, j int) bool {
		leftPriority := podRetentionPriority(&selected[i])
		rightPriority := podRetentionPriority(&selected[j])
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		leftObserved := podLastObserved(&selected[i])
		rightObserved := podLastObserved(&selected[j])
		if !leftObserved.Equal(rightObserved) {
			return leftObserved.After(rightObserved)
		}
		return selected[i].Name < selected[j].Name
	})
	selected = capSlice(selected, maxPods)

	pods := make([]Pod, 0, len(selected))
	for i := range selected {
		pod := &selected[i]
		restarts := int32(0)
		for _, container := range pod.Status.InitContainerStatuses {
			restarts += container.RestartCount
		}
		for _, container := range pod.Status.ContainerStatuses {
			restarts += container.RestartCount
		}
		reason, message := podReason(pod)
		startedAt := ""
		if pod.Status.StartTime != nil {
			startedAt = formatTime(pod.Status.StartTime.Time)
		}
		pods = append(pods, Pod{
			Name:      pod.Name,
			Component: pod.Labels[componentLabel],
			Phase:     podPhase(pod),
			Ready:     podReady(pod),
			Restarts:  restarts,
			Reason:    reason,
			Message:   boundedKubernetesText(message),
			StartedAt: startedAt,
			Deleting:  pod.DeletionTimestamp != nil,
		})
	}
	return pods
}

func podRetentionPriority(pod *corev1.Pod) int {
	if pod.DeletionTimestamp != nil {
		return 0
	}
	if pod.Status.Phase == corev1.PodFailed || (!podReady(pod) && pod.Status.Phase != corev1.PodSucceeded) {
		return 1
	}
	switch pod.Labels[componentLabel] {
	case apiComponent, "console", "ui", "worker":
		return 2
	}
	switch pod.Status.Phase {
	case corev1.PodPending, corev1.PodRunning, corev1.PodUnknown:
		return 3
	case corev1.PodSucceeded:
		return 5
	default:
		return 4
	}
}

func podLastObserved(pod *corev1.Pod) time.Time {
	if pod.Status.StartTime != nil {
		return pod.Status.StartTime.Time
	}
	return pod.CreationTimestamp.Time
}

func podPhase(pod *corev1.Pod) string {
	if pod.DeletionTimestamp != nil {
		return "Terminating"
	}
	if pod.Status.Phase == "" {
		return string(corev1.PodUnknown)
	}
	return string(pod.Status.Phase)
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func podReason(pod *corev1.Pod) (string, string) {
	if pod.Status.Reason != "" || pod.Status.Message != "" {
		return pod.Status.Reason, pod.Status.Message
	}
	statuses := append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...)
	statuses = append(statuses, pod.Status.ContainerStatuses...)
	for _, status := range statuses {
		if status.State.Waiting != nil && (status.State.Waiting.Reason != "" || status.State.Waiting.Message != "") {
			return status.State.Waiting.Reason, status.State.Waiting.Message
		}
		if status.State.Terminated != nil && status.State.Terminated.ExitCode != 0 {
			return status.State.Terminated.Reason, status.State.Terminated.Message
		}
	}
	return "", ""
}

func jobsFromKubernetes(items []batchv1.Job) []Job {
	selected := append([]batchv1.Job(nil), items...)
	sort.Slice(selected, func(i, j int) bool {
		left, right := jobLastObserved(&selected[i]), jobLastObserved(&selected[j])
		if left.Equal(right) {
			return selected[i].Name < selected[j].Name
		}
		return left.After(right)
	})
	selected = capSlice(selected, maxJobs)
	jobs := make([]Job, 0, len(selected))
	for i := range selected {
		job := &selected[i]
		conditions := make([]ResourceCondition, 0, len(job.Status.Conditions))
		for _, condition := range job.Status.Conditions {
			conditions = append(conditions, ResourceCondition{
				Type:               string(condition.Type),
				Status:             string(condition.Status),
				Reason:             condition.Reason,
				Message:            boundedKubernetesText(condition.Message),
				LastTransitionTime: formatTime(condition.LastTransitionTime.Time),
			})
		}
		sort.Slice(conditions, func(i, j int) bool { return conditions[i].Type < conditions[j].Type })
		conditions = capSlice(conditions, maxConditions)
		result := Job{
			Name:       job.Name,
			Component:  job.Labels[componentLabel],
			Status:     jobStatus(job),
			Active:     job.Status.Active,
			Succeeded:  job.Status.Succeeded,
			Failed:     job.Status.Failed,
			Conditions: conditions,
		}
		if job.Status.StartTime != nil {
			result.StartTime = formatTime(job.Status.StartTime.Time)
		}
		if job.Status.CompletionTime != nil {
			result.CompletionTime = formatTime(job.Status.CompletionTime.Time)
		}
		jobs = append(jobs, result)
	}
	return jobs
}

func jobLastObserved(job *batchv1.Job) time.Time {
	if job.Status.CompletionTime != nil {
		return job.Status.CompletionTime.Time
	}
	if job.Status.StartTime != nil {
		return job.Status.StartTime.Time
	}
	return job.CreationTimestamp.Time
}

func jobStatus(job *batchv1.Job) string {
	if job.Spec.Suspend != nil && *job.Spec.Suspend {
		return "suspended"
	}
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			return "succeeded"
		case batchv1.JobFailed:
			return "failed"
		}
	}
	if job.Status.Active > 0 {
		return "running"
	}
	return "pending"
}

func relevantEvents(
	events []corev1.Event,
	tamoss *tamossv1alpha1.Tamoss,
	deployments []appsv1.Deployment,
	services []corev1.Service,
	endpointSlices []discoveryv1.EndpointSlice,
	pods []corev1.Pod,
	jobs []batchv1.Job,
) []KubernetesEvent {
	objects := map[types.UID]struct{}{}
	names := map[string]struct{}{eventObjectKey("Tamoss", tamoss.Name): {}}
	if tamoss.UID != "" {
		objects[tamoss.UID] = struct{}{}
	}
	for i := range deployments {
		addEventObject(objects, names, "Deployment", &deployments[i].ObjectMeta)
	}
	for i := range services {
		addEventObject(objects, names, "Service", &services[i].ObjectMeta)
	}
	for i := range endpointSlices {
		addEventObject(objects, names, "EndpointSlice", &endpointSlices[i].ObjectMeta)
	}
	for i := range pods {
		addEventObject(objects, names, "Pod", &pods[i].ObjectMeta)
	}
	for i := range jobs {
		addEventObject(objects, names, "Job", &jobs[i].ObjectMeta)
	}

	filtered := make([]corev1.Event, 0, len(events))
	for i := range events {
		event := &events[i]
		_, uidMatch := objects[event.InvolvedObject.UID]
		_, nameMatch := names[eventObjectKey(event.InvolvedObject.Kind, event.InvolvedObject.Name)]
		relevant := nameMatch
		if event.InvolvedObject.UID != "" {
			relevant = uidMatch
		}
		if relevant {
			filtered = append(filtered, *event)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		left, right := eventLastObserved(&filtered[i]), eventLastObserved(&filtered[j])
		if left.Equal(right) {
			return filtered[i].Name < filtered[j].Name
		}
		return left.After(right)
	})
	if len(filtered) > maxEvents {
		filtered = filtered[:maxEvents]
	}

	result := make([]KubernetesEvent, 0, len(filtered))
	for i := range filtered {
		event := &filtered[i]
		count := event.Count
		if event.Series != nil && event.Series.Count > count {
			count = event.Series.Count
		}
		if count < 1 {
			count = 1
		}
		result = append(result, KubernetesEvent{
			Type:    event.Type,
			Reason:  event.Reason,
			Message: boundedKubernetesText(event.Message),
			Regarding: ObjectReference{
				Kind: event.InvolvedObject.Kind,
				Name: event.InvolvedObject.Name,
			},
			Count:           count,
			FirstObservedAt: formatTime(eventFirstObserved(event)),
			LastObservedAt:  formatTime(eventLastObserved(event)),
		})
	}
	return result
}

func addEventObject(uids map[types.UID]struct{}, names map[string]struct{}, kind string, metadata *metav1.ObjectMeta) {
	if metadata.UID != "" {
		uids[metadata.UID] = struct{}{}
	}
	names[eventObjectKey(kind, metadata.Name)] = struct{}{}
}

func eventObjectKey(kind, name string) string {
	return strings.ToLower(kind) + "/" + name
}

func eventFirstObserved(event *corev1.Event) time.Time {
	if !event.EventTime.IsZero() {
		return event.EventTime.Time
	}
	if !event.FirstTimestamp.IsZero() {
		return event.FirstTimestamp.Time
	}
	return event.CreationTimestamp.Time
}

func eventLastObserved(event *corev1.Event) time.Time {
	if event.Series != nil && !event.Series.LastObservedTime.IsZero() {
		return event.Series.LastObservedTime.Time
	}
	if !event.LastTimestamp.IsZero() {
		return event.LastTimestamp.Time
	}
	return eventFirstObserved(event)
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func boundedKubernetesText(value string) string {
	var result strings.Builder
	count := 0
	for _, char := range value {
		if unicode.IsControl(char) {
			continue
		}
		if count == maxKubernetesTextRunes {
			break
		}
		result.WriteRune(char)
		count++
	}
	return result.String()
}

func capSlice[T any](items []T, limit int) []T {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}
