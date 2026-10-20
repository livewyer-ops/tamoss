package workload_renderer

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestDatabaseTLSReachesAPIAndWorker(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Worker.Enabled = ptr.To(true)
	tamoss.Spec.Backends.DB.External.TLS = &tamossv1alpha1.DBTLSSpec{
		CAConfigMap: corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "database-ca"},
			Key:                  "root.pem",
		},
	}
	tamoss.Spec.API.Env = map[string]string{"PGSSLMODE": "disable"}
	tamoss.Spec.Worker.Env = map[string]string{"PGSSLROOTCERT": "/wrong/ca.pem"}
	objects := append(renderAPIDeployment(tamoss), renderWorkerDeployment(tamoss)...)
	for _, object := range objects {
		deployment := object.(*appsv1.Deployment)
		container := deployment.Spec.Template.Spec.Containers[0]
		values := map[string]string{}
		counts := map[string]int{}
		for _, entry := range container.Env {
			values[entry.Name] = entry.Value
			counts[entry.Name]++
		}
		if values["PGSSLMODE"] != "verify-full" || counts["PGSSLMODE"] != 1 ||
			values["PGSSLROOTCERT"] != "/etc/tamoss/database-ca/ca.pem" || counts["PGSSLROOTCERT"] != 1 {
			t.Fatalf("TLS settings not authoritative in %s", deployment.Name)
		}
	}
	if tamoss.Spec.API.Env["PGSSLMODE"] != "disable" {
		t.Fatal("render mutated the source configuration")
	}
	tamoss.Spec.Backends.DB.External.TLS = nil
	pod := corev1.PodSpec{Containers: []corev1.Container{{Name: "legacy"}}}
	if !reflect.DeepEqual(pod, WithDatabaseTLS(pod, tamoss)) {
		t.Fatal("omitted TLS changed the existing connection behaviour")
	}
}

func TestDatabaseTLSWithS3CredentialModes(t *testing.T) {
	for _, credential := range []tamossv1alpha1.S3CredentialSource{tamossv1alpha1.S3CredentialSourceSecret, tamossv1alpha1.S3CredentialSourceWebIdentity} {
		t.Run(string(credential), func(t *testing.T) {
			instance := rendererFixture()
			instance.Spec.Worker.Enabled = ptr.To(true)
			instance.Spec.Backends.DB.External.TLS = &tamossv1alpha1.DBTLSSpec{CAConfigMap: corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "database-ca"}, Key: "root.pem"}}
			instance.Spec.Backends.S3.ProvidedBy = tamossv1alpha1.S3BackendProvidedByExternal
			instance.Spec.Backends.S3.RustFSOperator = nil
			instance.Spec.Backends.S3.External.CredentialSource = credential
			if credential == tamossv1alpha1.S3CredentialSourceWebIdentity {
				instance.Spec.Backends.S3.External.Auth = tamossv1alpha1.SecretReferenceSpec{}
			}
			instance.Spec.ServiceAccount.Create = true
			instance.Spec.ServiceAccount.Annotations = map[string]string{"eks.amazonaws.com/role-arn": "arn:aws:iam::123456789012:role/media"}
			for name, render := range map[string]func(*tamossv1alpha1.Tamoss) []client.Object{"api": renderAPIDeployment, "worker": renderWorkerDeployment} {
				deployment := render(instance)[0].(*appsv1.Deployment)
				env := map[string]corev1.EnvVar{}
				for _, value := range deployment.Spec.Template.Spec.Containers[0].Env {
					env[value.Name] = value
				}
				if env["PGSSLMODE"].Value != "verify-full" || env["PGSSLROOTCERT"].Value != "/etc/tamoss/database-ca/ca.pem" {
					t.Fatalf("%s TLS settings missing", name)
				}
				_, hasStatic := env["TAMOSS_S3_ACCESS_KEY"]
				if hasStatic != (credential == tamossv1alpha1.S3CredentialSourceSecret) {
					t.Fatalf("%s S3 credentials changed", name)
				}
				if credential == tamossv1alpha1.S3CredentialSourceWebIdentity && deployment.Spec.Template.Annotations["tamoss.livewyer.io/workload-identity-hash"] == "" {
					t.Fatal("identity rollout hash lost")
				}
				mounted := false
				for _, mount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
					if mount.Name == "tamoss-database-ca" && mount.ReadOnly {
						mounted = true
					}
				}
				if !mounted {
					t.Fatal("CA mount missing")
				}
			}
		})
	}
}
