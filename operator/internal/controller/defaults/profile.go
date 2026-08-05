package defaults

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	defaultAppName                     = "tamoss"
	defaultCertManagerIssuerAnnotation = "cert-manager.io/cluster-issuer"
	defaultLocalKindIssuerName         = "tamoss-selfsigned"
	defaultPublicIssuerName            = "tamoss-public"
	defaultLocalKindTLSSecretName      = "tamoss-localtest-tls"
	defaultLocalKindS3TLSSecretName    = "tamoss-localtest-s3-tls"
	defaultEdgeIssuerName              = "tamoss-edge-selfsigned"
	defaultEdgeTLSSecretName           = "tamoss-edge-tls"
	defaultEdgeS3TLSSecretName         = "tamoss-edge-s3-tls"
	defaultPublicTLSSecretName         = "tamoss-public-tls"
	defaultPublicS3TLSSecretName       = "tamoss-s3-public-tls"
)

// Apply fills omitted Tamoss fields with operator-owned defaults.
func Apply(tamoss *tamossv1alpha1.Tamoss) {
	if tamoss == nil {
		return
	}
	applyImageDefaults(tamoss)
	switch tamoss.Spec.Profile {
	case tamossv1alpha1.TamossProfileLocalKind:
		applyLocalKind(tamoss)
	case tamossv1alpha1.TamossProfileSingleServer:
		applySingleServer(tamoss)
	case tamossv1alpha1.TamossProfileMultiServer:
		applyMultiServer(tamoss)
	case tamossv1alpha1.TamossProfileEdge:
		applyEdge(tamoss)
	}
	applyPublicEndpointDefaults(tamoss)
	applyBaseComponentDefaults(tamoss)
}

func applyImageDefaults(tamoss *tamossv1alpha1.Tamoss) {
	if tamoss.Spec.API.Image.Repository == "" {
		tamoss.Spec.API.Image.Repository = DefaultAPIRepository
	}
	if tamoss.Spec.API.Image.Tag == "" {
		tamoss.Spec.API.Image.Tag = DefaultOperandTag
	}
	if tamoss.Spec.API.Image.PullPolicy == "" {
		tamoss.Spec.API.Image.PullPolicy = corev1.PullIfNotPresent
	}
	if tamoss.Spec.UI.Image.Repository == "" {
		tamoss.Spec.UI.Image.Repository = DefaultUIRepository
	}
	if tamoss.Spec.UI.Image.Tag == "" {
		tamoss.Spec.UI.Image.Tag = DefaultOperandTag
	}
	if tamoss.Spec.UI.Image.PullPolicy == "" {
		tamoss.Spec.UI.Image.PullPolicy = corev1.PullIfNotPresent
	}
	if tamoss.Spec.Images.SchemaMigrationPostgresClient == "" {
		tamoss.Spec.Images.SchemaMigrationPostgresClient = DefaultPostgresClientImage
	}
}

func applyBaseComponentDefaults(tamoss *tamossv1alpha1.Tamoss) {
	setBool(&tamoss.Spec.API.Enabled, true)
	setBool(&tamoss.Spec.Worker.Enabled, false)
	setBool(&tamoss.Spec.UI.Enabled, true)
	setReplicas(&tamoss.Spec.API.WorkloadCommonSpec, 1)
	setReplicas(&tamoss.Spec.Worker.WorkloadCommonSpec, 1)
	setReplicas(&tamoss.Spec.UI.WorkloadCommonSpec, 1)
	defaultAPIProbes(&tamoss.Spec.API.WorkloadCommonSpec)
	defaultWorkerProbes(&tamoss.Spec.Worker.WorkloadCommonSpec)
}

func applyLocalKind(tamoss *tamossv1alpha1.Tamoss) {
	defaultPlatformPublicEndpoint(tamoss, publicEndpointProfileDefaults{
		BaseDomain:      "tamoss.localtest.me",
		IssuerName:      defaultLocalKindIssuerName,
		TLSSecretName:   defaultLocalKindTLSSecretName,
		S3TLSSecretName: defaultLocalKindS3TLSSecretName,
	})
	setBool(&tamoss.Spec.Worker.Enabled, true)
	setEnvDefault(&tamoss.Spec.API.Env, "TAMOSS_S3_CONNECT_TIMEOUT_SECONDS", "1")
	setEnvDefault(&tamoss.Spec.API.Env, "TAMOSS_S3_READ_TIMEOUT_SECONDS", "2")
	setEnvDefault(&tamoss.Spec.API.Env, "TAMOSS_WEBHOOK_ALLOWED_HOSTS", ".svc.cluster.local")
	setEnvDefault(&tamoss.Spec.Worker.Env, "TAMOSS_WORKER_POLL_INTERVAL_SECONDS", "1")
	setEnvDefault(&tamoss.Spec.Worker.Env, "TAMOSS_WORKER_MAX_REQUESTS", "25")
	setEnvDefault(&tamoss.Spec.Worker.Env, "TAMOSS_WEBHOOK_ALLOWED_HOSTS", ".svc.cluster.local")
	setEnvDefault(&tamoss.Spec.UI.Env, "TAMOSS_API_URL", "/api")

	defaultCNPG(tamoss, 1, "10Gi", false, false)
	defaultRustFSOperator(tamoss, 1, 4, "10Gi")
	setRustFSEnvDefault(tamoss, "RUSTFS_UNSAFE_BYPASS_DISK_CHECK", "true")
}

func applySingleServer(tamoss *tamossv1alpha1.Tamoss) {
	defaultPlatformPublicEndpoint(tamoss, publicEndpointProfileDefaults{
		IssuerName:      defaultPublicIssuerName,
		TLSSecretName:   defaultPublicTLSSecretName,
		S3TLSSecretName: defaultPublicS3TLSSecretName,
	})
	setBool(&tamoss.Spec.Worker.Enabled, true)
	setEnvDefault(&tamoss.Spec.UI.Env, "TAMOSS_API_URL", "/api")
	defaultWorkloadResources(&tamoss.Spec.API.WorkloadCommonSpec, "250m", "384Mi", "1", "768Mi")
	defaultWorkloadResources(&tamoss.Spec.Worker.WorkloadCommonSpec, "100m", "128Mi", "500m", "384Mi")
	defaultWorkloadResources(&tamoss.Spec.UI.WorkloadCommonSpec, "25m", "32Mi", "200m", "128Mi")
	defaultRestrictedWorkloadSecurity(&tamoss.Spec.API.WorkloadCommonSpec)
	defaultRestrictedWorkloadSecurity(&tamoss.Spec.Worker.WorkloadCommonSpec)
	defaultRestrictedWorkloadSecurity(&tamoss.Spec.UI.WorkloadCommonSpec)

	defaultCNPG(tamoss, 1, "50Gi", false, false)
	defaultRustFSOperator(tamoss, 1, 4, "100Gi")
}

func applyEdge(tamoss *tamossv1alpha1.Tamoss) {
	defaultPlatformPublicEndpointWithoutAuthentik(tamoss, publicEndpointProfileDefaults{
		BaseDomain:      "tamoss.edge",
		IssuerName:      defaultEdgeIssuerName,
		TLSSecretName:   defaultEdgeTLSSecretName,
		S3TLSSecretName: defaultEdgeS3TLSSecretName,
	})
	// Edge defaults to bearer-token auth; declaring authentik-blueprints on
	// the spec opts the install into the managed OAuth stack instead.
	if tamoss.Spec.Auth.Provider() == tamossv1alpha1.AuthProvidedByAuthentikBlueprints {
		defaultAuthentikBlueprints(tamoss)
	} else {
		defaultTokenOnlyAuth(tamoss)
	}
	setBool(&tamoss.Spec.Worker.Enabled, true)
	setEnvDefault(&tamoss.Spec.API.Env, "TAMOSS_DATABASE_POOL_MAX_SIZE", "3")
	setEnvDefault(&tamoss.Spec.API.Env, "TAMOSS_API_THREAD_POOL_TOKENS", "16")
	setEnvDefault(&tamoss.Spec.API.Env, "TAMOSS_S3_CONNECT_TIMEOUT_SECONDS", "2")
	setEnvDefault(&tamoss.Spec.API.Env, "TAMOSS_S3_READ_TIMEOUT_SECONDS", "5")
	setEnvDefault(&tamoss.Spec.API.Env, "TAMOSS_WEBHOOK_ALLOWED_HOSTS", ".svc.cluster.local")
	setEnvDefault(&tamoss.Spec.Worker.Env, "TAMOSS_WORKER_POLL_INTERVAL_SECONDS", "5")
	setEnvDefault(&tamoss.Spec.Worker.Env, "TAMOSS_WORKER_MAX_REQUESTS", "5")
	setEnvDefault(&tamoss.Spec.Worker.Env, "TAMOSS_DATABASE_POOL_MAX_SIZE", "3")
	setEnvDefault(&tamoss.Spec.Worker.Env, "TAMOSS_WEBHOOK_ALLOWED_HOSTS", ".svc.cluster.local")
	setEnvDefault(&tamoss.Spec.UI.Env, "TAMOSS_API_URL", "/api")
	defaultEdgeWorkerProbes(&tamoss.Spec.Worker.WorkloadCommonSpec)
	defaultWorkloadResources(&tamoss.Spec.API.WorkloadCommonSpec, "100m", "192Mi", "600m", "384Mi")
	defaultWorkloadResources(&tamoss.Spec.Worker.WorkloadCommonSpec, "100m", "128Mi", "500m", "384Mi")
	defaultWorkloadResources(&tamoss.Spec.UI.WorkloadCommonSpec, "25m", "32Mi", "150m", "96Mi")
	defaultRestrictedWorkloadSecurity(&tamoss.Spec.API.WorkloadCommonSpec)
	defaultRestrictedWorkloadSecurity(&tamoss.Spec.Worker.WorkloadCommonSpec)
	defaultRestrictedWorkloadSecurity(&tamoss.Spec.UI.WorkloadCommonSpec)

	defaultCNPG(tamoss, 1, "10Gi", false, false)
	defaultCNPGResources(tamoss, "100m", "256Mi", "800m", "768Mi")
	defaultRustFSOperator(tamoss, 1, 4, "10Gi")
	setRustFSEnvDefault(tamoss, "RUSTFS_UNSAFE_BYPASS_DISK_CHECK", "true")
}

func applyMultiServer(tamoss *tamossv1alpha1.Tamoss) {
	defaultPlatformPublicEndpoint(tamoss, publicEndpointProfileDefaults{
		IssuerName:      defaultPublicIssuerName,
		TLSSecretName:   defaultPublicTLSSecretName,
		S3TLSSecretName: defaultPublicS3TLSSecretName,
	})
	setBool(&tamoss.Spec.Worker.Enabled, true)
	setReplicas(&tamoss.Spec.API.WorkloadCommonSpec, 2)
	setReplicas(&tamoss.Spec.Worker.WorkloadCommonSpec, 2)
	setReplicas(&tamoss.Spec.UI.WorkloadCommonSpec, 2)
	setBool(&tamoss.Spec.API.PDB.Enabled, true)
	setBool(&tamoss.Spec.Worker.PDB.Enabled, true)
	setBool(&tamoss.Spec.UI.PDB.Enabled, true)
	setEnvDefault(&tamoss.Spec.UI.Env, "TAMOSS_API_URL", "/api")
	defaultWorkloadResources(&tamoss.Spec.API.WorkloadCommonSpec, "250m", "384Mi", "1", "768Mi")
	defaultWorkloadResources(&tamoss.Spec.Worker.WorkloadCommonSpec, "100m", "128Mi", "500m", "384Mi")
	defaultWorkloadResources(&tamoss.Spec.UI.WorkloadCommonSpec, "25m", "32Mi", "200m", "128Mi")
	defaultRestrictedWorkloadSecurity(&tamoss.Spec.API.WorkloadCommonSpec)
	defaultRestrictedWorkloadSecurity(&tamoss.Spec.Worker.WorkloadCommonSpec)
	defaultRestrictedWorkloadSecurity(&tamoss.Spec.UI.WorkloadCommonSpec)
	defaultAffinity(tamoss, &tamoss.Spec.API.WorkloadCommonSpec, "api")
	defaultAffinity(tamoss, &tamoss.Spec.Worker.WorkloadCommonSpec, "worker")
	defaultAffinity(tamoss, &tamoss.Spec.UI.WorkloadCommonSpec, "ui")
	defaultMultiServerNetworkPolicy(tamoss)

	defaultCNPG(tamoss, 3, "100Gi", false, true)
	defaultRustFSOperator(tamoss, 4, 4, "100Gi")
}

type publicEndpointProfileDefaults struct {
	BaseDomain      string
	IssuerName      string
	TLSSecretName   string
	S3TLSSecretName string
}

func defaultPlatformPublicEndpoint(tamoss *tamossv1alpha1.Tamoss, defaults publicEndpointProfileDefaults) {
	defaultPlatformPublicEndpointWithoutAuthentik(tamoss, defaults)
	defaultAuthentikBlueprints(tamoss)
}

func defaultPlatformPublicEndpointWithoutAuthentik(tamoss *tamossv1alpha1.Tamoss, defaults publicEndpointProfileDefaults) {
	if tamoss.Spec.PublicEndpoint.BaseDomain == "" {
		tamoss.Spec.PublicEndpoint.BaseDomain = defaults.BaseDomain
	}
	if tamoss.Spec.PublicEndpoint.TLSSecretName == "" {
		tamoss.Spec.PublicEndpoint.TLSSecretName = defaults.TLSSecretName
	}
	if tamoss.Spec.PublicEndpoint.S3TLSSecretName == "" {
		tamoss.Spec.PublicEndpoint.S3TLSSecretName = defaults.S3TLSSecretName
	}
	setBool(&tamoss.Spec.Ingress.Enabled, true)
	if tamoss.Spec.Ingress.ClassName == "" {
		tamoss.Spec.Ingress.ClassName = "traefik"
	}
	if tamoss.Spec.Ingress.Annotations == nil {
		tamoss.Spec.Ingress.Annotations = map[string]string{
			defaultCertManagerIssuerAnnotation: defaults.IssuerName,
		}
	}
}

func defaultTokenOnlyAuth(tamoss *tamossv1alpha1.Tamoss) {
	if tamoss.Spec.Auth.Provider() == tamossv1alpha1.AuthProvidedByNone {
		return
	}
	if tamoss.Spec.Auth.Provider() == tamossv1alpha1.AuthProvidedByAuthentikBlueprints {
		return
	}
	tamoss.Spec.Auth.ProvidedBy = tamossv1alpha1.AuthProvidedByExternal
	tamoss.Spec.Auth.Required = true
	if tamoss.Spec.Auth.External == nil {
		tamoss.Spec.Auth.External = &tamossv1alpha1.AuthExternalSpec{}
	}
	if len(tamoss.Spec.Auth.External.OAuth2.Algorithms) == 0 {
		tamoss.Spec.Auth.External.OAuth2.Algorithms = []string{"RS256"}
	}
}

func defaultAuthentikBlueprints(tamoss *tamossv1alpha1.Tamoss) {
	if tamoss.Spec.Auth.Provider() == tamossv1alpha1.AuthProvidedByExternal && externalAuthConfigured(tamoss.Spec.Auth.External) {
		return
	}
	if tamoss.Spec.Auth.ProvidedBy == "" || tamoss.Spec.Auth.Provider() == tamossv1alpha1.AuthProvidedByExternal {
		tamoss.Spec.Auth.ProvidedBy = tamossv1alpha1.AuthProvidedByAuthentikBlueprints
		tamoss.Spec.Auth.External = nil
	}
	if tamoss.Spec.Auth.Provider() != tamossv1alpha1.AuthProvidedByAuthentikBlueprints {
		return
	}
	tamoss.Spec.Auth.Required = true
	if tamoss.Spec.Auth.AuthentikBlueprints == nil {
		tamoss.Spec.Auth.AuthentikBlueprints = &tamossv1alpha1.AuthentikBlueprintsSpec{}
	}
	authentik := tamoss.Spec.Auth.AuthentikBlueprints
	if authentik.PlatformNamespace == "" {
		authentik.PlatformNamespace = "auth"
	}
	if authentik.APITokenSecretRef.Name == "" {
		authentik.APITokenSecretRef.Name = "authentik"
	}
	if authentik.APITokenSecretRef.Key == "" {
		authentik.APITokenSecretRef.Key = "AUTHENTIK_BOOTSTRAP_TOKEN"
	}
}

func externalAuthConfigured(external *tamossv1alpha1.AuthExternalSpec) bool {
	if external == nil {
		return false
	}
	oauth2 := external.OAuth2
	return oauth2.Enabled ||
		oauth2.Issuer != "" ||
		oauth2.JWKSURI != "" ||
		oauth2.Audience != "" ||
		oauth2.ClientCredentialsSecret.ExistingSecret != ""
}

func applyPublicEndpointDefaults(tamoss *tamossv1alpha1.Tamoss) {
	baseDomain := normalizeBaseDomain(tamoss.Spec.PublicEndpoint.BaseDomain)
	if baseDomain == "" {
		return
	}
	endpoints := publicEndpoints{
		APIHost:      "api." + baseDomain,
		UIHost:       "app." + baseDomain,
		S3URL:        "https://s3." + baseDomain,
		AuthentikURL: "https://auth." + baseDomain,
	}
	defaultIngressEndpoints(tamoss, endpoints)
	defaultHTTPRouteEndpoints(tamoss, endpoints)
	defaultAuthentikEndpoints(tamoss, endpoints)
	defaultS3PublicEndpoint(tamoss, endpoints)
}

type publicEndpoints struct {
	APIHost      string
	UIHost       string
	S3URL        string
	AuthentikURL string
}

func normalizeBaseDomain(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimSuffix(value, "/")
	return value
}

func defaultIngressEndpoints(tamoss *tamossv1alpha1.Tamoss, endpoints publicEndpoints) {
	if !tamoss.Spec.Ingress.IsEnabled() {
		return
	}
	if tamoss.Spec.Ingress.API.Host == "" {
		tamoss.Spec.Ingress.API.Host = endpoints.APIHost
	}
	if tamoss.Spec.Ingress.UI.Web.Host == "" {
		tamoss.Spec.Ingress.UI.Web.Host = endpoints.UIHost
	}
	tlsSecret := tamoss.Spec.PublicEndpoint.TLSSecretName
	if len(tamoss.Spec.Ingress.TLS) == 0 {
		tamoss.Spec.Ingress.TLS = []networkingv1.IngressTLS{{
			SecretName: tlsSecret,
			Hosts:      []string{endpoints.APIHost, endpoints.UIHost},
		}}
		return
	}
	if tamoss.Spec.Ingress.TLS[0].SecretName == "" {
		tamoss.Spec.Ingress.TLS[0].SecretName = tlsSecret
	}
	if len(tamoss.Spec.Ingress.TLS[0].Hosts) == 0 {
		tamoss.Spec.Ingress.TLS[0].Hosts = []string{endpoints.APIHost, endpoints.UIHost}
	}
}

func defaultHTTPRouteEndpoints(tamoss *tamossv1alpha1.Tamoss, endpoints publicEndpoints) {
	if !tamoss.Spec.HTTPRoute.Enabled {
		return
	}
	if len(tamoss.Spec.HTTPRoute.API.Hostnames) == 0 {
		tamoss.Spec.HTTPRoute.API.Hostnames = []string{endpoints.APIHost}
	}
	if len(tamoss.Spec.HTTPRoute.UI.Hostnames) == 0 {
		tamoss.Spec.HTTPRoute.UI.Hostnames = []string{endpoints.UIHost}
	}
}

func defaultAuthentikEndpoints(tamoss *tamossv1alpha1.Tamoss, endpoints publicEndpoints) {
	if tamoss.Spec.Auth.Provider() != tamossv1alpha1.AuthProvidedByAuthentikBlueprints {
		return
	}
	if tamoss.Spec.Auth.AuthentikBlueprints == nil {
		tamoss.Spec.Auth.AuthentikBlueprints = &tamossv1alpha1.AuthentikBlueprintsSpec{}
	}
	if tamoss.Spec.Auth.AuthentikBlueprints.IssuerURL == "" {
		tamoss.Spec.Auth.AuthentikBlueprints.IssuerURL = endpoints.AuthentikURL
	}
	defaultAuthentikInternalURL(tamoss)
}

func defaultAuthentikInternalURL(tamoss *tamossv1alpha1.Tamoss) {
	if tamoss.Spec.Auth.Provider() != tamossv1alpha1.AuthProvidedByAuthentikBlueprints ||
		tamoss.Spec.Auth.AuthentikBlueprints == nil ||
		tamoss.Spec.Auth.AuthentikBlueprints.InternalURL != "" ||
		tamoss.Spec.Auth.AuthentikBlueprints.PlatformNamespace == "" {
		return
	}
	tamoss.Spec.Auth.AuthentikBlueprints.InternalURL = fmt.Sprintf(
		"http://authentik-server.%s.svc.cluster.local",
		tamoss.Spec.Auth.AuthentikBlueprints.PlatformNamespace,
	)
}

func defaultS3PublicEndpoint(tamoss *tamossv1alpha1.Tamoss, endpoints publicEndpoints) {
	switch tamoss.Spec.Backends.S3.Provider() {
	case tamossv1alpha1.S3BackendProvidedByRustFSOperator:
		if tamoss.Spec.Backends.S3.RustFSOperator == nil {
			return
		}
		defaultManagedS3PublicEndpoint(&tamoss.Spec.Backends.S3.RustFSOperator.PublicEndpoint, tamoss, endpoints)
	}
}

func defaultManagedS3PublicEndpoint(endpoint *tamossv1alpha1.S3PublicEndpointSpec, tamoss *tamossv1alpha1.Tamoss, endpoints publicEndpoints) {
	if endpoint.URL == "" {
		endpoint.URL = endpoints.S3URL
	}
	if endpoint.TLSSecretName == "" {
		endpoint.TLSSecretName = tamoss.Spec.PublicEndpoint.S3TLSSecretName
	}
}

func defaultCNPG(tamoss *tamossv1alpha1.Tamoss, instances int32, storageSize string, applyFixtures, enablePodMonitor bool) {
	if tamoss.Spec.Backends.DB.ProvidedBy == "" {
		tamoss.Spec.Backends.DB.ProvidedBy = tamossv1alpha1.BackendProvidedByCNPG
	}
	if tamoss.Spec.Backends.DB.Provider() != tamossv1alpha1.BackendProvidedByCNPG {
		return
	}
	setBool(&tamoss.Spec.Backends.DB.ApplyFixtures, applyFixtures)
	if tamoss.Spec.Backends.DB.CNPG == nil {
		tamoss.Spec.Backends.DB.CNPG = &tamossv1alpha1.DBCNPGSpec{}
	}
	cnpg := tamoss.Spec.Backends.DB.CNPG
	if cnpg.Instances == 0 {
		cnpg.Instances = instances
	}
	if cnpg.PostgresVersion == "" {
		cnpg.PostgresVersion = DefaultCNPGPostgresVersion
	}
	if cnpg.Storage.Size == "" {
		cnpg.Storage.Size = storageSize
	}
	setBool(&cnpg.Monitoring.EnablePodMonitor, enablePodMonitor)
}

func defaultCNPGResources(tamoss *tamossv1alpha1.Tamoss, requestCPU, requestMemory, limitCPU, limitMemory string) {
	if tamoss.Spec.Backends.DB.Provider() != tamossv1alpha1.BackendProvidedByCNPG ||
		tamoss.Spec.Backends.DB.CNPG == nil {
		return
	}
	resources := &tamoss.Spec.Backends.DB.CNPG.Resources
	setResourceDefault(&resources.Requests, corev1.ResourceCPU, requestCPU)
	setResourceDefault(&resources.Requests, corev1.ResourceMemory, requestMemory)
	setResourceDefault(&resources.Limits, corev1.ResourceCPU, limitCPU)
	setResourceDefault(&resources.Limits, corev1.ResourceMemory, limitMemory)
}

func defaultRustFSOperator(tamoss *tamossv1alpha1.Tamoss, servers, volumesPerServer int32, storageSize string) {
	if tamoss.Spec.Backends.S3.ProvidedBy == "" {
		tamoss.Spec.Backends.S3.ProvidedBy = tamossv1alpha1.S3BackendProvidedByRustFSOperator
	}
	if tamoss.Spec.Backends.S3.Provider() != tamossv1alpha1.S3BackendProvidedByRustFSOperator {
		return
	}
	if tamoss.Spec.Backends.S3.RustFSOperator == nil {
		tamoss.Spec.Backends.S3.RustFSOperator = &tamossv1alpha1.S3RustFSOperatorSpec{}
	}
	rustfs := tamoss.Spec.Backends.S3.RustFSOperator
	if rustfs.Image == "" {
		rustfs.Image = DefaultRustFSImage
	}
	if len(rustfs.Pools) == 0 {
		rustfs.Pools = []tamossv1alpha1.S3RustFSPoolSpec{{
			Name:             "pool-0",
			Servers:          servers,
			VolumesPerServer: volumesPerServer,
			Storage: tamossv1alpha1.BackendStorageSpec{
				Size: storageSize,
			},
		}}
	}
	if rustfs.Bucket.Name == "" {
		rustfs.Bucket.Name = "tamoss"
	}
}

func defaultWorkloadResources(spec *tamossv1alpha1.WorkloadCommonSpec, requestCPU, requestMemory, limitCPU, limitMemory string) {
	setResourceDefault(&spec.Resources.Requests, corev1.ResourceCPU, requestCPU)
	setResourceDefault(&spec.Resources.Requests, corev1.ResourceMemory, requestMemory)
	setResourceDefault(&spec.Resources.Limits, corev1.ResourceCPU, limitCPU)
	setResourceDefault(&spec.Resources.Limits, corev1.ResourceMemory, limitMemory)
}

func defaultWorkerProbes(spec *tamossv1alpha1.WorkloadCommonSpec) {
	command := []string{"/bin/uv", "run", "python", "-m", "tamoss.worker", "health"}
	if spec.ReadinessProbe == nil {
		spec.ReadinessProbe = execProbe(command, 10, 5, 3)
	}
	if spec.LivenessProbe == nil {
		spec.LivenessProbe = execProbe(command, 30, 10, 3)
	}
	if spec.StartupProbe == nil {
		spec.StartupProbe = execProbe(command, 10, 5, 12)
	}
}

func defaultEdgeWorkerProbes(spec *tamossv1alpha1.WorkloadCommonSpec) {
	command := []string{"/bin/uv", "run", "python", "-m", "tamoss.worker", "health"}
	if spec.ReadinessProbe == nil {
		spec.ReadinessProbe = execProbe(command, 30, 30, 3)
	}
	if spec.LivenessProbe == nil {
		spec.LivenessProbe = execProbe(command, 60, 30, 3)
	}
	if spec.StartupProbe == nil {
		spec.StartupProbe = execProbe(command, 30, 30, 12)
	}
}

func defaultAPIProbes(spec *tamossv1alpha1.WorkloadCommonSpec) {
	if spec.ReadinessProbe == nil {
		spec.ReadinessProbe = httpProbe("/readyz", 10, 5, 3)
	}
	if spec.LivenessProbe == nil {
		spec.LivenessProbe = httpProbe("/healthz", 30, 5, 3)
	}
	if spec.StartupProbe == nil {
		spec.StartupProbe = httpProbe("/healthz", 10, 5, 12)
	}
}

func httpProbe(path string, periodSeconds, timeoutSeconds, failureThreshold int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: path,
				Port: intstr.FromString("http"),
			},
		},
		PeriodSeconds:    periodSeconds,
		TimeoutSeconds:   timeoutSeconds,
		FailureThreshold: failureThreshold,
	}
}

func execProbe(command []string, periodSeconds, timeoutSeconds, failureThreshold int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{Command: append([]string{}, command...)},
		},
		PeriodSeconds:    periodSeconds,
		TimeoutSeconds:   timeoutSeconds,
		FailureThreshold: failureThreshold,
	}
}

func defaultRestrictedWorkloadSecurity(spec *tamossv1alpha1.WorkloadCommonSpec) {
	if spec.PodSecurityContext == nil {
		spec.PodSecurityContext = &corev1.PodSecurityContext{}
	}
	if spec.PodSecurityContext.RunAsNonRoot == nil {
		spec.PodSecurityContext.RunAsNonRoot = ptr.To(true)
	}
	if spec.PodSecurityContext.SeccompProfile == nil {
		spec.PodSecurityContext.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	}
	if spec.SecurityContext == nil {
		spec.SecurityContext = &corev1.SecurityContext{}
	}
	if spec.SecurityContext.RunAsNonRoot == nil {
		spec.SecurityContext.RunAsNonRoot = ptr.To(true)
	}
	if spec.SecurityContext.AllowPrivilegeEscalation == nil {
		spec.SecurityContext.AllowPrivilegeEscalation = ptr.To(false)
	}
	if spec.SecurityContext.Capabilities == nil {
		spec.SecurityContext.Capabilities = &corev1.Capabilities{}
	}
	if len(spec.SecurityContext.Capabilities.Drop) == 0 {
		spec.SecurityContext.Capabilities.Drop = []corev1.Capability{"ALL"}
	}
}

func defaultMultiServerNetworkPolicy(tamoss *tamossv1alpha1.Tamoss) {
	setBool(&tamoss.Spec.NetworkPolicy.Enabled, true)
	if !tamoss.Spec.NetworkPolicy.IsEnabled() {
		return
	}
	if len(tamoss.Spec.NetworkPolicy.API.Ingress) == 0 {
		// 9090 mirrors apiMetricsPort in the workload renderer; the scrape
		// collector must reach the API metrics port as well as the HTTP port.
		tamoss.Spec.NetworkPolicy.API.Ingress = serviceIngressRules(8000, 9090)
	}
	if len(tamoss.Spec.NetworkPolicy.API.Egress) == 0 {
		tamoss.Spec.NetworkPolicy.API.Egress = appEgressRules()
	}
	if len(tamoss.Spec.NetworkPolicy.UI.Ingress) == 0 {
		tamoss.Spec.NetworkPolicy.UI.Ingress = serviceIngressRules(firstContainerPort(tamoss.Spec.UI.Ports, 8080))
	}
	if len(tamoss.Spec.NetworkPolicy.UI.Egress) == 0 {
		tamoss.Spec.NetworkPolicy.UI.Egress = uiEgressRules(firstServicePort(tamoss.Spec.Service.API.Ports, 8000))
	}
	if len(tamoss.Spec.NetworkPolicy.Worker.Ingress) == 0 {
		// The worker has no inbound traffic of its own, but the rendered
		// policy always declares policyTypes Ingress, so an empty rule list
		// denies all ingress. Open 9090 so the scrape collector can reach the
		// worker metrics port.
		tamoss.Spec.NetworkPolicy.Worker.Ingress = serviceIngressRules(9090)
	}
	if len(tamoss.Spec.NetworkPolicy.Worker.Egress) == 0 {
		tamoss.Spec.NetworkPolicy.Worker.Egress = appEgressRules()
	}
}

func serviceIngressRules(ports ...int32) []networkingv1.NetworkPolicyIngressRule {
	tcpPorts := make([]networkingv1.NetworkPolicyPort, 0, len(ports))
	for _, port := range ports {
		tcpPorts = append(tcpPorts, networkPolicyTCPPort(port))
	}
	return []networkingv1.NetworkPolicyIngressRule{{Ports: tcpPorts}}
}

func appEgressRules() []networkingv1.NetworkPolicyEgressRule {
	return []networkingv1.NetworkPolicyEgressRule{
		dnsEgressRule(),
		{
			Ports: []networkingv1.NetworkPolicyPort{
				networkPolicyTCPPort(80),
				networkPolicyTCPPort(443),
				networkPolicyTCPPort(5432),
				networkPolicyTCPPort(9000),
				networkPolicyTCPPort(8080),
			},
		},
	}
}

func uiEgressRules(apiPort int32) []networkingv1.NetworkPolicyEgressRule {
	return []networkingv1.NetworkPolicyEgressRule{
		dnsEgressRule(),
		{
			Ports: []networkingv1.NetworkPolicyPort{
				networkPolicyTCPPort(apiPort),
			},
		},
	}
}

func dnsEgressRule() networkingv1.NetworkPolicyEgressRule {
	// Some managed clusters evaluate NetworkPolicy after kube-dns Service DNAT,
	// where CoreDNS receives traffic on target port 8053 instead of Service port 53.
	return networkingv1.NetworkPolicyEgressRule{
		Ports: []networkingv1.NetworkPolicyPort{
			networkPolicyTCPPort(53),
			networkPolicyUDPPort(53),
			networkPolicyTCPPort(8053),
			networkPolicyUDPPort(8053),
		},
	}
}

func firstContainerPort(ports []corev1.ContainerPort, fallback int32) int32 {
	if len(ports) == 0 || ports[0].ContainerPort == 0 {
		return fallback
	}
	return ports[0].ContainerPort
}

func firstServicePort(ports []corev1.ServicePort, fallback int32) int32 {
	if len(ports) == 0 || ports[0].Port == 0 {
		return fallback
	}
	return ports[0].Port
}

func networkPolicyTCPPort(port int32) networkingv1.NetworkPolicyPort {
	protocol := corev1.ProtocolTCP
	value := intstr.FromInt(int(port))
	return networkingv1.NetworkPolicyPort{Protocol: &protocol, Port: &value}
}

func networkPolicyUDPPort(port int32) networkingv1.NetworkPolicyPort {
	protocol := corev1.ProtocolUDP
	value := intstr.FromInt(int(port))
	return networkingv1.NetworkPolicyPort{Protocol: &protocol, Port: &value}
}

func defaultAffinity(tamoss *tamossv1alpha1.Tamoss, spec *tamossv1alpha1.WorkloadCommonSpec, component string) {
	if spec.Affinity != nil {
		return
	}
	appName := defaultAppName
	if tamoss.Spec.NameOverride != "" {
		appName = tamoss.Spec.NameOverride
	}
	spec.Affinity = &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
				Weight: 100,
				PodAffinityTerm: corev1.PodAffinityTerm{
					TopologyKey: "kubernetes.io/hostname",
					LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
						"app.kubernetes.io/name":      appName,
						"app.kubernetes.io/instance":  tamoss.Name,
						"app.kubernetes.io/component": component,
					}},
				},
			}},
		},
	}
}

func setBool(target **bool, value bool) {
	if *target == nil {
		*target = ptr.To(value)
	}
}

func setReplicas(spec *tamossv1alpha1.WorkloadCommonSpec, value int32) {
	if spec.ReplicaCount == nil {
		spec.ReplicaCount = ptr.To(value)
	}
}

func setEnvDefault(target *map[string]string, name, value string) {
	if *target == nil {
		*target = map[string]string{}
	}
	if _, ok := (*target)[name]; !ok {
		(*target)[name] = value
	}
}

func setRustFSEnvDefault(tamoss *tamossv1alpha1.Tamoss, name, value string) {
	if tamoss.Spec.Backends.S3.RustFSOperator == nil {
		return
	}
	env := tamoss.Spec.Backends.S3.RustFSOperator.Env
	for _, item := range env {
		if item.Name == name {
			return
		}
	}
	tamoss.Spec.Backends.S3.RustFSOperator.Env = append(env, corev1.EnvVar{Name: name, Value: value})
}

func setResourceDefault(target *corev1.ResourceList, name corev1.ResourceName, value string) {
	if *target == nil {
		*target = corev1.ResourceList{}
	}
	if _, ok := (*target)[name]; !ok {
		(*target)[name] = resource.MustParse(value)
	}
}
