package workload_renderer

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	consoleNamespaceEnv = "TAMOSS_CONSOLE_NAMESPACE"
	consoleInstanceEnv  = "TAMOSS_CONSOLE_INSTANCE"
	consoleBindEnv      = "TAMOSS_CONSOLE_BIND_ADDRESS"
)

func renderConsoleDeployment(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	if !tamoss.Spec.ConsoleEnabled() {
		return nil
	}
	env := make([]corev1.EnvVar, 0, 3+len(tamoss.Spec.Console.Env))
	env = append(env,
		corev1.EnvVar{
			Name: consoleNamespaceEnv,
			ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{
				APIVersion: "v1",
				FieldPath:  "metadata.namespace",
			}},
		},
		corev1.EnvVar{Name: consoleInstanceEnv, Value: tamoss.Name},
		corev1.EnvVar{Name: consoleBindEnv, Value: ":8080"},
	)
	env = append(env, literalEnv(
		tamoss.Spec.Console.Env,
		consoleNamespaceEnv,
		consoleInstanceEnv,
		consoleBindEnv,
	)...)
	deployment := deploymentFor(
		tamoss,
		"console",
		tamoss.Spec.Console.WorkloadCommonSpec,
		consoleImage(tamoss.Spec.Console.Image.Repository, tamoss.Spec.Console.Image.Tag),
		tamoss.Spec.Console.Image.PullPolicy,
		nil,
		env,
		tamoss.Spec.Console.EnvFrom,
		[]corev1.ContainerPort{{Name: httpPortName, ContainerPort: 8080, Protocol: corev1.ProtocolTCP}},
	)
	deployment.Spec.Template.Spec.ServiceAccountName = consoleServiceAccountName(tamoss)
	deployment.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(true)
	deployment.Spec.Template.Spec.Containers[0].TerminationMessagePolicy = corev1.TerminationMessageFallbackToLogsOnError
	deployment.Spec.Template.Annotations = mergeStringMaps(
		deployment.Spec.Template.Annotations,
		map[string]string{"kubectl.kubernetes.io/default-container": "console"},
	)
	return []client.Object{deployment}
}
