package controller

import (
	"context"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/resource"
)

type applyResult struct {
	Changed    bool
	Created    bool
	DriftPaths []string
}

var mergePatchObjectTypes = map[reflect.Type]struct{}{
	reflect.TypeOf(&batchv1.Job{}):      {},
	reflect.TypeOf(&corev1.ConfigMap{}): {},
	reflect.TypeOf(&corev1.Secret{}):    {},
	reflect.TypeOf(&corev1.Service{}):   {},
}

func applyCanonicalObject(ctx context.Context, c client.Client, desired client.Object) (applyResult, error) {
	normalizeUnstructuredObject(desired)

	live := emptyObjectLike(desired)
	key := types.NamespacedName{Name: desired.GetName(), Namespace: desired.GetNamespace()}
	if err := c.Get(ctx, key, live); err != nil {
		if apierrors.IsNotFound(err) {
			return applyResult{Changed: true, Created: true}, createCanonicalObject(ctx, c, desired)
		}
		return applyResult{}, err
	}

	original := live.DeepCopyObject().(client.Object)
	driftPaths := canonicalDriftPaths(live, desired)
	changed := copyCanonicalFields(live, desired)
	if !changed {
		return applyResult{}, nil
	}
	if !usesMergePatch(desired) {
		ensureTypeMeta(desired)
		return applyResult{Changed: true, DriftPaths: driftPaths}, c.Patch(ctx, desired, client.Apply, client.FieldOwner(resource.FieldOwner), client.ForceOwnership)
	}
	return applyResult{Changed: true, DriftPaths: driftPaths}, c.Patch(ctx, live, client.MergeFrom(original))
}

func createCanonicalObject(ctx context.Context, c client.Client, desired client.Object) error {
	normalizeUnstructuredObject(desired)

	if usesMergePatch(desired) {
		return c.Create(ctx, desired)
	}
	ensureTypeMeta(desired)
	return c.Patch(ctx, desired, client.Apply, client.FieldOwner(resource.FieldOwner), client.ForceOwnership)
}

func usesMergePatch(obj client.Object) bool {
	// Jobs are immutable once created, ConfigMaps hold generated state that must
	// not claim field ownership from storage/schema jobs, and Secrets may carry
	// operator-generated or user-supplied credential material.
	_, ok := mergePatchObjectTypes[reflect.TypeOf(obj)]
	return ok
}

func emptyObjectLike(obj client.Object) client.Object {
	if typed, ok := obj.(*unstructured.Unstructured); ok {
		empty := &unstructured.Unstructured{}
		empty.SetGroupVersionKind(typed.GroupVersionKind())
		return empty
	}
	return reflect.New(reflect.TypeOf(obj).Elem()).Interface().(client.Object)
}

func normalizeUnstructuredObject(obj client.Object) {
	typed, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return
	}
	normalized, ok := normalizeJSONValue(typed.Object).(map[string]interface{})
	if ok {
		typed.Object = normalized
	}
}

func normalizeJSONValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		normalized := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			normalized[key] = normalizeJSONValue(item)
		}
		return normalized
	case []interface{}:
		normalized := make([]interface{}, len(typed))
		for index, item := range typed {
			normalized[index] = normalizeJSONValue(item)
		}
		return normalized
	default:
		reflected := reflect.ValueOf(value)
		if !reflected.IsValid() {
			return value
		}
		switch reflected.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return reflected.Int()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return reflected.Uint()
		case reflect.Float32, reflect.Float64:
			return reflected.Convert(reflect.TypeOf(float64(0))).Float()
		case reflect.Slice:
			if _, ok := value.([]byte); ok {
				return value
			}
			normalized := make([]interface{}, reflected.Len())
			for index := 0; index < reflected.Len(); index++ {
				normalized[index] = normalizeJSONValue(reflected.Index(index).Interface())
			}
			return normalized
		default:
			return value
		}
	}
}

func ensureTypeMeta(obj client.Object) {
	switch typed := obj.(type) {
	case *appsv1.Deployment:
		typed.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"}
	case *autoscalingv2.HorizontalPodAutoscaler:
		typed.TypeMeta = metav1.TypeMeta{APIVersion: "autoscaling/v2", Kind: "HorizontalPodAutoscaler"}
	case *batchv1.Job:
		typed.TypeMeta = metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"}
	case *corev1.ConfigMap:
		typed.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"}
	case *corev1.PersistentVolumeClaim:
		typed.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"}
	case *corev1.Service:
		typed.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Service"}
	case *corev1.ServiceAccount:
		typed.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"}
	case *networkingv1.Ingress:
		typed.TypeMeta = metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "Ingress"}
	case *networkingv1.NetworkPolicy:
		typed.TypeMeta = metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"}
	case *policyv1.PodDisruptionBudget:
		typed.TypeMeta = metav1.TypeMeta{APIVersion: "policy/v1", Kind: "PodDisruptionBudget"}
	case *gatewayv1.HTTPRoute:
		typed.TypeMeta = metav1.TypeMeta{APIVersion: gatewayv1.GroupVersion.String(), Kind: "HTTPRoute"}
	case *cnpgv1.Cluster:
		typed.TypeMeta = metav1.TypeMeta{APIVersion: cnpgv1.GroupVersion.String(), Kind: "Cluster"}
	case *cnpgv1.ScheduledBackup:
		typed.TypeMeta = metav1.TypeMeta{APIVersion: cnpgv1.GroupVersion.String(), Kind: "ScheduledBackup"}
	case *tamossv1alpha1.StorageBackend:
		typed.TypeMeta = metav1.TypeMeta{APIVersion: tamossv1alpha1.GroupVersion.String(), Kind: "StorageBackend"}
	}
}

func copyCanonicalFields(live, desired client.Object) bool {
	changed := false
	if !apiequality.Semantic.DeepEqual(live.GetLabels(), desired.GetLabels()) {
		live.SetLabels(desired.GetLabels())
		changed = true
	}
	desiredAnnotations := canonicalAnnotations(live.GetAnnotations(), desired.GetAnnotations())
	if !apiequality.Semantic.DeepEqual(live.GetAnnotations(), desiredAnnotations) {
		live.SetAnnotations(desiredAnnotations)
		changed = true
	}

	switch live := live.(type) {
	case *appsv1.Deployment:
		desired := desired.(*appsv1.Deployment)
		spec := canonicalDeploymentSpec(live.Spec, desired.Spec)
		if !apiequality.Semantic.DeepEqual(live.Spec, spec) {
			live.Spec = spec
			changed = true
		}
	case *corev1.Service:
		desired := desired.(*corev1.Service)
		spec := desired.Spec
		preserveServiceAllocatedFields(&spec, live.Spec)
		if !apiequality.Semantic.DeepEqual(live.Spec, spec) {
			live.Spec = spec
			changed = true
		}
	case *corev1.Secret:
		desired := desired.(*corev1.Secret)
		if live.Type != desired.Type {
			live.Type = desired.Type
			changed = true
		}
		if data, ok := desiredSecretData(desired); ok && !apiequality.Semantic.DeepEqual(live.Data, data) {
			live.Data = data
			changed = true
		}
	case *corev1.ConfigMap:
		desired := desired.(*corev1.ConfigMap)
		if !apiequality.Semantic.DeepEqual(live.Data, desired.Data) {
			live.Data = desired.Data
			changed = true
		}
		if !apiequality.Semantic.DeepEqual(live.BinaryData, desired.BinaryData) {
			live.BinaryData = desired.BinaryData
			changed = true
		}
	case *corev1.PersistentVolumeClaim:
		desired := desired.(*corev1.PersistentVolumeClaim)
		if !apiequality.Semantic.DeepEqual(live.Spec, desired.Spec) {
			live.Spec = desired.Spec
			changed = true
		}
	case *corev1.ServiceAccount:
		desired := desired.(*corev1.ServiceAccount)
		if !apiequality.Semantic.DeepEqual(live.AutomountServiceAccountToken, desired.AutomountServiceAccountToken) {
			live.AutomountServiceAccountToken = desired.AutomountServiceAccountToken
			changed = true
		}
	case *networkingv1.Ingress:
		desired := desired.(*networkingv1.Ingress)
		if !apiequality.Semantic.DeepEqual(live.Spec, desired.Spec) {
			live.Spec = desired.Spec
			changed = true
		}
	case *networkingv1.NetworkPolicy:
		desired := desired.(*networkingv1.NetworkPolicy)
		if !apiequality.Semantic.DeepEqual(live.Spec, desired.Spec) {
			live.Spec = desired.Spec
			changed = true
		}
	case *policyv1.PodDisruptionBudget:
		desired := desired.(*policyv1.PodDisruptionBudget)
		if !apiequality.Semantic.DeepEqual(live.Spec, desired.Spec) {
			live.Spec = desired.Spec
			changed = true
		}
	case *autoscalingv2.HorizontalPodAutoscaler:
		desired := desired.(*autoscalingv2.HorizontalPodAutoscaler)
		if !apiequality.Semantic.DeepEqual(live.Spec, desired.Spec) {
			live.Spec = desired.Spec
			changed = true
		}
	case *gatewayv1.HTTPRoute:
		desired := desired.(*gatewayv1.HTTPRoute)
		if !apiequality.Semantic.DeepEqual(live.Spec, desired.Spec) {
			live.Spec = desired.Spec
			changed = true
		}
	case *tamossv1alpha1.StorageBackend:
		desired := desired.(*tamossv1alpha1.StorageBackend)
		if !apiequality.Semantic.DeepEqual(live.Spec, desired.Spec) {
			live.Spec = desired.Spec
			changed = true
		}
	case *unstructured.Unstructured:
		desired := desired.(*unstructured.Unstructured)
		for _, field := range []string{"data", "spec"} {
			value, found, _ := unstructured.NestedFieldCopy(desired.Object, field)
			if !found {
				continue
			}
			current, currentFound, _ := unstructured.NestedFieldCopy(live.Object, field)
			if currentFound && apiequality.Semantic.DeepEqual(current, value) {
				continue
			}
			live.Object[field] = value
			changed = true
		}
	}
	return changed
}

func canonicalDriftPaths(live, desired client.Object) []string {
	var paths []string
	if !apiequality.Semantic.DeepEqual(live.GetLabels(), desired.GetLabels()) {
		paths = append(paths, "metadata.labels")
	}
	if !apiequality.Semantic.DeepEqual(live.GetAnnotations(), canonicalAnnotations(live.GetAnnotations(), desired.GetAnnotations())) {
		paths = append(paths, "metadata.annotations")
	}

	switch live := live.(type) {
	case *appsv1.Deployment:
		desiredSpec := canonicalDeploymentSpec(live.Spec, desired.(*appsv1.Deployment).Spec)
		if !apiequality.Semantic.DeepEqual(live.Spec, desiredSpec) {
			paths = append(paths, "spec")
		}
	case *corev1.Service:
		desiredSpec := desired.(*corev1.Service).Spec
		preserveServiceAllocatedFields(&desiredSpec, live.Spec)
		if !apiequality.Semantic.DeepEqual(live.Spec, desiredSpec) {
			paths = append(paths, "spec")
		}
	case *corev1.Secret:
		desiredSecret := desired.(*corev1.Secret)
		if live.Type != desiredSecret.Type {
			paths = append(paths, "type")
		}
		if data, ok := desiredSecretData(desiredSecret); ok && !apiequality.Semantic.DeepEqual(live.Data, data) {
			paths = append(paths, "data")
		}
	case *corev1.ConfigMap:
		desiredConfigMap := desired.(*corev1.ConfigMap)
		if !apiequality.Semantic.DeepEqual(live.Data, desiredConfigMap.Data) {
			paths = append(paths, "data")
		}
		if !apiequality.Semantic.DeepEqual(live.BinaryData, desiredConfigMap.BinaryData) {
			paths = append(paths, "binaryData")
		}
	case *corev1.PersistentVolumeClaim:
		if !apiequality.Semantic.DeepEqual(live.Spec, desired.(*corev1.PersistentVolumeClaim).Spec) {
			paths = append(paths, "spec")
		}
	case *corev1.ServiceAccount:
		if !apiequality.Semantic.DeepEqual(live.AutomountServiceAccountToken, desired.(*corev1.ServiceAccount).AutomountServiceAccountToken) {
			paths = append(paths, "automountServiceAccountToken")
		}
	case *networkingv1.Ingress:
		if !apiequality.Semantic.DeepEqual(live.Spec, desired.(*networkingv1.Ingress).Spec) {
			paths = append(paths, "spec")
		}
	case *networkingv1.NetworkPolicy:
		if !apiequality.Semantic.DeepEqual(live.Spec, desired.(*networkingv1.NetworkPolicy).Spec) {
			paths = append(paths, "spec")
		}
	case *policyv1.PodDisruptionBudget:
		if !apiequality.Semantic.DeepEqual(live.Spec, desired.(*policyv1.PodDisruptionBudget).Spec) {
			paths = append(paths, "spec")
		}
	case *autoscalingv2.HorizontalPodAutoscaler:
		if !apiequality.Semantic.DeepEqual(live.Spec, desired.(*autoscalingv2.HorizontalPodAutoscaler).Spec) {
			paths = append(paths, "spec")
		}
	case *gatewayv1.HTTPRoute:
		if !apiequality.Semantic.DeepEqual(live.Spec, desired.(*gatewayv1.HTTPRoute).Spec) {
			paths = append(paths, "spec")
		}
	case *unstructured.Unstructured:
		desired := desired.(*unstructured.Unstructured)
		for _, field := range []string{"data", "spec"} {
			value, found, _ := unstructured.NestedFieldCopy(desired.Object, field)
			if !found {
				continue
			}
			current, currentFound, _ := unstructured.NestedFieldCopy(live.Object, field)
			if !currentFound || !apiequality.Semantic.DeepEqual(current, value) {
				paths = append(paths, field)
			}
		}
	}
	return paths
}

func canonicalAnnotations(live, desired map[string]string) map[string]string {
	if len(desired) == 0 && len(live) == 0 {
		return nil
	}
	next := map[string]string{}
	for key, value := range desired {
		next[key] = value
	}
	for key, value := range live {
		if isPreservedAnnotation(key) {
			next[key] = value
		}
	}
	if len(next) == 0 {
		return nil
	}
	return next
}

func isPreservedAnnotation(key string) bool {
	switch key {
	case "deployment.kubernetes.io/revision":
		return true
	default:
		return false
	}
}

func canonicalDeploymentSpec(live, desired appsv1.DeploymentSpec) appsv1.DeploymentSpec {
	spec := *desired.DeepCopy()
	if spec.RevisionHistoryLimit == nil {
		spec.RevisionHistoryLimit = live.RevisionHistoryLimit
	}
	if spec.ProgressDeadlineSeconds == nil {
		spec.ProgressDeadlineSeconds = live.ProgressDeadlineSeconds
	}
	if spec.Strategy.Type == "" && spec.Strategy.RollingUpdate == nil {
		spec.Strategy = live.Strategy
	}
	canonicalPodSpec(&spec.Template.Spec, live.Template.Spec)
	return spec
}

func canonicalPodSpec(spec *corev1.PodSpec, live corev1.PodSpec) {
	if spec.RestartPolicy == "" {
		spec.RestartPolicy = live.RestartPolicy
	}
	if spec.DNSPolicy == "" {
		spec.DNSPolicy = live.DNSPolicy
	}
	if spec.SchedulerName == "" {
		spec.SchedulerName = live.SchedulerName
	}
	if spec.TerminationGracePeriodSeconds == nil {
		spec.TerminationGracePeriodSeconds = live.TerminationGracePeriodSeconds
	}
	if spec.DeprecatedServiceAccount == "" {
		spec.DeprecatedServiceAccount = live.DeprecatedServiceAccount
	}
	if spec.SecurityContext == nil {
		spec.SecurityContext = live.SecurityContext
	}
	if spec.EnableServiceLinks == nil {
		spec.EnableServiceLinks = live.EnableServiceLinks
	}
	for index := range spec.Containers {
		if index >= len(live.Containers) {
			continue
		}
		canonicalContainer(&spec.Containers[index], live.Containers[index])
	}
}

func canonicalContainer(container *corev1.Container, live corev1.Container) {
	if container.TerminationMessagePath == "" {
		container.TerminationMessagePath = live.TerminationMessagePath
	}
	if container.TerminationMessagePolicy == "" {
		container.TerminationMessagePolicy = live.TerminationMessagePolicy
	}
	for index := range container.Ports {
		if index >= len(live.Ports) {
			continue
		}
		if container.Ports[index].Protocol == "" {
			container.Ports[index].Protocol = live.Ports[index].Protocol
		}
	}
	canonicalProbe(container.LivenessProbe, live.LivenessProbe)
	canonicalProbe(container.ReadinessProbe, live.ReadinessProbe)
	canonicalProbe(container.StartupProbe, live.StartupProbe)
}

func canonicalProbe(probe, live *corev1.Probe) {
	if probe == nil || live == nil {
		return
	}
	if probe.TimeoutSeconds == 0 {
		probe.TimeoutSeconds = live.TimeoutSeconds
	}
	if probe.PeriodSeconds == 0 {
		probe.PeriodSeconds = live.PeriodSeconds
	}
	if probe.SuccessThreshold == 0 {
		probe.SuccessThreshold = live.SuccessThreshold
	}
	if probe.FailureThreshold == 0 {
		probe.FailureThreshold = live.FailureThreshold
	}
	if probe.HTTPGet != nil && live.HTTPGet != nil && probe.HTTPGet.Scheme == "" {
		probe.HTTPGet.Scheme = live.HTTPGet.Scheme
	}
}

func preserveServiceAllocatedFields(spec *corev1.ServiceSpec, live corev1.ServiceSpec) {
	spec.ClusterIP = live.ClusterIP
	spec.ClusterIPs = live.ClusterIPs
	spec.IPFamilies = live.IPFamilies
	spec.IPFamilyPolicy = live.IPFamilyPolicy
	spec.HealthCheckNodePort = live.HealthCheckNodePort
	spec.InternalTrafficPolicy = live.InternalTrafficPolicy
	spec.SessionAffinity = live.SessionAffinity
	spec.SessionAffinityConfig = live.SessionAffinityConfig
	spec.AllocateLoadBalancerNodePorts = live.AllocateLoadBalancerNodePorts
	spec.LoadBalancerClass = live.LoadBalancerClass
}

func desiredSecretData(secret *corev1.Secret) (map[string][]byte, bool) {
	if len(secret.Data) > 0 {
		return secret.Data, true
	}
	if len(secret.StringData) == 0 {
		return nil, false
	}
	data := make(map[string][]byte, len(secret.StringData))
	for key, value := range secret.StringData {
		data[key] = []byte(value)
	}
	return data, true
}
