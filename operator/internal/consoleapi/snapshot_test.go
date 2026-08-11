package consoleapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestSnapshotProjectsOnlyConfiguredInstance(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	mustAddToScheme(t, scheme)
	now := time.Date(2026, 8, 9, 12, 30, 0, 0, time.UTC)
	one := int32(1)
	started := metav1.NewTime(now.Add(-5 * time.Minute))
	completed := metav1.NewTime(now.Add(-time.Minute))
	endpointReady := false
	endpointPort := int32(8080)
	endpointPortName := "http"
	endpointProtocol := corev1.ProtocolTCP

	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "media",
			Namespace:  "tams",
			UID:        types.UID("tamoss-uid"),
			Generation: 4,
		},
		Status: tamossv1alpha1.TamossStatus{
			ObservedGeneration: 4,
			Phase:              "Ready",
			Conditions: []metav1.Condition{{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "Available",
				ObservedGeneration: 4,
				LastTransitionTime: started,
			}},
		},
	}
	api := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "media-api",
			Namespace:  "tams",
			UID:        types.UID("api-uid"),
			Generation: 3,
			Labels: map[string]string{
				instanceLabel:  "media",
				componentLabel: "api",
			},
		},
		Spec: appsv1.DeploymentSpec{Replicas: &one},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 3,
			Replicas:           1,
			UpdatedReplicas:    1,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
		},
	}
	apiService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "media-api",
			Namespace: "tams",
			UID:       types.UID("service-uid"),
			Labels: map[string]string{
				instanceLabel:  "media",
				componentLabel: "api",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				instanceLabel:  "media",
				componentLabel: "api",
			},
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Protocol:   corev1.ProtocolTCP,
				Port:       8181,
				TargetPort: intstr.FromString("http"),
			}},
		},
	}
	apiEndpoints := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "media-api-abc",
			Namespace: "tams",
			UID:       types.UID("endpoint-slice-uid"),
			Labels: map[string]string{
				instanceLabel:                "media",
				componentLabel:               "api",
				discoveryv1.LabelServiceName: "media-api",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{{
			Name:     &endpointPortName,
			Protocol: &endpointProtocol,
			Port:     &endpointPort,
		}},
		Endpoints: []discoveryv1.Endpoint{{
			Addresses:  []string{"10.0.0.42"},
			Conditions: discoveryv1.EndpointConditions{Ready: &endpointReady},
		}},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "media-api-abc",
			Namespace: "tams",
			UID:       types.UID("pod-uid"),
			Labels: map[string]string{
				instanceLabel:  "media",
				componentLabel: "api",
			},
		},
		Status: corev1.PodStatus{
			Phase:     corev1.PodRunning,
			StartTime: &started,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "api", RestartCount: 2}},
		},
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "media-schema",
			Namespace: "tams",
			UID:       types.UID("job-uid"),
			Labels: map[string]string{
				instanceLabel:  "media",
				componentLabel: "schema",
			},
		},
		Status: batchv1.JobStatus{
			Succeeded:      1,
			StartTime:      &started,
			CompletionTime: &completed,
			Conditions: []batchv1.JobCondition{{
				Type:   batchv1.JobComplete,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	relevantEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "api-ready", Namespace: "tams"},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod",
			Name: pod.Name,
			UID:  pod.UID,
		},
		Type:          corev1.EventTypeNormal,
		Reason:        "Ready",
		Message:       "Pod is ready",
		Count:         2,
		LastTimestamp: metav1.NewTime(now.Add(-time.Minute)),
	}
	unrelatedDeployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: "other-api", Namespace: "tams", Labels: map[string]string{instanceLabel: "other"},
	}}
	unrelatedService := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: "other-api", Namespace: "tams", Labels: map[string]string{instanceLabel: "other"},
	}}
	unrelatedEndpoints := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{
		Name: "other-api-abc", Namespace: "tams", Labels: map[string]string{instanceLabel: "other"},
	}}
	unrelatedEvent := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "other-event", Namespace: "tams"},
		InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "other-api"},
		Reason:         "Leaked",
	}
	staleEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "stale-api-event", Namespace: "tams"},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Deployment",
			Name: api.Name,
			UID:  types.UID("previous-api-uid"),
		},
		Reason: "Stale",
	}

	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		tamoss, api, apiService, apiEndpoints, pod, job, relevantEvent,
		unrelatedDeployment, unrelatedService, unrelatedEndpoints, unrelatedEvent, staleEvent,
	).Build()
	snapshotter, err := NewSnapshotter(reader, "tams", "media")
	if err != nil {
		t.Fatalf("NewSnapshotter() error = %v", err)
	}
	snapshotter.now = func() time.Time { return now }

	snapshot, err := snapshotter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.SchemaVersion != RuntimeSchemaVersion || snapshot.ObservedAt != now.Format(time.RFC3339Nano) || snapshot.Stale {
		t.Fatalf("unexpected snapshot envelope: %#v", snapshot)
	}
	if snapshot.Instance.Name != "media" || snapshot.Instance.Phase != "Ready" || len(snapshot.Instance.Conditions) != 1 {
		t.Fatalf("unexpected instance: %#v", snapshot.Instance)
	}
	if len(snapshot.Workloads) != 1 || snapshot.Workloads[0].Name != api.Name || snapshot.Workloads[0].Status != "ready" {
		t.Fatalf("unexpected workloads: %#v", snapshot.Workloads)
	}
	if len(snapshot.Services) != 1 || snapshot.Services[0].Name != apiService.Name ||
		snapshot.Services[0].Ports[0].TargetPort != "http" {
		t.Fatalf("unexpected services: %#v", snapshot.Services)
	}
	if len(snapshot.EndpointSlices) != 1 || snapshot.EndpointSlices[0].Name != apiEndpoints.Name ||
		snapshot.EndpointSlices[0].TotalEndpoints != 1 || snapshot.EndpointSlices[0].ReadyEndpoints != 0 ||
		snapshot.EndpointSlices[0].NotReadyEndpoints != 1 || snapshot.EndpointSlices[0].Ports[0].Port != endpointPort {
		t.Fatalf("unexpected EndpointSlices: %#v", snapshot.EndpointSlices)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "10.0.0.42") {
		t.Fatalf("runtime snapshot exposed an endpoint address: %s", encoded)
	}
	if len(snapshot.Pods) != 1 || !snapshot.Pods[0].Ready || snapshot.Pods[0].Restarts != 2 {
		t.Fatalf("unexpected pods: %#v", snapshot.Pods)
	}
	if len(snapshot.Jobs) != 1 || snapshot.Jobs[0].Status != "succeeded" {
		t.Fatalf("unexpected jobs: %#v", snapshot.Jobs)
	}
	if len(snapshot.Events) != 1 || snapshot.Events[0].Reason != "Ready" || snapshot.Events[0].Count != 2 {
		t.Fatalf("unexpected events: %#v", snapshot.Events)
	}
}

func TestDeploymentStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		desired    int32
		generation int64
		status     appsv1.DeploymentStatus
		want       string
	}{
		{name: "scaled down", desired: 0, want: "scaledDown"},
		{name: "new generation", desired: 1, generation: 2, status: appsv1.DeploymentStatus{ObservedGeneration: 1}, want: "progressing"},
		{name: "ready", desired: 1, generation: 2, status: appsv1.DeploymentStatus{ObservedGeneration: 2, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1}, want: "ready"},
		{name: "partially ready", desired: 2, generation: 2, status: appsv1.DeploymentStatus{ObservedGeneration: 2, UpdatedReplicas: 2, ReadyReplicas: 1}, want: "progressing"},
		{name: "unavailable", desired: 1, generation: 2, status: appsv1.DeploymentStatus{ObservedGeneration: 2, UpdatedReplicas: 1}, want: "unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Generation: test.generation}, Status: test.status}
			if got := deploymentStatus(deployment, test.desired); got != test.want {
				t.Fatalf("deploymentStatus() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRuntimeProjectionsAreBoundedAndDeterministic(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	deployments := make([]appsv1.Deployment, maxWorkloads+2)
	for i := range deployments {
		deployments[i].Name = fmt.Sprintf("workload-%03d", len(deployments)-i)
	}
	workloads := workloadsFromDeployments(deployments)
	if len(workloads) != maxWorkloads || workloads[0].Name != "workload-001" {
		t.Fatalf("workloads were not deterministically capped: first=%q count=%d", workloads[0].Name, len(workloads))
	}

	services := make([]corev1.Service, maxServices+2)
	for i := range services {
		services[i].Name = fmt.Sprintf("service-%03d", len(services)-i)
	}
	services[len(services)-1].Spec.Ports = make([]corev1.ServicePort, maxServicePorts+2)
	projectedServices := servicesFromKubernetes(services)
	if len(projectedServices) != maxServices || projectedServices[0].Name != "service-001" ||
		len(projectedServices[0].Ports) != maxServicePorts {
		t.Fatalf("services or ports were not deterministically capped: first=%#v count=%d", projectedServices[0], len(projectedServices))
	}

	endpointSlices := make([]discoveryv1.EndpointSlice, maxEndpointSlices+2)
	for i := range endpointSlices {
		endpointSlices[i].Name = fmt.Sprintf("endpoint-slice-%03d", len(endpointSlices)-i)
		endpointSlices[i].Labels = map[string]string{discoveryv1.LabelServiceName: "api"}
	}
	endpointSlices[len(endpointSlices)-1].Ports = make([]discoveryv1.EndpointPort, maxEndpointSlicePorts+2)
	projectedEndpointSlices := endpointSlicesFromKubernetes(endpointSlices)
	if len(projectedEndpointSlices) != maxEndpointSlices || projectedEndpointSlices[0].Name != "endpoint-slice-001" ||
		len(projectedEndpointSlices[0].Ports) != maxEndpointSlicePorts {
		t.Fatalf("EndpointSlices or ports were not deterministically capped: first=%#v count=%d", projectedEndpointSlices[0], len(projectedEndpointSlices))
	}

	pods := make([]corev1.Pod, maxPods+2)
	for i := range pods {
		pods[i].Name = fmt.Sprintf("pod-%03d", len(pods)-i)
	}
	projectedPods := podsFromKubernetes(pods)
	if len(projectedPods) != maxPods || projectedPods[0].Name != "pod-001" {
		t.Fatalf("pods were not deterministically capped: first=%q count=%d", projectedPods[0].Name, len(projectedPods))
	}

	jobs := make([]batchv1.Job, maxJobs+2)
	for i := range jobs {
		jobs[i].Name = fmt.Sprintf("job-%03d", i)
		jobs[i].CreationTimestamp = metav1.NewTime(base.Add(time.Duration(i) * time.Minute))
	}
	projectedJobs := jobsFromKubernetes(jobs)
	if len(projectedJobs) != maxJobs || projectedJobs[0].Name != fmt.Sprintf("job-%03d", len(jobs)-1) {
		t.Fatalf("jobs were not latest-first and capped: first=%q count=%d", projectedJobs[0].Name, len(projectedJobs))
	}
}

func TestEndpointSliceOmittedReadyConditionIsReady(t *testing.T) {
	t.Parallel()
	projected := endpointSlicesFromKubernetes([]discoveryv1.EndpointSlice{{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "api-abc",
			Labels: map[string]string{discoveryv1.LabelServiceName: "api"},
		},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.42"}}},
	}})
	if len(projected) != 1 || projected[0].ReadyEndpoints != 1 || projected[0].NotReadyEndpoints != 0 {
		t.Fatalf("omitted ready condition must be projected as ready: %#v", projected)
	}
}

func TestPodCapPrioritisesActiveAndCorePods(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	pods := make([]corev1.Pod, maxPods+2)
	for i := range pods {
		started := metav1.NewTime(base.Add(time.Duration(i) * time.Minute))
		pods[i] = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("completed-%03d", i)},
			Status:     corev1.PodStatus{Phase: corev1.PodSucceeded, StartTime: &started},
		}
	}
	pods[len(pods)-2] = corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "zz-core-api",
			Labels: map[string]string{componentLabel: "api"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionTrue,
			}},
		},
	}
	pods[len(pods)-1] = corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "zz-active-unready"},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}

	projected := podsFromKubernetes(pods)
	if len(projected) != maxPods {
		t.Fatalf("pod count = %d, want %d", len(projected), maxPods)
	}
	if projected[0].Name != "zz-active-unready" || projected[1].Name != "zz-core-api" {
		t.Fatalf("operational pods were not retained first: %#v", projected[:2])
	}
}

func TestKubernetesMessagesAreBoundedAndControlFree(t *testing.T) {
	t.Parallel()
	raw := "before\n\t\x00" + strings.Repeat("x", maxKubernetesTextRunes+10)
	got := boundedKubernetesText(raw)
	if strings.ContainsAny(got, "\n\t\x00") {
		t.Fatalf("bounded text retained control characters: %q", got[:10])
	}
	if len([]rune(got)) != maxKubernetesTextRunes {
		t.Fatalf("bounded text rune count = %d, want %d", len([]rune(got)), maxKubernetesTextRunes)
	}
}

func TestSnapshotRequiresExistingTamoss(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	mustAddToScheme(t, scheme)
	snapshotter, err := NewSnapshotter(fake.NewClientBuilder().WithScheme(scheme).Build(), "tams", "missing")
	if err != nil {
		t.Fatalf("NewSnapshotter() error = %v", err)
	}
	if _, err := snapshotter.Snapshot(context.Background()); err == nil {
		t.Fatal("Snapshot() error = nil, want missing-instance error")
	}
}

func TestNewSnapshotterValidatesAndNormalizesScope(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	mustAddToScheme(t, scheme)
	reader := fake.NewClientBuilder().WithScheme(scheme).Build()
	if _, err := NewSnapshotter(nil, "tams", "media"); err == nil {
		t.Fatal("NewSnapshotter(nil) error = nil")
	}
	if _, err := NewSnapshotter(reader, " ", "media"); err == nil {
		t.Fatal("NewSnapshotter(empty namespace) error = nil")
	}
	if _, err := NewSnapshotter(reader, "tams", " "); err == nil {
		t.Fatal("NewSnapshotter(empty instance) error = nil")
	}
	snapshotter, err := NewSnapshotter(reader, " tams ", " media ")
	if err != nil {
		t.Fatal(err)
	}
	if snapshotter.namespace != "tams" || snapshotter.instance != "media" {
		t.Fatalf("scope was not normalized: namespace=%q instance=%q", snapshotter.namespace, snapshotter.instance)
	}
}

func mustAddToScheme(t *testing.T, scheme *runtime.Scheme) {
	t.Helper()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := discoveryv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := tamossv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
}
