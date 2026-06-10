package cnpg

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestBuildClusterDefaults(t *testing.T) {
	cluster := BuildCluster(cnpgTamossFixture())

	if cluster.Name != "example-db" {
		t.Fatalf("expected cluster name example-db, got %q", cluster.Name)
	}
	if cluster.APIVersion != "postgresql.cnpg.io/v1" || cluster.Kind != "Cluster" {
		t.Fatalf("unexpected type meta %s/%s", cluster.APIVersion, cluster.Kind)
	}
	if cluster.Spec.Instances != 1 {
		t.Fatalf("expected one instance by default, got %d", cluster.Spec.Instances)
	}
	if cluster.Spec.ImageName != PostgresImage("") {
		t.Fatalf("unexpected image %q", cluster.Spec.ImageName)
	}
	if cluster.Spec.StorageConfiguration.Size != "10Gi" {
		t.Fatalf("expected default storage 10Gi, got %q", cluster.Spec.StorageConfiguration.Size)
	}
	if cluster.Spec.Bootstrap == nil || cluster.Spec.Bootstrap.InitDB == nil {
		t.Fatalf("expected initdb bootstrap")
	}
	if cluster.Spec.Bootstrap.InitDB.Database != "tams" || cluster.Spec.Bootstrap.InitDB.Owner != "tams" {
		t.Fatalf("expected tams database bootstrap, got %#v", cluster.Spec.Bootstrap.InitDB)
	}
	if cluster.Spec.EnableSuperuserAccess == nil || !*cluster.Spec.EnableSuperuserAccess {
		t.Fatalf("expected superuser access to be enabled")
	}
	if len(cluster.OwnerReferences) != 1 || cluster.OwnerReferences[0].Name != "example" {
		t.Fatalf("expected Tamoss owner reference, got %#v", cluster.OwnerReferences)
	}
}

func TestBuildClusterMapsInstancesStorageAndResources(t *testing.T) {
	tamoss := cnpgTamossFixture()
	cnpg := tamoss.Spec.Backends.DB.CNPG
	cnpg.Instances = 3
	cnpg.PostgresVersion = "16.4"
	cnpg.Storage = tamossv1alpha1.BackendStorageSpec{
		Size:         "50Gi",
		StorageClass: "fast",
	}
	cnpg.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
	}

	cluster := BuildCluster(tamoss)
	if cluster.Spec.Instances != 3 {
		t.Fatalf("expected three instances, got %d", cluster.Spec.Instances)
	}
	if cluster.Spec.ImageName != "ghcr.io/cloudnative-pg/postgresql:16.4" {
		t.Fatalf("unexpected image %q", cluster.Spec.ImageName)
	}
	if cluster.Spec.StorageConfiguration.Size != "50Gi" {
		t.Fatalf("expected storage 50Gi, got %q", cluster.Spec.StorageConfiguration.Size)
	}
	if cluster.Spec.StorageConfiguration.StorageClass == nil || *cluster.Spec.StorageConfiguration.StorageClass != "fast" {
		t.Fatalf("expected storage class fast, got %#v", cluster.Spec.StorageConfiguration.StorageClass)
	}
	if got := cluster.Spec.Resources.Requests[corev1.ResourceMemory]; got.String() != "1Gi" {
		t.Fatalf("expected memory request 1Gi, got %s", got.String())
	}
}

func TestBuildClusterMapsBackupAndMonitoring(t *testing.T) {
	tamoss := cnpgTamossFixture()
	cnpg := tamoss.Spec.Backends.DB.CNPG
	cnpg.Backup = tamossv1alpha1.DBCNPGBackupSpec{
		Enabled:         true,
		Schedule:        "0 0 2 * * *",
		RetentionPolicy: "30d",
		ObjectStore: tamossv1alpha1.DBCNPGObjectStoreSpec{
			EndpointURL:    "https://s3.example.com",
			Bucket:         "pg-backups",
			ExistingSecret: "backup-creds",
		},
	}
	cnpg.Monitoring.EnablePodMonitor = ptr.To(true)

	cluster := BuildCluster(tamoss)
	if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
		t.Fatalf("expected barman backup config")
	}
	store := cluster.Spec.Backup.BarmanObjectStore
	if store.EndpointURL != "https://s3.example.com" {
		t.Fatalf("expected endpoint https://s3.example.com, got %q", store.EndpointURL)
	}
	if store.DestinationPath != "s3://pg-backups/example-db" {
		t.Fatalf("unexpected destination path %q", store.DestinationPath)
	}
	if cluster.Spec.Backup.RetentionPolicy != "30d" {
		t.Fatalf("expected retention policy 30d, got %q", cluster.Spec.Backup.RetentionPolicy)
	}
	if store.AWS == nil || store.AWS.AccessKeyIDReference.Name != "backup-creds" {
		t.Fatalf("expected backup credential references, got %#v", store.AWS)
	}
	scheduled := BuildScheduledBackup(tamoss)
	if scheduled == nil {
		t.Fatalf("expected scheduled backup")
	}
	if scheduled.APIVersion != "postgresql.cnpg.io/v1" || scheduled.Kind != "ScheduledBackup" {
		t.Fatalf("unexpected scheduled backup type meta %s/%s", scheduled.APIVersion, scheduled.Kind)
	}
	if scheduled.Name != "example-db-backup" || scheduled.Spec.Cluster.Name != "example-db" {
		t.Fatalf("unexpected scheduled backup target: %#v", scheduled)
	}
	if scheduled.Spec.Schedule != "0 0 2 * * *" || scheduled.Spec.BackupOwnerReference != "cluster" {
		t.Fatalf("unexpected scheduled backup spec: %#v", scheduled.Spec)
	}
	if cluster.Spec.Monitoring == nil || !cluster.Spec.Monitoring.EnablePodMonitor { //nolint:staticcheck // The controller still sets the deprecated EnablePodMonitor knob deliberately; the test asserts it.
		t.Fatalf("expected PodMonitor enabled")
	}
}

func TestBuildClusterMapsRestore(t *testing.T) {
	tamoss := cnpgTamossFixture()
	cnpg := tamoss.Spec.Backends.DB.CNPG
	cnpg.Restore = tamossv1alpha1.DBCNPGRestoreSpec{
		Enabled:    true,
		Source:     "tamoss-source-db",
		TargetTime: "2026-05-22T12:00:00Z",
		ObjectStore: tamossv1alpha1.DBCNPGObjectStoreSpec{
			EndpointURL:    "https://s3.example.com",
			Bucket:         "pg-backups",
			ExistingSecret: "backup-creds",
		},
	}

	cluster := BuildCluster(tamoss)
	if cluster.Spec.Bootstrap == nil || cluster.Spec.Bootstrap.Recovery == nil {
		t.Fatalf("expected recovery bootstrap, got %#v", cluster.Spec.Bootstrap)
	}
	recovery := cluster.Spec.Bootstrap.Recovery
	if recovery.Source != "tamoss-source-db" || recovery.Database != "tams" || recovery.Owner != "tams" {
		t.Fatalf("unexpected recovery bootstrap: %#v", recovery)
	}
	if recovery.RecoveryTarget == nil ||
		recovery.RecoveryTarget.TargetTime != "2026-05-22T12:00:00Z" ||
		recovery.RecoveryTarget.TargetImmediate != nil {
		t.Fatalf("unexpected recovery target: %#v", recovery.RecoveryTarget)
	}
	if len(cluster.Spec.ExternalClusters) != 1 {
		t.Fatalf("expected one external cluster, got %#v", cluster.Spec.ExternalClusters)
	}
	external := cluster.Spec.ExternalClusters[0]
	if external.Name != "tamoss-source-db" || external.BarmanObjectStore == nil {
		t.Fatalf("unexpected external cluster: %#v", external)
	}
	if external.BarmanObjectStore.EndpointURL != "https://s3.example.com" ||
		external.BarmanObjectStore.DestinationPath != "s3://pg-backups/tamoss-source-db" {
		t.Fatalf("unexpected restore object store: %#v", external.BarmanObjectStore)
	}
	if external.BarmanObjectStore.AWS == nil ||
		external.BarmanObjectStore.AWS.AccessKeyIDReference.Name != "backup-creds" {
		t.Fatalf("expected restore credential references, got %#v", external.BarmanObjectStore.AWS)
	}
}

func TestBuildClusterOmitsDisabledBackupAndMonitoring(t *testing.T) {
	cluster := BuildCluster(cnpgTamossFixture())
	if cluster.Spec.Backup != nil {
		t.Fatalf("expected backup to be omitted by default")
	}
	if cluster.Spec.Monitoring != nil {
		t.Fatalf("expected monitoring to be omitted by default")
	}
	if scheduled := BuildScheduledBackup(cnpgTamossFixture()); scheduled != nil {
		t.Fatalf("expected scheduled backup to be omitted by default")
	}
}

func cnpgTamossFixture() *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "tams",
			UID:       types.UID("example-uid"),
		},
		Spec: tamossv1alpha1.TamossSpec{
			Backends: tamossv1alpha1.BackendsSpec{
				DB: tamossv1alpha1.DBBackendSpec{
					ProvidedBy: tamossv1alpha1.BackendProvidedByCNPG,
					CNPG:       &tamossv1alpha1.DBCNPGSpec{},
				},
			},
		},
	}
}
