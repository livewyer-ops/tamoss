package controller

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestDatabaseTLSReachesEveryJob(t *testing.T) {
	tamoss := tamossFixture()
	tamoss.Spec.Backends.DB.External = &tamossv1alpha1.DBExternalSpec{Host: "database.example.test"}
	tamoss.Spec.Backends.DB.External.TLS = &tamossv1alpha1.DBTLSSpec{
		CAConfigMap: corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "database-ca"},
			Key:                  "root.pem",
		},
	}
	storage := &tamossv1alpha1.StorageBackend{}
	profile := flowProfileFixture()
	resolved, err := resolveFlowProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	jobs := map[string]*batchv1.Job{
		"schema":             testSchemaController().schemaMigrationJob(tamoss, false),
		"storage-register":   storageBackendRegistrationJob(storage, tamoss, storage.Spec, "hash"),
		"storage-deregister": storageBackendDeregistrationJob(storage, tamoss, storage.Spec, "hash"),
		"profile-register":   flowProfileRegistrationJob(profile, tamoss, resolved),
		"profile-delete":     flowProfileDeletionJob(profile, tamoss, resolved),
	}
	for name, job := range jobs {
		t.Run(name, func(t *testing.T) {
			pod := job.Spec.Template.Spec
			found := false
			for _, volume := range pod.Volumes {
				if volume.ConfigMap != nil && volume.ConfigMap.Name == "database-ca" {
					found = len(volume.ConfigMap.Items) == 1 && volume.ConfigMap.Items[0].Key == "root.pem"
				}
			}
			if !found {
				t.Fatal("database CA projection is missing")
			}
			for _, container := range pod.Containers {
				env := map[string]string{}
				for _, entry := range container.Env {
					env[entry.Name] = entry.Value
				}
				if env["PGSSLMODE"] != "verify-full" || env["PGSSLROOTCERT"] != "/etc/tamoss/database-ca/ca.pem" {
					t.Fatalf("database TLS settings missing from %s", container.Name)
				}
				mounted := false
				for _, mount := range container.VolumeMounts {
					mounted = mounted || (mount.Name == "tamoss-database-ca" && mount.ReadOnly)
				}
				if !mounted {
					t.Fatal("database CA mount is missing")
				}
			}
		})
	}
	changed := tamoss.DeepCopy()
	changed.Spec.Backends.DB.External.TLS.CAConfigMap.Name = "replacement-ca"
	if !schemaJobTemplateDrifted(jobs["schema"], testSchemaController().schemaMigrationJob(changed, false)) {
		t.Fatal("CA reference change did not invalidate the schema Job")
	}
	if !flowProfileJobDrifted(jobs["profile-register"], flowProfileRegistrationJob(profile, changed, resolved)) {
		t.Fatal("CA reference change did not invalidate the Profile Job")
	}
}

func TestDatabaseTLSRetainsReleaseTargets(t *testing.T) {
	catalogue := selectionCatalogue(t)
	for _, version := range []string{"8.2.0-oss2", "dev"} {
		t.Run(version, func(t *testing.T) {
			instance := recoveryTamoss()
			instance.Spec.Version = version
			instance.Status.CurrentVersion = version
			instance.Spec.Backends.DB.ProvidedBy = tamossv1alpha1.BackendProvidedByExternal
			instance.Spec.Backends.DB.External = &tamossv1alpha1.DBExternalSpec{
				Host: "database.example.test",
				TLS:  &tamossv1alpha1.DBTLSSpec{CAConfigMap: corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "database-ca"}, Key: "ca.pem"}},
			}
			resolved, err := resolveTamoss(instance, catalogue, nil)
			if err != nil {
				t.Fatal(err)
			}
			c := SchemaController{Target: catalogue[version].Schema}
			job := c.schemaMigrationJob(resolved, false)
			container := job.Spec.Template.Spec.Containers[0]
			if container.Image != catalogue[version].Images.API || container.Args[4] != catalogue[version].Schema.DatabaseRevision {
				t.Fatal("TLS changed the selected runtime or database revision")
			}
			found := false
			for _, env := range container.Env {
				if env.Name == "PGSSLMODE" && env.Value == "verify-full" {
					found = true
				}
			}
			if !found || len(container.VolumeMounts) == 0 || job.Spec.Template.Spec.Volumes[0].ConfigMap.Name != "database-ca" {
				t.Fatal("TLS lost during release resolution")
			}
		})
	}
}
