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

type TamossLifecycleStatus struct {
	//+kubebuilder:validation:Enum=Running;Hibernating;Hibernated;Resuming;Failed
	Phase string `json:"phase,omitempty"`

	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`

	ActiveOperationRef *corev1.ObjectReference `json:"activeOperationRef,omitempty"`
	LastHibernateRef   *corev1.ObjectReference `json:"lastHibernateRef,omitempty"`
	LastResumeRef      *corev1.ObjectReference `json:"lastResumeRef,omitempty"`
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
