package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TamossLifecyclePhase string

const (
	TamossLifecyclePhaseRunning     TamossLifecyclePhase = "Running"
	TamossLifecyclePhaseHibernating TamossLifecyclePhase = "Hibernating"
	TamossLifecyclePhaseHibernated  TamossLifecyclePhase = "Hibernated"
	TamossLifecyclePhaseResuming    TamossLifecyclePhase = "Resuming"
	TamossLifecyclePhaseFailed      TamossLifecyclePhase = "Failed"
)

// TamossHibernationSpec declares the desired lifecycle state of an instance.
// Setting enabled true asks the operator to capture the managed database to
// the destination, quiesce the workloads, and remove database compute; the
// operator materialises a TamossHibernate operation to do the work. Setting
// enabled false on a hibernated instance restores the database from the most
// recent hibernation artifact. ResumeFrom bootstraps a new instance from an
// existing artifact and is only honoured before the database cluster exists.
// +kubebuilder:validation:XValidation:rule="has(self.resumeFrom) == has(oldSelf.resumeFrom)",message="spec.hibernation.resumeFrom is immutable"
// +kubebuilder:validation:XValidation:rule="!has(self.resumeFrom) || !has(self.resumeFrom.hibernationRef) || self.resumeFrom.hibernationRef.name == oldSelf.resumeFrom.hibernationRef.name",message="spec.hibernation.resumeFrom.hibernationRef is immutable"
// +kubebuilder:validation:XValidation:rule="!has(self.resumeFrom) || !has(self.resumeFrom.artifact) || (self.resumeFrom.artifact.storageBackendRef.name == oldSelf.resumeFrom.artifact.storageBackendRef.name && self.resumeFrom.artifact.manifestKey == oldSelf.resumeFrom.artifact.manifestKey)",message="spec.hibernation.resumeFrom.artifact is immutable"
// +kubebuilder:validation:XValidation:rule="!self.enabled || (has(self.destination) && has(self.destination.storageBackendRef) && has(self.destination.storageBackendRef.name) && self.destination.storageBackendRef.name.size() > 0 && has(self.destination.prefix) && self.destination.prefix.size() > 0)",message="spec.hibernation.destination is required when hibernation is enabled"
// +kubebuilder:validation:XValidation:rule="!has(self.resumeFrom) || ((has(self.resumeFrom.hibernationRef) && has(self.resumeFrom.hibernationRef.name) && self.resumeFrom.hibernationRef.name.size() > 0 && !has(self.resumeFrom.artifact)) || (!has(self.resumeFrom.hibernationRef) && has(self.resumeFrom.artifact) && has(self.resumeFrom.artifact.storageBackendRef) && has(self.resumeFrom.artifact.storageBackendRef.name) && self.resumeFrom.artifact.storageBackendRef.name.size() > 0 && has(self.resumeFrom.artifact.manifestKey) && self.resumeFrom.artifact.manifestKey.size() > 0))",message="spec.hibernation.resumeFrom must set exactly one of hibernationRef or artifact"
type TamossHibernationSpec struct {
	// Enabled declares that this instance should be hibernated.
	//+kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Destination selects the hibernate StorageBackend and object-key prefix
	// used for hibernation artifacts. Required while enabled is true.
	Destination *HibernationDestinationSpec `json:"destination,omitempty"`

	// ResumeFrom bootstraps the managed database from a hibernation artifact.
	// It follows CNPG bootstrap semantics: it is honoured only while the
	// database cluster does not exist yet and is ignored afterwards.
	ResumeFrom *TamossResumeSource `json:"resumeFrom,omitempty"`
}

type TamossLifecycleStatus struct {
	//+kubebuilder:validation:Enum=Running;Hibernating;Hibernated;Resuming;Failed
	Phase string `json:"phase,omitempty"`

	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`

	// LastTransitionTime records when Phase last changed.
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`

	// HibernationCycle counts spec-driven hibernation operations and names the
	// materialised TamossHibernate for each cycle.
	HibernationCycle int32 `json:"hibernationCycle,omitempty"`

	ActiveOperationRef *corev1.ObjectReference `json:"activeOperationRef,omitempty"`
	LastHibernateRef   *corev1.ObjectReference `json:"lastHibernateRef,omitempty"`
	LastResumeRef      *corev1.ObjectReference `json:"lastResumeRef,omitempty"`

	// ResolvedRestore records the database bootstrap source resolved from
	// spec.hibernation.resumeFrom (or from the latest hibernation artifact
	// when waking), plus the retention state of that artifact.
	ResolvedRestore *TamossResolvedRestore `json:"resolvedRestore,omitempty"`
}

// TamossResolvedRestore persists a resolved hibernation artifact so the
// database renderer can keep emitting the same recovery bootstrap across
// reconciles without re-reading the manifest from object storage.
type TamossResolvedRestore struct {
	Restore            DBCNPGRestoreSpec                `json:"restore,omitempty"`
	StorageBackendName string                           `json:"storageBackendName,omitempty"`
	ManifestKey        string                           `json:"manifestKey,omitempty"`
	Checksum           string                           `json:"checksum,omitempty"`
	ResumedAt          *metav1.Time                     `json:"resumedAt,omitempty"`
	Cleanup            HibernationArtifactCleanupStatus `json:"cleanup,omitempty"`
}

type (
	TamossOperationPhase            string
	HibernationArtifactCleanupPhase string
)

// StartingDatabase, RunningMigrations, and Verifying are reserved for future
// lifecycle stages.
const (
	TamossOperationPhasePending              TamossOperationPhase = "Pending"
	TamossOperationPhaseResolvingSource      TamossOperationPhase = "ResolvingSource"
	TamossOperationPhasePreparingTarget      TamossOperationPhase = "PreparingTarget"
	TamossOperationPhaseQuiescing            TamossOperationPhase = "Quiescing"
	TamossOperationPhaseCapturingDatabase    TamossOperationPhase = "CapturingDatabase"
	TamossOperationPhaseWritingManifest      TamossOperationPhase = "WritingManifest"
	TamossOperationPhaseDeprovisioningSource TamossOperationPhase = "DeprovisioningSource"
	TamossOperationPhaseRecoveringDatabase   TamossOperationPhase = "RecoveringDatabase"
	TamossOperationPhaseStartingDatabase     TamossOperationPhase = "StartingDatabase"
	TamossOperationPhaseRunningMigrations    TamossOperationPhase = "RunningMigrations"
	TamossOperationPhaseStartingServices     TamossOperationPhase = "StartingServices"
	TamossOperationPhaseVerifying            TamossOperationPhase = "Verifying"
	TamossOperationPhaseCompleted            TamossOperationPhase = "Completed"
	TamossOperationPhaseFailed               TamossOperationPhase = "Failed"

	HibernationArtifactCleanupPhaseRetained  HibernationArtifactCleanupPhase = "Retained"
	HibernationArtifactCleanupPhasePending   HibernationArtifactCleanupPhase = "Pending"
	HibernationArtifactCleanupPhaseCompleted HibernationArtifactCleanupPhase = "Completed"
	HibernationArtifactCleanupPhaseBlocked   HibernationArtifactCleanupPhase = "Blocked"
)

type TamossOperationStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`

	//+kubebuilder:validation:Enum=Pending;ResolvingSource;PreparingTarget;Quiescing;CapturingDatabase;WritingManifest;DeprovisioningSource;RecoveringDatabase;StartingDatabase;RunningMigrations;StartingServices;Verifying;Completed;Failed
	Phase string `json:"phase,omitempty"`

	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`

	StartedAt   *metav1.Time `json:"startedAt,omitempty"`
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	Artifact HibernationArtifactStatus `json:"artifact,omitempty"`
}

type HibernationArtifactStatus struct {
	Driver      string `json:"driver,omitempty"`
	ManifestURI string `json:"manifestURI,omitempty"`
	ManifestKey string `json:"manifestKey,omitempty"`
	Checksum    string `json:"checksum,omitempty"`

	CNPGBackup HibernationCNPGBackupStatus      `json:"cnpgBackup,omitempty"`
	Cleanup    HibernationArtifactCleanupStatus `json:"cleanup,omitempty"`
}

type HibernationCNPGBackupStatus struct {
	Name            string `json:"name,omitempty"`
	Namespace       string `json:"namespace,omitempty"`
	Phase           string `json:"phase,omitempty"`
	DestinationPath string `json:"destinationPath,omitempty"`
	BackupID        string `json:"backupID,omitempty"`
	Error           string `json:"error,omitempty"`
}

type HibernationArtifactCleanupStatus struct {
	Phase          string       `json:"phase,omitempty"`
	Reason         string       `json:"reason,omitempty"`
	Message        string       `json:"message,omitempty"`
	ObjectsDeleted int64        `json:"objectsDeleted,omitempty"`
	CompletedAt    *metav1.Time `json:"completedAt,omitempty"`
}

type HibernationDriver string

const (
	HibernationDriverCNPGPhysical HibernationDriver = "cnpgPhysical"
	HibernationDriverLogicalDump  HibernationDriver = "logicalDump"
)

type HibernationDestinationSpec struct {
	StorageBackendRef LocalObjectReference `json:"storageBackendRef,omitempty"`
	// Prefix is the object-key prefix used for hibernation metadata.
	//+kubebuilder:validation:MinLength=1
	Prefix string `json:"prefix,omitempty"`
}

type LocalObjectReference struct {
	//+kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`
}
