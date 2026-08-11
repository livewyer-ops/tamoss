package workload_renderer

import (
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	profiledefaults "github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
)

func TestRenderToggles(t *testing.T) {
	cpuTarget := int32(80)
	maxUnavailable := intstr.FromInt(1)
	tests := []struct {
		name    string
		mutate  func(*tamossv1alpha1.Tamoss)
		want    []string
		wantNot []string
	}{
		{
			name: "worker disabled by default",
			want: []string{
				"Deployment/example-api",
				"Deployment/example-console",
				"Deployment/example-ui",
			},
			wantNot: []string{"Deployment/example-worker"},
		},
		{
			name: "worker enabled",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.Worker.Enabled = ptr.To(true)
			},
			want: []string{"Deployment/example-worker"},
		},
		{
			name: "ingress enabled",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.Ingress.Enabled = ptr.To(true)
			},
			want:    []string{"Ingress/example-api", "Ingress/example-ui"},
			wantNot: []string{"HTTPRoute/example-api"},
		},
		{
			name: "httpRoute enabled",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.HTTPRoute.Enabled = true
			},
			want:    []string{"HTTPRoute/example-api", "HTTPRoute/example-ui"},
			wantNot: []string{"Ingress/example-api"},
		},
		{
			name: "networkPolicy enabled",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.Worker.Enabled = ptr.To(true)
				tamoss.Spec.NetworkPolicy.Enabled = ptr.To(true)
			},
			want: []string{
				"NetworkPolicy/example-api",
				"NetworkPolicy/example-console",
				"NetworkPolicy/example-ui",
				"NetworkPolicy/example-worker",
			},
		},
		{
			name: "autoscaling enabled",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.API.Autoscaling.Enabled = true
				tamoss.Spec.API.Autoscaling.TargetCPUUtilizationPercentage = &cpuTarget
				tamoss.Spec.UI.Autoscaling.Enabled = true
				tamoss.Spec.UI.Autoscaling.TargetCPUUtilizationPercentage = &cpuTarget
				tamoss.Spec.Console.Autoscaling.Enabled = true
				tamoss.Spec.Console.Autoscaling.TargetCPUUtilizationPercentage = &cpuTarget
			},
			want: []string{"HorizontalPodAutoscaler/example-api", "HorizontalPodAutoscaler/example-ui", "HorizontalPodAutoscaler/example-console"},
		},
		{
			name: "pdb enabled",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.Worker.Enabled = ptr.To(true)
				tamoss.Spec.API.PDB.Enabled = ptr.To(true)
				tamoss.Spec.API.PDB.MaxUnavailable = &maxUnavailable
				tamoss.Spec.UI.PDB.Enabled = ptr.To(true)
				tamoss.Spec.UI.PDB.MaxUnavailable = &maxUnavailable
				tamoss.Spec.Worker.PDB.Enabled = ptr.To(true)
				tamoss.Spec.Worker.PDB.MaxUnavailable = &maxUnavailable
				tamoss.Spec.Console.PDB.Enabled = ptr.To(true)
				tamoss.Spec.Console.PDB.MaxUnavailable = &maxUnavailable
			},
			want: []string{
				"PodDisruptionBudget/example-api",
				"PodDisruptionBudget/example-console",
				"PodDisruptionBudget/example-ui",
				"PodDisruptionBudget/example-worker",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tamoss := rendererFixture()
			if tt.mutate != nil {
				tt.mutate(tamoss)
			}
			objects := Render(tamoss)
			for _, id := range tt.want {
				if !hasObject(objects, id) {
					t.Fatalf("expected rendered object %s in %v", id, renderedIDs(objects))
				}
			}
			for _, id := range tt.wantNot {
				if hasObject(objects, id) {
					t.Fatalf("did not expect rendered object %s in %v", id, renderedIDs(objects))
				}
			}
		})
	}
}

func TestRenderOrdersServicesBeforeDependentDeployments(t *testing.T) {
	objects := Render(rendererFixture())

	apiService := objectIndex(t, objects, "Service/example-api", func(obj client.Object) bool {
		_, ok := obj.(*corev1.Service)
		return ok && obj.GetName() == "example-api"
	})
	apiDeployment := objectIndex(t, objects, "Deployment/example-api", func(obj client.Object) bool {
		_, ok := obj.(*appsv1.Deployment)
		return ok && obj.GetName() == "example-api"
	})
	uiDeployment := objectIndex(t, objects, "Deployment/example-ui", func(obj client.Object) bool {
		_, ok := obj.(*appsv1.Deployment)
		return ok && obj.GetName() == "example-ui"
	})
	if apiService > apiDeployment || apiService > uiDeployment {
		t.Fatalf("expected API Service to render before dependent Deployments, got service=%d apiDeployment=%d uiDeployment=%d", apiService, apiDeployment, uiDeployment)
	}
}

func TestRenderExposesAPIInternalMetricsService(t *testing.T) {
	tamoss := rendererFixture()

	objects := Render(tamoss)

	apiService := serviceByName(t, objects, "example-api")
	if len(apiService.Annotations) != 0 {
		t.Fatalf("API Service should not expose metrics scrape annotations, got %#v", apiService.Annotations)
	}
	metricsService := serviceByName(t, objects, "example-api-metrics")
	if metricsService.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Fatalf("metrics Service should be ClusterIP, got %q", metricsService.Spec.Type)
	}
	if metricsService.Annotations[metricsPathAnnotation] != "/metrics" ||
		metricsService.Annotations[metricsPortAnnotation] != "9090" ||
		metricsService.Annotations[prometheusScrapeAnnotation] != "true" ||
		metricsService.Annotations[prometheusPathAnnotation] != "/metrics" ||
		metricsService.Annotations[prometheusPortAnnotation] != "9090" {
		t.Fatalf("expected metrics Service scrape annotations, got %#v", metricsService.Annotations)
	}
	if metricsService.Spec.Selector["app.kubernetes.io/component"] != "api" {
		t.Fatalf("metrics Service should select API pods, got %#v", metricsService.Spec.Selector)
	}
	if len(metricsService.Spec.Ports) != 1 ||
		metricsService.Spec.Ports[0].Name != metricsPortName ||
		metricsService.Spec.Ports[0].Port != 9090 ||
		metricsService.Spec.Ports[0].TargetPort.StrVal != metricsPortName {
		t.Fatalf("unexpected metrics Service ports: %#v", metricsService.Spec.Ports)
	}
	uiService := serviceByName(t, objects, "example-ui")
	if len(uiService.Annotations) != 0 {
		t.Fatalf("UI Service should not expose metrics scrape annotations, got %#v", uiService.Annotations)
	}
}

func TestRenderKeepsMetricsServiceSeparateFromConfiguredAPIServicePort(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Service.API.Ports = []corev1.ServicePort{{
		Name:       "http",
		Port:       9000,
		TargetPort: intstr.FromString("http"),
		Protocol:   corev1.ProtocolTCP,
	}}

	objects := Render(tamoss)

	apiService := serviceByName(t, objects, "example-api")
	if len(apiService.Annotations) != 0 {
		t.Fatalf("API Service should not expose metrics scrape annotations, got %#v", apiService.Annotations)
	}
	metricsService := serviceByName(t, objects, "example-api-metrics")
	if metricsService.Annotations[metricsPortAnnotation] != "9090" ||
		metricsService.Annotations[prometheusPortAnnotation] != "9090" {
		t.Fatalf("expected metrics Service port 9090, got %#v", metricsService.Annotations)
	}
}

func TestRenderUsesExplicitDevelopmentTagWhenImageTagOmitted(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.API.Image.Tag = ""
	tamoss.Spec.UI.Image.Tag = ""

	objects := Render(tamoss)

	api := deploymentByName(t, objects, "example-api")
	if got := api.Spec.Template.Spec.Containers[0].Image; got != "livewyer/tamoss-api:dev" {
		t.Fatalf("expected API image to use explicit development tag, got %q", got)
	}
	ui := deploymentByName(t, objects, "example-ui")
	if got := ui.Spec.Template.Spec.Containers[0].Image; got != "livewyer/tamoss-ui:dev" {
		t.Fatalf("expected UI image to use explicit development tag, got %q", got)
	}
}

func objectIndex(t *testing.T, objects []client.Object, id string, match func(client.Object) bool) int {
	t.Helper()
	for i, obj := range objects {
		if match(obj) {
			return i
		}
	}
	t.Fatalf("expected object %s in %v", id, renderedIDs(objects))
	return -1
}

func TestRenderAdvancedExtraResources(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Advanced.ExtraResources = []apiextensionsv1.JSON{{
		Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"example-extra"},"data":{"mode":"advanced"}}`),
	}}

	objects := Render(tamoss)

	extra := unstructuredByName(t, objects, "ConfigMap", "example-extra")
	if extra.GetNamespace() != tamoss.Namespace {
		t.Fatalf("expected advanced extra resource namespace %q, got %q", tamoss.Namespace, extra.GetNamespace())
	}
	if got, _, _ := unstructured.NestedString(extra.Object, "data", "mode"); got != "advanced" {
		t.Fatalf("expected advanced extra resource data, got %q", got)
	}
}

func TestRenderUsesServicePortForAPIContainer(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.API.Env = map[string]string{
		"TAMOSS_METRICS_BIND_ADDRESS": "127.0.0.1",
		"TAMOSS_METRICS_PORT":         "0",
	}
	tamoss.Spec.Service.API.Ports = []corev1.ServicePort{{
		Name:       "http",
		Port:       9000,
		TargetPort: intstr.FromString("http"),
		Protocol:   corev1.ProtocolTCP,
	}}

	objects := Render(tamoss)
	for _, obj := range objects {
		deployment, ok := obj.(*appsv1.Deployment)
		if !ok || deployment.Name != "example-api" {
			continue
		}
		container := deployment.Spec.Template.Spec.Containers[0]
		if got := container.Ports[0].ContainerPort; got != 9000 {
			t.Fatalf("expected API container port 9000, got %d", got)
		}
		if len(container.Ports) != 2 ||
			container.Ports[1].Name != metricsPortName ||
			container.Ports[1].ContainerPort != apiMetricsPort {
			t.Fatalf("expected API metrics container port, got %#v", container.Ports)
		}
		if envValue(container.Env, "TAMOSS_METRICS_PORT") != "9090" {
			t.Fatalf("expected API metrics port env, got %#v", container.Env)
		}
		if envValue(container.Env, "TAMOSS_METRICS_BIND_ADDRESS") != "0.0.0.0" {
			t.Fatalf("expected API metrics bind address env, got %#v", container.Env)
		}
		if envCount(container.Env, "TAMOSS_METRICS_PORT") != 1 ||
			envCount(container.Env, "TAMOSS_METRICS_BIND_ADDRESS") != 1 {
			t.Fatalf("expected operator-managed API metrics env once, got %#v", container.Env)
		}
		return
	}
	t.Fatalf("expected API deployment in %v", renderedIDs(objects))
}

func TestRenderExposesWorkerMetricsPort(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Worker.Enabled = ptr.To(true)
	tamoss.Spec.Worker.Env = map[string]string{
		"TAMOSS_METRICS_BIND_ADDRESS": "127.0.0.1",
		"TAMOSS_METRICS_PORT":         "0",
	}

	objects := Render(tamoss)
	for _, obj := range objects {
		deployment, ok := obj.(*appsv1.Deployment)
		if !ok || deployment.Name != "example-worker" {
			continue
		}
		container := deployment.Spec.Template.Spec.Containers[0]
		if len(container.Ports) != 1 ||
			container.Ports[0].Name != metricsPortName ||
			container.Ports[0].ContainerPort != apiMetricsPort {
			t.Fatalf("expected worker metrics container port, got %#v", container.Ports)
		}
		if envValue(container.Env, "TAMOSS_METRICS_PORT") != "9090" {
			t.Fatalf("expected worker metrics port env, got %#v", container.Env)
		}
		if envValue(container.Env, "TAMOSS_METRICS_BIND_ADDRESS") != "0.0.0.0" {
			t.Fatalf("expected worker metrics bind address env, got %#v", container.Env)
		}
		if envCount(container.Env, "TAMOSS_METRICS_PORT") != 1 ||
			envCount(container.Env, "TAMOSS_METRICS_BIND_ADDRESS") != 1 {
			t.Fatalf("expected operator-managed worker metrics env once, got %#v", container.Env)
		}
		return
	}
	t.Fatalf("expected worker deployment in %v", renderedIDs(objects))
}

func TestRenderAPICORSAllowedOrigins(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Worker.Enabled = ptr.To(true)
	tamoss.Spec.API.CORS.AllowedOrigins = []string{
		"https://cuttingroom.github.io",
		" https://app.example.com ",
	}
	tamoss.Spec.API.CORS.AllowedOriginRegexes = []string{
		" ^https://[a-z0-9-]+\\.example-pages\\.com$ ",
	}

	objects := Render(tamoss)
	api := deploymentByName(t, objects, "example-api")
	if got := envValue(api.Spec.Template.Spec.Containers[0].Env, "TAMOSS_CORS_ALLOWED_ORIGINS"); got != "https://cuttingroom.github.io,https://app.example.com" {
		t.Fatalf("expected API CORS origins env, got %q", got)
	}
	if got := envValue(api.Spec.Template.Spec.Containers[0].Env, "TAMOSS_CORS_ALLOWED_ORIGIN_REGEXES"); got != "^https://[a-z0-9-]+\\.example-pages\\.com$" {
		t.Fatalf("expected API CORS origin regex env, got %q", got)
	}

	for _, name := range []string{"example-ui", "example-worker", "example-console"} {
		deployment := deploymentByName(t, objects, name)
		if hasEnv(deployment.Spec.Template.Spec.Containers[0].Env, "TAMOSS_CORS_ALLOWED_ORIGINS") {
			t.Fatalf("%s should not receive API CORS origins env", name)
		}
		if hasEnv(deployment.Spec.Template.Spec.Containers[0].Env, "TAMOSS_CORS_ALLOWED_ORIGIN_REGEXES") {
			t.Fatalf("%s should not receive API CORS origin regex env", name)
		}
	}
}

func TestRenderMultiServerSecurityDefaults(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Profile = tamossv1alpha1.TamossProfileMultiServer
	profiledefaults.Apply(tamoss)

	objects := Render(tamoss)
	for _, name := range []string{"example-api", "example-ui", "example-worker", "example-console"} {
		deployment := deploymentByName(t, objects, name)
		podSecurityContext := deployment.Spec.Template.Spec.SecurityContext
		if podSecurityContext == nil || podSecurityContext.RunAsNonRoot == nil || !*podSecurityContext.RunAsNonRoot {
			t.Fatalf("expected %s pod runAsNonRoot, got %#v", name, podSecurityContext)
		}
		if podSecurityContext.SeccompProfile == nil || podSecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
			t.Fatalf("expected %s runtime default seccomp, got %#v", name, podSecurityContext)
		}
		container := deployment.Spec.Template.Spec.Containers[0]
		if container.SecurityContext == nil ||
			container.SecurityContext.AllowPrivilegeEscalation == nil ||
			*container.SecurityContext.AllowPrivilegeEscalation ||
			container.SecurityContext.Capabilities == nil ||
			len(container.SecurityContext.Capabilities.Drop) != 1 ||
			container.SecurityContext.Capabilities.Drop[0] != "ALL" {
			t.Fatalf("expected %s restricted container security context, got %#v", name, container.SecurityContext)
		}
		if len(container.Resources.Requests) == 0 {
			t.Fatalf("expected %s resource requests", name)
		}
		if !hasObject(objects, "PodDisruptionBudget/"+name) {
			t.Fatalf("expected PDB for %s in %v", name, renderedIDs(objects))
		}
		policy := networkPolicyByName(t, objects, name)
		if len(policy.Spec.PolicyTypes) != 2 {
			t.Fatalf("expected ingress and egress policy types for %s, got %#v", name, policy.Spec.PolicyTypes)
		}
	}
}

func TestRenderHTTPRouteDefaultsToComponentBackend(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.HTTPRoute.Enabled = true
	tamoss.Spec.HTTPRoute.ParentRefs = []tamossv1alpha1.HTTPRouteParentRef{{
		Name:        "public-gateway",
		Namespace:   "platform",
		SectionName: "https",
	}}
	tamoss.Spec.HTTPRoute.API.Hostnames = []string{"api.example.com"}

	objects := Render(tamoss)
	route := httpRouteByName(t, objects, "example-api")
	if len(route.Spec.Hostnames) != 1 || string(route.Spec.Hostnames[0]) != "api.example.com" {
		t.Fatalf("expected API HTTPRoute hostname, got %#v", route.Spec.Hostnames)
	}
	parentRefs := route.Spec.ParentRefs
	if len(parentRefs) != 1 {
		t.Fatalf("expected API HTTPRoute parentRefs, got %#v", parentRefs)
	}
	parentRef := parentRefs[0]
	if string(parentRef.Name) != "public-gateway" || parentRef.Namespace == nil || string(*parentRef.Namespace) != "platform" ||
		parentRef.SectionName == nil || string(*parentRef.SectionName) != "https" {
		t.Fatalf("expected configured parentRef, got %#v", parentRef)
	}
	if len(route.Spec.Rules) == 0 || len(route.Spec.Rules[0].BackendRefs) == 0 ||
		string(route.Spec.Rules[0].BackendRefs[0].Name) != "example-api" {
		t.Fatalf("expected default API backendRef on route: %#v", route.Spec.Rules)
	}
}

func TestValidateHTTPRouteFiltersReportsUnsupportedCombinations(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.HTTPRoute.Enabled = true
	tamoss.Spec.HTTPRoute.API.Filters = []apiextensionsv1.JSON{
		{Raw: []byte(`{"type":"RequestRedirect","requestRedirect":{"scheme":"https"}}`)},
		{Raw: []byte(`{"type":"URLRewrite","urlRewrite":{"hostname":"api.internal.example.com"}}`)},
	}

	invalid := ValidateHTTPRouteFilters(tamoss)
	if len(invalid) == 0 {
		t.Fatal("expected incompatible RequestRedirect and URLRewrite filters to be reported")
	}
}

func TestRenderUsesDedicatedWorkloadServiceAccount(t *testing.T) {
	tamoss := rendererFixture()
	objects := Render(tamoss)

	var serviceAccount *corev1.ServiceAccount
	var apiDeployment *appsv1.Deployment
	for _, obj := range objects {
		if obj.GetName() == "example-workload" {
			serviceAccount, _ = obj.(*corev1.ServiceAccount)
		}
		if deployment, ok := obj.(*appsv1.Deployment); ok && deployment.GetName() == "example-api" {
			apiDeployment = deployment
		}
	}
	if serviceAccount == nil {
		t.Fatalf("expected dedicated workload ServiceAccount in %v", renderedIDs(objects))
	}
	if serviceAccount.AutomountServiceAccountToken == nil || *serviceAccount.AutomountServiceAccountToken {
		t.Fatalf("expected automountServiceAccountToken=false, got %#v", serviceAccount.AutomountServiceAccountToken)
	}
	if apiDeployment == nil || apiDeployment.Spec.Template.Spec.ServiceAccountName != "example-workload" {
		t.Fatalf("expected API deployment to use example-workload ServiceAccount")
	}
}

func TestRenderConsoleUsesIsolatedReadOnlyIdentity(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Service.Type = corev1.ServiceTypeLoadBalancer
	tamoss.Spec.Service.Console.Ports = []corev1.ServicePort{{
		Name:       "http",
		Port:       8181,
		TargetPort: intstr.FromInt32(9999),
		Protocol:   corev1.ProtocolUDP,
		NodePort:   32000,
	}}
	tamoss.Spec.UI.Env = map[string]string{consoleUpstreamEnv: "http://untrusted.invalid"}
	profiledefaults.Apply(tamoss)
	objects := Render(tamoss)

	console := deploymentByName(t, objects, "example-console")
	if console.Spec.Template.Spec.ServiceAccountName != "example-console" ||
		console.Spec.Template.Spec.ServiceAccountName == serviceAccountName(tamoss) {
		t.Fatalf("Console must use its isolated ServiceAccount, got %q", console.Spec.Template.Spec.ServiceAccountName)
	}
	if console.Spec.Template.Spec.AutomountServiceAccountToken == nil || !*console.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatalf("Console must mount its scoped Kubernetes token")
	}
	container := console.Spec.Template.Spec.Containers[0]
	if container.Image != "livewyer/tamoss-console-api:1.0.0" || len(container.Ports) != 1 || container.Ports[0].ContainerPort != 8080 {
		t.Fatalf("unexpected Console image or port: %#v", container)
	}
	if envValue(container.Env, consoleInstanceEnv) != "example" || envValue(container.Env, consoleBindEnv) != ":8080" {
		t.Fatalf("unexpected Console scope env: %#v", container.Env)
	}
	if len(container.EnvFrom) != 0 {
		t.Fatalf("default Console must not consume Secrets via envFrom: %#v", container.EnvFrom)
	}
	if container.ReadinessProbe == nil || container.ReadinessProbe.HTTPGet == nil || container.ReadinessProbe.HTTPGet.Path != "/ui-api/v1/readyz" ||
		container.LivenessProbe == nil || container.LivenessProbe.HTTPGet == nil || container.LivenessProbe.HTTPGet.Path != "/ui-api/v1/healthz" {
		t.Fatalf("unexpected Console probes: readiness=%#v liveness=%#v", container.ReadinessProbe, container.LivenessProbe)
	}
	if container.SecurityContext == nil || container.SecurityContext.ReadOnlyRootFilesystem == nil || !*container.SecurityContext.ReadOnlyRootFilesystem ||
		container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatalf("unexpected Console security context: %#v", container.SecurityContext)
	}

	service := serviceByName(t, objects, "example-console")
	if service.Spec.Type != corev1.ServiceTypeClusterIP || service.Spec.Selector["app.kubernetes.io/component"] != "console" {
		t.Fatalf("Console Service must remain an internal ClusterIP: %#v", service.Spec)
	}
	if len(service.Spec.Ports) != 1 || service.Spec.Ports[0].Port != 8181 || service.Spec.Ports[0].TargetPort.StrVal != "http" ||
		service.Spec.Ports[0].Protocol != corev1.ProtocolTCP || service.Spec.Ports[0].NodePort != 0 {
		t.Fatalf("custom Console Service port must target the Console HTTP container port: %#v", service.Spec.Ports)
	}
	ui := deploymentByName(t, objects, "example-ui")
	uiEnv := ui.Spec.Template.Spec.Containers[0].Env
	if got := envValue(uiEnv, consoleUpstreamEnv); got != "http://example-console:8181" || envCount(uiEnv, consoleUpstreamEnv) != 1 {
		t.Fatalf("expected one operator-owned Console upstream, got %#v", uiEnv)
	}

	var account *corev1.ServiceAccount
	var role *rbacv1.Role
	var binding *rbacv1.RoleBinding
	for _, object := range objects {
		switch object := object.(type) {
		case *corev1.ServiceAccount:
			if object.Name == "example-console" {
				account = object
			}
		case *rbacv1.Role:
			if object.Name == "example-console" {
				role = object
			}
		case *rbacv1.RoleBinding:
			if object.Name == "example-console" {
				binding = object
			}
		}
	}
	if account == nil || account.AutomountServiceAccountToken == nil || !*account.AutomountServiceAccountToken {
		t.Fatalf("expected token-mounting Console ServiceAccount")
	}
	if role == nil || binding == nil || binding.RoleRef.Name != "example-console" ||
		len(binding.Subjects) != 1 || binding.Subjects[0].Name != "example-console" {
		t.Fatalf("unexpected Console RoleBinding: role=%#v binding=%#v", role, binding)
	}
	for _, rule := range role.Rules {
		for _, resource := range rule.Resources {
			if resource == "secrets" || resource == "pods/log" || resource == "pods/exec" || resource == "services/proxy" {
				t.Fatalf("Console Role exposes forbidden resource %q", resource)
			}
			if resource == "tamosses" && (len(rule.ResourceNames) != 1 || rule.ResourceNames[0] != "example") {
				t.Fatalf("Console Tamoss access is not instance-scoped: %#v", rule)
			}
		}
		for _, verb := range rule.Verbs {
			if verb != "get" && verb != "list" && verb != "watch" {
				t.Fatalf("Console Role contains mutating verb %q", verb)
			}
		}
	}
}

func TestRenderConsoleCanBeDisabled(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Console.Enabled = ptr.To(false)
	objects := Render(tamoss)
	for _, id := range []string{
		"Deployment/example-console",
		"Service/example-console",
		"ServiceAccount/example-console",
		"Role/example-console",
		"RoleBinding/example-console",
	} {
		if hasObject(objects, id) {
			t.Fatalf("did not expect disabled Console object %s in %v", id, renderedIDs(objects))
		}
	}
}

func TestRenderConsoleIsOmittedByDefault(t *testing.T) {
	t.Parallel()
	tamoss := rendererFixture()
	tamoss.Spec.Console = tamossv1alpha1.ConsoleComponentSpec{}
	profiledefaults.Apply(tamoss)
	objects := Render(tamoss)
	if hasObject(objects, "Deployment/example-console") || hasObject(objects, "Role/example-console") {
		t.Fatalf("did not expect opt-in Console resources in %v", renderedIDs(objects))
	}
}

func TestRenderCNPGRuntimeSecrets(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Worker.Enabled = ptr.To(true)
	tamoss.Spec.Backends.DB = tamossv1alpha1.DBBackendSpec{
		ProvidedBy: tamossv1alpha1.BackendProvidedByCNPG,
		CNPG:       &tamossv1alpha1.DBCNPGSpec{},
	}

	objects := Render(tamoss)
	for _, name := range []string{"example-api", "example-worker"} {
		deployment := deploymentByName(t, objects, name)
		container := deployment.Spec.Template.Spec.Containers[0]
		if !hasSecretKeyEnv(container.Env, "POSTGRES_USER", "example-db-app", "username") ||
			!hasSecretKeyEnv(container.Env, "POSTGRES_PASSWORD", "example-db-app", "password") {
			t.Fatalf("%s should expose CNPG credentials as POSTGRES_* env vars, got %#v", name, container.Env)
		}
		if !hasEnvFromSecret(container.EnvFrom, "example-db-app") {
			t.Fatalf("%s should read CNPG app secret via envFrom, got %#v", name, container.EnvFrom)
		}
	}

	ui := deploymentByName(t, objects, "example-ui")
	if hasEnvFromSecret(ui.Spec.Template.Spec.Containers[0].EnvFrom, "example-db-app") {
		t.Fatalf("UI should not read CNPG app secret")
	}
}

func TestRenderRustFSOperatorRuntimeSecrets(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Worker.Enabled = ptr.To(true)
	tamoss.Spec.Backends.S3 = tamossv1alpha1.S3BackendSpec{
		ProvidedBy:     tamossv1alpha1.S3BackendProvidedByRustFSOperator,
		RustFSOperator: &tamossv1alpha1.S3RustFSOperatorSpec{},
	}

	objects := Render(tamoss)
	for _, name := range []string{"example-api", "example-worker"} {
		deployment := deploymentByName(t, objects, name)
		container := deployment.Spec.Template.Spec.Containers[0]
		if !hasEnvFromSecret(container.EnvFrom, "example-s3-creds") {
			t.Fatalf("%s should read RustFS credentials secret via envFrom, got %#v", name, container.EnvFrom)
		}
		if !hasEnv(container.Env, "TAMOSS_S3_ACCESS_KEY") || !hasEnv(container.Env, "TAMOSS_S3_SECRET_KEY") {
			t.Fatalf("%s should keep TAMOSS S3 env aliases for app config, got %#v", name, container.Env)
		}
	}

	ui := deploymentByName(t, objects, "example-ui")
	if hasEnvFromSecret(ui.Spec.Template.Spec.Containers[0].EnvFrom, "example-s3-creds") {
		t.Fatalf("UI should not read RustFS credentials secret")
	}
}

func TestRenderStorageBackendCredentialsRuntimeFileMount(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Worker.Enabled = ptr.To(true)

	objects := Render(tamoss)
	for _, name := range []string{"example-api", "example-worker"} {
		deployment := deploymentByName(t, objects, name)
		container := deployment.Spec.Template.Spec.Containers[0]
		if envValue(container.Env, "TAMOSS_STORAGE_BACKEND_CREDENTIALS_FILE") != "/run/tamoss/storage-backend-credentials/credentials.json" {
			t.Fatalf("%s should receive runtime credentials file path, got %#v", name, container.Env)
		}
		if !hasVolumeMount(container.VolumeMounts, "storage-backend-credentials", "/run/tamoss/storage-backend-credentials") {
			t.Fatalf("%s should mount runtime credentials secret, got %#v", name, container.VolumeMounts)
		}
		if !hasSecretVolume(deployment.Spec.Template.Spec.Volumes, "storage-backend-credentials", "example-storage-backend-credentials", 0o444) {
			t.Fatalf("%s should use runtime credentials secret volume, got %#v", name, deployment.Spec.Template.Spec.Volumes)
		}
	}

	ui := deploymentByName(t, objects, "example-ui")
	if hasSecretVolume(ui.Spec.Template.Spec.Volumes, "storage-backend-credentials", "example-storage-backend-credentials", 0o444) {
		t.Fatalf("UI should not mount runtime storage backend credentials")
	}
}

func TestRenderAuthentikOAuthCredentialSecret(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Worker.Enabled = ptr.To(true)
	tamoss.Spec.Auth = tamossv1alpha1.AuthSpec{
		ProvidedBy: tamossv1alpha1.AuthProvidedByAuthentikBlueprints,
		Required:   true,
		AuthentikBlueprints: &tamossv1alpha1.AuthentikBlueprintsSpec{
			PlatformNamespace: "authentik",
			IssuerURL:         "https://auth.example.com",
			InternalURL:       "http://authentik.auth.svc:9000",
			RedirectURIs:      []string{"https://app.example.com/auth/callback"},
		},
	}

	objects := Render(tamoss)
	for _, name := range []string{"example-api", "example-ui"} {
		deployment := deploymentByName(t, objects, name)
		container := deployment.Spec.Template.Spec.Containers[0]
		if !hasEnvFromSecret(container.EnvFrom, "example-oauth2-creds") {
			t.Fatalf("%s should read Authentik OAuth2 credentials via envFrom, got %#v", name, container.EnvFrom)
		}
		if envValue(container.Env, "TAMOSS_OAUTH2_ENABLED") != "true" {
			t.Fatalf("%s should enable OAuth2, got %#v", name, container.Env)
		}
		if envValue(container.Env, "TAMOSS_OAUTH2_ISSUER") != "https://auth.example.com/application/o/tamoss-tams-example/" {
			t.Fatalf("%s should use public issuer, got %#v", name, container.Env)
		}
		if envValue(container.Env, "TAMOSS_OAUTH2_JWKS_URI") != "http://authentik.auth.svc:9000/application/o/tamoss-tams-example/jwks/" {
			t.Fatalf("%s should use internal JWKS URI, got %#v", name, container.Env)
		}
		if hasEnv(container.Env, "TAMOSS_OAUTH2_ALLOW_UNSCOPED_FULL_ACCESS") {
			t.Fatalf("%s should rely on the API default for OAuth2 unscoped access, got %#v", name, container.Env)
		}
	}

	worker := deploymentByName(t, objects, "example-worker")
	if hasEnvFromSecret(worker.Spec.Template.Spec.Containers[0].EnvFrom, "example-oauth2-creds") {
		t.Fatalf("worker should not read Authentik OAuth2 credentials")
	}
}

func TestAuthEnvPreservesLegacyDefaultOrder(t *testing.T) {
	tamoss := rendererFixture()

	got := envNames(authEnv(tamoss))
	want := []string{
		"TAMOSS_AUTH_REQUIRED",
		"TAMOSS_TRUST_FORWARD_AUTH_HEADERS",
		"TAMOSS_OAUTH2_ENABLED",
		"TAMOSS_OAUTH2_ISSUER",
		"TAMOSS_OAUTH2_JWKS_URI",
		"TAMOSS_OAUTH2_AUDIENCE",
		"TAMOSS_OAUTH2_REQUIRED_SCOPES",
		"TAMOSS_OAUTH2_ALGORITHMS",
	}

	if !slices.Equal(got, want) {
		t.Fatalf("unexpected default auth env order:\ngot  %#v\nwant %#v", got, want)
	}
}

func TestRenderExternalOAuthCredentialSecret(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Worker.Enabled = ptr.To(true)
	tamoss.Spec.Auth = tamossv1alpha1.AuthSpec{
		ProvidedBy:                    tamossv1alpha1.AuthProvidedByExternal,
		Required:                      true,
		OAuth2AllowUnscopedFullAccess: ptr.To(false),
		External: &tamossv1alpha1.AuthExternalSpec{
			OAuth2: tamossv1alpha1.OAuth2Spec{
				Enabled:    true,
				Issuer:     "https://auth.example.com/application/o/tamoss/",
				JWKSURI:    "https://auth.example.com/application/o/tamoss/jwks/",
				Algorithms: []string{"RS256"},
				ClientCredentialsSecret: tamossv1alpha1.OAuth2ClientCredentialsSecretSpec{
					ExistingSecret: "external-oauth-creds",
				},
			},
		},
	}

	objects := Render(tamoss)
	for _, name := range []string{"example-api", "example-ui"} {
		deployment := deploymentByName(t, objects, name)
		container := deployment.Spec.Template.Spec.Containers[0]
		if !hasEnvFromSecret(container.EnvFrom, "external-oauth-creds") {
			t.Fatalf("%s should read external OAuth2 credentials via envFrom, got %#v", name, container.EnvFrom)
		}
		if hasEnvFromSecret(container.EnvFrom, "example-oauth2-creds") {
			t.Fatalf("%s should not read generated Authentik OAuth2 credentials", name)
		}
		if envValue(container.Env, "TAMOSS_OAUTH2_ENABLED") != "true" {
			t.Fatalf("%s should enable OAuth2, got %#v", name, container.Env)
		}
		if envValue(container.Env, "TAMOSS_OAUTH2_ALLOW_UNSCOPED_FULL_ACCESS") != "false" {
			t.Fatalf("%s should render strict OAuth2 unscoped access, got %#v", name, container.Env)
		}
	}

	worker := deploymentByName(t, objects, "example-worker")
	if hasEnvFromSecret(worker.Spec.Template.Spec.Containers[0].EnvFrom, "external-oauth-creds") {
		t.Fatalf("worker should not read external OAuth2 credentials")
	}
}

func TestRenderAuthentikTraefikForwardAuth(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Ingress.Enabled = ptr.To(true)
	tamoss.Spec.Ingress.ClassName = "traefik"
	tamoss.Spec.Ingress.Annotations = map[string]string{
		"cert-manager.io/cluster-issuer": "tamoss-selfsigned",
	}
	tamoss.Spec.Ingress.TLS = []networkingv1.IngressTLS{{
		SecretName: "tamoss-tls",
		Hosts:      []string{"api.example.com", "app.example.com"},
	}}
	tamoss.Spec.Ingress.API.Host = "api.example.com"
	tamoss.Spec.Ingress.UI.Web.Host = "app.example.com"
	tamoss.Spec.Auth = tamossv1alpha1.AuthSpec{
		ProvidedBy: tamossv1alpha1.AuthProvidedByAuthentikBlueprints,
		Required:   true,
		AuthentikBlueprints: &tamossv1alpha1.AuthentikBlueprintsSpec{
			PlatformNamespace: "auth",
			IssuerURL:         "https://auth.example.com",
			InternalURL:       "http://authentik-server.auth.svc.cluster.local",
		},
	}

	objects := Render(tamoss)
	if !hasObject(objects, "Middleware/example-authentik") ||
		!hasObject(objects, "Service/example-authentik-outpost") ||
		!hasObject(objects, "Ingress/example-authentik-outpost") {
		t.Fatalf("expected Authentik forward-auth resources in %v", renderedIDs(objects))
	}
	middleware := unstructuredByName(t, objects, "Middleware", "example-authentik")
	address, _, _ := unstructured.NestedString(middleware.Object, "spec", "forwardAuth", "address")
	if address != "http://authentik-server.auth.svc.cluster.local/outpost.goauthentik.io/auth/traefik" {
		t.Fatalf("expected Authentik forward-auth address, got %q", address)
	}
	headers, _, _ := unstructured.NestedStringSlice(middleware.Object, "spec", "forwardAuth", "authResponseHeaders")
	if len(headers) == 0 || headers[0] != "X-authentik-username" {
		t.Fatalf("expected Authentik response headers, got %#v", headers)
	}

	uiIngress := ingressByName(t, objects, "example-ui")
	if got := uiIngress.Annotations[traefikMiddlewareAnnotation]; got != "tams-example-authentik@kubernetescrd" {
		t.Fatalf("expected UI ingress Authentik middleware annotation, got %q", got)
	}
	apiIngress := ingressByName(t, objects, "example-api")
	if got := apiIngress.Annotations[traefikMiddlewareAnnotation]; got != "" {
		t.Fatalf("API ingress should not receive Authentik middleware, got %q", got)
	}
	outpostIngress := ingressByName(t, objects, "example-authentik-outpost")
	if got := outpostIngress.Spec.Rules[0].HTTP.Paths[0].Path; got != "/outpost.goauthentik.io/" {
		t.Fatalf("expected Authentik outpost path, got %q", got)
	}
	outpostService := serviceByName(t, objects, "example-authentik-outpost")
	if outpostService.Spec.Type != corev1.ServiceTypeExternalName ||
		outpostService.Spec.ExternalName != "authentik-server.auth.svc.cluster.local" {
		t.Fatalf("unexpected Authentik outpost Service: %#v", outpostService.Spec)
	}
}

func TestRenderManagedS3PublicExposure(t *testing.T) {
	tests := []struct {
		name               string
		mutate             func(*tamossv1alpha1.Tamoss)
		corsOriginRegexes  []string
		wantIngress        bool
		wantService        string
		wantMiddleware     bool
		wantMiddlewareAnno bool
		wantTLSSecret      string
		wantOriginRegexes  []string
	}{
		{
			name: "rustfs operator routes to service alias",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.Backends.S3 = tamossv1alpha1.S3BackendSpec{
					ProvidedBy: tamossv1alpha1.S3BackendProvidedByRustFSOperator,
					RustFSOperator: &tamossv1alpha1.S3RustFSOperatorSpec{
						PublicEndpoint: tamossv1alpha1.S3PublicEndpointSpec{URL: "https://s3.example.com"},
					},
				}
			},
			wantIngress:        true,
			wantService:        "example-s3",
			wantMiddleware:     true,
			wantMiddlewareAnno: true,
			wantTLSSecret:      "tamoss-tls",
		},
		{
			name: "configured origin regexes are rendered",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.Backends.S3 = tamossv1alpha1.S3BackendSpec{
					ProvidedBy: tamossv1alpha1.S3BackendProvidedByRustFSOperator,
					RustFSOperator: &tamossv1alpha1.S3RustFSOperatorSpec{
						PublicEndpoint: tamossv1alpha1.S3PublicEndpointSpec{URL: "https://s3.example.com"},
					},
				}
			},
			corsOriginRegexes:  []string{" ", `^https://[a-z0-9-]+\.github\.io$`, `^https://[a-z0-9-]+\.github\.io$`},
			wantIngress:        true,
			wantService:        "example-s3",
			wantMiddleware:     true,
			wantMiddlewareAnno: true,
			wantTLSSecret:      "tamoss-tls",
			wantOriginRegexes:  []string{`^https://[a-z0-9-]+\.github\.io$`},
		},
		{
			name: "external s3 does not render managed ingress",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.Backends.S3 = tamossv1alpha1.S3BackendSpec{
					ProvidedBy: tamossv1alpha1.S3BackendProvidedByExternal,
					External: &tamossv1alpha1.S3ExternalSpec{
						Endpoint: tamossv1alpha1.S3EndpointSpec{
							Public: tamossv1alpha1.EndpointURLSpec{URL: "https://s3.example.com"},
						},
					},
				}
			},
			wantIngress: false,
		},
		{
			name: "managed s3 without public endpoint does not render ingress",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.Backends.S3 = tamossv1alpha1.S3BackendSpec{
					ProvidedBy:     tamossv1alpha1.S3BackendProvidedByRustFSOperator,
					RustFSOperator: &tamossv1alpha1.S3RustFSOperatorSpec{},
				}
			},
			wantIngress: false,
		},
		{
			name: "non traefik ingress omits traefik middleware",
			mutate: func(tamoss *tamossv1alpha1.Tamoss) {
				tamoss.Spec.Ingress.ClassName = "nginx"
				tamoss.Spec.Backends.S3 = tamossv1alpha1.S3BackendSpec{
					ProvidedBy: tamossv1alpha1.S3BackendProvidedByRustFSOperator,
					RustFSOperator: &tamossv1alpha1.S3RustFSOperatorSpec{
						PublicEndpoint: tamossv1alpha1.S3PublicEndpointSpec{URL: "https://s3.example.com"},
					},
				}
			},
			wantIngress:    true,
			wantService:    "example-s3",
			wantTLSSecret:  "tamoss-tls",
			wantMiddleware: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tamoss := rendererFixture()
			tamoss.Spec.Ingress.Enabled = ptr.To(true)
			tamoss.Spec.Ingress.ClassName = "traefik"
			tamoss.Spec.Ingress.Annotations = map[string]string{
				"cert-manager.io/cluster-issuer": "tamoss-selfsigned",
			}
			tamoss.Spec.Ingress.TLS = []networkingv1.IngressTLS{{
				SecretName: "tamoss-tls",
				Hosts:      []string{"api.example.com", "app.example.com"},
			}}
			tamoss.Spec.Ingress.API.Host = "api.example.com"
			tamoss.Spec.Ingress.UI.Web.Host = "app.example.com"
			tamoss.Spec.API.CORS.AllowedOrigins = []string{"https://cuttingroom.github.io"}
			tamoss.Spec.API.CORS.AllowedOriginRegexes = tt.corsOriginRegexes
			tt.mutate(tamoss)

			objects := Render(tamoss)
			if tt.wantIngress {
				ingress := ingressByName(t, objects, "example-s3")
				if got := ingress.Spec.Rules[0].Host; got != "s3.example.com" {
					t.Fatalf("expected S3 ingress host s3.example.com, got %q", got)
				}
				paths := ingress.Spec.Rules[0].HTTP.Paths
				if len(paths) != 2 {
					t.Fatalf("expected S3 ingress to expose console and API paths, got %#v", paths)
				}
				consoleBackend := paths[0].Backend.Service
				if paths[0].Path != rustFSConsolePath ||
					consoleBackend == nil ||
					consoleBackend.Name != tt.wantService+"-console" ||
					consoleBackend.Port.Name != rustFSConsoleServicePort {
					t.Fatalf("expected RustFS console path to route to %s-console:%s, got %#v", tt.wantService, rustFSConsoleServicePort, paths[0])
				}
				backend := paths[1].Backend.Service
				if paths[1].Path != "/" {
					t.Fatalf("expected S3 API path /, got %q", paths[1].Path)
				}
				if backend == nil || backend.Name != tt.wantService || backend.Port.Name != "s3" {
					t.Fatalf("expected S3 ingress backend %s:s3, got %#v", tt.wantService, backend)
				}
				if ingress.Spec.IngressClassName == nil || *ingress.Spec.IngressClassName != tamoss.Spec.Ingress.ClassName {
					t.Fatalf("expected inherited ingress class %q, got %#v", tamoss.Spec.Ingress.ClassName, ingress.Spec.IngressClassName)
				}
				if got := ingress.Annotations["cert-manager.io/cluster-issuer"]; got != "tamoss-selfsigned" {
					t.Fatalf("expected inherited cert-manager annotation, got %q", got)
				}
				if tt.wantTLSSecret != "" {
					if len(ingress.Spec.TLS) != 1 || ingress.Spec.TLS[0].SecretName != tt.wantTLSSecret ||
						len(ingress.Spec.TLS[0].Hosts) != 1 || ingress.Spec.TLS[0].Hosts[0] != "s3.example.com" {
						t.Fatalf("unexpected S3 ingress TLS: %#v", ingress.Spec.TLS)
					}
				}
				if tt.wantMiddlewareAnno {
					if got := ingress.Annotations[traefikMiddlewareAnnotation]; got != "tams-example-s3-cors@kubernetescrd" {
						t.Fatalf("expected S3 CORS middleware annotation, got %q", got)
					}
				} else if got := ingress.Annotations[traefikMiddlewareAnnotation]; got != "" {
					t.Fatalf("did not expect S3 CORS middleware annotation, got %q", got)
				}
			} else if hasObject(objects, "Ingress/example-s3") {
				t.Fatalf("did not expect managed S3 ingress in %v", renderedIDs(objects))
			}

			if tt.wantMiddleware {
				middleware := unstructuredByName(t, objects, "Middleware", "example-s3-cors")
				origins, _, _ := unstructured.NestedStringSlice(middleware.Object, "spec", "headers", "accessControlAllowOriginList")
				if len(origins) != 2 ||
					origins[0] != "https://app.example.com" ||
					origins[1] != "https://cuttingroom.github.io" {
					t.Fatalf("expected S3 CORS origins for UI and browser tools, got %#v", origins)
				}
				methods, _, _ := unstructured.NestedStringSlice(middleware.Object, "spec", "headers", "accessControlAllowMethods")
				if len(methods) != 5 || methods[0] != "GET" || methods[4] != "OPTIONS" {
					t.Fatalf("unexpected CORS methods: %#v", methods)
				}
				headers, _, _ := unstructured.NestedStringSlice(middleware.Object, "spec", "headers", "accessControlAllowHeaders")
				if !slices.Contains(headers, "Range") || !slices.Contains(headers, "X-Requested-With") {
					t.Fatalf("expected video/XHR request headers to be allowed, got %#v", headers)
				}
				expose, _, _ := unstructured.NestedStringSlice(middleware.Object, "spec", "headers", "accessControlExposeHeaders")
				if !slices.Contains(expose, "Content-Range") || !slices.Contains(expose, "Accept-Ranges") {
					t.Fatalf("expected video range response headers to be exposed, got %#v", expose)
				}
				regexes, _, _ := unstructured.NestedStringSlice(middleware.Object, "spec", "headers", "accessControlAllowOriginListRegex")
				if len(tt.wantOriginRegexes) == 0 {
					if len(regexes) > 0 {
						t.Fatalf("did not expect configured origin regexes, got %#v", regexes)
					}
				} else if !slices.Equal(regexes, tt.wantOriginRegexes) {
					t.Fatalf("expected configured origin regexes %#v, got %#v", tt.wantOriginRegexes, regexes)
				}
			} else if hasObject(objects, "Middleware/example-s3-cors") {
				t.Fatalf("did not expect Traefik S3 CORS middleware in %v", renderedIDs(objects))
			}
		})
	}
}

func rendererFixture() *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "tams",
		},
		Spec: tamossv1alpha1.TamossSpec{
			API: tamossv1alpha1.APIComponentSpec{
				Enabled: ptr.To(true),
				Image: tamossv1alpha1.ImageSpec{
					Repository: "livewyer/tamoss-api",
					Tag:        "1.0.0",
					PullPolicy: corev1.PullIfNotPresent,
				},
				WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{ReplicaCount: ptr.To[int32](1)},
			},
			Worker: tamossv1alpha1.WorkerComponentSpec{
				WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{ReplicaCount: ptr.To[int32](1)},
			},
			UI: tamossv1alpha1.UIComponentSpec{
				Enabled: ptr.To(true),
				Image: tamossv1alpha1.ImageSpec{
					Repository: "livewyer/tamoss-ui",
					Tag:        "1.0.0",
					PullPolicy: corev1.PullIfNotPresent,
				},
				WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{ReplicaCount: ptr.To[int32](1)},
			},
			Console: tamossv1alpha1.ConsoleComponentSpec{
				Enabled: ptr.To(true),
				Image: tamossv1alpha1.ImageSpec{
					Repository: "livewyer/tamoss-console-api",
					Tag:        "1.0.0",
					PullPolicy: corev1.PullIfNotPresent,
				},
				WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{ReplicaCount: ptr.To[int32](1)},
			},
			Backends: tamossv1alpha1.BackendsSpec{
				DB: tamossv1alpha1.DBBackendSpec{
					ProvidedBy: tamossv1alpha1.BackendProvidedByExternal,
					External: &tamossv1alpha1.DBExternalSpec{
						Host:     "postgresql",
						Port:     "5432",
						Database: "tams",
						Auth: tamossv1alpha1.SecretReferenceSpec{
							ExistingSecret: "tams-postgresql-auth",
							SecretKeys: tamossv1alpha1.SecretKeySpec{
								Username: "username",
								Password: "password",
							},
						},
					},
				},
				S3: tamossv1alpha1.S3BackendSpec{
					ProvidedBy: tamossv1alpha1.S3BackendProvidedByExternal,
					External: &tamossv1alpha1.S3ExternalSpec{
						Endpoint: tamossv1alpha1.S3EndpointSpec{
							Default: tamossv1alpha1.EndpointURLSpec{URL: "http://rustfs-svc:9000"},
						},
						Auth: tamossv1alpha1.SecretReferenceSpec{
							ExistingSecret: "tams-rustfs-auth",
							SecretKeys: tamossv1alpha1.SecretKeySpec{
								AccessKey: "RUSTFS_ACCESS_KEY",
								SecretKey: "RUSTFS_SECRET_KEY",
							},
						},
						Region: "us-east-1",
						Bucket: "tamoss",
					},
				},
			},
			Auth: tamossv1alpha1.AuthSpec{
				ProvidedBy: tamossv1alpha1.AuthProvidedByExternal,
				Required:   true,
				External: &tamossv1alpha1.AuthExternalSpec{
					OAuth2: tamossv1alpha1.OAuth2Spec{
						Algorithms: []string{"RS256"},
					},
				},
			},
			Service: tamossv1alpha1.ServiceSpec{
				Enabled: true,
				Type:    corev1.ServiceTypeClusterIP,
			},
			ServiceAccount: tamossv1alpha1.ServiceAccountSpec{
				Create: true,
			},
			Secrets: tamossv1alpha1.SecretsSpec{
				APIToken: tamossv1alpha1.APITokenSecretSpec{
					Generate: true,
				},
			},
		},
	}
}

func ingressByName(t *testing.T, objects []client.Object, name string) *networkingv1.Ingress {
	t.Helper()
	for _, obj := range objects {
		ingress, ok := obj.(*networkingv1.Ingress)
		if ok && ingress.Name == name {
			return ingress
		}
	}
	t.Fatalf("expected ingress %s in %v", name, renderedIDs(objects))
	return nil
}

func serviceByName(t *testing.T, objects []client.Object, name string) *corev1.Service {
	t.Helper()
	for _, obj := range objects {
		service, ok := obj.(*corev1.Service)
		if ok && service.Name == name {
			return service
		}
	}
	t.Fatalf("expected service %s in %v", name, renderedIDs(objects))
	return nil
}

func networkPolicyByName(t *testing.T, objects []client.Object, name string) *networkingv1.NetworkPolicy {
	t.Helper()
	for _, obj := range objects {
		policy, ok := obj.(*networkingv1.NetworkPolicy)
		if ok && policy.Name == name {
			return policy
		}
	}
	t.Fatalf("expected NetworkPolicy %s in %v", name, renderedIDs(objects))
	return nil
}

func unstructuredByName(t *testing.T, objects []client.Object, kind, name string) *unstructured.Unstructured {
	t.Helper()
	for _, obj := range objects {
		item, ok := obj.(*unstructured.Unstructured)
		if ok && item.GetKind() == kind && item.GetName() == name {
			return item
		}
	}
	t.Fatalf("expected %s %s in %v", kind, name, renderedIDs(objects))
	return nil
}

func httpRouteByName(t *testing.T, objects []client.Object, name string) *gatewayv1.HTTPRoute {
	t.Helper()
	for _, obj := range objects {
		route, ok := obj.(*gatewayv1.HTTPRoute)
		if ok && route.Name == name {
			return route
		}
	}
	t.Fatalf("expected HTTPRoute %s in %v", name, renderedIDs(objects))
	return nil
}

func deploymentByName(t *testing.T, objects []client.Object, name string) *appsv1.Deployment {
	t.Helper()
	for _, obj := range objects {
		deployment, ok := obj.(*appsv1.Deployment)
		if ok && deployment.Name == name {
			return deployment
		}
	}
	t.Fatalf("expected deployment %s in %v", name, renderedIDs(objects))
	return nil
}

func hasEnv(env []corev1.EnvVar, name string) bool {
	for _, item := range env {
		if item.Name == name {
			return true
		}
	}
	return false
}

func envValue(env []corev1.EnvVar, name string) string {
	for _, item := range env {
		if item.Name == name {
			return item.Value
		}
	}
	return ""
}

func envCount(env []corev1.EnvVar, name string) int {
	count := 0
	for _, item := range env {
		if item.Name == name {
			count++
		}
	}
	return count
}

func envNames(env []corev1.EnvVar) []string {
	names := make([]string, 0, len(env))
	for _, item := range env {
		names = append(names, item.Name)
	}
	return names
}

func hasSecretKeyEnv(env []corev1.EnvVar, name, secretName, key string) bool {
	for _, item := range env {
		if item.Name != name || item.ValueFrom == nil || item.ValueFrom.SecretKeyRef == nil {
			continue
		}
		if item.ValueFrom.SecretKeyRef.Name == secretName && item.ValueFrom.SecretKeyRef.Key == key {
			return true
		}
	}
	return false
}

func hasEnvFromSecret(envFrom []corev1.EnvFromSource, name string) bool {
	for _, item := range envFrom {
		if item.SecretRef != nil && item.SecretRef.Name == name {
			return true
		}
	}
	return false
}

func hasVolumeMount(mounts []corev1.VolumeMount, name, mountPath string) bool {
	for _, item := range mounts {
		if item.Name == name && item.MountPath == mountPath && item.ReadOnly {
			return true
		}
	}
	return false
}

func hasSecretVolume(volumes []corev1.Volume, name, secretName string, mode int32) bool {
	for _, item := range volumes {
		if item.Name == name && item.Secret != nil && item.Secret.SecretName == secretName {
			if item.Secret.DefaultMode == nil || *item.Secret.DefaultMode != mode {
				return false
			}
			if len(item.Secret.Items) != 1 || item.Secret.Items[0].Mode == nil || *item.Secret.Items[0].Mode != mode {
				return false
			}
			return true
		}
	}
	return false
}

func hasObject(objects []client.Object, id string) bool {
	for _, obj := range objects {
		if objectID(obj) == id {
			return true
		}
	}
	return false
}

func renderedIDs(objects []client.Object) []string {
	ids := make([]string, 0, len(objects))
	for _, obj := range objects {
		ids = append(ids, objectID(obj))
	}
	return ids
}

func objectID(obj client.Object) string {
	switch obj := obj.(type) {
	case *appsv1.Deployment:
		return "Deployment/" + obj.GetName()
	case *autoscalingv2.HorizontalPodAutoscaler:
		return "HorizontalPodAutoscaler/" + obj.GetName()
	case *corev1.ConfigMap:
		return "ConfigMap/" + obj.GetName()
	case *corev1.Secret:
		return "Secret/" + obj.GetName()
	case *corev1.Service:
		return "Service/" + obj.GetName()
	case *corev1.ServiceAccount:
		return "ServiceAccount/" + obj.GetName()
	case *networkingv1.Ingress:
		return "Ingress/" + obj.GetName()
	case *networkingv1.NetworkPolicy:
		return "NetworkPolicy/" + obj.GetName()
	case *policyv1.PodDisruptionBudget:
		return "PodDisruptionBudget/" + obj.GetName()
	case *rbacv1.Role:
		return "Role/" + obj.GetName()
	case *rbacv1.RoleBinding:
		return "RoleBinding/" + obj.GetName()
	case *gatewayv1.HTTPRoute:
		return "HTTPRoute/" + obj.GetName()
	case *unstructured.Unstructured:
		return obj.GetKind() + "/" + obj.GetName()
	default:
		return "Unknown/" + obj.GetName()
	}
}
