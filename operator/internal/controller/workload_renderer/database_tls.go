package workload_renderer

import (
	corev1 "k8s.io/api/core/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	databaseCAVolume = "tamoss-database-ca"
	databaseCAPath   = "/etc/tamoss/database-ca"
)

// WithDatabaseTLS applies the same libpq trust settings to workloads and database Jobs.
func WithDatabaseTLS(pod corev1.PodSpec, tamoss *tamossv1alpha1.Tamoss) corev1.PodSpec {
	tls := tamoss.DBConnection().TLS
	if tls == nil {
		return pod
	}
	pod = *pod.DeepCopy()
	mode := int32(0o444)
	pod.Volumes = append(pod.Volumes, corev1.Volume{
		Name: databaseCAVolume,
		VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: tls.CAConfigMap.LocalObjectReference,
			DefaultMode:          &mode,
			Items:                []corev1.KeyToPath{{Key: tls.CAConfigMap.Key, Path: "ca.pem"}},
		}},
	})
	for i := range pod.Containers {
		container := &pod.Containers[i]
		env := make([]corev1.EnvVar, 0, len(container.Env)+2)
		for _, entry := range container.Env {
			if entry.Name != "PGSSLMODE" && entry.Name != "PGSSLROOTCERT" {
				env = append(env, entry)
			}
		}
		container.Env = append(env,
			corev1.EnvVar{Name: "PGSSLMODE", Value: "verify-full"},
			corev1.EnvVar{Name: "PGSSLROOTCERT", Value: databaseCAPath + "/ca.pem"},
		)
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name: databaseCAVolume, MountPath: databaseCAPath, ReadOnly: true,
		})
	}
	return pod
}
