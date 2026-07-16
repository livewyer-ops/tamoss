package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:validation:XValidation:rule="has(self.tamossRef) && has(self.tamossRef.name) && self.tamossRef.name.size() > 0",message="spec.tamossRef.name is required"
// +kubebuilder:validation:XValidation:rule="has(self.destination) && has(self.destination.storageBackendRef) && has(self.destination.storageBackendRef.name) && self.destination.storageBackendRef.name.size() > 0",message="spec.destination.storageBackendRef.name is required"
// +kubebuilder:validation:XValidation:rule="has(self.destination) && has(self.destination.prefix) && self.destination.prefix.size() > 0",message="spec.destination.prefix is required"
// +kubebuilder:validation:XValidation:rule="self.tamossRef.name == oldSelf.tamossRef.name",message="spec.tamossRef.name is immutable"
// +kubebuilder:validation:XValidation:rule="self.destination.storageBackendRef.name == oldSelf.destination.storageBackendRef.name",message="spec.destination.storageBackendRef.name is immutable"
// +kubebuilder:validation:XValidation:rule="self.destination.prefix == oldSelf.destination.prefix",message="spec.destination.prefix is immutable"
// +kubebuilder:validation:XValidation:rule="!has(self.destination.prefix) || (!self.destination.prefix.startsWith('/') && !self.destination.prefix.endsWith('/') && !self.destination.prefix.contains('//'))",message="spec.destination.prefix must be a normalized relative object-key prefix"
// +kubebuilder:validation:XValidation:rule="has(self.driver) == has(oldSelf.driver) && (!has(self.driver) || self.driver == oldSelf.driver)",message="spec.driver is immutable"
type TamossHibernateSpec struct {
	TamossRef TamossReferenceSpec `json:"tamossRef,omitempty"`

	Destination HibernationDestinationSpec `json:"destination,omitempty"`

	// Driver selects the DB hibernation implementation. cnpgPhysical is the
	// managed-CNPG native path. logicalDump is reserved for a later portable
	// PostgreSQL dump implementation.
	//+kubebuilder:validation:Enum=cnpgPhysical;logicalDump
	//+kubebuilder:default=cnpgPhysical
	Driver HibernationDriver `json:"driver,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Namespaced,path=tamosshibernations,shortName=th
//+kubebuilder:printcolumn:name="Tamoss",type=string,JSONPath=`.spec.tamossRef.name`
//+kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
//+kubebuilder:printcolumn:name="Driver",type=string,JSONPath=`.status.artifact.driver`
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TamossHibernate records a request to export TAMOSS database state to an
// external hibernation destination.
type TamossHibernate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TamossHibernateSpec   `json:"spec,omitempty"`
	Status TamossOperationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TamossHibernateList contains a list of TamossHibernate resources.
type TamossHibernateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TamossHibernate `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &TamossHibernate{}, &TamossHibernateList{})
		return nil
	})
}
