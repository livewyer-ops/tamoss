package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// TamossStatus defines the observed state of Tamoss
type TamossStatus struct {
	ObservedGeneration int64                 `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition    `json:"conditions,omitempty"`
	Replicas           ReplicaStatus         `json:"replicas,omitempty"`
	Backends           BackendStatus         `json:"backends,omitempty"`
	Auth               AuthStatus            `json:"auth,omitempty"`
	Endpoints          EndpointStatus        `json:"endpoints,omitempty"`
	BackupPolicy       BackupPolicyStatus    `json:"backupPolicy,omitempty"`
	Providers          ProviderStatus        `json:"providers,omitempty"`
	Resolved           ResolvedStatus        `json:"resolved,omitempty"`
	SchemaVersion      string                `json:"schemaVersion,omitempty"`
	Upgrade            UpgradeStatus         `json:"upgrade,omitempty"`
	SchemaMigration    SchemaMigrationStatus `json:"schemaMigration,omitempty"`
	Lifecycle          TamossLifecycleStatus `json:"lifecycle,omitempty"`
	//+kubebuilder:validation:Enum=Pending;Progressing;Ready;Degraded;Paused;Hibernating;Hibernated;Resuming
	Phase string `json:"phase,omitempty"`
}

type BackendStatus struct {
	DB DBBackendStatus `json:"db,omitempty"`
	S3 S3BackendStatus `json:"s3,omitempty"`
}

type DBBackendStatus struct {
	Provider BackendProvidedBy `json:"provider,omitempty"`
}

type S3BackendStatus struct {
	Provider S3BackendProvidedBy `json:"provider,omitempty"`
}

type AuthStatus struct {
	Provider         AuthProvidedBy `json:"provider,omitempty"`
	ApplicationSlug  string         `json:"applicationSlug,omitempty"`
	ManagedBlueprint string         `json:"managedBlueprint,omitempty"`
}

type ReplicaStatus struct {
	API     ComponentReplicaStatus `json:"api,omitempty"`
	Worker  ComponentReplicaStatus `json:"worker,omitempty"`
	UI      ComponentReplicaStatus `json:"ui,omitempty"`
	Console ComponentReplicaStatus `json:"console,omitempty"`
}

type ComponentReplicaStatus struct {
	Desired   int32 `json:"desired,omitempty"`
	Available int32 `json:"available,omitempty"`
}

type EndpointStatus struct {
	API string `json:"api,omitempty"`
	UI  string `json:"ui,omitempty"`
}

type BackupPolicyStatus struct {
	Managed                  bool                   `json:"managed,omitempty"`
	Enabled                  bool                   `json:"enabled,omitempty"`
	Status                   metav1.ConditionStatus `json:"status,omitempty"`
	Reason                   string                 `json:"reason,omitempty"`
	Message                  string                 `json:"message,omitempty"`
	Cluster                  string                 `json:"cluster,omitempty"`
	ScheduledBackup          string                 `json:"scheduledBackup,omitempty"`
	LastSuccessfulBackup     string                 `json:"lastSuccessfulBackup,omitempty"`
	LastFailedBackup         string                 `json:"lastFailedBackup,omitempty"`
	FirstRecoverabilityPoint string                 `json:"firstRecoverabilityPoint,omitempty"`
}

type ProviderOwnership string

const (
	ProviderOwnershipManaged  ProviderOwnership = "managed"
	ProviderOwnershipExternal ProviderOwnership = "external"
)

type ProviderStatus struct {
	DB      ProviderDomainStatus `json:"db,omitempty"`
	S3      ProviderDomainStatus `json:"s3,omitempty"`
	Auth    ProviderDomainStatus `json:"auth,omitempty"`
	Routing ProviderDomainStatus `json:"routing,omitempty"`
}

type ProviderDomainStatus struct {
	Provider string `json:"provider,omitempty"`
	//+kubebuilder:validation:Enum=managed;external
	Ownership ProviderOwnership `json:"ownership,omitempty"`
}

type ResolvedStatus struct {
	Images           ResolvedImageStatus            `json:"images,omitempty"`
	Versions         ResolvedVersionStatus          `json:"versions,omitempty"`
	GeneratedSecrets ResolvedGeneratedSecretsStatus `json:"generatedSecrets,omitempty"`
	Resources        ResolvedResourceStatus         `json:"resources,omitempty"`
	Routes           ResolvedRouteStatus            `json:"routes,omitempty"`
}

type ResolvedImageStatus struct {
	API                           string `json:"api,omitempty"`
	UI                            string `json:"ui,omitempty"`
	Console                       string `json:"console,omitempty"`
	Worker                        string `json:"worker,omitempty"`
	SchemaMigrationPostgresClient string `json:"schemaMigrationPostgresClient,omitempty"`
	CNPGPostgres                  string `json:"cnpgPostgres,omitempty"`
	RustFS                        string `json:"rustfs,omitempty"`
	TAMSin                        string `json:"tamsin,omitempty"`
}

type ResolvedVersionStatus struct {
	Schema  string `json:"schema,omitempty"`
	Tamoss  string `json:"tamoss,omitempty"`
	TAMSAPI string `json:"tamsAPI,omitempty"`
}

type ResolvedGeneratedSecretsStatus struct {
	APIToken                  string `json:"apiToken,omitempty"`
	OAuth2Credentials         string `json:"oauth2Credentials,omitempty"`
	StorageBackendCredentials string `json:"storageBackendCredentials,omitempty"`
}

type ResolvedResourceStatus struct {
	API                   string `json:"api,omitempty"`
	UI                    string `json:"ui,omitempty"`
	Console               string `json:"console,omitempty"`
	Worker                string `json:"worker,omitempty"`
	DefaultStorageBackend string `json:"defaultStorageBackend,omitempty"`
}

type ResolvedRouteStatus struct {
	API string `json:"api,omitempty"`
	UI  string `json:"ui,omitempty"`
}

type UpgradeStatus struct {
	Phase   string `json:"phase,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type SchemaMigrationStatus struct {
	Phase                     string       `json:"phase,omitempty"`
	LastAttemptTime           *metav1.Time `json:"lastAttemptTime,omitempty"`
	LastAttemptResult         string       `json:"lastAttemptResult,omitempty"`
	Attempts                  int32        `json:"attempts,omitempty"`
	AppliedRevision           string       `json:"appliedRevision,omitempty"`
	ObservedRevision          string       `json:"observedRevision,omitempty"`
	CurrentRevision           string       `json:"currentRevision,omitempty"`
	PreviousSupportedRevision string       `json:"previousSupportedRevision,omitempty"`
	SupportedTAMSAPI          string       `json:"supportedTAMSAPI,omitempty"`
}
