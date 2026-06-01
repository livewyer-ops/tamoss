package workload_renderer

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func renderWorkerDeployment(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	if !tamoss.Spec.Worker.IsEnabled() {
		return nil
	}
	env := []corev1.EnvVar{}
	if _, ok := tamoss.Spec.Worker.Env["TAMOSS_WORKER_ID"]; !ok {
		env = append(env, corev1.EnvVar{
			Name: "TAMOSS_WORKER_ID",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.name",
				},
			},
		})
	}
	env = append(env, backendEnv(tamoss)...)
	env = append(env, literalEnv(tamoss.Spec.Worker.Env)...)
	spec := withStorageBackendCredentialsVolume(tamoss.Spec.Worker.WorkloadCommonSpec, tamoss)
	deployment := deploymentFor(
		tamoss,
		"worker",
		spec,
		image(tamoss.Spec.API.Image.Repository, tamoss.Spec.API.Image.Tag),
		tamoss.Spec.API.Image.PullPolicy,
		[]string{"/bin/uv", "run", "python", "-m", "tamoss.worker"},
		env,
		envFromSecrets(tamoss, false, tamoss.Spec.Backends.DB.Provider() == tamossv1alpha1.BackendProvidedByCNPG, tamoss.Spec.Backends.S3.Provider() == tamossv1alpha1.S3BackendProvidedByRustFSOperator, "", tamoss.Spec.Worker.EnvFrom),
		nil,
	)
	deployment.ObjectMeta.Name = tamoss.ResourceName("worker")
	deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: selectorLabels(tamoss, "worker")}
	return []client.Object{deployment}
}
