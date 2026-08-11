package workload_renderer

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/auth/authentik"
)

const consoleUpstreamEnv = "TAMOSS_CONSOLE_UPSTREAM"

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
	if tamoss.Spec.ConsoleEnabled() {
		env = append(env, corev1.EnvVar{
			Name:  consoleUpstreamEnv,
			Value: fmt.Sprintf("http://%s:%d", tamoss.ResourceName("console"), firstServicePort(tamoss.Spec.Service.Console.Ports, defaultConsoleServicePort)),
		})
	}
	if forwardAuthEnabled(tamoss) {
		env = append(env,
			corev1.EnvVar{Name: "TAMOSS_UI_AUTH_MODE", Value: "authentik"},
			corev1.EnvVar{Name: "TAMOSS_AUTHENTIK_AUTH_REQUEST_URL", Value: authentik.OutpostNginxAuthAddress(tamoss)},
			corev1.EnvVar{Name: "TAMOSS_API_FORWARD_AUTH_SHARED_SECRET_FILE", Value: ForwardAuthProofFilePath(ForwardAuthAPIProofSecretKey)},
		)
		if tamoss.Spec.ConsoleEnabled() {
			env = append(env, corev1.EnvVar{
				Name:  "TAMOSS_CONSOLE_FORWARD_AUTH_SHARED_SECRET_FILE",
				Value: ForwardAuthProofFilePath(ForwardAuthConsoleProofSecretKey),
			})
		}
	} else if !tamoss.Spec.Auth.RequiredForRuntime() {
		env = append(env, corev1.EnvVar{Name: "TAMOSS_UI_AUTH_MODE", Value: "none"})
	} else {
		env = append(env, corev1.EnvVar{Name: "TAMOSS_UI_AUTH_MODE", Value: "unavailable"})
	}
	env = append(env, literalEnv(
		tamoss.Spec.UI.Env,
		consoleUpstreamEnv,
		"TAMOSS_UI_AUTH_MODE",
		"TAMOSS_AUTHENTIK_AUTH_REQUEST_URL",
		"TAMOSS_API_FORWARD_AUTH_SHARED_SECRET_FILE",
		"TAMOSS_CONSOLE_FORWARD_AUTH_SHARED_SECRET_FILE",
		"TAMOSS_API_TOKEN",
		"TAMOSS_OAUTH2_CLIENT_ID",
		"TAMOSS_OAUTH2_CLIENT_SECRET",
	)...)
	ports := tamoss.Spec.UI.Ports
	if len(ports) == 0 {
		ports = []corev1.ContainerPort{{Name: httpPortName, ContainerPort: 8080, Protocol: corev1.ProtocolTCP}}
	}
	proofKeys := []string{ForwardAuthAPIProofSecretKey}
	if tamoss.Spec.ConsoleEnabled() {
		proofKeys = append(proofKeys, ForwardAuthConsoleProofSecretKey)
	}
	spec := withForwardAuthProofVolume(tamoss.Spec.UI.WorkloadCommonSpec, tamoss, proofKeys...)
	return []client.Object{
		deploymentFor(
			tamoss,
			"ui",
			spec,
			uiImage(tamoss.Spec.UI.Image.Repository, tamoss.Spec.UI.Image.Tag),
			tamoss.Spec.UI.Image.PullPolicy,
			nil,
			env,
			tamoss.Spec.UI.EnvFrom,
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
