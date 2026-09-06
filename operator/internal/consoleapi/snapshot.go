package consoleapi

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
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
	instanceLabel               = "app.kubernetes.io/instance"
	componentLabel              = "app.kubernetes.io/component"
	apiComponent                = "api"
	maxWorkloads                = 32
	maxServices                 = 64
	maxServicePorts             = 16
	maxEndpointSlices           = 128
	maxEndpointSlicePorts       = 16
	maxPods                     = 200
	maxJobs                     = 100
	maxEvents                   = 50
	maxConditions               = 32
	maxKubernetesTextRunes      = 1024
	maxKubernetesCodeLength     = 128
	unknownDiagnosticCode       = "Unknown"
	maxIngestRunRootReads       = 16
	defaultIngestRunReadTimeout = 4 * time.Second
	ingestRunValidationTTL      = 30 * time.Second
)

type SnapshotSource interface {
	Snapshot(context.Context) (RuntimeSnapshot, error)
}

// ResourceGetter is the narrow capability used for live reads that must not
// create or depend on a controller-runtime informer.
type ResourceGetter interface {
	Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error
}

type Snapshotter struct {
	reader               client.Reader
	ingestRunGetter      ResourceGetter
	ingestRunReadTimeout time.Duration
	ingestRunCacheMu     sync.Mutex
	ingestRunCache       map[resourceIdentity]cachedIngestRun
	namespace            string
	instance             string
	now                  func() time.Time
}

func NewSnapshotter(reader client.Reader, ingestRunGetter ResourceGetter, namespace, instance string) (*Snapshotter, error) {
	if reader == nil {
		return nil, fmt.Errorf("kubernetes reader is required")
	}
	if ingestRunGetter == nil {
		return nil, fmt.Errorf("live IngestRun getter is required")
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
		reader:               reader,
		ingestRunGetter:      ingestRunGetter,
		ingestRunReadTimeout: defaultIngestRunReadTimeout,
		ingestRunCache:       make(map[resourceIdentity]cachedIngestRun),
		namespace:            namespace,
		instance:             instance,
		now:                  time.Now,
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
	if tamoss.UID == "" {
		return RuntimeSnapshot{}, fmt.Errorf("tamoss %s/%s has no UID", s.namespace, s.instance)
	}

	// The instance label bounds discovery. Exact controller references establish
	// ownership before any discovered object is projected.
	selector := client.MatchingLabels{instanceLabel: s.instance}
	deployments := &appsv1.DeploymentList{}
	if err := s.reader.List(ctx, deployments, client.InNamespace(s.namespace), selector); err != nil {
		return RuntimeSnapshot{}, fmt.Errorf("list Deployments: %w", err)
	}
	replicaSets := &appsv1.ReplicaSetList{}
	if err := s.reader.List(ctx, replicaSets, client.InNamespace(s.namespace), selector); err != nil {
		return RuntimeSnapshot{}, fmt.Errorf("list ReplicaSets: %w", err)
	}
	services := &corev1.ServiceList{}
	if err := s.reader.List(ctx, services, client.InNamespace(s.namespace), selector); err != nil {
		return RuntimeSnapshot{}, fmt.Errorf("list Services: %w", err)
	}
	endpointSlices := &discoveryv1.EndpointSliceList{}
	if err := s.reader.List(ctx, endpointSlices, client.InNamespace(s.namespace), selector); err != nil {
		return RuntimeSnapshot{}, fmt.Errorf("list EndpointSlices: %w", err)
	}
	storageBackends := &tamossv1alpha1.StorageBackendList{}
	if err := s.reader.List(ctx, storageBackends, client.InNamespace(s.namespace)); err != nil {
		return RuntimeSnapshot{}, fmt.Errorf("list StorageBackends: %w", err)
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

	tamossOwners := make(resourceIdentitySet)
	addResourceIdentity(tamossOwners, tamossv1alpha1.GroupVersion.String(), "Tamoss", tamoss)
	deployments.Items = retainControllerOwned(deployments.Items, s.instance, tamossOwners, func(item *appsv1.Deployment) metav1.Object { return item })
	services.Items = retainControllerOwned(services.Items, s.instance, tamossOwners, func(item *corev1.Service) metav1.Object { return item })
	storageBackends.Items = retainStorageBackends(storageBackends.Items, s.instance)

	deploymentOwners := make(resourceIdentitySet)
	addResourceIdentities(deploymentOwners, appsv1.SchemeGroupVersion.String(), "Deployment", deployments.Items, func(item *appsv1.Deployment) metav1.Object { return item })
	replicaSets.Items = retainControllerOwned(replicaSets.Items, s.instance, deploymentOwners, func(item *appsv1.ReplicaSet) metav1.Object { return item })

	serviceOwners := make(resourceIdentitySet)
	addResourceIdentities(serviceOwners, corev1.SchemeGroupVersion.String(), "Service", services.Items, func(item *corev1.Service) metav1.Object { return item })
	endpointSlices.Items = retainEndpointSlices(endpointSlices.Items, s.instance, serviceOwners)

	ingestRunRoots, err := s.resolveReferencedIngestRuns(ctx, jobs.Items)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	ingestRuns := ingestRunRoots.items
	jobOwners := make(resourceIdentitySet)
	addResourceIdentity(jobOwners, tamossv1alpha1.GroupVersion.String(), "Tamoss", tamoss)
	addResourceIdentities(jobOwners, tamossv1alpha1.GroupVersion.String(), "StorageBackend", storageBackends.Items, func(item *tamossv1alpha1.StorageBackend) metav1.Object { return item })
	addResourceIdentities(jobOwners, tamossv1alpha1.GroupVersion.String(), "IngestRun", ingestRuns, func(item *tamossv1alpha1.IngestRun) metav1.Object { return item })
	jobs.Items = retainControllerOwned(jobs.Items, s.instance, jobOwners, func(item *batchv1.Job) metav1.Object { return item })

	podOwners := make(resourceIdentitySet)
	addResourceIdentity(podOwners, tamossv1alpha1.GroupVersion.String(), "Tamoss", tamoss)
	addResourceIdentities(podOwners, appsv1.SchemeGroupVersion.String(), "ReplicaSet", replicaSets.Items, func(item *appsv1.ReplicaSet) metav1.Object { return item })
	addResourceIdentities(podOwners, batchv1.SchemeGroupVersion.String(), "Job", jobs.Items, func(item *batchv1.Job) metav1.Object { return item })
	pods.Items = retainControllerOwned(pods.Items, s.instance, podOwners, func(item *corev1.Pod) metav1.Object { return item })

	eventObjects := make(resourceIdentitySet)
	addResourceIdentity(eventObjects, tamossv1alpha1.GroupVersion.String(), "Tamoss", tamoss)
	addResourceIdentities(eventObjects, appsv1.SchemeGroupVersion.String(), "Deployment", deployments.Items, func(item *appsv1.Deployment) metav1.Object { return item })
	addResourceIdentities(eventObjects, appsv1.SchemeGroupVersion.String(), "ReplicaSet", replicaSets.Items, func(item *appsv1.ReplicaSet) metav1.Object { return item })
	addResourceIdentities(eventObjects, corev1.SchemeGroupVersion.String(), "Service", services.Items, func(item *corev1.Service) metav1.Object { return item })
	addResourceIdentities(eventObjects, discoveryv1.SchemeGroupVersion.String(), "EndpointSlice", endpointSlices.Items, func(item *discoveryv1.EndpointSlice) metav1.Object { return item })
	addResourceIdentities(eventObjects, tamossv1alpha1.GroupVersion.String(), "StorageBackend", storageBackends.Items, func(item *tamossv1alpha1.StorageBackend) metav1.Object { return item })
	addResourceIdentities(eventObjects, tamossv1alpha1.GroupVersion.String(), "IngestRun", ingestRuns, func(item *tamossv1alpha1.IngestRun) metav1.Object { return item })
	addResourceIdentities(eventObjects, corev1.SchemeGroupVersion.String(), "Pod", pods.Items, func(item *corev1.Pod) metav1.Object { return item })
	addResourceIdentities(eventObjects, batchv1.SchemeGroupVersion.String(), "Job", jobs.Items, func(item *batchv1.Job) metav1.Object { return item })

	snapshot := RuntimeSnapshot{
		SchemaVersion:          RuntimeSchemaVersion,
		ObservedAt:             s.now().UTC().Format(time.RFC3339Nano),
		IngestRuntimeTruncated: ingestRunRoots.truncated,
		Instance:               instanceFromTamoss(tamoss),
		Workloads:              workloadsFromDeployments(deployments.Items),
		Services:               servicesFromKubernetes(services.Items),
		EndpointSlices:         endpointSlicesFromKubernetes(endpointSlices.Items),
		Pods:                   podsFromKubernetes(pods.Items),
		Jobs:                   jobsFromKubernetes(jobs.Items),
	}
	snapshot.Events = relevantEvents(events.Items, eventObjects)
	return snapshot, nil
}

type resourceIdentity struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	UID        types.UID
}

type resourceIdentitySet map[resourceIdentity]struct{}

type cachedIngestRun struct {
	run         tamossv1alpha1.IngestRun
	validatedAt time.Time
}

type ingestRunRootResolution struct {
	items     []tamossv1alpha1.IngestRun
	truncated bool
}

func retainControllerOwned[T any](items []T, instance string, owners resourceIdentitySet, object func(*T) metav1.Object) []T {
	retained := make([]T, 0, len(items))
	for i := range items {
		candidate := object(&items[i])
		if candidate.GetUID() == "" || candidate.GetLabels()[instanceLabel] != instance || !hasExactControllerOwner(candidate, owners) {
			continue
		}
		retained = append(retained, items[i])
	}
	return retained
}

func retainStorageBackends(items []tamossv1alpha1.StorageBackend, instance string) []tamossv1alpha1.StorageBackend {
	// Additional StorageBackends are user-created logical roots and are not
	// adopted by Tamoss. Their immutable typed reference establishes scope.
	retained := make([]tamossv1alpha1.StorageBackend, 0, len(items))
	for i := range items {
		if items[i].UID != "" && items[i].Spec.TamossRef.Name == instance {
			retained = append(retained, items[i])
		}
	}
	return retained
}

func retainEndpointSlices(items []discoveryv1.EndpointSlice, instance string, owners resourceIdentitySet) []discoveryv1.EndpointSlice {
	retained := make([]discoveryv1.EndpointSlice, 0, len(items))
	for i := range items {
		candidate := &items[i]
		owner, owned := exactControllerOwner(candidate, owners)
		if candidate.UID == "" || candidate.Labels[instanceLabel] != instance || !owned ||
			candidate.Labels[discoveryv1.LabelServiceName] != owner.Name {
			continue
		}
		retained = append(retained, items[i])
	}
	return retained
}

func (s *Snapshotter) resolveReferencedIngestRuns(ctx context.Context, jobs []batchv1.Job) (ingestRunRootResolution, error) {
	ctx, cancel := context.WithTimeout(ctx, s.ingestRunReadTimeout)
	defer cancel()

	type candidate struct {
		job   *batchv1.Job
		owner resourceIdentity
	}
	candidates := make([]candidate, 0, len(jobs))
	for i := range jobs {
		job := &jobs[i]
		if job.UID == "" || job.Labels[instanceLabel] != s.instance {
			continue
		}
		owner, found := controllerOwnerIdentity(job)
		if !found || owner.APIVersion != tamossv1alpha1.GroupVersion.String() || owner.Kind != "IngestRun" ||
			owner.Namespace != s.namespace || owner.Name == "" {
			continue
		}
		candidates = append(candidates, candidate{job: job, owner: owner})
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i].job, candidates[j].job
		leftPriority, rightPriority := ingestJobOperationalPriority(left), ingestJobOperationalPriority(right)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		leftObserved, rightObserved := jobLastObserved(left), jobLastObserved(right)
		if !leftObserved.Equal(rightObserved) {
			return leftObserved.After(rightObserved)
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if candidates[i].owner.Name != candidates[j].owner.Name {
			return candidates[i].owner.Name < candidates[j].owner.Name
		}
		return candidates[i].owner.UID < candidates[j].owner.UID
	})

	seenReferences := make(resourceIdentitySet)
	selectedReferences := make(resourceIdentitySet)
	referencedUIDs := make(map[string]map[types.UID]struct{})
	names := make([]string, 0, min(len(candidates), maxIngestRunRootReads))
	truncated := false
	for _, candidate := range candidates {
		if _, seen := seenReferences[candidate.owner]; seen {
			continue
		}
		seenReferences[candidate.owner] = struct{}{}
		if uids := referencedUIDs[candidate.owner.Name]; uids != nil {
			uids[candidate.owner.UID] = struct{}{}
			selectedReferences[candidate.owner] = struct{}{}
			continue
		}
		if len(names) == maxIngestRunRootReads {
			truncated = true
			continue
		}
		referencedUIDs[candidate.owner.Name] = map[types.UID]struct{}{candidate.owner.UID: {}}
		selectedReferences[candidate.owner] = struct{}{}
		names = append(names, candidate.owner.Name)
	}

	now := s.now()
	cached := s.loadCachedIngestRuns(selectedReferences, referencedUIDs, now)
	retained := make([]tamossv1alpha1.IngestRun, 0, len(names))
	for _, name := range names {
		if run, found := cached[name]; found {
			retained = append(retained, run)
			continue
		}
		run := &tamossv1alpha1.IngestRun{}
		key := client.ObjectKey{Namespace: s.namespace, Name: name}
		if err := s.ingestRunGetter.Get(ctx, key, run); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return ingestRunRootResolution{}, fmt.Errorf("read referenced IngestRun %s/%s: %w", s.namespace, name, err)
		}
		_, uidMatches := referencedUIDs[name][run.UID]
		if run.Namespace != s.namespace || run.Name != name || run.UID == "" || !uidMatches || run.Spec.TamossRef.Name != s.instance {
			continue
		}
		validated := *run.DeepCopy()
		s.storeCachedIngestRun(validated, now)
		retained = append(retained, validated)
	}
	return ingestRunRootResolution{items: retained, truncated: truncated}, nil
}

func (s *Snapshotter) loadCachedIngestRuns(
	selected resourceIdentitySet,
	referencedUIDs map[string]map[types.UID]struct{},
	now time.Time,
) map[string]tamossv1alpha1.IngestRun {
	s.ingestRunCacheMu.Lock()
	defer s.ingestRunCacheMu.Unlock()

	result := make(map[string]tamossv1alpha1.IngestRun)
	for identity, cached := range s.ingestRunCache {
		_, referenced := selected[identity]
		if !referenced || len(referencedUIDs[identity.Name]) != 1 ||
			!now.Before(cached.validatedAt.Add(ingestRunValidationTTL)) {
			delete(s.ingestRunCache, identity)
			continue
		}
		result[identity.Name] = *cached.run.DeepCopy()
	}
	return result
}

func (s *Snapshotter) storeCachedIngestRun(run tamossv1alpha1.IngestRun, now time.Time) {
	identity := resourceIdentity{
		APIVersion: tamossv1alpha1.GroupVersion.String(),
		Kind:       "IngestRun",
		Namespace:  run.Namespace,
		Name:       run.Name,
		UID:        run.UID,
	}

	s.ingestRunCacheMu.Lock()
	defer s.ingestRunCacheMu.Unlock()
	if s.ingestRunCache == nil {
		s.ingestRunCache = make(map[resourceIdentity]cachedIngestRun)
	}
	delete(s.ingestRunCache, identity)
	for existing := range s.ingestRunCache {
		if existing.Namespace == identity.Namespace && existing.Name == identity.Name {
			delete(s.ingestRunCache, existing)
		}
	}
	if len(s.ingestRunCache) >= maxIngestRunRootReads {
		var oldest resourceIdentity
		var oldestTime time.Time
		for existing, cached := range s.ingestRunCache {
			if oldestTime.IsZero() || cached.validatedAt.Before(oldestTime) ||
				(cached.validatedAt.Equal(oldestTime) && resourceIdentityLess(existing, oldest)) {
				oldest = existing
				oldestTime = cached.validatedAt
			}
		}
		delete(s.ingestRunCache, oldest)
	}
	s.ingestRunCache[identity] = cachedIngestRun{run: *run.DeepCopy(), validatedAt: now}
}

func resourceIdentityLess(left, right resourceIdentity) bool {
	leftKey := left.APIVersion + "\x00" + left.Kind + "\x00" + left.Namespace + "\x00" + left.Name + "\x00" + string(left.UID)
	rightKey := right.APIVersion + "\x00" + right.Kind + "\x00" + right.Namespace + "\x00" + right.Name + "\x00" + string(right.UID)
	return leftKey < rightKey
}

func ingestJobOperationalPriority(job *batchv1.Job) int {
	if job.Status.Active > 0 {
		return 0
	}
	for _, condition := range job.Status.Conditions {
		if condition.Status == corev1.ConditionTrue && (condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed) {
			return 2
		}
	}
	return 1
}

func hasExactControllerOwner(object metav1.Object, owners resourceIdentitySet) bool {
	_, found := exactControllerOwner(object, owners)
	return found
}

func exactControllerOwner(object metav1.Object, owners resourceIdentitySet) (resourceIdentity, bool) {
	identity, found := controllerOwnerIdentity(object)
	if !found {
		return resourceIdentity{}, false
	}
	_, found = owners[identity]
	return identity, found
}

func controllerOwnerIdentity(object metav1.Object) (resourceIdentity, bool) {
	var controller *metav1.OwnerReference
	references := object.GetOwnerReferences()
	for i := range references {
		owner := &references[i]
		if owner.Controller == nil || !*owner.Controller {
			continue
		}
		if controller != nil {
			return resourceIdentity{}, false
		}
		controller = owner
	}
	if controller == nil || controller.UID == "" {
		return resourceIdentity{}, false
	}
	identity := resourceIdentity{
		APIVersion: controller.APIVersion,
		Kind:       controller.Kind,
		Namespace:  object.GetNamespace(),
		Name:       controller.Name,
		UID:        controller.UID,
	}
	return identity, true
}

func addResourceIdentity(targets resourceIdentitySet, apiVersion, kind string, object metav1.Object) {
	if object.GetUID() == "" {
		return
	}
	targets[resourceIdentity{APIVersion: apiVersion, Kind: kind, Namespace: object.GetNamespace(), Name: object.GetName(), UID: object.GetUID()}] = struct{}{}
}

func addResourceIdentities[T any](targets resourceIdentitySet, apiVersion, kind string, items []T, object func(*T) metav1.Object) {
	for i := range items {
		addResourceIdentity(targets, apiVersion, kind, object(&items[i]))
	}
}

func instanceFromTamoss(tamoss *tamossv1alpha1.Tamoss) Instance {
	conditions := make([]InstanceCondition, 0, len(tamoss.Status.Conditions))
	for _, condition := range tamoss.Status.Conditions {
		conditions = append(conditions, InstanceCondition{
			Type:               projectedKubernetesCode(condition.Type),
			Status:             string(condition.Status),
			Reason:             projectedKubernetesCode(condition.Reason),
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
				Type:               projectedKubernetesCode(string(condition.Type)),
				Status:             string(condition.Status),
				Reason:             projectedKubernetesCode(condition.Reason),
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
		reason := podReason(pod)
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
			Reason:    projectedKubernetesCode(reason),
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

func podReason(pod *corev1.Pod) string {
	if pod.Status.Reason != "" {
		return pod.Status.Reason
	}
	statuses := append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...)
	statuses = append(statuses, pod.Status.ContainerStatuses...)
	for _, status := range statuses {
		if status.State.Waiting != nil && status.State.Waiting.Reason != "" {
			return status.State.Waiting.Reason
		}
		if status.State.Terminated != nil && status.State.Terminated.ExitCode != 0 && status.State.Terminated.Reason != "" {
			return status.State.Terminated.Reason
		}
	}
	return ""
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
				Type:               projectedKubernetesCode(string(condition.Type)),
				Status:             string(condition.Status),
				Reason:             projectedKubernetesCode(condition.Reason),
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
	objects resourceIdentitySet,
) []KubernetesEvent {
	filtered := make([]corev1.Event, 0, len(events))
	for i := range events {
		event := &events[i]
		_, relevant := objects[resourceIdentity{
			APIVersion: event.InvolvedObject.APIVersion,
			Kind:       event.InvolvedObject.Kind,
			Namespace:  event.InvolvedObject.Namespace,
			Name:       event.InvolvedObject.Name,
			UID:        event.InvolvedObject.UID,
		}]
		// Kubernetes names are reusable, so an Event without an involved-object
		// UID is never attributed to the current incarnation by name alone.
		if relevant && event.InvolvedObject.UID != "" {
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
			Type:   projectedEventType(event.Type),
			Reason: projectedKubernetesCode(event.Reason),
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

func projectedKubernetesCode(value string) string {
	if value == "" {
		return ""
	}
	if len(value) > maxKubernetesCodeLength {
		return unknownDiagnosticCode
	}
	for index := range len(value) {
		character := value[index]
		letter := character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
		digit := character >= '0' && character <= '9'
		if index == 0 {
			if !letter {
				return unknownDiagnosticCode
			}
			continue
		}
		if !letter && !digit {
			return unknownDiagnosticCode
		}
	}
	return value
}

func projectedEventType(value string) string {
	switch value {
	case corev1.EventTypeNormal, corev1.EventTypeWarning:
		return value
	default:
		return unknownDiagnosticCode
	}
}

func capSlice[T any](items []T, limit int) []T {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}
