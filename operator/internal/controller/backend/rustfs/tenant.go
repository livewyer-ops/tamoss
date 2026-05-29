package rustfs

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/resource"
)

type ObjectMutator func(client.Object) error

func BuildTenant(tamoss *tamossv1alpha1.Tamoss) (*unstructured.Unstructured, error) {
	tenant := NewTenant()
	tenant.SetName(tamoss.ResourceName("s3"))
	tenant.SetNamespace(tamoss.Namespace)
	tenant.SetLabels(resource.TamossLabels(tamoss, "s3"))
	tenant.SetOwnerReferences(resource.TamossOwnerReferences(tamoss))

	spec, err := tenantSpecToUnstructured(buildTenantSpec(tamoss))
	if err != nil {
		return nil, err
	}
	tenant.Object["spec"] = spec
	return tenant, nil
}

func BuildServiceAlias(tamoss *tamossv1alpha1.Tamoss) *corev1.Service {
	tenantName := tamoss.ResourceName("s3")
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            tenantName,
			Namespace:       tamoss.Namespace,
			Labels:          resource.TamossLabels(tamoss, "s3"),
			OwnerReferences: resource.TamossOwnerReferences(tamoss),
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"rustfs.tenant": tenantName,
			},
			Ports: []corev1.ServicePort{{
				Name:       "s3",
				Port:       9000,
				TargetPort: intstr.FromInt(9000),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
}

func Reconcile(ctx context.Context, c client.Client, tamoss *tamossv1alpha1.Tamoss, mutators ...ObjectMutator) error {
	if tamoss.Spec.Backends.S3.Provider() != tamossv1alpha1.S3BackendProvidedByRustFSOperator {
		return nil
	}
	tenant, err := BuildTenant(tamoss)
	if err != nil {
		return err
	}
	for _, mutate := range mutators {
		if mutate == nil {
			continue
		}
		if err := mutate(tenant); err != nil {
			return err
		}
	}
	return c.Patch(ctx, tenant, client.Apply, client.FieldOwner(resource.FieldOwner))
}

func rustfsOperatorSpec(tamoss *tamossv1alpha1.Tamoss) tamossv1alpha1.S3RustFSOperatorSpec {
	if tamoss.Spec.Backends.S3.RustFSOperator == nil {
		return tamossv1alpha1.S3RustFSOperatorSpec{}
	}
	return *tamoss.Spec.Backends.S3.RustFSOperator
}

func buildTenantSpec(tamoss *tamossv1alpha1.Tamoss) tenantSpec {
	rustfsSpec := rustfsOperatorSpec(tamoss)
	spec := tenantSpec{
		Image:           rustfsSpec.Image,
		ImagePullPolicy: rustfsSpec.ImagePullPolicy,
		Env:             rustfsSpec.Env,
		Pools:           tenantPools(rustfsSpec.Pools),
	}
	if secretName := tamoss.S3Connection().Auth.ExistingSecret; secretName != "" {
		spec.CredsSecret = &tenantSecretRef{Name: secretName}
	}
	return spec
}

func tenantPools(specs []tamossv1alpha1.S3RustFSPoolSpec) []tenantPool {
	if len(specs) == 0 {
		specs = []tamossv1alpha1.S3RustFSPoolSpec{{
			Name:             "pool-0",
			Servers:          2,
			VolumesPerServer: 2,
			Storage: tamossv1alpha1.BackendStorageSpec{
				Size: "10Gi",
			},
		}}
	}
	result := make([]tenantPool, 0, len(specs))
	for i, spec := range specs {
		name := spec.Name
		if name == "" {
			name = fmt.Sprintf("pool-%d", i)
		}
		servers := spec.Servers
		if servers < 1 {
			servers = 2
		}
		volumesPerServer := spec.VolumesPerServer
		if volumesPerServer < 1 {
			volumesPerServer = 2
		}
		size := spec.Storage.Size
		if size == "" {
			size = "10Gi"
		}
		volumeClaimTemplate := tenantVolumeClaimTemplate{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: tenantVolumeResources{
				Requests: map[corev1.ResourceName]string{
					corev1.ResourceStorage: size,
				},
			},
		}
		if spec.Storage.StorageClass != "" {
			volumeClaimTemplate.StorageClassName = spec.Storage.StorageClass
		}
		result = append(result, tenantPool{
			Name:    name,
			Servers: servers,
			Persistence: tenantPersistence{
				VolumesPerServer:    volumesPerServer,
				VolumeClaimTemplate: volumeClaimTemplate,
			},
		})
	}
	return result
}

func tenantSpecToUnstructured(spec tenantSpec) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"pools": tenantPoolsToUnstructured(spec.Pools),
	}
	if spec.Image != "" {
		result["image"] = spec.Image
	}
	if spec.ImagePullPolicy != "" {
		result["imagePullPolicy"] = string(spec.ImagePullPolicy)
	}
	if len(spec.Env) > 0 {
		env, err := envVarsToUnstructured(spec.Env)
		if err != nil {
			return nil, err
		}
		result["env"] = env
	}
	if spec.CredsSecret != nil && spec.CredsSecret.Name != "" {
		result["credsSecret"] = map[string]interface{}{"name": spec.CredsSecret.Name}
	}
	return result, nil
}

func tenantPoolsToUnstructured(pools []tenantPool) []interface{} {
	result := make([]interface{}, 0, len(pools))
	for _, pool := range pools {
		volumeClaimTemplate := map[string]interface{}{
			"accessModes": accessModesToUnstructured(pool.Persistence.VolumeClaimTemplate.AccessModes),
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"storage": pool.Persistence.VolumeClaimTemplate.Resources.Requests[corev1.ResourceStorage],
				},
			},
		}
		if pool.Persistence.VolumeClaimTemplate.StorageClassName != "" {
			volumeClaimTemplate["storageClassName"] = pool.Persistence.VolumeClaimTemplate.StorageClassName
		}
		result = append(result, map[string]interface{}{
			"name":    pool.Name,
			"servers": int64(pool.Servers),
			"persistence": map[string]interface{}{
				"volumesPerServer":    int64(pool.Persistence.VolumesPerServer),
				"volumeClaimTemplate": volumeClaimTemplate,
			},
		})
	}
	return result
}

func accessModesToUnstructured(values []corev1.PersistentVolumeAccessMode) []interface{} {
	result := make([]interface{}, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func envVarsToUnstructured(vars []corev1.EnvVar) ([]interface{}, error) {
	result := make([]interface{}, 0, len(vars))
	for _, env := range vars {
		item, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&env)
		if err != nil {
			return nil, fmt.Errorf("convert RustFS Tenant env var %q: %w", env.Name, err)
		}
		result = append(result, item)
	}
	return result, nil
}
