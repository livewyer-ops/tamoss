package v1alpha1

import (
	"crypto/sha256"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type FlowProfilePhase string

const (
	FlowProfilePhasePending     FlowProfilePhase = "Pending"
	FlowProfilePhaseProgressing FlowProfilePhase = "Progressing"
	FlowProfilePhaseReady       FlowProfilePhase = "Ready"
	FlowProfilePhaseDegraded    FlowProfilePhase = "Degraded"
	FlowProfilePhaseDeleting    FlowProfilePhase = "Deleting"
)

// +kubebuilder:validation:XValidation:rule="has(self.id) == has(oldSelf.id) && (!has(self.id) || self.id == oldSelf.id)",message="spec.id is immutable"
// +kubebuilder:validation:XValidation:rule="self.tamossRef == oldSelf.tamossRef",message="spec.tamossRef is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.label) == has(oldSelf.label) && (!has(self.label) || self.label == oldSelf.label)",message="spec.label is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.description) == has(oldSelf.description) && (!has(self.description) || self.description == oldSelf.description)",message="spec.description is immutable"
type FlowProfileSpec struct {
	// TamossRef identifies the same-namespace Tamoss instance receiving the Profile.
	TamossRef TamossReferenceSpec `json:"tamossRef"`

	// ID is the immutable TAMS Profile UUID. Empty derives a deterministic UUID
	// from the FlowProfile namespace and name.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`
	ID string `json:"id,omitempty"`

	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`

	// Tags use the TAMS string-or-string-array value union.
	Tags map[string]apiextensionsv1.JSON `json:"tags,omitempty"`

	// FlowMetadata is copied to the TAMS flow_metadata object. The operator
	// preserves extensions and the TAMOSS registration command applies the full
	// TAMS contract validation before writing anything.
	// +kubebuilder:validation:Type=object
	// +kubebuilder:pruning:PreserveUnknownFields
	FlowMetadata apiextensionsv1.JSON `json:"flowMetadata"`
}

type FlowProfileResolvedStatus struct {
	TamossName string `json:"tamossName,omitempty"`
	Format     string `json:"format,omitempty"`
	Codec      string `json:"codec,omitempty"`
}

type FlowProfileStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +kubebuilder:validation:Enum=Pending;Progressing;Ready;Degraded;Deleting
	Phase     FlowProfilePhase          `json:"phase,omitempty"`
	ProfileID string                    `json:"profileID,omitempty"`
	Resolved  FlowProfileResolvedStatus `json:"resolved,omitempty"`
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,path=flowprofiles,shortName=fp
// +kubebuilder:printcolumn:name="Tamoss",type=string,JSONPath=`.spec.tamossRef.name`
// +kubebuilder:printcolumn:name="Profile ID",type=string,JSONPath=`.status.profileID`
// +kubebuilder:printcolumn:name="Format",type=string,JSONPath=`.status.resolved.format`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// FlowProfile declaratively registers an immutable TAMS Flow Profile.
type FlowProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FlowProfileSpec   `json:"spec"`
	Status FlowProfileStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FlowProfileList contains a list of FlowProfile resources.
type FlowProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FlowProfile `json:"items"`
}

func (s *FlowProfileSpec) ApplyDefaults(namespace, name string) {
	if s.ID == "" {
		s.ID = DeterministicFlowProfileID(namespace, name)
	}
}

func DeterministicFlowProfileID(namespace, name string) string {
	sum := sha256.Sum256([]byte("tamoss-flowprofile:" + namespace + "/" + name))
	value := sum[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16],
	)
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &FlowProfile{}, &FlowProfileList{})
		return nil
	})
}
