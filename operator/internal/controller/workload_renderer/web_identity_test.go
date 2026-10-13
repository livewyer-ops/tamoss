package workload_renderer

import (
	"testing"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRenderS3WebIdentityAndRollback(t *testing.T) {
	instance := rendererFixture()
	instance.Spec.Worker.Enabled = ptr.To(true)
	instance.Spec.Backends.S3.ProvidedBy = tamossv1alpha1.S3BackendProvidedByExternal
	instance.Spec.Backends.S3.RustFSOperator = nil
	instance.Spec.Backends.S3.External.CredentialSource = tamossv1alpha1.S3CredentialSourceWebIdentity
	instance.Spec.Backends.S3.External.Auth = tamossv1alpha1.SecretReferenceSpec{}
	instance.Spec.ServiceAccount.Annotations = map[string]string{"eks.amazonaws.com/role-arn": "arn:aws:iam::123456789012:role/media"}
	instance.Spec.UI.PodAnnotations = map[string]string{"eks.amazonaws.com/skip-containers": "sidecar"}
	for component, render := range map[string]func(*tamossv1alpha1.Tamoss) []client.Object{
		"api": renderAPIDeployment, "worker": renderWorkerDeployment,
	} {
		deployment := render(instance)[0].(*appsv1.Deployment)
		for _, env := range deployment.Spec.Template.Spec.Containers[0].Env {
			if env.Name == "TAMOSS_S3_ACCESS_KEY" || env.Name == "TAMOSS_S3_SECRET_KEY" {
				t.Fatalf("%s contains static keys", component)
			}
		}
		before := deployment.Spec.Template.Annotations["tamoss.livewyer.io/workload-identity-hash"]
		instance.Spec.ServiceAccount.Annotations["eks.amazonaws.com/role-arn"] += "-rotated"
		next := render(instance)[0].(*appsv1.Deployment)
		if before == "" || before == next.Spec.Template.Annotations["tamoss.livewyer.io/workload-identity-hash"] {
			t.Fatal("identity change did not change rollout hash")
		}
	}
	ui := renderUIDeployment(instance)[0].(*appsv1.Deployment)
	if got := ui.Spec.Template.Annotations["eks.amazonaws.com/skip-containers"]; got != "sidecar,ui" {
		t.Fatalf("skip containers=%q", got)
	}
	if ui.Spec.Template.Spec.AutomountServiceAccountToken == nil || *ui.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatal("UI mounts Kubernetes token")
	}
	instance.Spec.Backends.S3.External.CredentialSource = ""
	ui = renderUIDeployment(instance)[0].(*appsv1.Deployment)
	if ui.Spec.Template.Annotations["eks.amazonaws.com/skip-containers"] != "sidecar,ui" {
		t.Fatal("Secret rollback exposes the UI to the retained identity annotation")
	}
	instance.Spec.Backends.S3.External.Auth = tamossv1alpha1.SecretReferenceSpec{ExistingSecret: "retained", SecretKeys: tamossv1alpha1.SecretKeySpec{AccessKey: "access", SecretKey: "secret"}}
	keys := 0
	for _, env := range backendEnv(instance) {
		if env.Name == "TAMOSS_S3_ACCESS_KEY" || env.Name == "TAMOSS_S3_SECRET_KEY" {
			keys++
			if env.ValueFrom.SecretKeyRef.Name != "retained" {
				t.Fatal("wrong rollback Secret")
			}
		}
	}
	if keys != 2 {
		t.Fatal("rollback did not restore static credentials")
	}
}
