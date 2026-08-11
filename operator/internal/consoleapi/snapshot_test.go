package consoleapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
			OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
				tamossv1alpha1.GroupVersion.String(), "Tamoss", tamoss.Name, tamoss.UID,
			)},
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
			OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
				tamossv1alpha1.GroupVersion.String(), "Tamoss", tamoss.Name, tamoss.UID,
			)},
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
	apiReplicaSet := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name:      "media-api-rs",
		Namespace: "tams",
		UID:       types.UID("replicaset-uid"),
		Labels: map[string]string{
			instanceLabel:  "media",
			componentLabel: "api",
		},
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
			appsv1.SchemeGroupVersion.String(), "Deployment", api.Name, api.UID,
		)},
	}}
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
			OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
				corev1.SchemeGroupVersion.String(), "Service", apiService.Name, apiService.UID,
			)},
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
			OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
				appsv1.SchemeGroupVersion.String(), "ReplicaSet", apiReplicaSet.Name, apiReplicaSet.UID,
			)},
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
			OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
				tamossv1alpha1.GroupVersion.String(), "Tamoss", tamoss.Name, tamoss.UID,
			)},
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
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Pod",
			Namespace:  "tams",
			Name:       pod.Name,
			UID:        pod.UID,
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
		tamoss, api, apiReplicaSet, apiService, apiEndpoints, pod, job, relevantEvent,
		unrelatedDeployment, unrelatedService, unrelatedEndpoints, unrelatedEvent, staleEvent,
	).Build()
	snapshotter, err := NewSnapshotter(reader, reader, "tams", "media")
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

func TestSnapshotFiltersUIDBoundOwnerChains(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	mustAddToScheme(t, scheme)
	labels := func(component string) map[string]string {
		return map[string]string{instanceLabel: "media", componentLabel: component}
	}
	tamoss := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{
		Name: "media", Namespace: "tams", UID: types.UID("tamoss-uid"),
	}}
	tamossOwner := controllerOwnerReference(tamossv1alpha1.GroupVersion.String(), "Tamoss", tamoss.Name, tamoss.UID)

	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: "media-api", Namespace: "tams", UID: types.UID("deployment-uid"), Labels: labels("api"),
		OwnerReferences: []metav1.OwnerReference{tamossOwner},
	}}
	replicaSet := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name: "media-api-rs", Namespace: "tams", UID: types.UID("replicaset-uid"), Labels: labels("api"),
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
			appsv1.SchemeGroupVersion.String(), "Deployment", deployment.Name, deployment.UID,
		)},
	}}
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: "media-api", Namespace: "tams", UID: types.UID("service-uid"), Labels: labels("api"),
		OwnerReferences: []metav1.OwnerReference{tamossOwner},
	}}
	endpointSliceLabels := labels("api")
	endpointSliceLabels[discoveryv1.LabelServiceName] = service.Name
	endpointSlice := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{
		Name: "media-api-endpoints", Namespace: "tams", UID: types.UID("endpoints-uid"), Labels: endpointSliceLabels,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
			corev1.SchemeGroupVersion.String(), "Service", service.Name, service.UID,
		)},
	}}
	storageBackend := &tamossv1alpha1.StorageBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "archive", Namespace: "tams", UID: types.UID("storage-uid")},
		Spec:       tamossv1alpha1.StorageBackendSpec{TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: tamoss.Name}},
	}
	ingestRun := &tamossv1alpha1.IngestRun{
		ObjectMeta: metav1.ObjectMeta{Name: "run-1", Namespace: "tams", UID: types.UID("run-uid")},
		Spec:       tamossv1alpha1.IngestRunSpec{TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: tamoss.Name}},
	}
	directJob := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: "direct-job", Namespace: "tams", UID: types.UID("direct-job-uid"), Labels: labels("schema"),
		OwnerReferences: []metav1.OwnerReference{tamossOwner},
	}}
	storageJob := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: "storage-job", Namespace: "tams", UID: types.UID("storage-job-uid"), Labels: labels("storage"),
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
			tamossv1alpha1.GroupVersion.String(), "StorageBackend", storageBackend.Name, storageBackend.UID,
		)},
	}}
	ingestJob := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: "ingest-job", Namespace: "tams", UID: types.UID("ingest-job-uid"), Labels: labels("ingest"),
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
			tamossv1alpha1.GroupVersion.String(), "IngestRun", ingestRun.Name, ingestRun.UID,
		)},
	}}
	replicaSetPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "api-pod", Namespace: "tams", UID: types.UID("api-pod-uid"), Labels: labels("api"),
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
			appsv1.SchemeGroupVersion.String(), "ReplicaSet", replicaSet.Name, replicaSet.UID,
		)},
	}}
	jobPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "ingest-pod", Namespace: "tams", UID: types.UID("ingest-pod-uid"), Labels: labels("ingest"),
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
			batchv1.SchemeGroupVersion.String(), "Job", ingestJob.Name, ingestJob.UID,
		)},
	}}
	storageJobPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "storage-pod", Namespace: "tams", UID: types.UID("storage-pod-uid"), Labels: labels("storage"),
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
			batchv1.SchemeGroupVersion.String(), "Job", storageJob.Name, storageJob.UID,
		)},
	}}
	directPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "direct-pod", Namespace: "tams", UID: types.UID("direct-pod-uid"), Labels: labels("diagnostic"),
		OwnerReferences: []metav1.OwnerReference{tamossOwner},
	}}

	wrongUIDDeployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: "forged-api", Namespace: "tams", UID: types.UID("forged-deployment-uid"), Labels: labels("api"),
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
			tamossv1alpha1.GroupVersion.String(), "Tamoss", tamoss.Name, types.UID("replaced-tamoss-uid"),
		)},
	}}
	duplicateControllerDeployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: "duplicate-controller", Namespace: "tams", UID: types.UID("duplicate-controller-uid"), Labels: labels("api"),
		OwnerReferences: []metav1.OwnerReference{
			tamossOwner,
			controllerOwnerReference(tamossv1alpha1.GroupVersion.String(), "Tamoss", tamoss.Name, tamoss.UID),
		},
	}}
	wrongGVKReplicaSet := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name: "wrong-gvk-rs", Namespace: "tams", UID: types.UID("wrong-gvk-rs-uid"), Labels: labels("api"),
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
			"apps/v1beta1", "Deployment", deployment.Name, deployment.UID,
		)},
	}}
	mismatchedEndpointLabels := labels("api")
	mismatchedEndpointLabels[discoveryv1.LabelServiceName] = "replacement-service"
	wrongNameEndpointSlice := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{
		Name: "wrong-service-endpoints", Namespace: "tams", UID: types.UID("wrong-service-endpoints-uid"), Labels: mismatchedEndpointLabels,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
			corev1.SchemeGroupVersion.String(), "Service", service.Name, service.UID,
		)},
	}}
	wrongTargetStorageBackend := &tamossv1alpha1.StorageBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "foreign-storage", Namespace: "tams", UID: types.UID("foreign-storage-uid")},
		Spec:       tamossv1alpha1.StorageBackendSpec{TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "other"}},
	}
	unrelatedRun := &tamossv1alpha1.IngestRun{
		ObjectMeta: metav1.ObjectMeta{Name: "other-run", Namespace: "tams", UID: types.UID("other-run-uid")},
		Spec:       tamossv1alpha1.IngestRunSpec{TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "other"}},
	}
	wrongRunJob := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: "foreign-ingest-job", Namespace: "tams", UID: types.UID("foreign-ingest-job-uid"), Labels: labels("ingest"),
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
			tamossv1alpha1.GroupVersion.String(), "IngestRun", unrelatedRun.Name, unrelatedRun.UID,
		)},
	}}
	deploymentOwnedPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "deployment-owned-pod", Namespace: "tams", UID: types.UID("deployment-owned-pod-uid"), Labels: labels("api"),
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
			appsv1.SchemeGroupVersion.String(), "Deployment", deployment.Name, deployment.UID,
		)},
	}}

	bridgeEvent := func(name, reason, apiVersion, kind, objectName string, uid types.UID) *corev1.Event {
		return &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "tams"},
			InvolvedObject: corev1.ObjectReference{
				APIVersion: apiVersion, Kind: kind, Namespace: "tams", Name: objectName, UID: uid,
			},
			Reason: reason,
		}
	}
	replicaSetEvent := bridgeEvent("replicaset-event", "ReplicaSetRoot", appsv1.SchemeGroupVersion.String(), "ReplicaSet", replicaSet.Name, replicaSet.UID)
	storageEvent := bridgeEvent("storage-event", "StorageBackendRoot", tamossv1alpha1.GroupVersion.String(), "StorageBackend", storageBackend.Name, storageBackend.UID)
	ingestRunEvent := bridgeEvent("ingest-run-event", "IngestRunRoot", tamossv1alpha1.GroupVersion.String(), "IngestRun", ingestRun.Name, ingestRun.UID)
	uidlessEvent := bridgeEvent("uidless-event", "UIDLess", appsv1.SchemeGroupVersion.String(), "ReplicaSet", replicaSet.Name, "")
	wrongGVKEvent := bridgeEvent("wrong-gvk-event", "WrongGVK", batchv1.SchemeGroupVersion.String(), "Job", replicaSet.Name, replicaSet.UID)
	wrongUIDEvent := bridgeEvent("wrong-uid-event", "WrongUID", appsv1.SchemeGroupVersion.String(), "ReplicaSet", replicaSet.Name, types.UID("previous-replicaset-uid"))
	wrongNamespaceEvent := bridgeEvent("wrong-namespace-event", "WrongNamespace", appsv1.SchemeGroupVersion.String(), "ReplicaSet", replicaSet.Name, replicaSet.UID)
	wrongNamespaceEvent.InvolvedObject.Namespace = "other"
	unrelatedRunEvent := bridgeEvent("other-run-event", "OtherRun", tamossv1alpha1.GroupVersion.String(), "IngestRun", unrelatedRun.Name, unrelatedRun.UID)

	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		tamoss,
		deployment, replicaSet, service, endpointSlice, storageBackend, ingestRun,
		directJob, storageJob, ingestJob, replicaSetPod, jobPod, storageJobPod, directPod,
		wrongUIDDeployment, duplicateControllerDeployment, wrongGVKReplicaSet, wrongNameEndpointSlice,
		wrongTargetStorageBackend, unrelatedRun, wrongRunJob, deploymentOwnedPod,
		replicaSetEvent, storageEvent, ingestRunEvent, uidlessEvent, wrongGVKEvent, wrongUIDEvent, wrongNamespaceEvent, unrelatedRunEvent,
	).Build()
	snapshotter, err := NewSnapshotter(reader, reader, "tams", "media")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshotter.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	assertProjectedNames(t, "workloads", []string{deployment.Name}, workloadNames(snapshot.Workloads))
	assertProjectedNames(t, "services", []string{service.Name}, serviceNames(snapshot.Services))
	assertProjectedNames(t, "EndpointSlices", []string{endpointSlice.Name}, endpointSliceNames(snapshot.EndpointSlices))
	assertProjectedNames(t, "Jobs", []string{directJob.Name, ingestJob.Name, storageJob.Name}, jobNames(snapshot.Jobs))
	assertProjectedNames(t, "Pods", []string{directPod.Name, jobPod.Name, replicaSetPod.Name, storageJobPod.Name}, podNames(snapshot.Pods))
	assertProjectedNames(t, "Events", []string{"IngestRunRoot", "ReplicaSetRoot", "StorageBackendRoot"}, eventReasons(snapshot.Events))
}

func TestReferencedIngestRunResolutionIsBoundedPrioritizedAndCached(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	mustAddToScheme(t, scheme)
	reader := fake.NewClientBuilder().WithScheme(scheme).Build()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	calls := make([]string, 0)
	getter := resourceGetterFunc(func(_ context.Context, key client.ObjectKey, object client.Object, _ ...client.GetOption) error {
		calls = append(calls, key.Name)
		run := object.(*tamossv1alpha1.IngestRun)
		*run = tamossv1alpha1.IngestRun{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, UID: types.UID(key.Name + "-uid")},
			Spec:       tamossv1alpha1.IngestRunSpec{TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "media"}},
		}
		return nil
	})
	snapshotter, err := NewSnapshotter(reader, getter, "tams", "media")
	if err != nil {
		t.Fatal(err)
	}
	snapshotter.now = func() time.Time { return now }

	jobs := make([]batchv1.Job, 0, maxIngestRunRootReads+8)
	active := referencedIngestJob("active-job", "active", types.UID("active-uid"), now.Add(-3*time.Hour))
	active.Status.Active = 1
	jobs = append(jobs, active)
	jobs = append(jobs, referencedIngestJob("active-duplicate", "active", types.UID("active-uid"), now.Add(-2*time.Hour)))
	jobs = append(jobs, referencedIngestJob("pending-job", "pending", types.UID("pending-uid"), now.Add(-4*time.Hour)))
	for i := 0; i < maxIngestRunRootReads+4; i++ {
		name := fmt.Sprintf("terminal-%02d", i)
		job := referencedIngestJob(name+"-job", name, types.UID(name+"-uid"), now.Add(time.Duration(i)*time.Minute))
		job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
		if i == 0 {
			completed := metav1.NewTime(now.Add(time.Hour))
			job.Status.CompletionTime = &completed
		}
		jobs = append(jobs, job)
	}

	resolution, err := snapshotter.resolveReferencedIngestRuns(context.Background(), jobs)
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.truncated {
		t.Fatal("resolution was not marked truncated")
	}
	if len(resolution.items) != maxIngestRunRootReads || len(calls) != maxIngestRunRootReads {
		t.Fatalf("resolved=%d live reads=%d, want %d", len(resolution.items), len(calls), maxIngestRunRootReads)
	}
	expectedCalls := []string{"active", "pending", "terminal-00"}
	for i := maxIngestRunRootReads + 3; len(expectedCalls) < maxIngestRunRootReads; i-- {
		expectedCalls = append(expectedCalls, fmt.Sprintf("terminal-%02d", i))
	}
	if !slices.Equal(calls, expectedCalls) {
		t.Fatalf("live read order = %v, want %v", calls, expectedCalls)
	}

	if _, err := snapshotter.resolveReferencedIngestRuns(context.Background(), jobs); err != nil {
		t.Fatal(err)
	}
	if len(calls) != maxIngestRunRootReads {
		t.Fatalf("positive cache did not suppress repeated reads: %v", calls)
	}
	now = now.Add(ingestRunValidationTTL + time.Second)
	if _, err := snapshotter.resolveReferencedIngestRuns(context.Background(), jobs); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2*maxIngestRunRootReads {
		t.Fatalf("expired cache read count = %d, want %d", len(calls), 2*maxIngestRunRootReads)
	}
	if _, err := snapshotter.resolveReferencedIngestRuns(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(snapshotter.ingestRunCache) != 0 {
		t.Fatalf("unreferenced cache entries were not swept: %#v", snapshotter.ingestRunCache)
	}
}

func TestReferencedIngestRunResolutionFailsClosed(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	mustAddToScheme(t, scheme)
	reader := fake.NewClientBuilder().WithScheme(scheme).Build()
	getter := resourceGetterFunc(func(_ context.Context, key client.ObjectKey, object client.Object, _ ...client.GetOption) error {
		if key.Name == "missing" {
			return apierrors.NewNotFound(schema.GroupResource{Group: tamossv1alpha1.GroupVersion.Group, Resource: "ingestruns"}, key.Name)
		}
		run := object.(*tamossv1alpha1.IngestRun)
		uid := types.UID(key.Name + "-uid")
		target := "media"
		if key.Name == "wrong-uid" {
			uid = "replacement-uid"
		}
		if key.Name == "wrong-target" {
			target = "other"
		}
		*run = tamossv1alpha1.IngestRun{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, UID: uid},
			Spec:       tamossv1alpha1.IngestRunSpec{TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: target}},
		}
		return nil
	})
	snapshotter, err := NewSnapshotter(reader, getter, "tams", "media")
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	jobs := []batchv1.Job{
		referencedIngestJob("valid-job", "valid", "valid-uid", created),
		referencedIngestJob("missing-job", "missing", "missing-uid", created),
		referencedIngestJob("wrong-uid-job", "wrong-uid", "wrong-uid-uid", created),
		referencedIngestJob("wrong-target-job", "wrong-target", "wrong-target-uid", created),
	}
	resolution, err := snapshotter.resolveReferencedIngestRuns(context.Background(), jobs)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.truncated || len(resolution.items) != 1 || resolution.items[0].Name != "valid" {
		t.Fatalf("unexpected fail-closed resolution: %#v", resolution)
	}
}

func TestReferencedIngestRunCacheRefreshesAmbiguousReplacementUID(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	mustAddToScheme(t, scheme)
	reader := fake.NewClientBuilder().WithScheme(scheme).Build()
	liveUID := types.UID("run-uid-a")
	calls := 0
	getter := resourceGetterFunc(func(_ context.Context, key client.ObjectKey, object client.Object, _ ...client.GetOption) error {
		calls++
		run := object.(*tamossv1alpha1.IngestRun)
		*run = tamossv1alpha1.IngestRun{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, UID: liveUID},
			Spec:       tamossv1alpha1.IngestRunSpec{TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "media"}},
		}
		return nil
	})
	snapshotter, err := NewSnapshotter(reader, getter, "tams", "media")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	snapshotter.now = func() time.Time { return now }
	oldJob := referencedIngestJob("run-a-job", "run", "run-uid-a", now.Add(-time.Minute))

	resolution, err := snapshotter.resolveReferencedIngestRuns(context.Background(), []batchv1.Job{oldJob})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(resolution.items) != 1 || resolution.items[0].UID != "run-uid-a" {
		t.Fatalf("initial resolution calls=%d items=%#v", calls, resolution.items)
	}

	liveUID = "run-uid-b"
	newJob := referencedIngestJob("run-b-job", "run", "run-uid-b", now)
	resolution, err = snapshotter.resolveReferencedIngestRuns(context.Background(), []batchv1.Job{oldJob, newJob})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(resolution.items) != 1 || resolution.items[0].UID != "run-uid-b" {
		t.Fatalf("replacement resolution calls=%d items=%#v", calls, resolution.items)
	}

	resolution, err = snapshotter.resolveReferencedIngestRuns(context.Background(), []batchv1.Job{newJob})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(resolution.items) != 1 || resolution.items[0].UID != "run-uid-b" {
		t.Fatalf("replacement cache calls=%d items=%#v", calls, resolution.items)
	}
}

func TestReferencedIngestRunReadFailureMarksMonitorStale(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	mustAddToScheme(t, scheme)
	tamoss := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Name: "media", Namespace: "tams", UID: "tamoss-uid"}}
	job := referencedIngestJob("ingest-job", "run", "run-uid", time.Now())
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tamoss, &job).Build()
	readErr := error(nil)
	getter := resourceGetterFunc(func(_ context.Context, key client.ObjectKey, object client.Object, _ ...client.GetOption) error {
		if readErr != nil {
			return readErr
		}
		run := object.(*tamossv1alpha1.IngestRun)
		*run = tamossv1alpha1.IngestRun{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, UID: "run-uid"},
			Spec:       tamossv1alpha1.IngestRunSpec{TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "media"}},
		}
		return nil
	})
	snapshotter, err := NewSnapshotter(reader, getter, "tams", "media")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	snapshotter.now = func() time.Time { return now }
	monitor := NewMonitor(snapshotter)
	if err := monitor.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	readErr = errors.New("API unavailable")
	now = now.Add(ingestRunValidationTTL + time.Second)
	if err := monitor.Refresh(context.Background()); !errors.Is(err, readErr) {
		t.Fatalf("Refresh() error = %v, want %v", err, readErr)
	}
	_, ready, current := monitor.Current()
	if ready || !current {
		t.Fatalf("monitor state after failed live read: ready=%t current=%t", ready, current)
	}
	snapshot, _, _ := monitor.Current()
	if !snapshot.Stale {
		t.Fatal("last good snapshot was not marked stale")
	}
}

func TestReferencedIngestRunResolutionHasBatchDeadline(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	mustAddToScheme(t, scheme)
	reader := fake.NewClientBuilder().WithScheme(scheme).Build()
	getter := resourceGetterFunc(func(ctx context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
		<-ctx.Done()
		return ctx.Err()
	})
	snapshotter, err := NewSnapshotter(reader, getter, "tams", "media")
	if err != nil {
		t.Fatal(err)
	}
	snapshotter.ingestRunReadTimeout = 20 * time.Millisecond
	job := referencedIngestJob("ingest-job", "run", "run-uid", time.Now())
	started := time.Now()
	_, err = snapshotter.resolveReferencedIngestRuns(context.Background(), []batchv1.Job{job})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("resolution error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("batch deadline took %s", elapsed)
	}
}

func TestExactControllerOwnerValidation(t *testing.T) {
	t.Parallel()
	owners := resourceIdentitySet{
		{APIVersion: tamossv1alpha1.GroupVersion.String(), Kind: "Tamoss", Name: "media", UID: types.UID("tamoss-uid")}:            {},
		{APIVersion: appsv1.SchemeGroupVersion.String(), Kind: "Deployment", Name: "api", UID: types.UID("deployment-uid")}:        {},
		{APIVersion: corev1.SchemeGroupVersion.String(), Kind: "Service", Name: "api", UID: types.UID("service-uid")}:              {},
		{APIVersion: tamossv1alpha1.GroupVersion.String(), Kind: "StorageBackend", Name: "archive", UID: types.UID("storage-uid")}: {},
		{APIVersion: tamossv1alpha1.GroupVersion.String(), Kind: "IngestRun", Name: "run", UID: types.UID("run-uid")}:              {},
		{APIVersion: appsv1.SchemeGroupVersion.String(), Kind: "ReplicaSet", Name: "api-rs", UID: types.UID("replicaset-uid")}:     {},
		{APIVersion: batchv1.SchemeGroupVersion.String(), Kind: "Job", Name: "run-job", UID: types.UID("job-uid")}:                 {},
	}
	validOwners := []metav1.OwnerReference{
		controllerOwnerReference(tamossv1alpha1.GroupVersion.String(), "Tamoss", "media", types.UID("tamoss-uid")),
		controllerOwnerReference(appsv1.SchemeGroupVersion.String(), "Deployment", "api", types.UID("deployment-uid")),
		controllerOwnerReference(corev1.SchemeGroupVersion.String(), "Service", "api", types.UID("service-uid")),
		controllerOwnerReference(tamossv1alpha1.GroupVersion.String(), "StorageBackend", "archive", types.UID("storage-uid")),
		controllerOwnerReference(tamossv1alpha1.GroupVersion.String(), "IngestRun", "run", types.UID("run-uid")),
		controllerOwnerReference(appsv1.SchemeGroupVersion.String(), "ReplicaSet", "api-rs", types.UID("replicaset-uid")),
		controllerOwnerReference(batchv1.SchemeGroupVersion.String(), "Job", "run-job", types.UID("job-uid")),
	}
	for _, owner := range validOwners {
		t.Run("valid "+owner.Kind, func(t *testing.T) {
			object := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{owner}}}
			if !hasExactControllerOwner(object, owners) {
				t.Fatalf("expected exact %s owner to match: %#v", owner.Kind, owner)
			}
		})
	}

	notController := controllerOwnerReference(tamossv1alpha1.GroupVersion.String(), "Tamoss", "media", types.UID("tamoss-uid"))
	notController.Controller = nil
	controllerFalse := false
	falseController := notController
	falseController.Controller = &controllerFalse
	exact := controllerOwnerReference(tamossv1alpha1.GroupVersion.String(), "Tamoss", "media", types.UID("tamoss-uid"))
	tests := []struct {
		name   string
		owners []metav1.OwnerReference
	}{
		{name: "missing"},
		{name: "controller nil", owners: []metav1.OwnerReference{notController}},
		{name: "controller false", owners: []metav1.OwnerReference{falseController}},
		{name: "wrong apiVersion", owners: []metav1.OwnerReference{controllerOwnerReference("tamoss.livewyer.io/v2", "Tamoss", "media", types.UID("tamoss-uid"))}},
		{name: "wrong kind", owners: []metav1.OwnerReference{controllerOwnerReference(tamossv1alpha1.GroupVersion.String(), "StorageBackend", "media", types.UID("tamoss-uid"))}},
		{name: "wrong name", owners: []metav1.OwnerReference{controllerOwnerReference(tamossv1alpha1.GroupVersion.String(), "Tamoss", "replacement", types.UID("tamoss-uid"))}},
		{name: "wrong uid", owners: []metav1.OwnerReference{controllerOwnerReference(tamossv1alpha1.GroupVersion.String(), "Tamoss", "media", types.UID("replacement-uid"))}},
		{name: "empty uid", owners: []metav1.OwnerReference{controllerOwnerReference(tamossv1alpha1.GroupVersion.String(), "Tamoss", "media", "")}},
		{name: "duplicate controllers", owners: []metav1.OwnerReference{exact, exact}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			object := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{OwnerReferences: test.owners}}
			if hasExactControllerOwner(object, owners) {
				t.Fatalf("unexpected owner match for %#v", test.owners)
			}
		})
	}
}

func TestRetainControllerOwnedRequiresExactDiscoveryLabelAndUID(t *testing.T) {
	t.Parallel()
	owners := resourceIdentitySet{{
		APIVersion: tamossv1alpha1.GroupVersion.String(),
		Kind:       "Tamoss",
		Namespace:  "tams",
		Name:       "media",
		UID:        types.UID("tamoss-uid"),
	}: {}}
	exactOwner := controllerOwnerReference(tamossv1alpha1.GroupVersion.String(), "Tamoss", "media", types.UID("tamoss-uid"))
	wrongOwner := controllerOwnerReference(tamossv1alpha1.GroupVersion.String(), "Tamoss", "media", types.UID("previous-tamoss-uid"))
	items := []batchv1.Job{
		{ObjectMeta: metav1.ObjectMeta{Name: "retained", Namespace: "tams", UID: types.UID("retained-uid"), Labels: map[string]string{instanceLabel: "media"}, OwnerReferences: []metav1.OwnerReference{exactOwner}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "missing-label", Namespace: "tams", UID: types.UID("missing-label-uid"), OwnerReferences: []metav1.OwnerReference{exactOwner}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "wrong-label", Namespace: "tams", UID: types.UID("wrong-label-uid"), Labels: map[string]string{instanceLabel: "other"}, OwnerReferences: []metav1.OwnerReference{exactOwner}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "missing-uid", Namespace: "tams", Labels: map[string]string{instanceLabel: "media"}, OwnerReferences: []metav1.OwnerReference{exactOwner}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "wrong-owner-uid", Namespace: "tams", UID: types.UID("wrong-owner-uid"), Labels: map[string]string{instanceLabel: "media"}, OwnerReferences: []metav1.OwnerReference{wrongOwner}}},
	}
	retained := retainControllerOwned(items, "media", owners, func(item *batchv1.Job) metav1.Object { return item })
	if len(retained) != 1 || retained[0].Name != "retained" {
		t.Fatalf("retained Jobs = %#v, want only the exact discovery and owner match", retained)
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
	reader := fake.NewClientBuilder().WithScheme(scheme).Build()
	snapshotter, err := NewSnapshotter(reader, reader, "tams", "missing")
	if err != nil {
		t.Fatalf("NewSnapshotter() error = %v", err)
	}
	if _, err := snapshotter.Snapshot(context.Background()); err == nil {
		t.Fatal("Snapshot() error = nil, want missing-instance error")
	}
}

func TestSnapshotRejectsUIDLessTamoss(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	mustAddToScheme(t, scheme)
	tamoss := &tamossv1alpha1.Tamoss{ObjectMeta: metav1.ObjectMeta{Name: "media", Namespace: "tams"}}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tamoss).Build()
	snapshotter, err := NewSnapshotter(reader, reader, "tams", "media")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snapshotter.Snapshot(context.Background()); err == nil || !strings.Contains(err.Error(), "has no UID") {
		t.Fatalf("Snapshot() error = %v, want UID failure", err)
	}
}

func TestNewSnapshotterValidatesAndNormalizesScope(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	mustAddToScheme(t, scheme)
	reader := fake.NewClientBuilder().WithScheme(scheme).Build()
	if _, err := NewSnapshotter(nil, reader, "tams", "media"); err == nil {
		t.Fatal("NewSnapshotter(nil) error = nil")
	}
	if _, err := NewSnapshotter(reader, nil, "tams", "media"); err == nil {
		t.Fatal("NewSnapshotter(nil live getter) error = nil")
	}
	if _, err := NewSnapshotter(reader, reader, " ", "media"); err == nil {
		t.Fatal("NewSnapshotter(empty namespace) error = nil")
	}
	if _, err := NewSnapshotter(reader, reader, "tams", " "); err == nil {
		t.Fatal("NewSnapshotter(empty instance) error = nil")
	}
	snapshotter, err := NewSnapshotter(reader, reader, " tams ", " media ")
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

func assertProjectedNames(t *testing.T, projection string, want, got []string) {
	t.Helper()
	want = append([]string(nil), want...)
	got = append([]string(nil), got...)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("%s names = %v, want %v", projection, got, want)
	}
}

func workloadNames(items []Workload) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Name)
	}
	return result
}

func serviceNames(items []Service) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Name)
	}
	return result
}

func endpointSliceNames(items []EndpointSlice) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Name)
	}
	return result
}

func podNames(items []Pod) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Name)
	}
	return result
}

func jobNames(items []Job) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Name)
	}
	return result
}

func eventReasons(items []KubernetesEvent) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Reason)
	}
	return result
}

func controllerOwnerReference(apiVersion, kind, name string, uid types.UID) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		UID:        uid,
		Controller: &controller,
	}
}

type resourceGetterFunc func(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error

func (f resourceGetterFunc) Get(ctx context.Context, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
	return f(ctx, key, object, options...)
}

func referencedIngestJob(jobName, runName string, runUID types.UID, created time.Time) batchv1.Job {
	return batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name:              jobName,
		Namespace:         "tams",
		UID:               types.UID(jobName + "-uid"),
		CreationTimestamp: metav1.NewTime(created),
		Labels:            map[string]string{instanceLabel: "media", componentLabel: "ingest"},
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(
			tamossv1alpha1.GroupVersion.String(), "IngestRun", runName, runUID,
		)},
	}}
}
