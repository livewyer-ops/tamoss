package rustfs

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestBuildTenantDefaults(t *testing.T) {
	tenant, err := BuildTenant(rustfsTamossFixture())
	if err != nil {
		t.Fatal(err)
	}

	if tenant.GetName() != "example-s3" {
		t.Fatalf("expected tenant name example-s3, got %q", tenant.GetName())
	}
	if tenant.GetAPIVersion() != "rustfs.com/v1alpha1" || tenant.GetKind() != "Tenant" {
		t.Fatalf("unexpected type meta %s/%s", tenant.GetAPIVersion(), tenant.GetKind())
	}
	if len(tenant.GetOwnerReferences()) != 1 || tenant.GetOwnerReferences()[0].Name != "example" {
		t.Fatalf("expected Tamoss owner reference, got %#v", tenant.GetOwnerReferences())
	}
	credsSecret, found, err := unstructured.NestedString(tenant.Object, "spec", "credsSecret", "name")
	if err != nil || !found || credsSecret != "example-s3-creds" {
		t.Fatalf("expected default credsSecret example-s3-creds, got %q found=%t err=%v", credsSecret, found, err)
	}
	pools, found, err := unstructured.NestedSlice(tenant.Object, "spec", "pools")
	if err != nil || !found || len(pools) != 1 {
		t.Fatalf("expected one default pool, got %#v found=%t err=%v", pools, found, err)
	}
	pool := pools[0].(map[string]interface{})
	if pool["name"] != "pool-0" || pool["servers"] != int64(2) {
		t.Fatalf("unexpected default pool %#v", pool)
	}
	persistence := pool["persistence"].(map[string]interface{})
	if persistence["volumesPerServer"] != int64(2) {
		t.Fatalf("expected two volumes per server, got %#v", persistence["volumesPerServer"])
	}
}

func TestBuildTenantMapsPoolsStorageImageAndCredentials(t *testing.T) {
	tamoss := rustfsTamossFixture()
	tamoss.Spec.Backends.S3.RustFSOperator = &tamossv1alpha1.S3RustFSOperatorSpec{
		Image:           "rustfs/rustfs:1.0.0",
		ImagePullPolicy: corev1.PullAlways,
		Env: []corev1.EnvVar{{
			Name:  "RUSTFS_UNSAFE_BYPASS_DISK_CHECK",
			Value: "true",
		}},
		CredsSecret: tamossv1alpha1.SecretReferenceSpec{
			ExistingSecret: "custom-s3-creds",
		},
		Pools: []tamossv1alpha1.S3RustFSPoolSpec{{
			Name:             "fast",
			Servers:          4,
			VolumesPerServer: 4,
			Storage: tamossv1alpha1.BackendStorageSpec{
				Size:         "100Gi",
				StorageClass: "fast-ssd",
			},
		}},
	}

	tenant, err := BuildTenant(tamoss)
	if err != nil {
		t.Fatal(err)
	}
	image, _, _ := unstructured.NestedString(tenant.Object, "spec", "image")
	if image != "rustfs/rustfs:1.0.0" {
		t.Fatalf("expected image override, got %q", image)
	}
	pullPolicy, _, _ := unstructured.NestedString(tenant.Object, "spec", "imagePullPolicy")
	if pullPolicy != "Always" {
		t.Fatalf("expected imagePullPolicy Always, got %q", pullPolicy)
	}
	credsSecret, _, _ := unstructured.NestedString(tenant.Object, "spec", "credsSecret", "name")
	if credsSecret != "custom-s3-creds" {
		t.Fatalf("expected custom creds secret, got %q", credsSecret)
	}
	pools, _, _ := unstructured.NestedSlice(tenant.Object, "spec", "pools")
	pool := pools[0].(map[string]interface{})
	if pool["name"] != "fast" || pool["servers"] != int64(4) {
		t.Fatalf("unexpected pool %#v", pool)
	}
	persistence := pool["persistence"].(map[string]interface{})
	volumeClaimTemplate := persistence["volumeClaimTemplate"].(map[string]interface{})
	if persistence["volumesPerServer"] != int64(4) {
		t.Fatalf("expected four volumes per server, got %#v", persistence["volumesPerServer"])
	}
	if volumeClaimTemplate["storageClassName"] != "fast-ssd" {
		t.Fatalf("expected storage class fast-ssd, got %#v", volumeClaimTemplate["storageClassName"])
	}
	env, _, _ := unstructured.NestedSlice(tenant.Object, "spec", "env")
	if len(env) != 1 {
		t.Fatalf("expected one env var, got %#v", env)
	}
	if env[0].(map[string]interface{})["name"] != "RUSTFS_UNSAFE_BYPASS_DISK_CHECK" ||
		env[0].(map[string]interface{})["value"] != "true" {
		t.Fatalf("unexpected env vars %#v", env)
	}
	requests := volumeClaimTemplate["resources"].(map[string]interface{})["requests"].(map[string]interface{})
	if requests["storage"] != "100Gi" {
		t.Fatalf("expected storage 100Gi, got %#v", requests["storage"])
	}
}

func TestBuildServiceAlias(t *testing.T) {
	service := BuildServiceAlias(rustfsTamossFixture())

	if service.Name != "example-s3" {
		t.Fatalf("expected alias service name example-s3, got %q", service.Name)
	}
	if service.Spec.Selector["rustfs.tenant"] != "example-s3" {
		t.Fatalf("expected rustfs tenant selector, got %#v", service.Spec.Selector)
	}
	if len(service.Spec.Ports) != 1 || service.Spec.Ports[0].Port != 9000 || service.Spec.Ports[0].TargetPort.IntVal != 9000 {
		t.Fatalf("expected s3 port 9000, got %#v", service.Spec.Ports)
	}
	if len(service.OwnerReferences) != 1 || service.OwnerReferences[0].Name != "example" {
		t.Fatalf("expected Tamoss owner reference, got %#v", service.OwnerReferences)
	}
}

func TestRustFSOperatorConnectionUsesPublicEndpoint(t *testing.T) {
	tamoss := rustfsTamossFixture()
	tamoss.Spec.Backends.S3.RustFSOperator.PublicEndpoint = tamossv1alpha1.S3PublicEndpointSpec{
		URL: "https://s3.tamoss.localtest.me",
	}

	connection := tamoss.S3Connection()
	if connection.Endpoint.Default.URL != "http://example-s3.tams.svc:9000" {
		t.Fatalf("expected namespaced in-cluster endpoint, got %q", connection.Endpoint.Default.URL)
	}
	if connection.Endpoint.Public.URL != "https://s3.tamoss.localtest.me" {
		t.Fatalf("expected public endpoint, got %q", connection.Endpoint.Public.URL)
	}
}

func rustfsTamossFixture() *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "tams",
			UID:       types.UID("example-uid"),
		},
		Spec: tamossv1alpha1.TamossSpec{
			Backends: tamossv1alpha1.BackendsSpec{
				S3: tamossv1alpha1.S3BackendSpec{
					ProvidedBy:     tamossv1alpha1.S3BackendProvidedByRustFSOperator,
					RustFSOperator: &tamossv1alpha1.S3RustFSOperatorSpec{},
				},
			},
		},
	}
}
