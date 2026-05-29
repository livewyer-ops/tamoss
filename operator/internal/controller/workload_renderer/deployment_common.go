package workload_renderer

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	StorageBackendCredentialsFileName = "credentials.json"

	storageBackendCredentialsVolumeName = "storage-backend-credentials"
	storageBackendCredentialsMountPath  = "/run/tamoss/storage-backend-credentials"
)

func deploymentFor(
	tamoss *tamossv1alpha1.Tamoss,
	component string,
	spec tamossv1alpha1.WorkloadCommonSpec,
	image string,
	imagePullPolicy corev1.PullPolicy,
	command []string,
	env []corev1.EnvVar,
	envFrom []corev1.EnvFromSource,
	ports []corev1.ContainerPort,
) *appsv1.Deployment {
	replicas := spec.DesiredReplicaCount()
	podLabels := mergeStringMaps(labels(tamoss, component), spec.PodLabels)
	podAnnotations := mergeStringMaps(nil, spec.PodAnnotations)
	container := corev1.Container{
		Name:            component,
		Image:           image,
		ImagePullPolicy: imagePullPolicy,
		Command:         command,
		Env:             env,
		EnvFrom:         envFrom,
		Ports:           ports,
		SecurityContext: spec.SecurityContext,
		Resources:       spec.Resources,
		VolumeMounts:    spec.VolumeMounts,
		LivenessProbe:   spec.LivenessProbe,
		ReadinessProbe:  spec.ReadinessProbe,
		StartupProbe:    spec.StartupProbe,
	}
	if spec.PreStopSleepSeconds != nil && *spec.PreStopSleepSeconds > 0 {
		container.Lifecycle = &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"/bin/sh", "-c", fmt.Sprintf("sleep %d", *spec.PreStopSleepSeconds)},
				},
			},
		}
	}
	podSpec := corev1.PodSpec{
		ServiceAccountName: serviceAccountName(tamoss),
		ImagePullSecrets:   tamoss.Spec.ImagePullSecrets,
		SecurityContext:    spec.PodSecurityContext,
		Containers:         []corev1.Container{container},
		Volumes:            spec.Volumes,
		NodeSelector:       spec.NodeSelector,
		Tolerations:        spec.Tolerations,
		Affinity:           spec.Affinity,
	}
	if spec.TerminationGracePeriodSeconds != nil {
		podSpec.TerminationGracePeriodSeconds = spec.TerminationGracePeriodSeconds
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tamoss.ResourceName(component),
			Namespace: tamoss.Namespace,
			Labels:    labels(tamoss, component),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels(tamoss, component),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: podAnnotations,
				},
				Spec: podSpec,
			},
		},
	}
	if !spec.Autoscaling.Enabled {
		deployment.Spec.Replicas = &replicas
	}
	return deployment
}

func backendEnv(tamoss *tamossv1alpha1.Tamoss) []corev1.EnvVar {
	db := tamoss.DBConnection()
	s3 := tamoss.S3Connection()
	return []corev1.EnvVar{
		secretKeyEnv("POSTGRES_USER", db.Auth.ExistingSecret, db.Auth.SecretKeys.Username),
		secretKeyEnv("POSTGRES_PASSWORD", db.Auth.ExistingSecret, db.Auth.SecretKeys.Password),
		secretKeyEnv("TAMOSS_S3_ACCESS_KEY", s3.Auth.ExistingSecret, s3.Auth.SecretKeys.AccessKey),
		secretKeyEnv("TAMOSS_S3_SECRET_KEY", s3.Auth.ExistingSecret, s3.Auth.SecretKeys.SecretKey),
		{Name: "TAMOSS_STORAGE_BACKEND_REGISTRATION_ENABLED", Value: "false"},
		{Name: "TAMOSS_STORAGE_BACKEND_CREDENTIALS_FILE", Value: StorageBackendCredentialsFilePath()},
	}
}

func StorageBackendCredentialsSecretName(tamoss *tamossv1alpha1.Tamoss) string {
	return tamoss.ResourceName("storage-backend-credentials")
}

func StorageBackendCredentialsFilePath() string {
	return storageBackendCredentialsMountPath + "/" + StorageBackendCredentialsFileName
}

func withStorageBackendCredentialsVolume(spec tamossv1alpha1.WorkloadCommonSpec, tamoss *tamossv1alpha1.Tamoss) tamossv1alpha1.WorkloadCommonSpec {
	mode := int32(0o444)
	next := spec
	next.VolumeMounts = append([]corev1.VolumeMount{}, spec.VolumeMounts...)
	next.VolumeMounts = append(next.VolumeMounts, corev1.VolumeMount{
		Name:      storageBackendCredentialsVolumeName,
		MountPath: storageBackendCredentialsMountPath,
		ReadOnly:  true,
	})
	next.Volumes = append([]corev1.Volume{}, spec.Volumes...)
	next.Volumes = append(next.Volumes, corev1.Volume{
		Name: storageBackendCredentialsVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  StorageBackendCredentialsSecretName(tamoss),
				DefaultMode: &mode,
				Items: []corev1.KeyToPath{{
					Key:  StorageBackendCredentialsFileName,
					Path: StorageBackendCredentialsFileName,
					Mode: &mode,
				}},
			},
		},
	})
	return next
}

func authEnv(tamoss *tamossv1alpha1.Tamoss) []corev1.EnvVar {
	oauth2 := tamoss.Spec.Auth.OAuth2Config(tamoss.Namespace, tamoss.Name)
	return []corev1.EnvVar{
		{Name: "TAMOSS_AUTH_REQUIRED", Value: fmt.Sprintf("%t", tamoss.Spec.Auth.RequiredForRuntime())},
		{Name: "TAMOSS_TRUST_FORWARD_AUTH_HEADERS", Value: fmt.Sprintf("%t", tamoss.Spec.Auth.TrustForwardAuthHeaders)},
		{Name: "TAMOSS_OAUTH2_ENABLED", Value: fmt.Sprintf("%t", oauth2.Enabled)},
		{Name: "TAMOSS_OAUTH2_ISSUER", Value: oauth2.Issuer},
		{Name: "TAMOSS_OAUTH2_JWKS_URI", Value: oauth2.JWKSURI},
		{Name: "TAMOSS_OAUTH2_AUDIENCE", Value: oauth2.Audience},
		{Name: "TAMOSS_OAUTH2_REQUIRED_SCOPES", Value: strings.Join(oauth2.RequiredScopes, ",")},
		{Name: "TAMOSS_OAUTH2_ALGORITHMS", Value: strings.Join(oauth2.Algorithms, ",")},
	}
}

func secretKeyEnv(name, secretName, key string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  key,
			},
		},
	}
}

func literalEnv(env map[string]string) []corev1.EnvVar {
	vars := make([]corev1.EnvVar, 0, len(env))
	for _, key := range sortedEnv(env) {
		vars = append(vars, corev1.EnvVar{Name: key, Value: env[key]})
	}
	return vars
}

func envFromSecrets(tamoss *tamossv1alpha1.Tamoss, includeAPIToken bool, includeCNPGApp bool, includeRustFSCreds bool, oauth2CredsSecret string, extra []corev1.EnvFromSource) []corev1.EnvFromSource {
	envFrom := []corev1.EnvFromSource{{
		SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: tamoss.ResourceName("backends")},
		},
	}}
	if includeCNPGApp {
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: tamoss.DBConnection().Auth.ExistingSecret},
			},
		})
	}
	if includeRustFSCreds {
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: tamoss.S3Connection().Auth.ExistingSecret},
			},
		})
	}
	if includeAPIToken {
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: tamoss.ResourceName("api-token")},
			},
		})
	}
	if oauth2CredsSecret != "" {
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: oauth2CredsSecret},
			},
		})
	}
	return append(envFrom, extra...)
}

func oauth2CredentialsSecretName(tamoss *tamossv1alpha1.Tamoss) string {
	switch tamoss.Spec.Auth.Provider() {
	case tamossv1alpha1.AuthProvidedByAuthentikBlueprints:
		return tamoss.OAuth2CredentialsSecretName()
	case tamossv1alpha1.AuthProvidedByExternal:
		return tamoss.Spec.Auth.OAuth2Config(tamoss.Namespace, tamoss.Name).ClientCredentialsSecret.ExistingSecret
	default:
		return ""
	}
}
