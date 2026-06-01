package defaults

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestApplyMultiServerDefaults(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "tamoss-multi-server", Namespace: "tams"},
		Spec: tamossv1alpha1.TamossSpec{
			Profile: tamossv1alpha1.TamossProfileMultiServer,
		},
	}

	Apply(tamoss)

	if got := tamoss.Spec.API.DesiredReplicaCount(); got != 2 {
		t.Fatalf("expected API replicas 2, got %d", got)
	}
	if got := tamoss.Spec.Worker.DesiredReplicaCount(); got != 2 {
		t.Fatalf("expected worker replicas 2, got %d", got)
	}
	if got := tamoss.Spec.UI.DesiredReplicaCount(); got != 2 {
		t.Fatalf("expected UI replicas 2, got %d", got)
	}
	if !tamoss.Spec.API.PDB.IsEnabled() || !tamoss.Spec.Worker.PDB.IsEnabled() || !tamoss.Spec.UI.PDB.IsEnabled() {
		t.Fatalf("expected multi-server PDB defaults to be enabled")
	}
	assertRestrictedWorkloadSecurity(t, tamoss.Spec.API.WorkloadCommonSpec)
	assertRestrictedWorkloadSecurity(t, tamoss.Spec.Worker.WorkloadCommonSpec)
	assertRestrictedWorkloadSecurity(t, tamoss.Spec.UI.WorkloadCommonSpec)
	if !tamoss.Spec.NetworkPolicy.IsEnabled() {
		t.Fatalf("expected multi-server NetworkPolicy default enabled")
	}
	assertAPIHTTPProbes(t, tamoss.Spec.API.WorkloadCommonSpec)
	if len(tamoss.Spec.NetworkPolicy.API.Ingress) == 0 || len(tamoss.Spec.NetworkPolicy.API.Egress) == 0 ||
		len(tamoss.Spec.NetworkPolicy.UI.Ingress) == 0 || len(tamoss.Spec.NetworkPolicy.UI.Egress) == 0 ||
		len(tamoss.Spec.NetworkPolicy.Worker.Egress) == 0 {
		t.Fatalf("expected multi-server NetworkPolicy rules, got %#v", tamoss.Spec.NetworkPolicy)
	}
	assertNetworkPolicyPort(t, tamoss.Spec.NetworkPolicy.UI.Ingress[0].Ports[0], 8080)
	assertNetworkPolicyPortAndProtocol(t, tamoss.Spec.NetworkPolicy.API.Egress[0].Ports[0], 53, corev1.ProtocolTCP)
	assertNetworkPolicyPortAndProtocol(t, tamoss.Spec.NetworkPolicy.API.Egress[0].Ports[1], 53, corev1.ProtocolUDP)
	assertNetworkPolicyPortAndProtocol(t, tamoss.Spec.NetworkPolicy.API.Egress[0].Ports[2], 8053, corev1.ProtocolTCP)
	assertNetworkPolicyPortAndProtocol(t, tamoss.Spec.NetworkPolicy.API.Egress[0].Ports[3], 8053, corev1.ProtocolUDP)
	assertNetworkPolicyPort(t, tamoss.Spec.NetworkPolicy.UI.Egress[1].Ports[0], 8000)
	assertNetworkPolicyPort(t, tamoss.Spec.NetworkPolicy.Worker.Egress[1].Ports[4], 8080)
	if tamoss.Spec.API.Affinity == nil || tamoss.Spec.API.Affinity.PodAntiAffinity == nil {
		t.Fatalf("expected API pod anti-affinity default")
	}
	terms := tamoss.Spec.API.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
	if len(terms) != 1 || terms[0].PodAffinityTerm.TopologyKey != "kubernetes.io/hostname" {
		t.Fatalf("unexpected API anti-affinity terms: %#v", terms)
	}
	labels := terms[0].PodAffinityTerm.LabelSelector.MatchLabels
	if labels["app.kubernetes.io/component"] != "api" ||
		labels["app.kubernetes.io/instance"] != "tamoss-multi-server" ||
		labels["app.kubernetes.io/name"] != "tamoss" {
		t.Fatalf("unexpected anti-affinity labels: %#v", labels)
	}
	if tamoss.Spec.Backends.DB.Provider() != tamossv1alpha1.BackendProvidedByCNPG {
		t.Fatalf("expected CNPG backend, got %s", tamoss.Spec.Backends.DB.Provider())
	}
	if tamoss.Spec.Images.SchemaMigrationPostgresClient != DefaultPostgresClientImage {
		t.Fatalf("expected schema migration helper image default, got %q", tamoss.Spec.Images.SchemaMigrationPostgresClient)
	}
	if tamoss.Spec.Backends.DB.ShouldApplyFixtures() {
		t.Fatalf("did not expect multi-server fixtures enabled")
	}
	if tamoss.Spec.Backends.DB.CNPG == nil || tamoss.Spec.Backends.DB.CNPG.Instances != 3 {
		t.Fatalf("expected CNPG instances default, got %#v", tamoss.Spec.Backends.DB.CNPG)
	}
	if !tamoss.Spec.Backends.DB.CNPG.Monitoring.ShouldEnablePodMonitor() {
		t.Fatalf("expected CNPG PodMonitor default")
	}
	if tamoss.Spec.Backends.S3.Provider() != tamossv1alpha1.S3BackendProvidedByRustFSOperator {
		t.Fatalf("expected RustFS Operator backend, got %s", tamoss.Spec.Backends.S3.Provider())
	}
	if tamoss.Spec.Backends.S3.RustFSOperator == nil || len(tamoss.Spec.Backends.S3.RustFSOperator.Pools) != 1 {
		t.Fatalf("expected RustFS pool default, got %#v", tamoss.Spec.Backends.S3.RustFSOperator)
	}
	pool := tamoss.Spec.Backends.S3.RustFSOperator.Pools[0]
	if pool.Servers != 4 || pool.VolumesPerServer != 4 || pool.Storage.Size != "100Gi" {
		t.Fatalf("unexpected multi-server RustFS pool default: %#v", pool)
	}
}

func TestApplyLocalKindPublicEndpointDefaults(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "tamoss-kind", Namespace: "tams"},
		Spec: tamossv1alpha1.TamossSpec{
			Profile: tamossv1alpha1.TamossProfileLocalKind,
		},
	}

	Apply(tamoss)

	if got := tamoss.Spec.PublicEndpoint.BaseDomain; got != "tamoss.localtest.me" {
		t.Fatalf("expected local base domain, got %q", got)
	}
	if !tamoss.Spec.Ingress.IsEnabled() {
		t.Fatalf("expected local ingress enabled")
	}
	if got := tamoss.Spec.Ingress.ClassName; got != "traefik" {
		t.Fatalf("expected ingress class traefik, got %q", got)
	}
	if got := tamoss.Spec.Ingress.Annotations["cert-manager.io/cluster-issuer"]; got != "tamoss-selfsigned" {
		t.Fatalf("expected cert issuer annotation, got %q", got)
	}
	if got := tamoss.Spec.Ingress.API.Host; got != "api.tamoss.localtest.me" {
		t.Fatalf("expected derived API host, got %q", got)
	}
	if got := tamoss.Spec.Ingress.UI.Web.Host; got != "app.tamoss.localtest.me" {
		t.Fatalf("expected derived UI host, got %q", got)
	}
	if len(tamoss.Spec.Ingress.TLS) != 1 ||
		tamoss.Spec.Ingress.TLS[0].SecretName != "tamoss-localtest-tls" ||
		len(tamoss.Spec.Ingress.TLS[0].Hosts) != 2 ||
		tamoss.Spec.Ingress.TLS[0].Hosts[0] != "api.tamoss.localtest.me" ||
		tamoss.Spec.Ingress.TLS[0].Hosts[1] != "app.tamoss.localtest.me" {
		t.Fatalf("unexpected ingress TLS defaults: %#v", tamoss.Spec.Ingress.TLS)
	}
	if tamoss.Spec.Auth.Provider() != tamossv1alpha1.AuthProvidedByAuthentikBlueprints {
		t.Fatalf("expected Authentik Blueprint auth, got %s", tamoss.Spec.Auth.Provider())
	}
	authentik := tamoss.Spec.Auth.AuthentikBlueprints
	if authentik == nil ||
		authentik.PlatformNamespace != "auth" ||
		authentik.IssuerURL != "https://auth.tamoss.localtest.me" ||
		authentik.InternalURL != "http://authentik-server.auth.svc.cluster.local" ||
		authentik.APITokenSecretRef.Name != "authentik" ||
		authentik.APITokenSecretRef.Key != "AUTHENTIK_BOOTSTRAP_TOKEN" {
		t.Fatalf("unexpected Authentik defaults: %#v", authentik)
	}
	if tamoss.Spec.Backends.DB.Provider() != tamossv1alpha1.BackendProvidedByCNPG {
		t.Fatalf("expected local CNPG backend, got %s", tamoss.Spec.Backends.DB.Provider())
	}
	cnpg := tamoss.Spec.Backends.DB.CNPG
	if cnpg == nil || cnpg.Instances != 1 || cnpg.Storage.Size != "10Gi" {
		t.Fatalf("unexpected local CNPG defaults: %#v", cnpg)
	}
	if cnpg.Monitoring.ShouldEnablePodMonitor() {
		t.Fatalf("did not expect local CNPG PodMonitor default")
	}
	if tamoss.Spec.Backends.DB.ShouldApplyFixtures() {
		t.Fatalf("did not expect local fixtures enabled")
	}
	if tamoss.Spec.Backends.S3.Provider() != tamossv1alpha1.S3BackendProvidedByRustFSOperator {
		t.Fatalf("expected local RustFS Operator backend, got %s", tamoss.Spec.Backends.S3.Provider())
	}
	rustfs := tamoss.Spec.Backends.S3.RustFSOperator
	if rustfs == nil ||
		rustfs.PublicEndpoint.URL != "https://s3.tamoss.localtest.me" ||
		rustfs.PublicEndpoint.TLSSecretName != "tamoss-localtest-s3-tls" ||
		len(rustfs.Pools) != 1 ||
		rustfs.Pools[0].Servers != 1 ||
		rustfs.Pools[0].VolumesPerServer != 4 ||
		rustfs.Pools[0].Storage.Size != "10Gi" {
		t.Fatalf("unexpected local RustFS Operator defaults: %#v", rustfs)
	}
	if got := rustFSEnvValue(rustfs, "RUSTFS_UNSAFE_BYPASS_DISK_CHECK"); got != "true" {
		t.Fatalf("expected local RustFS disk check bypass, got %q", got)
	}
	if got := tamoss.Spec.API.Env["TAMOSS_WEBHOOK_ALLOWED_HOSTS"]; got != ".svc.cluster.local" {
		t.Fatalf("expected local API webhook host allow-list, got %q", got)
	}
	if got := tamoss.Spec.Worker.Env["TAMOSS_WEBHOOK_ALLOWED_HOSTS"]; got != ".svc.cluster.local" {
		t.Fatalf("expected local worker webhook host allow-list, got %q", got)
	}
}

func TestApplySingleServerManagedBackendDefaults(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "tamoss-single-server", Namespace: "tams"},
		Spec: tamossv1alpha1.TamossSpec{
			Profile: tamossv1alpha1.TamossProfileSingleServer,
			PublicEndpoint: tamossv1alpha1.PublicEndpointSpec{
				BaseDomain: "tamoss.example.com",
			},
		},
	}

	Apply(tamoss)

	if tamoss.Spec.Backends.DB.Provider() != tamossv1alpha1.BackendProvidedByCNPG {
		t.Fatalf("expected single-server CNPG backend, got %s", tamoss.Spec.Backends.DB.Provider())
	}
	cnpg := tamoss.Spec.Backends.DB.CNPG
	if cnpg == nil || cnpg.Instances != 1 || cnpg.Storage.Size != "50Gi" {
		t.Fatalf("unexpected single-server CNPG defaults: %#v", cnpg)
	}
	if cnpg.Monitoring.ShouldEnablePodMonitor() {
		t.Fatalf("did not expect single-server CNPG PodMonitor default")
	}
	if tamoss.Spec.Backends.DB.ShouldApplyFixtures() {
		t.Fatalf("did not expect single-server fixtures enabled")
	}
	if tamoss.Spec.Backends.S3.Provider() != tamossv1alpha1.S3BackendProvidedByRustFSOperator {
		t.Fatalf("expected single-server RustFS Operator backend, got %s", tamoss.Spec.Backends.S3.Provider())
	}
	rustfs := tamoss.Spec.Backends.S3.RustFSOperator
	if rustfs == nil ||
		rustfs.PublicEndpoint.URL != "https://s3.tamoss.example.com" ||
		rustfs.PublicEndpoint.TLSSecretName != "tamoss-s3-public-tls" ||
		len(rustfs.Pools) != 1 ||
		rustfs.Pools[0].Servers != 1 ||
		rustfs.Pools[0].VolumesPerServer != 4 ||
		rustfs.Pools[0].Storage.Size != "100Gi" {
		t.Fatalf("unexpected single-server RustFS Operator defaults: %#v", rustfs)
	}
	assertRestrictedWorkloadSecurity(t, tamoss.Spec.API.WorkloadCommonSpec)
	assertRestrictedWorkloadSecurity(t, tamoss.Spec.Worker.WorkloadCommonSpec)
	assertRestrictedWorkloadSecurity(t, tamoss.Spec.UI.WorkloadCommonSpec)
	if tamoss.Spec.NetworkPolicy.IsEnabled() {
		t.Fatalf("did not expect single-server NetworkPolicy default enabled")
	}
}

func TestApplyRemoteProfilesDefaultToPublicTLSIssuer(t *testing.T) {
	profiles := []tamossv1alpha1.TamossProfile{
		tamossv1alpha1.TamossProfileSingleServer,
		tamossv1alpha1.TamossProfileMultiServer,
	}

	for _, profile := range profiles {
		t.Run(string(profile), func(t *testing.T) {
			tamoss := &tamossv1alpha1.Tamoss{
				ObjectMeta: metav1.ObjectMeta{Name: "tamoss", Namespace: "tams"},
				Spec: tamossv1alpha1.TamossSpec{
					Profile: profile,
					PublicEndpoint: tamossv1alpha1.PublicEndpointSpec{
						BaseDomain: "tamoss.example.com",
					},
				},
			}

			Apply(tamoss)

			if got := tamoss.Spec.Ingress.Annotations["cert-manager.io/cluster-issuer"]; got != "tamoss-public" {
				t.Fatalf("expected public cert issuer annotation, got %q", got)
			}
			if len(tamoss.Spec.Ingress.TLS) != 1 || tamoss.Spec.Ingress.TLS[0].SecretName != "tamoss-public-tls" {
				t.Fatalf("unexpected ingress TLS defaults: %#v", tamoss.Spec.Ingress.TLS)
			}
			if tamoss.Spec.Backends.S3.RustFSOperator == nil ||
				tamoss.Spec.Backends.S3.RustFSOperator.PublicEndpoint.TLSSecretName != "tamoss-s3-public-tls" {
				t.Fatalf("unexpected S3 TLS default: %#v", tamoss.Spec.Backends.S3.RustFSOperator)
			}
		})
	}
}

func TestSupportedProfilesDefaultToManagedAuthentik(t *testing.T) {
	profiles := []tamossv1alpha1.TamossProfile{
		tamossv1alpha1.TamossProfileLocalKind,
		tamossv1alpha1.TamossProfileSingleServer,
		tamossv1alpha1.TamossProfileMultiServer,
	}

	for _, profile := range profiles {
		t.Run(string(profile), func(t *testing.T) {
			tamoss := &tamossv1alpha1.Tamoss{
				ObjectMeta: metav1.ObjectMeta{Name: "tamoss", Namespace: "tams"},
				Spec: tamossv1alpha1.TamossSpec{
					Profile: profile,
					PublicEndpoint: tamossv1alpha1.PublicEndpointSpec{
						BaseDomain: "tamoss.example.com",
					},
				},
			}

			Apply(tamoss)

			if got := tamoss.Spec.Auth.Provider(); got != tamossv1alpha1.AuthProvidedByAuthentikBlueprints {
				t.Fatalf("expected Authentik Blueprint auth for %s, got %s", profile, got)
			}
			if !tamoss.Spec.Auth.RequiredForRuntime() {
				t.Fatalf("expected auth to be required for %s", profile)
			}
			authentik := tamoss.Spec.Auth.AuthentikBlueprints
			if authentik == nil ||
				authentik.PlatformNamespace != "auth" ||
				authentik.IssuerURL != "https://auth.tamoss.example.com" ||
				authentik.InternalURL != "http://authentik-server.auth.svc.cluster.local" ||
				authentik.APITokenSecretRef.Name != "authentik" ||
				authentik.APITokenSecretRef.Key != "AUTHENTIK_BOOTSTRAP_TOKEN" {
				t.Fatalf("unexpected Authentik defaults for %s: %#v", profile, authentik)
			}
		})
	}
}

func TestApplyPreservesExplicitIngressAnnotations(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "tamoss", Namespace: "tams"},
		Spec: tamossv1alpha1.TamossSpec{
			Profile: tamossv1alpha1.TamossProfileMultiServer,
			PublicEndpoint: tamossv1alpha1.PublicEndpointSpec{
				BaseDomain: "tamoss.example.com",
			},
			Ingress: tamossv1alpha1.IngressSpec{
				Annotations: map[string]string{},
			},
		},
	}

	Apply(tamoss)

	if _, ok := tamoss.Spec.Ingress.Annotations["cert-manager.io/cluster-issuer"]; ok {
		t.Fatalf("did not expect default cert issuer annotation when annotations are explicit")
	}
}

func TestManagedRustFSProfileDefaultsMeetOperatorMinimum(t *testing.T) {
	profiles := []tamossv1alpha1.TamossProfile{
		tamossv1alpha1.TamossProfileLocalKind,
		tamossv1alpha1.TamossProfileSingleServer,
		tamossv1alpha1.TamossProfileMultiServer,
	}

	for _, profile := range profiles {
		t.Run(string(profile), func(t *testing.T) {
			tamoss := &tamossv1alpha1.Tamoss{
				ObjectMeta: metav1.ObjectMeta{Name: "tamoss", Namespace: "tams"},
				Spec: tamossv1alpha1.TamossSpec{
					Profile: profile,
					PublicEndpoint: tamossv1alpha1.PublicEndpointSpec{
						BaseDomain: "tamoss.example.com",
					},
				},
			}

			Apply(tamoss)

			rustfs := tamoss.Spec.Backends.S3.RustFSOperator
			if rustfs == nil || len(rustfs.Pools) == 0 {
				t.Fatalf("expected RustFS Operator pool defaults, got %#v", rustfs)
			}
			pool := rustfs.Pools[0]
			if total := pool.Servers * pool.VolumesPerServer; total < 4 {
				t.Fatalf("expected at least 4 total RustFS volumes, got %d from %#v", total, pool)
			}
		})
	}
}

func TestApplyMultiServerPublicEndpointDefaultsRustFS(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "tamoss-multi-server", Namespace: "tams"},
		Spec: tamossv1alpha1.TamossSpec{
			Profile: tamossv1alpha1.TamossProfileMultiServer,
			PublicEndpoint: tamossv1alpha1.PublicEndpointSpec{
				BaseDomain:      "tamoss.example.com",
				TLSSecretName:   "shared-tls",
				S3TLSSecretName: "objects-tls",
			},
		},
	}

	Apply(tamoss)

	if got := tamoss.Spec.Ingress.API.Host; got != "api.tamoss.example.com" {
		t.Fatalf("expected derived API ingress host, got %q", got)
	}
	if got := tamoss.Spec.Ingress.UI.Web.Host; got != "app.tamoss.example.com" {
		t.Fatalf("expected derived UI ingress host, got %q", got)
	}
	if len(tamoss.Spec.Ingress.TLS) != 1 ||
		tamoss.Spec.Ingress.TLS[0].SecretName != "shared-tls" ||
		len(tamoss.Spec.Ingress.TLS[0].Hosts) != 2 ||
		tamoss.Spec.Ingress.TLS[0].Hosts[0] != "api.tamoss.example.com" ||
		tamoss.Spec.Ingress.TLS[0].Hosts[1] != "app.tamoss.example.com" {
		t.Fatalf("unexpected ingress TLS defaults: %#v", tamoss.Spec.Ingress.TLS)
	}
	if tamoss.Spec.Auth.AuthentikBlueprints == nil ||
		tamoss.Spec.Auth.AuthentikBlueprints.IssuerURL != "https://auth.tamoss.example.com" {
		t.Fatalf("expected derived Authentik issuer, got %#v", tamoss.Spec.Auth.AuthentikBlueprints)
	}
	if tamoss.Spec.Backends.S3.RustFSOperator == nil ||
		tamoss.Spec.Backends.S3.RustFSOperator.PublicEndpoint.URL != "https://s3.tamoss.example.com" ||
		tamoss.Spec.Backends.S3.RustFSOperator.PublicEndpoint.TLSSecretName != "objects-tls" {
		t.Fatalf("unexpected RustFS public endpoint defaults: %#v", tamoss.Spec.Backends.S3.RustFSOperator)
	}
}

func TestApplyPublicEndpointDefaultsNormalizeBaseDomain(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "tamoss-single-server", Namespace: "tams"},
		Spec: tamossv1alpha1.TamossSpec{
			Profile: tamossv1alpha1.TamossProfileSingleServer,
			PublicEndpoint: tamossv1alpha1.PublicEndpointSpec{
				BaseDomain: "https://tamoss.example.com/",
			},
			HTTPRoute: tamossv1alpha1.HTTPRouteSpec{
				Enabled: true,
			},
		},
	}

	Apply(tamoss)

	if got := tamoss.Spec.Ingress.API.Host; got != "api.tamoss.example.com" {
		t.Fatalf("expected normalized API ingress host, got %q", got)
	}
	if got := tamoss.Spec.Ingress.UI.Web.Host; got != "app.tamoss.example.com" {
		t.Fatalf("expected normalized UI ingress host, got %q", got)
	}
	if got := tamoss.Spec.HTTPRoute.API.Hostnames[0]; got != "api.tamoss.example.com" {
		t.Fatalf("expected normalized API HTTPRoute host, got %q", got)
	}
	if got := tamoss.Spec.HTTPRoute.UI.Hostnames[0]; got != "app.tamoss.example.com" {
		t.Fatalf("expected normalized UI HTTPRoute host, got %q", got)
	}
	if got := tamoss.Spec.Auth.AuthentikBlueprints.IssuerURL; got != "https://auth.tamoss.example.com" {
		t.Fatalf("expected normalized Authentik issuer, got %q", got)
	}
	if got := tamoss.Spec.Backends.S3.RustFSOperator.PublicEndpoint.URL; got != "https://s3.tamoss.example.com" {
		t.Fatalf("expected normalized S3 public endpoint, got %q", got)
	}
}

func TestApplyPublicEndpointDefaultsPreserveOverrides(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "tamoss-single-server", Namespace: "tams"},
		Spec: tamossv1alpha1.TamossSpec{
			Profile: tamossv1alpha1.TamossProfileSingleServer,
			PublicEndpoint: tamossv1alpha1.PublicEndpointSpec{
				BaseDomain:      "tamoss.example.com",
				TLSSecretName:   "shared-tls",
				S3TLSSecretName: "s3-default-tls",
			},
			Ingress: tamossv1alpha1.IngressSpec{
				Enabled: ptr.To(true),
				API:     tamossv1alpha1.IngressHostSpec{Host: "api.override.example.com"},
				TLS: []networkingv1.IngressTLS{{
					SecretName: "explicit-tls",
					Hosts:      []string{"explicit.example.com"},
				}},
			},
			HTTPRoute: tamossv1alpha1.HTTPRouteSpec{
				Enabled: true,
				API:     tamossv1alpha1.HTTPRouteHostSpec{Hostnames: []string{"api-route.override.example.com"}},
			},
			Auth: tamossv1alpha1.AuthSpec{
				ProvidedBy: tamossv1alpha1.AuthProvidedByAuthentikBlueprints,
				AuthentikBlueprints: &tamossv1alpha1.AuthentikBlueprintsSpec{
					PlatformNamespace: "identity",
					IssuerURL:         "https://auth.override.example.com",
					InternalURL:       "http://authentik.identity.svc.cluster.local",
				},
			},
			Backends: tamossv1alpha1.BackendsSpec{
				S3: tamossv1alpha1.S3BackendSpec{
					RustFSOperator: &tamossv1alpha1.S3RustFSOperatorSpec{
						PublicEndpoint: tamossv1alpha1.S3PublicEndpointSpec{
							URL:           "https://objects.override.example.com",
							TLSSecretName: "objects-tls",
						},
					},
				},
			},
		},
	}

	Apply(tamoss)

	if got := tamoss.Spec.Ingress.API.Host; got != "api.override.example.com" {
		t.Fatalf("expected explicit API ingress host preserved, got %q", got)
	}
	if got := tamoss.Spec.Ingress.UI.Web.Host; got != "app.tamoss.example.com" {
		t.Fatalf("expected missing UI ingress host derived, got %q", got)
	}
	if got := tamoss.Spec.Ingress.TLS[0].SecretName; got != "explicit-tls" {
		t.Fatalf("expected explicit TLS secret preserved, got %q", got)
	}
	if got := tamoss.Spec.Ingress.TLS[0].Hosts[0]; got != "explicit.example.com" {
		t.Fatalf("expected explicit TLS hosts preserved, got %q", got)
	}
	if got := tamoss.Spec.HTTPRoute.API.Hostnames[0]; got != "api-route.override.example.com" {
		t.Fatalf("expected explicit API HTTPRoute host preserved, got %q", got)
	}
	if got := tamoss.Spec.HTTPRoute.UI.Hostnames[0]; got != "app.tamoss.example.com" {
		t.Fatalf("expected missing UI HTTPRoute host derived, got %q", got)
	}
	if got := tamoss.Spec.Auth.AuthentikBlueprints.IssuerURL; got != "https://auth.override.example.com" {
		t.Fatalf("expected explicit Authentik issuer preserved, got %q", got)
	}
	if got := tamoss.Spec.Backends.S3.RustFSOperator.PublicEndpoint.URL; got != "https://objects.override.example.com" {
		t.Fatalf("expected explicit S3 public endpoint preserved, got %q", got)
	}
	if got := tamoss.Spec.Backends.S3.RustFSOperator.PublicEndpoint.TLSSecretName; got != "objects-tls" {
		t.Fatalf("expected explicit S3 TLS secret preserved, got %q", got)
	}
}

func TestSupportedProfilesUseOperatorManagedBackends(t *testing.T) {
	profiles := []tamossv1alpha1.TamossProfile{
		tamossv1alpha1.TamossProfileLocalKind,
		tamossv1alpha1.TamossProfileSingleServer,
		tamossv1alpha1.TamossProfileMultiServer,
	}

	for _, profile := range profiles {
		t.Run(string(profile), func(t *testing.T) {
			tamoss := &tamossv1alpha1.Tamoss{
				ObjectMeta: metav1.ObjectMeta{Name: "tamoss", Namespace: "tams"},
				Spec: tamossv1alpha1.TamossSpec{
					Profile: profile,
				},
			}

			Apply(tamoss)

			if got := tamoss.Spec.Backends.DB.Provider(); got != tamossv1alpha1.BackendProvidedByCNPG {
				t.Fatalf("expected CNPG database backend for %s, got %s", profile, got)
			}
			if got := tamoss.Spec.Backends.S3.Provider(); got != tamossv1alpha1.S3BackendProvidedByRustFSOperator {
				t.Fatalf("expected RustFS Operator S3 backend for %s, got %s", profile, got)
			}
		})
	}
}

func TestApplyPublicEndpointDefaultsPreserveExternalProviders(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "tamoss-external", Namespace: "tams"},
		Spec: tamossv1alpha1.TamossSpec{
			Profile:        tamossv1alpha1.TamossProfileLocalKind,
			PublicEndpoint: tamossv1alpha1.PublicEndpointSpec{BaseDomain: "tamoss.example.com"},
			Auth: tamossv1alpha1.AuthSpec{
				ProvidedBy: tamossv1alpha1.AuthProvidedByExternal,
				External: &tamossv1alpha1.AuthExternalSpec{OAuth2: tamossv1alpha1.OAuth2Spec{
					Enabled: true,
					Issuer:  "https://issuer.example.com",
					JWKSURI: "https://issuer.example.com/jwks",
				}},
			},
			Backends: tamossv1alpha1.BackendsSpec{
				S3: tamossv1alpha1.S3BackendSpec{
					ProvidedBy: tamossv1alpha1.S3BackendProvidedByExternal,
					External: &tamossv1alpha1.S3ExternalSpec{
						Endpoint: tamossv1alpha1.S3EndpointSpec{
							Default: tamossv1alpha1.EndpointURLSpec{URL: "https://s3.external.example.com"},
						},
					},
				},
			},
		},
	}

	Apply(tamoss)

	if got := tamoss.Spec.Auth.Provider(); got != tamossv1alpha1.AuthProvidedByExternal {
		t.Fatalf("expected external auth preserved, got %s", got)
	}
	if tamoss.Spec.Auth.AuthentikBlueprints != nil {
		t.Fatalf("did not expect Authentik defaults for external auth: %#v", tamoss.Spec.Auth.AuthentikBlueprints)
	}
	if got := tamoss.Spec.Backends.S3.External.Endpoint.Public.URL; got != "" {
		t.Fatalf("did not expect external S3 public endpoint derived, got %q", got)
	}
}

func TestApplyPreservesExplicitOverrides(t *testing.T) {
	customAffinity := &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
				Weight: 50,
				PodAffinityTerm: corev1.PodAffinityTerm{
					TopologyKey: "topology.kubernetes.io/zone",
				},
			}},
		},
	}
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "tamoss-multi-server", Namespace: "tams"},
		Spec: tamossv1alpha1.TamossSpec{
			Profile: tamossv1alpha1.TamossProfileMultiServer,
			API: tamossv1alpha1.APIComponentSpec{
				WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{
					ReplicaCount: ptr.To[int32](4),
					PDB:          tamossv1alpha1.PDBSpec{Enabled: ptr.To(false)},
					PodSecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(false),
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: ptr.To(true),
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("500m"),
						},
					},
				},
			},
			Worker: tamossv1alpha1.WorkerComponentSpec{
				WorkloadCommonSpec: tamossv1alpha1.WorkloadCommonSpec{
					Affinity: customAffinity,
				},
			},
			Backends: tamossv1alpha1.BackendsSpec{
				DB: tamossv1alpha1.DBBackendSpec{
					ApplyFixtures: ptr.To(false),
					CNPG: &tamossv1alpha1.DBCNPGSpec{
						Monitoring: tamossv1alpha1.DBCNPGMonitoringSpec{
							EnablePodMonitor: ptr.To(false),
						},
					},
				},
			},
			NetworkPolicy: tamossv1alpha1.NetworkPolicySpec{
				Enabled: ptr.To(false),
			},
		},
	}

	Apply(tamoss)

	if got := tamoss.Spec.API.DesiredReplicaCount(); got != 4 {
		t.Fatalf("expected explicit API replicas 4, got %d", got)
	}
	if tamoss.Spec.API.PDB.IsEnabled() {
		t.Fatalf("expected explicit API PDB disabled override")
	}
	if got := tamoss.Spec.API.Resources.Requests[corev1.ResourceCPU]; got.String() != "500m" {
		t.Fatalf("expected explicit CPU request preserved, got %s", got.String())
	}
	if got := tamoss.Spec.API.Resources.Requests[corev1.ResourceMemory]; got.String() != "384Mi" {
		t.Fatalf("expected missing memory request defaulted, got %s", got.String())
	}
	if *tamoss.Spec.API.PodSecurityContext.RunAsNonRoot {
		t.Fatalf("expected explicit API pod runAsNonRoot override to remain false")
	}
	if !*tamoss.Spec.API.SecurityContext.AllowPrivilegeEscalation {
		t.Fatalf("expected explicit API privilege escalation override to remain true")
	}
	if tamoss.Spec.NetworkPolicy.IsEnabled() {
		t.Fatalf("expected explicit NetworkPolicy disabled override")
	}
	if len(tamoss.Spec.NetworkPolicy.API.Ingress) > 0 || len(tamoss.Spec.NetworkPolicy.API.Egress) > 0 ||
		len(tamoss.Spec.NetworkPolicy.UI.Ingress) > 0 || len(tamoss.Spec.NetworkPolicy.Worker.Egress) > 0 {
		t.Fatalf("did not expect NetworkPolicy rules when explicitly disabled, got %#v", tamoss.Spec.NetworkPolicy)
	}
	if tamoss.Spec.Worker.Affinity != customAffinity {
		t.Fatalf("expected explicit worker affinity preserved")
	}
	if tamoss.Spec.Backends.DB.ShouldApplyFixtures() {
		t.Fatalf("expected explicit fixture override to remain false")
	}
	if tamoss.Spec.Backends.DB.CNPG.Monitoring.ShouldEnablePodMonitor() {
		t.Fatalf("expected explicit PodMonitor override to remain false")
	}
}

func rustFSEnvValue(spec *tamossv1alpha1.S3RustFSOperatorSpec, name string) string {
	if spec == nil {
		return ""
	}
	for _, env := range spec.Env {
		if env.Name == name {
			return env.Value
		}
	}
	return ""
}

func assertRestrictedWorkloadSecurity(t *testing.T, spec tamossv1alpha1.WorkloadCommonSpec) {
	t.Helper()
	if spec.PodSecurityContext == nil || spec.PodSecurityContext.RunAsNonRoot == nil || !*spec.PodSecurityContext.RunAsNonRoot {
		t.Fatalf("expected pod runAsNonRoot default, got %#v", spec.PodSecurityContext)
	}
	if spec.PodSecurityContext.SeccompProfile == nil || spec.PodSecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("expected runtime default seccomp, got %#v", spec.PodSecurityContext)
	}
	if spec.SecurityContext == nil || spec.SecurityContext.RunAsNonRoot == nil || !*spec.SecurityContext.RunAsNonRoot {
		t.Fatalf("expected container runAsNonRoot default, got %#v", spec.SecurityContext)
	}
	if spec.SecurityContext.AllowPrivilegeEscalation == nil || *spec.SecurityContext.AllowPrivilegeEscalation {
		t.Fatalf("expected privilege escalation disabled, got %#v", spec.SecurityContext)
	}
	if spec.SecurityContext.Capabilities == nil || len(spec.SecurityContext.Capabilities.Drop) != 1 || spec.SecurityContext.Capabilities.Drop[0] != "ALL" {
		t.Fatalf("expected all capabilities dropped, got %#v", spec.SecurityContext)
	}
}

func assertNetworkPolicyPort(t *testing.T, port networkingv1.NetworkPolicyPort, want int32) {
	t.Helper()
	if port.Port == nil || port.Port.IntVal != want {
		t.Fatalf("expected NetworkPolicy port %d, got %#v", want, port.Port)
	}
}

func assertNetworkPolicyPortAndProtocol(t *testing.T, port networkingv1.NetworkPolicyPort, want int32, protocol corev1.Protocol) {
	t.Helper()
	assertNetworkPolicyPort(t, port, want)
	if port.Protocol == nil || *port.Protocol != protocol {
		t.Fatalf("expected NetworkPolicy port protocol %s, got %#v", protocol, port.Protocol)
	}
}

func assertAPIHTTPProbes(t *testing.T, spec tamossv1alpha1.WorkloadCommonSpec) {
	t.Helper()
	if spec.ReadinessProbe == nil || spec.ReadinessProbe.HTTPGet == nil {
		t.Fatalf("expected API readiness HTTP probe")
	}
	if spec.ReadinessProbe.HTTPGet.Path != "/readyz" {
		t.Fatalf("expected API readiness path /readyz, got %q", spec.ReadinessProbe.HTTPGet.Path)
	}
	if spec.LivenessProbe == nil || spec.LivenessProbe.HTTPGet == nil {
		t.Fatalf("expected API liveness HTTP probe")
	}
	if spec.LivenessProbe.HTTPGet.Path != "/healthz" {
		t.Fatalf("expected API liveness path /healthz, got %q", spec.LivenessProbe.HTTPGet.Path)
	}
	if spec.StartupProbe == nil || spec.StartupProbe.HTTPGet == nil {
		t.Fatalf("expected API startup HTTP probe")
	}
	if spec.StartupProbe.HTTPGet.Path != "/healthz" {
		t.Fatalf("expected API startup path /healthz, got %q", spec.StartupProbe.HTTPGet.Path)
	}
}
