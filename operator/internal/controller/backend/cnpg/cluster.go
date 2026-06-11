package cnpg

import (
	"context"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/resource"
)

type ObjectMutator func(client.Object) error

func BuildCluster(tamoss *tamossv1alpha1.Tamoss) *cnpgv1.Cluster {
	clusterName := tamoss.ResourceName("db")
	spec := cnpgSpec(tamoss)
	enableSuperuserAccess := true
	return &cnpgv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cnpgv1.SchemeGroupVersion.String(),
			Kind:       "Cluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            clusterName,
			Namespace:       tamoss.Namespace,
			Labels:          resource.TamossLabels(tamoss, "postgres"),
			OwnerReferences: resource.TamossOwnerReferences(tamoss),
		},
		Spec: cnpgv1.ClusterSpec{
			Instances:             instances(spec.Instances),
			ImageName:             PostgresImage(spec.PostgresVersion),
			StorageConfiguration:  storage(spec.Storage),
			Resources:             spec.Resources,
			EnableSuperuserAccess: &enableSuperuserAccess,
			PostgresConfiguration: defaultPostgresConfiguration(),
			Bootstrap:             bootstrap(spec),
			ExternalClusters:      restoreExternalClusters(spec.Restore),
			Monitoring:            backupMonitoring(spec.Monitoring),
			Backup:                backup(clusterName, spec.Backup),
		},
	}
}

func BuildScheduledBackup(tamoss *tamossv1alpha1.Tamoss) *cnpgv1.ScheduledBackup {
	clusterName := tamoss.ResourceName("db")
	spec := cnpgSpec(tamoss)
	if !spec.Backup.Enabled {
		return nil
	}
	return &cnpgv1.ScheduledBackup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cnpgv1.SchemeGroupVersion.String(),
			Kind:       "ScheduledBackup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            tamoss.ResourceName("db-backup"),
			Namespace:       tamoss.Namespace,
			Labels:          resource.TamossLabels(tamoss, "postgres"),
			OwnerReferences: resource.TamossOwnerReferences(tamoss),
		},
		Spec: cnpgv1.ScheduledBackupSpec{
			Schedule: spec.Backup.Schedule,
			Cluster: cnpgv1.LocalObjectReference{
				Name: clusterName,
			},
			BackupOwnerReference: "cluster",
		},
	}
}

func Reconcile(ctx context.Context, c client.Client, tamoss *tamossv1alpha1.Tamoss, mutators ...ObjectMutator) error {
	if tamoss.Spec.Backends.DB.Provider() != tamossv1alpha1.BackendProvidedByCNPG {
		return nil
	}
	cluster := BuildCluster(tamoss)
	if err := mutateObject(cluster, mutators...); err != nil {
		return err
	}
	if err := c.Patch(ctx, cluster, client.Apply, client.FieldOwner(resource.FieldOwner)); err != nil { //nolint:staticcheck // client.Apply patches remain supported; migrating to client.Client.Apply(ApplyConfiguration) is a wider refactor than this upgrade.
		return err
	}
	if scheduledBackup := BuildScheduledBackup(tamoss); scheduledBackup != nil {
		if err := mutateObject(scheduledBackup, mutators...); err != nil {
			return err
		}
		return c.Patch(ctx, scheduledBackup, client.Apply, client.FieldOwner(resource.FieldOwner)) //nolint:staticcheck // client.Apply patches remain supported; migrating to client.Client.Apply(ApplyConfiguration) is a wider refactor than this upgrade.
	}
	return nil
}

func mutateObject(obj client.Object, mutators ...ObjectMutator) error {
	for _, mutate := range mutators {
		if mutate == nil {
			continue
		}
		if err := mutate(obj); err != nil {
			return err
		}
	}
	return nil
}

func cnpgSpec(tamoss *tamossv1alpha1.Tamoss) tamossv1alpha1.DBCNPGSpec {
	if tamoss.Spec.Backends.DB.CNPG == nil {
		return tamossv1alpha1.DBCNPGSpec{}
	}
	return *tamoss.Spec.Backends.DB.CNPG
}

func instances(value int32) int {
	if value < 1 {
		return 1
	}
	return int(value)
}

func PostgresImage(version string) string {
	if version == "" {
		version = defaults.DefaultCNPGPostgresVersion
	}
	return fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", version)
}

func storage(spec tamossv1alpha1.BackendStorageSpec) cnpgv1.StorageConfiguration {
	if spec.Size == "" {
		spec.Size = "10Gi"
	}
	config := cnpgv1.StorageConfiguration{Size: spec.Size}
	if spec.StorageClass != "" {
		config.StorageClass = &spec.StorageClass
	}
	return config
}

func defaultPostgresConfiguration() cnpgv1.PostgresConfiguration {
	return cnpgv1.PostgresConfiguration{
		Parameters: map[string]string{
			"max_connections": "200",
		},
	}
}

func bootstrap(spec tamossv1alpha1.DBCNPGSpec) *cnpgv1.BootstrapConfiguration {
	if spec.Restore.Enabled {
		return &cnpgv1.BootstrapConfiguration{
			Recovery: &cnpgv1.BootstrapRecovery{
				Source:         spec.Restore.Source,
				Database:       "tams",
				Owner:          "tams",
				RecoveryTarget: recoveryTarget(spec.Restore),
			},
		}
	}
	return &cnpgv1.BootstrapConfiguration{
		InitDB: &cnpgv1.BootstrapInitDB{
			Database: "tams",
			Owner:    "tams",
		},
	}
}

func recoveryTarget(spec tamossv1alpha1.DBCNPGRestoreSpec) *cnpgv1.RecoveryTarget {
	if spec.TargetTime == "" && spec.TargetImmediate == nil {
		return nil
	}
	return &cnpgv1.RecoveryTarget{
		TargetTime:      spec.TargetTime,
		TargetImmediate: spec.TargetImmediate,
	}
}

func backupMonitoring(spec tamossv1alpha1.DBCNPGMonitoringSpec) *cnpgv1.MonitoringConfiguration {
	if !spec.ShouldEnablePodMonitor() {
		return nil
	}
	return &cnpgv1.MonitoringConfiguration{EnablePodMonitor: true}
}

func backup(clusterName string, spec tamossv1alpha1.DBCNPGBackupSpec) *cnpgv1.BackupConfiguration {
	if !spec.Enabled {
		return nil
	}
	store := barmanObjectStore(clusterName, spec.ObjectStore)
	return &cnpgv1.BackupConfiguration{
		BarmanObjectStore: &store,
		RetentionPolicy:   spec.RetentionPolicy,
	}
}

func restoreExternalClusters(spec tamossv1alpha1.DBCNPGRestoreSpec) []cnpgv1.ExternalCluster {
	if !spec.Enabled || spec.Source == "" {
		return nil
	}
	store := barmanObjectStore(spec.Source, spec.ObjectStore)
	return []cnpgv1.ExternalCluster{{
		Name:              spec.Source,
		BarmanObjectStore: &store,
	}}
}

func barmanObjectStore(clusterName string, spec tamossv1alpha1.DBCNPGObjectStoreSpec) cnpgv1.BarmanObjectStoreConfiguration {
	store := cnpgv1.BarmanObjectStoreConfiguration{
		EndpointURL:     spec.EndpointURL,
		DestinationPath: fmt.Sprintf("s3://%s/%s", spec.Bucket, clusterName),
	}
	if spec.ExistingSecret != "" {
		store.AWS = &cnpgv1.S3Credentials{
			AccessKeyIDReference:     secretKey(spec.ExistingSecret, "AWS_ACCESS_KEY_ID"),
			SecretAccessKeyReference: secretKey(spec.ExistingSecret, "AWS_SECRET_ACCESS_KEY"),
		}
	}
	return store
}

func secretKey(secretName, key string) *cnpgv1.SecretKeySelector {
	return &cnpgv1.SecretKeySelector{
		LocalObjectReference: cnpgv1.LocalObjectReference{Name: secretName},
		Key:                  key,
	}
}
