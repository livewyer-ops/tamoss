package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:validation:XValidation:rule="has(self.tamossRef) && has(self.tamossRef.name) && self.tamossRef.name.size() > 0",message="spec.tamossRef.name is required"
// +kubebuilder:validation:XValidation:rule="has(self.source) && ((has(self.source.hibernationRef) && has(self.source.hibernationRef.name) && self.source.hibernationRef.name.size() > 0 && !has(self.source.artifact)) || (!has(self.source.hibernationRef) && has(self.source.artifact) && has(self.source.artifact.storageBackendRef) && has(self.source.artifact.storageBackendRef.name) && self.source.artifact.storageBackendRef.name.size() > 0 && has(self.source.artifact.manifestKey) && self.source.artifact.manifestKey.size() > 0 && has(self.source.artifact.checksum) && self.source.artifact.checksum.size() > 0))",message="spec.source must set exactly one of hibernationRef or artifact; artifact sources require checksum"
// +kubebuilder:validation:XValidation:rule="self.tamossRef.name == oldSelf.tamossRef.name",message="spec.tamossRef.name is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.source.hibernationRef) == has(oldSelf.source.hibernationRef)",message="spec.source.hibernationRef is immutable"
// +kubebuilder:validation:XValidation:rule="!has(self.source.hibernationRef) || self.source.hibernationRef.name == oldSelf.source.hibernationRef.name",message="spec.source.hibernationRef.name is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.source.artifact) == has(oldSelf.source.artifact)",message="spec.source.artifact is immutable"
// +kubebuilder:validation:XValidation:rule="!has(self.source.artifact) || self.source.artifact.storageBackendRef.name == oldSelf.source.artifact.storageBackendRef.name",message="spec.source.artifact.storageBackendRef.name is immutable"
// +kubebuilder:validation:XValidation:rule="!has(self.source.artifact) || self.source.artifact.manifestKey == oldSelf.source.artifact.manifestKey",message="spec.source.artifact.manifestKey is immutable"
// +kubebuilder:validation:XValidation:rule="!has(self.source.artifact) || self.source.artifact.checksum == oldSelf.source.artifact.checksum",message="spec.source.artifact.checksum is immutable"
type TamossResumeSpec struct {
	TamossRef TamossReferenceSpec `json:"tamossRef,omitempty"`
	Source    TamossResumeSource  `json:"source,omitempty"`
}

type TamossResumeSource struct {
	HibernationRef *LocalObjectReference       `json:"hibernationRef,omitempty"`
	Artifact       *TamossResumeArtifactSource `json:"artifact,omitempty"`
}

type TamossResumeArtifactSource struct {
	StorageBackendRef LocalObjectReference `json:"storageBackendRef,omitempty"`
	// ManifestKey is the S3 object key of the hibernation manifest.
	//+kubebuilder:validation:MinLength=1
	ManifestKey string `json:"manifestKey,omitempty"`
	// Checksum is the trusted SHA-256 digest emitted by TamossHibernate. It is
	// required for direct artifact restores because S3 object metadata is not an
	// independent integrity anchor.
	//+kubebuilder:validation:Pattern=`^sha256:[a-f0-9]{64}$`
	Checksum string `json:"checksum,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Namespaced,path=tamossresumes,shortName=tr
//+kubebuilder:printcolumn:name="Tamoss",type=string,JSONPath=`.spec.tamossRef.name`
//+kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
//+kubebuilder:printcolumn:name="Manifest",type=string,JSONPath=`.status.artifact.manifestKey`
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TamossResume records a request to bring TAMOSS back to a running state from
// a hibernation source.
type TamossResume struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TamossResumeSpec      `json:"spec,omitempty"`
	Status TamossOperationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TamossResumeList contains a list of TamossResume resources.
type TamossResumeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TamossResume `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &TamossResume{}, &TamossResumeList{})
		return nil
	})
}
