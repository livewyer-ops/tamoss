package v1alpha1

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

type BackendsSpec struct {
	DB DBBackendSpec `json:"db,omitempty"`
	S3 S3BackendSpec `json:"s3,omitempty"`
}

type BackendProvidedBy string

const (
	BackendProvidedByExternal BackendProvidedBy = "external"
	BackendProvidedByCNPG     BackendProvidedBy = "cnpg"
)

// +kubebuilder:validation:XValidation:rule="!has(self.bundled) && ((!has(self.providedBy) && !(has(self.external) && has(self.cnpg))) || (self.providedBy == 'external' && !has(self.cnpg)) || (self.providedBy == 'cnpg' && !has(self.external)))",message="only external and cnpg database backend modes are supported"
type DBBackendSpec struct {
	//+kubebuilder:validation:Enum=external;cnpg
	ProvidedBy    BackendProvidedBy `json:"providedBy,omitempty"`
	External      *DBExternalSpec   `json:"external,omitempty"`
	Bundled       *DBBundledSpec    `json:"bundled,omitempty"`
	CNPG          *DBCNPGSpec       `json:"cnpg,omitempty"`
	ApplyFixtures *bool             `json:"applyFixtures,omitempty"`
}

type DBExternalSpec struct {
	//+kubebuilder:default=postgresql
	//+kubebuilder:validation:MinLength=1
	Host string `json:"host,omitempty"`
	//+kubebuilder:default="5432"
	Port string `json:"port,omitempty"`
	//+kubebuilder:default=tams
	//+kubebuilder:validation:MinLength=1
	Database string              `json:"database,omitempty"`
	Auth     SecretReferenceSpec `json:"auth,omitempty"`
}

type DBBundledSpec struct {
	Storage   BackendStorageSpec          `json:"storage,omitempty"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

type DBCNPGSpec struct {
	//+kubebuilder:validation:Minimum=1
	//+kubebuilder:default=1
	Instances int32 `json:"instances,omitempty"`
	//+kubebuilder:default="18"
	PostgresVersion string                      `json:"postgresVersion,omitempty"`
	Storage         BackendStorageSpec          `json:"storage,omitempty"`
	Resources       corev1.ResourceRequirements `json:"resources,omitempty"`
	Backup          DBCNPGBackupSpec            `json:"backup,omitempty"`
	Restore         DBCNPGRestoreSpec           `json:"restore,omitempty"`
	Monitoring      DBCNPGMonitoringSpec        `json:"monitoring,omitempty"`
}

type BackendStorageSpec struct {
	//+kubebuilder:default="10Gi"
	Size string `json:"size,omitempty"`
	// StorageClass selects the storage class for provisioned volumes. Empty uses the cluster default.
	StorageClass string `json:"storageClass,omitempty"`
}

type DBCNPGBackupSpec struct {
	//+kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`
	// Schedule is a CNPG ScheduledBackup cron expression. CNPG includes a seconds field.
	Schedule string `json:"schedule,omitempty"`
	// RetentionPolicy is the CNPG backup retention policy, for example "30d".
	RetentionPolicy string                `json:"retentionPolicy,omitempty"`
	ObjectStore     DBCNPGObjectStoreSpec `json:"objectStore,omitempty"`
}

type DBCNPGObjectStoreSpec struct {
	EndpointURL string `json:"endpointURL,omitempty"`
	Bucket      string `json:"bucket,omitempty"`
	// DestinationPath overrides the derived s3://<bucket>/<cluster> path.
	DestinationPath string `json:"destinationPath,omitempty"`
	// ServerName identifies the Barman server folder when it differs from the
	// CNPG external cluster name.
	ServerName string `json:"serverName,omitempty"`
	// ExistingSecret contains object-store credentials for CNPG backup/restore.
	ExistingSecret string `json:"existingSecret,omitempty"`
	// SecretKeys overrides the access key and secret key names inside ExistingSecret.
	SecretKeys SecretKeySpec `json:"secretKeys,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!self.enabled || (has(self.source) && self.source.size() > 0 && has(self.objectStore) && has(self.objectStore.endpointURL) && self.objectStore.endpointURL.size() > 0 && has(self.objectStore.bucket) && self.objectStore.bucket.size() > 0 && has(self.objectStore.existingSecret) && self.objectStore.existingSecret.size() > 0)",message="restore requires source, objectStore.endpointURL, objectStore.bucket, and objectStore.existingSecret when enabled"
type DBCNPGRestoreSpec struct {
	//+kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`
	// Source is the CNPG external cluster name and backup folder to recover from.
	Source string `json:"source,omitempty"`
	// BackupID pins recovery to the exact CNPG/Barman backup captured by a
	// portable hibernation artifact. Empty retains CNPG's normal target-based
	// backup selection for user-configured restores.
	BackupID string `json:"backupID,omitempty"`
	// TargetTime performs point-in-time recovery to an RFC3339 timestamp.
	TargetTime string `json:"targetTime,omitempty"`
	// TargetImmediate ends recovery as soon as a consistent state is reached.
	TargetImmediate *bool                 `json:"targetImmediate,omitempty"`
	ObjectStore     DBCNPGObjectStoreSpec `json:"objectStore,omitempty"`
}

func (b DBCNPGBackupSpec) MissingRequiredFields() []string {
	if !b.Enabled {
		return nil
	}
	missing := []string{}
	if strings.TrimSpace(b.Schedule) == "" {
		missing = append(missing, "schedule")
	}
	if strings.TrimSpace(b.RetentionPolicy) == "" {
		missing = append(missing, "retentionPolicy")
	}
	if strings.TrimSpace(b.ObjectStore.EndpointURL) == "" {
		missing = append(missing, "objectStore.endpointURL")
	}
	if strings.TrimSpace(b.ObjectStore.Bucket) == "" {
		missing = append(missing, "objectStore.bucket")
	}
	if strings.TrimSpace(b.ObjectStore.ExistingSecret) == "" {
		missing = append(missing, "objectStore.existingSecret")
	}
	return missing
}

func (r DBCNPGRestoreSpec) MissingRequiredFields() []string {
	if !r.Enabled {
		return nil
	}
	missing := []string{}
	if strings.TrimSpace(r.Source) == "" {
		missing = append(missing, "source")
	}
	if strings.TrimSpace(r.ObjectStore.EndpointURL) == "" {
		missing = append(missing, "objectStore.endpointURL")
	}
	if strings.TrimSpace(r.ObjectStore.Bucket) == "" {
		missing = append(missing, "objectStore.bucket")
	}
	if strings.TrimSpace(r.ObjectStore.ExistingSecret) == "" {
		missing = append(missing, "objectStore.existingSecret")
	}
	return missing
}

type DBCNPGMonitoringSpec struct {
	EnablePodMonitor *bool `json:"enablePodMonitor,omitempty"`
}

type S3BackendProvidedBy string

const (
	S3BackendProvidedByExternal       S3BackendProvidedBy = "external"
	S3BackendProvidedByRustFSOperator S3BackendProvidedBy = "rustfs-operator"
)

// +kubebuilder:validation:XValidation:rule="!has(self.bundled) && ((!has(self.providedBy) && !(has(self.external) && has(self.rustfsOperator))) || (self.providedBy == 'external' && !has(self.rustfsOperator)) || (self.providedBy == 'rustfs-operator' && !has(self.external)))",message="only external and rustfs-operator S3 backend modes are supported"
// +kubebuilder:validation:XValidation:rule="(!(has(self.providedBy) && self.providedBy == 'external') && !has(self.external)) || (has(self.external) && has(self.external.endpoint) && has(self.external.endpoint.default) && has(self.external.endpoint.default.url) && self.external.endpoint.default.url.size() > 0)",message="external S3 requires spec.backends.s3.external.endpoint.default.url"
type S3BackendSpec struct {
	//+kubebuilder:validation:Enum=external;rustfs-operator
	ProvidedBy     S3BackendProvidedBy   `json:"providedBy,omitempty"`
	External       *S3ExternalSpec       `json:"external,omitempty"`
	Bundled        *S3BundledSpec        `json:"bundled,omitempty"`
	RustFSOperator *S3RustFSOperatorSpec `json:"rustfsOperator,omitempty"`
	// Tags are advertised on the operator-managed default TAMS storage backend.
	//+kubebuilder:validation:MaxProperties=64
	Tags map[string][]string `json:"tags,omitempty"`
}

type S3ExternalSpec struct {
	Endpoint S3EndpointSpec      `json:"endpoint,omitempty"`
	Auth     SecretReferenceSpec `json:"auth,omitempty"`
	//+kubebuilder:default=us-east-1
	Region string `json:"region,omitempty"`
	//+kubebuilder:default=tamoss
	//+kubebuilder:validation:MinLength=1
	Bucket string `json:"bucket,omitempty"`
}

type S3BundledSpec struct {
	Storage        BackendStorageSpec          `json:"storage,omitempty"`
	Resources      corev1.ResourceRequirements `json:"resources,omitempty"`
	PublicEndpoint S3PublicEndpointSpec        `json:"publicEndpoint,omitempty"`
	Service        S3BundledServiceSpec        `json:"service,omitempty"`
}

type S3BundledServiceSpec struct {
	Type  corev1.ServiceType   `json:"type,omitempty"`
	Ports []corev1.ServicePort `json:"ports,omitempty"`
}

type S3RustFSOperatorSpec struct {
	Pools           []S3RustFSPoolSpec         `json:"pools,omitempty"`
	Image           string                     `json:"image,omitempty"`
	CredsSecret     SecretReferenceSpec        `json:"credsSecret,omitempty"`
	ImagePullPolicy corev1.PullPolicy          `json:"imagePullPolicy,omitempty"`
	Env             []corev1.EnvVar            `json:"env,omitempty"`
	PublicEndpoint  S3PublicEndpointSpec       `json:"publicEndpoint,omitempty"`
	Bucket          S3RustFSOperatorBucketSpec `json:"bucket,omitempty"`
}

type S3RustFSPoolSpec struct {
	Name             string             `json:"name,omitempty"`
	Servers          int32              `json:"servers,omitempty"`
	VolumesPerServer int32              `json:"volumesPerServer,omitempty"`
	Storage          BackendStorageSpec `json:"storage,omitempty"`
}

type S3RustFSOperatorBucketSpec struct {
	//+kubebuilder:default=tamoss
	Name string `json:"name,omitempty"`
	//+kubebuilder:default=true
	CreateIfMissing bool `json:"createIfMissing,omitempty"`
}

type S3EndpointSpec struct {
	Default EndpointURLSpec `json:"default,omitempty"`
	Public  EndpointURLSpec `json:"public,omitempty"`
}

type EndpointURLSpec struct {
	//+kubebuilder:validation:MinLength=1
	URL string `json:"url,omitempty"`
}

type S3PublicEndpointSpec struct {
	//+kubebuilder:validation:MinLength=1
	URL string `json:"url,omitempty"`
	// TLSSecretName overrides the TLS secret used by the operator-managed S3 ingress.
	TLSSecretName string `json:"tlsSecretName,omitempty"`
}

type SecretReferenceSpec struct {
	ExistingSecret string        `json:"existingSecret,omitempty"`
	SecretKeys     SecretKeySpec `json:"secretKeys,omitempty"`
}

type SecretKeySpec struct {
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	AccessKey string `json:"accessKey,omitempty"`
	SecretKey string `json:"secretKey,omitempty"`
}
