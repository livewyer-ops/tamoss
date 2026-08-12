package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

type (
	IngestRunProfile      string
	IngestRunDesiredState string
	IngestRunPhase        string
	IngestRunSizeClass    string
	IngestInputKind       string
)

const (
	IngestRunProfilePreserve        IngestRunProfile = "preserve@1"
	IngestRunProfileDemux           IngestRunProfile = "demux@1"
	IngestRunProfileMuxedSegments   IngestRunProfile = "muxed-segments@1"
	IngestRunProfileEssenceSegments IngestRunProfile = "essence-segments@1"
	IngestRunProfileMPEGTSegments   IngestRunProfile = "mpegts-segments@1"

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

	IngestInputKindHTTP IngestInputKind = "HTTP"
	IngestInputKindS3   IngestInputKind = "S3"
)

type IngestSourceReference struct {
	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=63
	//+kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`
}

// IngestRunInput is the immutable source selector submitted through
// Kubernetes. It cannot select credentials; a source owns those when needed.
type IngestRunInput struct {
	//+kubebuilder:validation:Enum=HTTP;S3
	Kind IngestInputKind `json:"kind"`

	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=2048
	URI string `json:"uri"`

	SourceRef *IngestSourceReference `json:"sourceRef,omitempty"`
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

type IngestFlowProfileReference struct {
	// Name selects a Ready FlowProfile in the IngestRun namespace.
	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
}

// IngestRunTAMSFlowProfile assigns an immutable TAMS 8.2 Flow Profile to one
// essence stream produced by TAMSin. It is separate from the TAMSin treatment
// selected by spec.profile.
// +kubebuilder:validation:XValidation:rule="has(self.profileID) != has(self.profileRef)",message="exactly one of profileID or profileRef is required"
type IngestRunTAMSFlowProfile struct {
	//+kubebuilder:validation:Enum=video;audio;image;data
	Format string `json:"format"`

	// Index selects the zero-based stream of the requested essence format.
	//+kubebuilder:default=0
	//+kubebuilder:validation:Minimum=0
	//+kubebuilder:validation:Maximum=255
	Index int32 `json:"index"`

	// ProfileID is the canonical UUID of an externally managed TAMS Flow Profile.
	//+kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`
	ProfileID string `json:"profileID,omitempty"`

	// ProfileRef selects an operator-managed FlowProfile. The controller resolves
	// the Ready resource to an immutable TAMS UUID before creating the TAMSin Job.
	ProfileRef *IngestFlowProfileReference `json:"profileRef,omitempty"`
}

type IngestRunOptions struct {
	// StorageBackendRef selects a Ready, media-purpose StorageBackend belonging
	// to spec.tamossRef. Unset uses the TAMS instance's default backend. This is
	// a pointer because encoding/json never omits a struct value, and an omitted
	// reference must not serialise as an empty name that fails admission.
	StorageBackendRef *IngestStorageBackendReference `json:"storageBackendRef,omitempty"`

	// Verify downloads and verifies uploaded Object bytes.
	//+kubebuilder:default=true
	Verify *bool `json:"verify,omitempty"`

	// DryRun performs the render and Object plan without changing TAMS.
	//+kubebuilder:default=false
	DryRun bool `json:"dryRun,omitempty"`

	// MaxInputs bounds manifest and source-prefix expansion.
	//+kubebuilder:default=1000
	//+kubebuilder:validation:Minimum=1
	//+kubebuilder:validation:Maximum=10000
	MaxInputs int32 `json:"maxInputs,omitempty"`

	// Concurrency bounds simultaneous input ingests. Zero selects the
	// operator-owned default for the requested size class.
	//+kubebuilder:validation:Minimum=0
	//+kubebuilder:validation:Maximum=32
	Concurrency int32 `json:"concurrency,omitempty"`

	// TAMSFlowProfiles assigns service-owned TAMS 8.2 Flow Profiles to the
	// essence streams produced by TAMSin.
	//+kubebuilder:validation:MaxItems=64
	//+listType=map
	//+listMapKey=format
	//+listMapKey=index
	TAMSFlowProfiles []IngestRunTAMSFlowProfile `json:"tamsFlowProfiles,omitempty"`
}

// IngestRunFlowMetadata is a deliberately constrained subset of TAMS Flow
// metadata. TAMSin applies it to every Flow in the generated graph without
// exposing its general JSON override or technical media fields through the
// Kubernetes API.
type IngestRunFlowMetadata struct {
	// Label is the short human-readable name applied to generated Flows.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Label string `json:"label,omitempty"`

	// Description is the longer human-readable description applied to generated
	// Flows.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	Description string `json:"description,omitempty"`

	// Tags accepts the TAMS string-or-string-array value union. The controller
	// rejects reserved _tamsin_ keys and values outside that union before it
	// creates a Job.
	// +kubebuilder:validation:MaxProperties=32
	Tags map[string]apiextensionsv1.JSON `json:"tags,omitempty"`
}

type IngestRunOutputIntent struct {
	FlowMetadata IngestRunFlowMetadata `json:"flowMetadata"`
}

// +kubebuilder:validation:XValidation:rule="has(self.tamossRef) && has(self.tamossRef.name) && self.tamossRef.name.size() > 0",message="spec.tamossRef.name is required"
// +kubebuilder:validation:XValidation:rule="self.tamossRef.name == oldSelf.tamossRef.name",message="spec.tamossRef.name is immutable"
// +kubebuilder:validation:XValidation:rule="self.input == oldSelf.input",message="spec.input is immutable"
// +kubebuilder:validation:XValidation:rule="self.profile == oldSelf.profile",message="spec.profile is immutable"
// +kubebuilder:validation:XValidation:rule="self.sizeClass == oldSelf.sizeClass",message="spec.sizeClass is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.options) == has(oldSelf.options) && (!has(self.options) || self.options == oldSelf.options)",message="spec.options is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.output) == has(oldSelf.output) && (!has(self.output) || self.output == oldSelf.output)",message="spec.output is immutable"
// +kubebuilder:validation:XValidation:rule="!has(self.output) || (has(self.options) && self.options.maxInputs == 1)",message="spec.output requires spec.options.maxInputs to equal 1"
// +kubebuilder:validation:XValidation:rule="has(self.retryOf) == has(oldSelf.retryOf) && (!has(self.retryOf) || self.retryOf == oldSelf.retryOf)",message="spec.retryOf is immutable"
// +kubebuilder:validation:XValidation:rule="oldSelf.desiredState != 'Cancelled' || self.desiredState == 'Cancelled'",message="a cancelled run cannot be restarted; create a retry instead"
type IngestRunSpec struct {
	// TamossRef identifies the instance that will receive the media.
	TamossRef TamossReferenceSpec `json:"tamossRef"`

	// Input is validated against the target Tamoss source policy immediately
	// before the operator creates the TAMSin Job.
	Input IngestRunInput `json:"input"`

	// Profile is a versioned TAMSin ingest profile.
	//+kubebuilder:validation:Enum=preserve@1;demux@1;muxed-segments@1;essence-segments@1;mpegts-segments@1
	//+kubebuilder:default=essence-segments@1
	Profile IngestRunProfile `json:"profile,omitempty"`

	// SizeClass selects an operator-owned resource and staging budget. Users
	// cannot supply arbitrary Pod resources.
	//+kubebuilder:validation:Enum=small;standard;large
	//+kubebuilder:default=standard
	SizeClass IngestRunSizeClass `json:"sizeClass,omitempty"`

	Options IngestRunOptions `json:"options,omitempty"`

	// Output is optional human-facing metadata for the Flow graph produced from
	// one input. It is immutable and requires options.maxInputs=1 so one intent
	// cannot ambiguously name several input graphs.
	Output *IngestRunOutputIntent `json:"output,omitempty"`

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

type IngestRunResolvedSourceStatus struct {
	Name string `json:"name,omitempty"`

	// PolicyDigest identifies the immutable operator validation snapshot which
	// admitted the Job without exposing selectors, endpoints or credentials.
	//+kubebuilder:validation:Pattern=`^[a-f0-9]{64}$`
	PolicyDigest string `json:"policyDigest,omitempty"`
}

type IngestRunResolvedFlowProfileStatus struct {
	//+kubebuilder:validation:Enum=video;audio;image;data
	Format string `json:"format"`
	//+kubebuilder:validation:Minimum=0
	//+kubebuilder:validation:Maximum=255
	Index int32 `json:"index"`
	//+kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`
	ProfileID  string `json:"profileID"`
	ProfileRef string `json:"profileRef,omitempty"`
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

type IngestRunOutputFlowStatus struct {
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`
	ID string `json:"id"`
	// +kubebuilder:validation:MaxLength=128
	Format string `json:"format,omitempty"`
	// +kubebuilder:validation:MaxLength=64
	Role string `json:"role,omitempty"`
}

// IngestRunOutputStatus contains only identities from TAMSin's validated event
// stream. It never discovers results by listing TAMS resources after a run.
type IngestRunOutputStatus struct {
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`
	RootFlowID string `json:"rootFlowID"`
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`
	SourceID string `json:"sourceID"`
	// +kubebuilder:validation:MaxItems=16
	MemberFlows          []IngestRunOutputFlowStatus `json:"memberFlows,omitempty"`
	MemberFlowsTruncated bool                        `json:"memberFlowsTruncated,omitempty"`
}

type IngestRunStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	//+kubebuilder:validation:Enum=Pending;Queued;Running;Succeeded;PartiallySucceeded;Failed;Cancelled
	Phase IngestRunPhase `json:"phase,omitempty"`
	//+kubebuilder:validation:MaxItems=16
	//+listType=map
	//+listMapKey=type
	Conditions     []metav1.Condition            `json:"conditions,omitempty"`
	JobRef         IngestRunJobStatus            `json:"jobRef,omitempty"`
	ResolvedSource IngestRunResolvedSourceStatus `json:"resolvedSource,omitempty"`
	//+kubebuilder:validation:MaxItems=64
	//+listType=map
	//+listMapKey=format
	//+listMapKey=index
	ResolvedTAMSFlowProfiles []IngestRunResolvedFlowProfileStatus `json:"resolvedTamsFlowProfiles,omitempty"`
	TamsinRunID              string                               `json:"tamsinRunId,omitempty"`
	Attempt                  int32                                `json:"attempt,omitempty"`
	Progress                 IngestRunProgressStatus              `json:"progress,omitempty"`
	LastEventSequence        int64                                `json:"lastEventSequence,omitempty"`
	ResultRef                IngestRunResultStatus                `json:"resultRef,omitempty"`
	Output                   *IngestRunOutputStatus               `json:"output,omitempty"`
	StartedAt                *metav1.Time                         `json:"startedAt,omitempty"`
	CompletedAt              *metav1.Time                         `json:"completedAt,omitempty"`
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
// TAMSin Job template.
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
