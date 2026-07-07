package workload_renderer

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func renderAPIDeployment(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	if !tamoss.Spec.API.IsEnabled() {
		return nil
	}
	apiPort := firstServicePort(tamoss.Spec.Service.API.Ports, 8000)
	env := append(backendEnv(tamoss), authEnv(tamoss)...)
	env = append(
		env,
		corev1.EnvVar{Name: "TAMOSS_METRICS_BIND_ADDRESS", Value: "0.0.0.0"},
		corev1.EnvVar{Name: "TAMOSS_METRICS_PORT", Value: fmt.Sprintf("%d", apiMetricsPort)},
	)
	if origins := apiCORSAllowedOrigins(tamoss.Spec.API.CORS.AllowedOrigins); origins != "" {
		env = append(env, corev1.EnvVar{Name: "TAMOSS_CORS_ALLOWED_ORIGINS", Value: origins})
	}
	if regexes := apiCORSAllowedOriginRegexes(tamoss.Spec.API.CORS.AllowedOriginRegexes); regexes != "" {
		env = append(env, corev1.EnvVar{Name: "TAMOSS_CORS_ALLOWED_ORIGIN_REGEXES", Value: regexes})
	}
	if !tamoss.Spec.Secrets.APIToken.Generate {
		env = append(env, corev1.EnvVar{Name: "TAMOSS_API_TOKEN", Value: tamoss.Spec.Secrets.APIToken.Token})
	}
	env = append(env, literalEnv(tamoss.Spec.API.Env)...)
	spec := withStorageBackendCredentialsVolume(tamoss.Spec.API.WorkloadCommonSpec, tamoss)
	return []client.Object{
		deploymentFor(
			tamoss,
			"api",
			spec,
			image(tamoss.Spec.API.Image.Repository, tamoss.Spec.API.Image.Tag),
			tamoss.Spec.API.Image.PullPolicy,
			[]string{"/bin/uv", "run", "uvicorn", "tamoss.app:app", "--host", "0.0.0.0", "--port", fmt.Sprintf("%d", apiPort), "--proxy-headers", "--forwarded-allow-ips", "*"},
			env,
			envFromSecrets(tamoss, tamoss.Spec.Secrets.APIToken.Generate, tamoss.Spec.Backends.DB.Provider() == tamossv1alpha1.BackendProvidedByCNPG, tamoss.Spec.Backends.S3.Provider() == tamossv1alpha1.S3BackendProvidedByRustFSOperator, oauth2CredentialsSecretName(tamoss), tamoss.Spec.API.EnvFrom),
			[]corev1.ContainerPort{
				{Name: "http", ContainerPort: apiPort, Protocol: corev1.ProtocolTCP},
				{Name: metricsPortName, ContainerPort: apiMetricsPort, Protocol: corev1.ProtocolTCP},
			},
		),
	}
}

func apiCORSAllowedOrigins(origins []string) string {
	allowed := make([]string, 0, len(origins))
	for _, origin := range origins {
		if trimmed := strings.TrimSpace(origin); trimmed != "" {
			allowed = append(allowed, trimmed)
		}
	}
	return strings.Join(allowed, ",")
}

func apiCORSAllowedOriginRegexes(regexes []string) string {
	allowed := make([]string, 0, len(regexes))
	for _, regex := range regexes {
		if trimmed := strings.TrimSpace(regex); trimmed != "" {
			allowed = append(allowed, trimmed)
		}
	}
	return strings.Join(allowed, "\n")
}
