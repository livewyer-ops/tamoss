package workload_renderer

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
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
			},
			want: []string{"HorizontalPodAutoscaler/example-api", "HorizontalPodAutoscaler/example-ui"},
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
			},
			want: []string{
				"PodDisruptionBudget/example-api",
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
		return
	}
	t.Fatalf("expected API deployment in %v", renderedIDs(objects))
}

func TestRenderMultiServerSecurityDefaults(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Profile = tamossv1alpha1.TamossProfileMultiServer
	profiledefaults.Apply(tamoss)

	objects := Render(tamoss)
	for _, name := range []string{"example-api", "example-ui", "example-worker"} {
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
	}

	worker := deploymentByName(t, objects, "example-worker")
	if hasEnvFromSecret(worker.Spec.Template.Spec.Containers[0].EnvFrom, "example-oauth2-creds") {
		t.Fatalf("worker should not read Authentik OAuth2 credentials")
	}
}

func TestRenderExternalOAuthCredentialSecret(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Worker.Enabled = ptr.To(true)
	tamoss.Spec.Auth = tamossv1alpha1.AuthSpec{
		ProvidedBy: tamossv1alpha1.AuthProvidedByExternal,
		Required:   true,
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
		wantIngress        bool
		wantService        string
		wantMiddleware     bool
		wantMiddlewareAnno bool
		wantTLSSecret      string
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
			tt.mutate(tamoss)

			objects := Render(tamoss)
			if tt.wantIngress {
				ingress := ingressByName(t, objects, "example-s3")
				if got := ingress.Spec.Rules[0].Host; got != "s3.example.com" {
					t.Fatalf("expected S3 ingress host s3.example.com, got %q", got)
				}
				backend := ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service
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
				if len(origins) != 1 || origins[0] != "https://app.example.com" {
					t.Fatalf("expected CORS origin https://app.example.com, got %#v", origins)
				}
				methods, _, _ := unstructured.NestedStringSlice(middleware.Object, "spec", "headers", "accessControlAllowMethods")
				if len(methods) != 5 || methods[0] != "GET" || methods[4] != "OPTIONS" {
					t.Fatalf("unexpected CORS methods: %#v", methods)
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
	case *gatewayv1.HTTPRoute:
		return "HTTPRoute/" + obj.GetName()
	case *unstructured.Unstructured:
		return obj.GetKind() + "/" + obj.GetName()
	default:
		return "Unknown/" + obj.GetName()
	}
}
