package workload_renderer

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func renderUIDeployment(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	if !tamoss.Spec.UI.IsEnabled() {
		return nil
	}
	env := []corev1.EnvVar{}
	if tamoss.Spec.API.IsEnabled() {
		env = append(env, corev1.EnvVar{
			Name:  "TAMOSS_API_UPSTREAM",
			Value: fmt.Sprintf("http://%s:%d", tamoss.ResourceName("api"), firstServicePort(tamoss.Spec.Service.API.Ports, 8000)),
		})
	}
	if !tamoss.Spec.Secrets.APIToken.Generate {
		env = append(env, corev1.EnvVar{Name: "TAMOSS_API_TOKEN", Value: tamoss.Spec.Secrets.APIToken.Token})
	}
	env = append(env, authEnv(tamoss)...)
	env = append(env, literalEnv(tamoss.Spec.UI.Env)...)
	ports := tamoss.Spec.UI.Ports
	if len(ports) == 0 {
		ports = []corev1.ContainerPort{{Name: "http", ContainerPort: 8080, Protocol: corev1.ProtocolTCP}}
	}
	return []client.Object{
		deploymentFor(
			tamoss,
			"ui",
			tamoss.Spec.UI.WorkloadCommonSpec,
			uiImage(tamoss.Spec.UI.Image.Repository, tamoss.Spec.UI.Image.Tag),
			tamoss.Spec.UI.Image.PullPolicy,
			nil,
			env,
			envFromSecrets(tamoss, tamoss.Spec.Secrets.APIToken.Generate, false, false, oauth2CredentialsSecretName(tamoss), tamoss.Spec.UI.EnvFrom),
			ports,
		),
	}
}

func firstServicePort(ports []corev1.ServicePort, fallback int32) int32 {
	if len(ports) == 0 {
		return fallback
	}
	return ports[0].Port
}
