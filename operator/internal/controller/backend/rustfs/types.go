package rustfs

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// The upstream RustFS operator is implemented in Rust with kube-rs and does
// not publish Go API packages. Keep this shim narrow and aligned with the CRD.
var TenantGVR = schema.GroupVersionResource{
	Group:    "rustfs.com",
	Version:  "v1alpha1",
	Resource: "tenants",
}

var TenantGVK = TenantGVR.GroupVersion().WithKind("Tenant")

func NewTenant() *unstructured.Unstructured {
	tenant := &unstructured.Unstructured{}
	tenant.SetGroupVersionKind(TenantGVK)
	return tenant
}

func NewTenantList() *unstructured.UnstructuredList {
	tenants := &unstructured.UnstructuredList{}
	tenants.SetGroupVersionKind(TenantGVR.GroupVersion().WithKind("TenantList"))
	return tenants
}

type tenantSpec struct {
	Image           string            `json:"image,omitempty"`
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
	Env             []corev1.EnvVar   `json:"env,omitempty"`
	CredsSecret     *tenantSecretRef  `json:"credsSecret,omitempty"`
	Pools           []tenantPool      `json:"pools,omitempty"`
}

type tenantSecretRef struct {
	Name string `json:"name,omitempty"`
}

type tenantPool struct {
	Name        string            `json:"name,omitempty"`
	Servers     int32             `json:"servers,omitempty"`
	Persistence tenantPersistence `json:"persistence,omitempty"`
}

type tenantPersistence struct {
	VolumesPerServer    int32                     `json:"volumesPerServer,omitempty"`
	VolumeClaimTemplate tenantVolumeClaimTemplate `json:"volumeClaimTemplate,omitempty"`
}

type tenantVolumeClaimTemplate struct {
	AccessModes      []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
	Resources        tenantVolumeResources               `json:"resources,omitempty"`
	StorageClassName string                              `json:"storageClassName,omitempty"`
}

type tenantVolumeResources struct {
	Requests map[corev1.ResourceName]string `json:"requests,omitempty"`
}
