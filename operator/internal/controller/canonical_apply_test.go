package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestCopyCanonicalFieldsPreservesServiceAllocatedFields(t *testing.T) {
	live := &corev1.Service{}
	live.Name = "example-api"
	live.Namespace = "default"
	internalTrafficPolicy := corev1.ServiceInternalTrafficPolicyCluster
	live.Spec = corev1.ServiceSpec{
		Type:      corev1.ServiceTypeClusterIP,
		ClusterIP: "10.96.0.12",
		ClusterIPs: []string{
			"10.96.0.12",
		},
		IPFamilies:            []corev1.IPFamily{corev1.IPv4Protocol},
		InternalTrafficPolicy: &internalTrafficPolicy,
		SessionAffinity:       corev1.ServiceAffinityNone,
		Selector:              map[string]string{"old": "selector"},
		Ports: []corev1.ServicePort{{
			Name:       "http",
			Port:       8000,
			TargetPort: intstr.FromString("http"),
		}},
	}

	desired := &corev1.Service{}
	desired.Name = live.Name
	desired.Namespace = live.Namespace
	desired.Labels = map[string]string{"app.kubernetes.io/name": "tamoss"}
	desired.Spec = corev1.ServiceSpec{
		Type:     corev1.ServiceTypeClusterIP,
		Selector: map[string]string{"app.kubernetes.io/component": "api"},
		Ports: []corev1.ServicePort{{
			Name:       "http",
			Port:       9000,
			TargetPort: intstr.FromString("http"),
		}},
	}

	changed := copyCanonicalFields(live, desired)
	if !changed {
		t.Fatal("expected canonical fields to update service")
	}
	if live.Spec.ClusterIP != "10.96.0.12" {
		t.Fatalf("expected ClusterIP to be preserved, got %q", live.Spec.ClusterIP)
	}
	if live.Spec.Ports[0].Port != 9000 {
		t.Fatalf("expected desired port 9000, got %d", live.Spec.Ports[0].Port)
	}
	if live.Spec.InternalTrafficPolicy == nil || *live.Spec.InternalTrafficPolicy != corev1.ServiceInternalTrafficPolicyCluster {
		t.Fatalf("expected defaulted internal traffic policy to be preserved, got %#v", live.Spec.InternalTrafficPolicy)
	}
	if live.Spec.SessionAffinity != corev1.ServiceAffinityNone {
		t.Fatalf("expected defaulted session affinity to be preserved, got %q", live.Spec.SessionAffinity)
	}
	if live.Spec.Selector["app.kubernetes.io/component"] != "api" {
		t.Fatalf("expected desired selector to be applied, got %#v", live.Spec.Selector)
	}
}

func TestApplyCanonicalObjectRemovesServicePortDrift(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}
	live := &corev1.Service{}
	live.Name = "example-api"
	live.Namespace = "default"
	internalTrafficPolicy := corev1.ServiceInternalTrafficPolicyCluster
	live.Spec = corev1.ServiceSpec{
		Type:                  corev1.ServiceTypeClusterIP,
		ClusterIP:             "10.96.0.12",
		InternalTrafficPolicy: &internalTrafficPolicy,
		SessionAffinity:       corev1.ServiceAffinityNone,
		Ports: []corev1.ServicePort{
			{Name: "http", Port: 8000, TargetPort: intstr.FromString("http")},
			{Name: "drift", Port: 9999, TargetPort: intstr.FromString("http")},
		},
		Selector: map[string]string{"app.kubernetes.io/component": "api"},
	}
	desired := &corev1.Service{}
	desired.Name = "example-api"
	desired.Namespace = "default"
	desired.Spec = corev1.ServiceSpec{
		Type:     corev1.ServiceTypeClusterIP,
		Ports:    []corev1.ServicePort{{Name: "http", Port: 8000, TargetPort: intstr.FromString("http")}},
		Selector: map[string]string{"app.kubernetes.io/component": "api"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(live).Build()

	result, err := applyCanonicalObject(ctx, c, desired)
	if err != nil {
		t.Fatalf("expected canonical Service apply to correct drift, got %v", err)
	}
	if !result.Changed {
		t.Fatal("expected Service drift to be changed")
	}
	updated := &corev1.Service{}
	if err := c.Get(ctx, client.ObjectKey{Name: "example-api", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get updated Service: %v", err)
	}
	if updated.Spec.ClusterIP != "10.96.0.12" {
		t.Fatalf("expected allocated ClusterIP to be preserved, got %q", updated.Spec.ClusterIP)
	}
	if got := len(updated.Spec.Ports); got != 1 {
		t.Fatalf("expected drift port to be removed, got %#v", updated.Spec.Ports)
	}
}

func TestCopyCanonicalFieldsUpdatesHTTPRouteSpec(t *testing.T) {
	live := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "example-api", Namespace: "default"},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"old.example.com"},
		},
	}
	port := gatewayv1.PortNumber(8000)
	desired := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: live.Name, Namespace: live.Namespace},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"api.example.com"},
			Rules: []gatewayv1.HTTPRouteRule{{
				BackendRefs: []gatewayv1.HTTPBackendRef{{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: gatewayv1.ObjectName("example-api"),
							Port: &port,
						},
					},
				}},
			}},
		},
	}

	if paths := canonicalDriftPaths(live, desired); len(paths) != 1 || paths[0] != "spec" {
		t.Fatalf("expected HTTPRoute spec drift, got %#v", paths)
	}
	if !copyCanonicalFields(live, desired) {
		t.Fatal("expected canonical fields to update HTTPRoute")
	}
	if len(live.Spec.Hostnames) != 1 || live.Spec.Hostnames[0] != "api.example.com" {
		t.Fatalf("expected desired hostnames, got %#v", live.Spec.Hostnames)
	}
	if len(live.Spec.Rules) != 1 || len(live.Spec.Rules[0].BackendRefs) != 1 ||
		live.Spec.Rules[0].BackendRefs[0].Port == nil || *live.Spec.Rules[0].BackendRefs[0].Port != 8000 {
		t.Fatalf("expected desired backend refs, got %#v", live.Spec.Rules)
	}
}

func TestApplyCanonicalObjectIgnoresDeploymentDefaults(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add appsv1 scheme: %v", err)
	}
	replicas := int32(1)
	revisionHistoryLimit := int32(10)
	progressDeadlineSeconds := int32(600)
	terminationGracePeriodSeconds := int64(30)
	live := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "example-api",
			Namespace:   "default",
			Annotations: map[string]string{"deployment.kubernetes.io/revision": "1"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:                &replicas,
			RevisionHistoryLimit:    &revisionHistoryLimit,
			ProgressDeadlineSeconds: &progressDeadlineSeconds,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge:       &intstr.IntOrString{Type: intstr.String, StrVal: "25%"},
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "25%"},
				},
			},
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "example"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "example"}},
				Spec: corev1.PodSpec{
					ServiceAccountName:            "example",
					DeprecatedServiceAccount:      "example",
					RestartPolicy:                 corev1.RestartPolicyAlways,
					DNSPolicy:                     corev1.DNSClusterFirst,
					SchedulerName:                 corev1.DefaultSchedulerName,
					TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
					SecurityContext:               &corev1.PodSecurityContext{},
					Containers: []corev1.Container{{
						Name:  "api",
						Image: "example:latest",
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path:   "/healthz",
									Port:   intstr.FromString("http"),
									Scheme: corev1.URISchemeHTTP,
								},
							},
							PeriodSeconds:    30,
							TimeoutSeconds:   5,
							SuccessThreshold: 1,
							FailureThreshold: 3,
						},
						TerminationMessagePath:   corev1.TerminationMessagePathDefault,
						TerminationMessagePolicy: corev1.TerminationMessageReadFile,
						Ports: []corev1.ContainerPort{{
							Name:          "http",
							ContainerPort: 8000,
							Protocol:      corev1.ProtocolTCP,
						}},
					}},
				},
			},
		},
	}
	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "example-api", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "example"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "example"}},
				Spec: corev1.PodSpec{
					ServiceAccountName: "example",
					Containers: []corev1.Container{{
						Name:  "api",
						Image: "example:latest",
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/healthz",
									Port: intstr.FromString("http"),
								},
							},
							PeriodSeconds:    30,
							TimeoutSeconds:   5,
							FailureThreshold: 3,
						},
						Ports: []corev1.ContainerPort{{
							Name:          "http",
							ContainerPort: 8000,
						}},
					}},
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(live).Build()

	result, err := applyCanonicalObject(ctx, c, desired)
	if err != nil {
		t.Fatalf("apply canonical Deployment: %v", err)
	}
	if result.Changed {
		t.Fatalf("expected Kubernetes defaulted Deployment to be unchanged, drift paths: %#v", result.DriftPaths)
	}
}
