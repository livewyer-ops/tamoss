package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

type (
	IngestRunProfile      string
	IngestRunDesiredState string
	IngestRunPhase        string
	IngestRunSizeClass    string
)

const (
	IngestRunProfilePreserve    IngestRunProfile = "preserve@1"
	IngestRunProfileEditorial   IngestRunProfile = "editorial@1"
	IngestRunProfileStreamingTS IngestRunProfile = "streaming-ts@1"

	IngestRunDesiredStateRunning   IngestRunDesiredState = "Running"
	IngestRunDesiredStateCancelled IngestRunDesiredState = "Cancelled"

	IngestRunPhasePending            IngestRunPhase = "Pending"
	IngestRunPhaseQueued             IngestRunPhase = "Queued"
	IngestRunPhaseRunning            IngestRunPhase = "Running"
	IngestRunPhaseSucceeded          IngestRunPhase = "Succeeded"
	IngestRunPhasePartiallySucceeded IngestRunPhase = "PartiallySucceeded"
	IngestRunPhaseFailed             IngestRunPhase = "Failed"
	IngestRunPhaseCancelled          IngestRunPhase = "Cancelled"

	IngestRunSizeClassSmall    IngestRunSizeClass = "small"
	IngestRunSizeClassStandard IngestRunSizeClass = "standard"
	IngestRunSizeClassLarge    IngestRunSizeClass = "large"
)

// IngestInputReference identifies a server-managed input record. It never
// contains a media locator, signed URL, Secret name, or credential value.
type IngestInputReference struct {
	//+kubebuilder:validation:Enum=StagedObject;Manifest;ApprovedS3;ApprovedHTTP
	Kind string `json:"kind"`

	// ID is opaque outside the Console API and operator input resolver.
	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=128
	//+kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9._-]*$`
	ID string `json:"id"`
}

type IngestCredentialProfileReference struct {
	// Name selects an operator-approved profile belonging to the target
	// Tamoss. It does not name a Kubernetes Secret directly.
	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=63
	Name string `json:"name"`
}

// IngestStorageBackendReference selects an operator-managed media destination.
// The controller resolves this Kubernetes name to the registered TAMS backend
// ID; callers cannot submit a backend UUID directly.
type IngestStorageBackendReference struct {
	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
}

type IngestRunReference struct {
	//+kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// UID prevents a replacement object with the same name from becoming the
	// retry parent.
	//+kubebuilder:validation:MinLength=1
	UID string `json:"uid"`
}

type IngestRunOptions struct {
	// StorageBackendRef selects a Ready, media-purpose StorageBackend belonging
	// to spec.tamossRef. Empty uses the TAMS instance's default backend.
	StorageBackendRef IngestStorageBackendReference `json:"storageBackendRef,omitempty"`

	// Verify downloads and verifies uploaded Object bytes.
	//+kubebuilder:default=true
	Verify *bool `json:"verify,omitempty"`

	// DryRun performs the render and Object plan without changing TAMS.
	//+kubebuilder:default=false
	DryRun bool `json:"dryRun,omitempty"`

	// MaxInputs bounds manifest and approved-prefix expansion.
	//+kubebuilder:default=1000
	//+kubebuilder:validation:Minimum=1
	//+kubebuilder:validation:Maximum=10000
	MaxInputs int32 `json:"maxInputs,omitempty"`

	// Concurrency bounds simultaneous input ingests. Zero selects the
	// operator-owned default for the requested size class.
	//+kubebuilder:validation:Minimum=0
	//+kubebuilder:validation:Maximum=32
	Concurrency int32 `json:"concurrency,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="has(self.tamossRef) && has(self.tamossRef.name) && self.tamossRef.name.size() > 0",message="spec.tamossRef.name is required"
// +kubebuilder:validation:XValidation:rule="self.tamossRef.name == oldSelf.tamossRef.name",message="spec.tamossRef.name is immutable"
// +kubebuilder:validation:XValidation:rule="self.inputRef == oldSelf.inputRef",message="spec.inputRef is immutable"
// +kubebuilder:validation:XValidation:rule="self.profile == oldSelf.profile",message="spec.profile is immutable"
// +kubebuilder:validation:XValidation:rule="self.sizeClass == oldSelf.sizeClass",message="spec.sizeClass is immutable"
// +kubebuilder:validation:XValidation:rule="self.options == oldSelf.options",message="spec.options is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.credentialProfileRef) == has(oldSelf.credentialProfileRef) && (!has(self.credentialProfileRef) || self.credentialProfileRef == oldSelf.credentialProfileRef)",message="spec.credentialProfileRef is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.retryOf) == has(oldSelf.retryOf) && (!has(self.retryOf) || self.retryOf == oldSelf.retryOf)",message="spec.retryOf is immutable"
// +kubebuilder:validation:XValidation:rule="oldSelf.desiredState != 'Cancelled' || self.desiredState == 'Cancelled'",message="a cancelled run cannot be restarted; create a retry instead"
type IngestRunSpec struct {
	// TamossRef identifies the instance that will receive the media.
	TamossRef TamossReferenceSpec `json:"tamossRef"`

	// InputRef is resolved through the approved server-side input boundary.
	InputRef IngestInputReference `json:"inputRef"`

	// Profile is a versioned Tamsin ingest profile.
	//+kubebuilder:validation:Enum=preserve@1;editorial@1;streaming-ts@1
	//+kubebuilder:default=editorial@1
	Profile IngestRunProfile `json:"profile,omitempty"`

	// SizeClass selects an operator-owned resource and staging budget. Users
	// cannot supply arbitrary Pod resources.
	//+kubebuilder:validation:Enum=small;standard;large
	//+kubebuilder:default=standard
	SizeClass IngestRunSizeClass `json:"sizeClass,omitempty"`

	Options IngestRunOptions `json:"options,omitempty"`

	CredentialProfileRef *IngestCredentialProfileReference `json:"credentialProfileRef,omitempty"`

	// DesiredState provides one-way declarative cancellation. Retrying creates
	// a new IngestRun so completed history remains immutable.
	//+kubebuilder:validation:Enum=Running;Cancelled
	//+kubebuilder:default=Running
	DesiredState IngestRunDesiredState `json:"desiredState,omitempty"`

	RetryOf *IngestRunReference `json:"retryOf,omitempty"`
}

type IngestRunJobStatus struct {
	Name string    `json:"name,omitempty"`
	UID  types.UID `json:"uid,omitempty"`
}

type IngestRunProgressStatus struct {
	InputsTotal     int32 `json:"inputsTotal,omitempty"`
	InputsCompleted int32 `json:"inputsCompleted,omitempty"`
	InputsSucceeded int32 `json:"inputsSucceeded,omitempty"`
	InputsFailed    int32 `json:"inputsFailed,omitempty"`
	BytesUploaded   int64 `json:"bytesUploaded,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!has(self.verified) || !self.verified || (has(self.key) && has(self.sha256) && has(self.size) && self.size > 0 && has(self.mediaType))",message="a verified result requires a key, SHA-256 digest, positive size, and media type"
type IngestRunResultStatus struct {
	// Key is a durable, non-presigned artifact reference.
	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=1024
	Key string `json:"key,omitempty"`
	//+kubebuilder:validation:Pattern=`^[a-f0-9]{64}$`
	SHA256 string `json:"sha256,omitempty"`
	//+kubebuilder:validation:Minimum=1
	Size int64 `json:"size,omitempty"`
	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=255
	MediaType string `json:"mediaType,omitempty"`
	// Verified is set only after the operator-side collector has read the
	// durable artifact and matched its SHA-256 digest and size.
	Verified bool `json:"verified,omitempty"`
}

type IngestRunStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	//+kubebuilder:validation:Enum=Pending;Queued;Running;Succeeded;PartiallySucceeded;Failed;Cancelled
	Phase IngestRunPhase `json:"phase,omitempty"`
	//+kubebuilder:validation:MaxItems=16
	//+listType=map
	//+listMapKey=type
	Conditions        []metav1.Condition      `json:"conditions,omitempty"`
	JobRef            IngestRunJobStatus      `json:"jobRef,omitempty"`
	TamsinRunID       string                  `json:"tamsinRunId,omitempty"`
	Attempt           int32                   `json:"attempt,omitempty"`
	Progress          IngestRunProgressStatus `json:"progress,omitempty"`
	LastEventSequence int64                   `json:"lastEventSequence,omitempty"`
	ResultRef         IngestRunResultStatus   `json:"resultRef,omitempty"`
	StartedAt         *metav1.Time            `json:"startedAt,omitempty"`
	CompletedAt       *metav1.Time            `json:"completedAt,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Namespaced,path=ingestruns,shortName=ir
//+kubebuilder:printcolumn:name="Tamoss",type=string,JSONPath=`.spec.tamossRef.name`
//+kubebuilder:printcolumn:name="Profile",type=string,JSONPath=`.spec.profile`
//+kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
//+kubebuilder:printcolumn:name="Job",type=string,JSONPath=`.status.jobRef.name`
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// IngestRun is a durable request to ingest media through an operator-owned
// Tamsin Job template.
type IngestRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IngestRunSpec   `json:"spec"`
	Status IngestRunStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// IngestRunList contains a list of IngestRun resources.
type IngestRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IngestRun `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &IngestRun{}, &IngestRunList{})
		return nil
	})
}
